-- +goose Up
CREATE TABLE events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type            text        NOT NULL,
    payload         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    occurred_at     timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    idempotency_key text,
    CONSTRAINT events_type_check CHECK (type <> '' AND length(type) <= 64),
    CONSTRAINT events_payload_check CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX events_user_occurred_idx ON events (user_id, occurred_at DESC, id DESC);
CREATE INDEX events_occurred_at_idx ON events (occurred_at);

CREATE UNIQUE INDEX events_idempotency_key ON events (user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP TABLE events;
