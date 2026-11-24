-- Session store for alexedwards/scs (pgxstore), which does not create its
-- own schema. scs's cleanup goroutine deletes expired rows.

-- +goose Up
CREATE TABLE sessions (
    token  text PRIMARY KEY,
    data   bytea NOT NULL,
    expiry timestamptz NOT NULL DEFAULT now() + interval '1 day'
);

CREATE INDEX sessions_expiry_idx ON sessions (expiry);

-- +goose Down
DROP TABLE sessions;
