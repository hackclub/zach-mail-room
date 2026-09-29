-- +goose Up
CREATE TABLE users (
    id                  BIGSERIAL PRIMARY KEY,
    hca_id              TEXT NOT NULL UNIQUE,
    email               TEXT NOT NULL,
    name                TEXT NOT NULL DEFAULT '',
    slack_id            TEXT NOT NULL DEFAULT '',
    verification_status TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX users_email_idx ON users (lower(email));

CREATE TABLE sessions (
    token_hash BYTEA PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions (user_id);

-- Single-row table of admin-managed global limits.
CREATE TABLE settings (
    id                               BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    requests_open                    BOOLEAN NOT NULL DEFAULT TRUE,
    max_items_per_request            INT NOT NULL DEFAULT 5 CHECK (max_items_per_request >= 0),
    request_cooldown_days            INT NOT NULL DEFAULT 30 CHECK (request_cooldown_days >= 0),
    international_shipping_fee_cents INT NOT NULL DEFAULT 1500 CHECK (international_shipping_fee_cents >= 0),
    updated_at                       TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO settings DEFAULT VALUES;

-- Items listed for request. Each is backed by a mail.hackclub.com warehouse SKU.
CREATE TABLE items (
    id              BIGSERIAL PRIMARY KEY,
    sku             TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    image_url       TEXT NOT NULL DEFAULT '',
    visible         BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order      INT NOT NULL DEFAULT 0,
    max_per_request INT NOT NULL DEFAULT 1 CHECK (max_per_request >= 1),
    max_per_user    INT CHECK (max_per_user IS NULL OR max_per_user >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE swag_requests (
    id                 BIGSERIAL PRIMARY KEY,
    user_id            BIGINT NOT NULL REFERENCES users (id),
    status             TEXT NOT NULL CHECK (status IN ('awaiting_payment', 'pending', 'dispatched', 'rejected', 'cancelled')),
    first_name         TEXT NOT NULL,
    last_name          TEXT NOT NULL,
    line_1             TEXT NOT NULL,
    line_2             TEXT NOT NULL DEFAULT '',
    city               TEXT NOT NULL,
    state              TEXT NOT NULL DEFAULT '',
    postal_code        TEXT NOT NULL,
    country            TEXT NOT NULL CHECK (country ~ '^[A-Z]{2}$'),
    phone_number       TEXT NOT NULL DEFAULT '',
    shipping_fee_cents INT NOT NULL DEFAULT 0,
    paid_at            TIMESTAMPTZ,
    note               TEXT NOT NULL DEFAULT '',
    admin_note         TEXT NOT NULL DEFAULT '',
    theseus_order_id   TEXT,
    tracking_number    TEXT NOT NULL DEFAULT '',
    carrier            TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX swag_requests_user_idx ON swag_requests (user_id, created_at DESC);
CREATE INDEX swag_requests_status_idx ON swag_requests (status, created_at);

CREATE TABLE swag_request_items (
    request_id BIGINT NOT NULL REFERENCES swag_requests (id) ON DELETE CASCADE,
    item_id    BIGINT NOT NULL REFERENCES items (id),
    quantity   INT NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (request_id, item_id)
);

-- Material YSWS authors submit to be printed and stocked in the warehouse.
CREATE TABLE author_submissions (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users (id),
    program     TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL DEFAULT 'sticker',
    file_url    TEXT NOT NULL,
    quantity    INT NOT NULL CHECK (quantity > 0),
    status      TEXT NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'approved', 'printing', 'stocked', 'rejected')),
    admin_note  TEXT NOT NULL DEFAULT '',
    sku         TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX author_submissions_user_idx ON author_submissions (user_id, created_at DESC);

-- +goose Down
DROP TABLE author_submissions;
DROP TABLE swag_request_items;
DROP TABLE swag_requests;
DROP TABLE items;
DROP TABLE settings;
DROP TABLE sessions;
DROP TABLE users;
