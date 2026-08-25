-- +goose Up
ALTER TABLE users
    ALTER COLUMN password_hash DROP NOT NULL,
    ADD COLUMN google_sub     text,
    ADD COLUMN name           text,
    ADD COLUMN email_verified boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT users_login_method_check
        CHECK (password_hash IS NOT NULL OR google_sub IS NOT NULL);

CREATE UNIQUE INDEX users_google_sub_key ON users (google_sub) WHERE google_sub IS NOT NULL;

CREATE TABLE refresh_tokens (
    id          uuid PRIMARY KEY,
    family_id   uuid        NOT NULL,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  bytea       NOT NULL,
    user_agent  text,
    ip          inet,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    replaced_by uuid REFERENCES refresh_tokens (id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX refresh_tokens_token_hash_key ON refresh_tokens (token_hash);
CREATE INDEX refresh_tokens_family_id_idx ON refresh_tokens (family_id);
CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id) WHERE revoked_at IS NULL;
CREATE INDEX refresh_tokens_expires_at_idx ON refresh_tokens (expires_at);

-- +goose Down
DROP TABLE refresh_tokens;

UPDATE users SET password_hash = COALESCE(password_hash, '');

ALTER TABLE users
    DROP CONSTRAINT users_login_method_check,
    DROP COLUMN google_sub,
    DROP COLUMN name,
    DROP COLUMN email_verified,
    ALTER COLUMN password_hash SET NOT NULL;
