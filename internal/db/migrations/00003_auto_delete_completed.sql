-- Per-user setting: auto-delete completed todos N days after completion.
-- NULL = never delete (the default for new and existing accounts).

-- +goose Up
ALTER TABLE users ADD COLUMN auto_delete_completed_days int;

-- 0 and negatives are meaningless (NULL means never); the ceiling catches
-- typos. The web UI validates the same range before writing.
ALTER TABLE users ADD CONSTRAINT users_auto_delete_days_check
    CHECK (auto_delete_completed_days IS NULL
           OR auto_delete_completed_days BETWEEN 1 AND 3650);

-- +goose Down
ALTER TABLE users DROP CONSTRAINT users_auto_delete_days_check;
ALTER TABLE users DROP COLUMN auto_delete_completed_days;
