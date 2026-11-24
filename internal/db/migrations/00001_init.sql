-- Initial schema: users, api_tokens, todos.
-- Dates are plain DATE columns (no times); ordering is alphabetical (no position column).

-- +goose Up
CREATE TABLE users (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email         text NOT NULL,
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- Uniqueness is enforced on the lowercased email; the app also stores it lowercased.
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

CREATE TABLE api_tokens (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users (id),
    token_hash   text NOT NULL UNIQUE,
    token_prefix text NOT NULL,
    name         text NOT NULL,
    last_used_at timestamptz,
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX api_tokens_expires_at_idx ON api_tokens (expires_at);

CREATE TABLE todos (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users (id),
    title        text NOT NULL,
    notes        text,
    due_date     date,
    completed    boolean NOT NULL DEFAULT false,
    completed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX todos_user_completed_due_idx ON todos (user_id, completed, due_date);

-- +goose Down
DROP TABLE todos;
DROP TABLE api_tokens;
DROP TABLE users;
