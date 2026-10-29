-- +goose Up
ALTER TABLE albums ADD COLUMN removed_at INTEGER;

-- +goose Down
ALTER TABLE albums DROP COLUMN removed_at;
