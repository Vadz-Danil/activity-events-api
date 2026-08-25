-- +goose Up
CREATE TABLE users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    role          text        NOT NULL DEFAULT 'user',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_role_check CHECK (role IN ('user', 'admin'))
);

CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

-- +goose Down
DROP TABLE users;
