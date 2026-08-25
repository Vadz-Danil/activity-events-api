-- +goose Up
CREATE TABLE google_oauth_states (
    state_hash    bytea PRIMARY KEY,
    code_verifier text        NOT NULL,
    redirect_to   text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL
);

CREATE INDEX google_oauth_states_expires_at_idx ON google_oauth_states (expires_at);

CREATE TABLE google_oauth_codes (
    code_hash  bytea PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);

CREATE INDEX google_oauth_codes_expires_at_idx ON google_oauth_codes (expires_at);

-- +goose Down
DROP TABLE google_oauth_codes;
DROP TABLE google_oauth_states;
