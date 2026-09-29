# zach-mail-room

Hack Club's mail room: hack clubbers request swag, YSWS authors stock the warehouse with their
program's printed material, and admins run it all. Fulfillment goes through
[mail.hackclub.com](https://mail.hackclub.com), and sign-in uses [Hack Club Auth](https://auth.hackclub.com).

- **Backend:** Go (`cmd/server`, `internal/`), Postgres, goose migrations
- **Frontend:** SvelteKit PWA (`web/`), installable and with an offline app shell
- **Deploy:** one Docker image on Orchard, configured entirely through env vars

## Quick start

```sh
cp .env.example ../path/to/main/worktree/.env   # see `make env-path`; fill in secrets
make db       # Postgres in docker compose
make test     # Go + web tests
make dev      # http://localhost:5173
```

See [AGENTS.md](AGENTS.md) for architecture, the TDD workflow, config, deployment, and how
worktree sessions land on `main`.
