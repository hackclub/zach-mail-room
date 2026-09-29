# zach-mail-room — agent guide

`CLAUDE.md` is a symlink to this file. Edit `AGENTS.md` only.

Hack Club's swag and warehouse app. It has a Go backend (`cmd/`, `internal/`), a SvelteKit PWA
(`web/`), and Postgres. Physical fulfillment goes through the mail.hackclub.com warehouse API.

**This repo is public.** Never commit secrets, `.env` files, tokens, or real addresses.
Secrets live in env vars only.

## The three user flows

| Who | How we know | What they do |
| --- | --- | --- |
| Hack clubber (anyone signed in) | Hack Club Auth login | Request listed swag. US shipping is free. Outside the US the request starts in `awaiting_payment`, and the person pays a flat fee on HCB before we ship. |
| YSWS author | Their HCA email matches `Hack Club Auth Email` in the **YSWS Authors** table of the Unified YSWS Projects DB (Airtable `app3A5kJwYqxMLOgh`) | Submit print-ready material (stickers, posters…) to be printed and stocked in the warehouse. |
| Admin | Email is in `ADMIN_EMAILS` (admins are also authors) | Manage global limits, list warehouse SKUs as items, set visibility and order, review requests (mark paid → ship → tracking), and review author submissions. |

Request status machine (`internal/swag`): `awaiting_payment → pending → dispatched`. From
`awaiting_payment` or `pending`, a request can also go to `rejected` or `cancelled`. Rejected and
cancelled requests don't count toward limits or cooldowns.

## Layout

```
cmd/server/          main: env config → migrate → wire deps → HTTP server
internal/config/     env-var config (12-factor), no file reads
internal/swag/       pure business rules: limits, cooldown, intl shipping, status transitions
internal/store/      Postgres persistence (pgx); per-user lock around limit check + insert
internal/db/         pool, embedded goose migrations (internal/db/migrations/*.sql)
internal/db/dbtest/  per-test throwaway database (CREATE DATABASE test_<rand> … DROP)
internal/auth/       Hack Club Auth OAuth client (auth.hackclub.com, GET /api/v1/me)
internal/authors/    YSWS author lookup (Airtable REST, 10 min cache)
internal/theseus/    mail.hackclub.com warehouse client (SKUs, warehouse_orders)
internal/httpapi/    routes, role middleware, CSRF guard, SPA static serving
web/                 SvelteKit SPA (adapter-static, ssr=false) + service worker + manifest
```

## TDD workflow (required)

1. Write or extend a failing test first. Run it and watch it fail for the right reason.
2. Write the smallest change that passes it.
3. Refactor with the tests green. Then run `make check test` before every commit.

Where tests go:
- Business rules → table tests in `internal/swag` (pure, no DB).
- SQL → `internal/store` tests against a real Postgres via `dbtest.New(t)`. Never mock the DB.
- Endpoints and role checks → `internal/httpapi` tests. They use a real test DB with fakes for
  HCA (`fakeProvider`), authors (`authors.Static`), and the warehouse (`fakeWarehouse`).
- External clients → `httptest.Server` fakes. Tests must never call auth.hackclub.com,
  mail.hackclub.com, or Airtable.
- Frontend logic → `web/src/lib/*.test.ts` (Vitest). Keep logic in `$lib`, not in `.svelte` files,
  so it stays testable.

## Commands

```sh
make db        # shared dev Postgres via docker compose (127.0.0.1:54329)
make test      # go test ./... + vitest   (make test-go / make test-web)
make check     # gofmt, go vet, svelte-check
make dev       # Go server :8080 + Vite :5173 (proxies /api, /auth) — open http://localhost:5173
make server    # Go server only; serves web/build if built (make web)
make docker    # production image
```

One Postgres container serves every worktree. Each test gets its own database, so parallel
worktrees don't collide. If Docker is down on porygon, OrbStack is the runtime (`orb start`).
Its data dir must be on the internal disk. See "Gotchas".

## Config: 12-factor, one shared `.env`

- The app reads **only env vars** (`internal/config`). It never reads files. `.env.example`
  lists every variable.
- In dev, `make` sources the **`.env` in the main worktree root**. Right now that's
  `~/dev/hackclub/zach-mail-room/.env`. `scripts/env-file` finds it through
  `git rev-parse --git-common-dir`, so every worktree (`~/.paseo/worktrees/...`) shares it.
  Never copy `.env` into a worktree. `ENV_FILE=...` overrides the path.
- In production (Orchard), set the same variables on the deployment. There is no `.env` file.

## Landing work on main (worktrees)

Sessions often start in a paseo worktree on a throwaway branch. By default, finished work
**lands on `main`**:

```sh
make check test                      # green first
git add -A && git commit             # small, descriptive commits
git fetch origin
git rebase origin/main               # other agents may have landed work; rebase, don't merge
make check test                      # re-verify after the rebase
git push origin HEAD:main            # land it
# fast-forward the main worktree if it's clean:
git -C "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")" pull --ff-only
```

If `git push` is rejected because main moved, fetch, rebase, re-test, and push again. Never
force-push `main`. If a migration filename collides with one that just landed, renumber yours.

## Deploying (Orchard)

- One container: `Dockerfile`, which builds the PWA and a static Go binary on distroless. It
  listens on `$PORT` (default 8080). Health check: `GET /healthz` (pings the DB).
- Migrations run automatically at startup (goose, embedded).
- Required env: `DATABASE_URL`, `BASE_URL` (public https origin), `SESSION_SECRET`,
  `HCA_CLIENT_ID`, `HCA_CLIENT_SECRET`, `THESEUS_API_KEY`, `ADMIN_EMAILS`. Also needed:
  `AIRTABLE_API_KEY` for authors, and `HCB_SHIPPING_PAYMENT_URL` for international payments.
- `$BASE_URL/auth/callback` must be registered as a redirect URI on the Hack Club Auth app.
- Read the `orchard-ops` skill before touching Orchard (`pdw call hack_club_orchard_k8s__…`).

## External systems

- **Hack Club Auth**: OAuth2 authorization code flow. The token comes from `/oauth/token`, then
  identity from `GET /api/v1/me` (`identity.primary_email`, `id`, `slack_id`,
  `verification_status`). The default scopes are the community set. We're HQ, so `address` can
  be requested to prefill shipping.
- **mail.hackclub.com (Theseus)** (source: github.com/hackclub/theseus):
  - Auth is `Authorization: Bearer <THESEUS_API_KEY>`.
  - `GET /api/v1/warehouse/skus` returns `{"skus":[…]}` (enabled and in stock).
  - `POST /api/v1/warehouse_orders` takes `{warehouse_order:{recipient_email, user_facing_title,
    idempotency_key, tags[], metadata}, address:{first_name,last_name,line_1,line_2,city,state,
    postal_code,country,phone_number}, contents:[{sku,quantity}]}`. It creates **and
    dispatches** a real order to the warehouse. Our idempotency key is
    `<THESEUS_ORDER_TAG>-request-<id>`, so retries replay the order instead of shipping twice.
  - International orders need a phone number for customs.
  - A billing profile is required; it comes from the API key's default.
  - **Never call CreateOrder outside the admin "Ship" action. It ships real mail.**
- **Unified YSWS DB authors**: only about 64 of 114 authors had `Hack Club Auth Email` filled
  in (warehouse snapshot, 2026-09-29). An author missing it won't be recognized; fix it in
  Airtable.

## Gotchas

- porygon's OrbStack was once configured with `data_dir` on `/Volumes/PorygonWork`. When that
  drive was unmounted, Docker hung at "waiting for external data drive". On 2026-09-29 it was
  reset to the internal disk (`~/.orbstack/vmconfig.json`); the old config is saved as
  `vmconfig.json.bak-porygonwork-2026-09-29`.
- CSRF: mutating `/api/*` calls must be `Content-Type: application/json`, and their `Origin`
  (if present) must equal `BASE_URL`. In dev, `BASE_URL` is the Vite origin
  (`http://localhost:5173`).
- The service worker never caches `/api/*` or `/auth/*`. Keep it that way; limits and statuses
  must be live.
