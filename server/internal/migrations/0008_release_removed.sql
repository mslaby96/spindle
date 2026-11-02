-- +goose Up
ALTER TABLE releases ADD COLUMN removed_at INTEGER;

-- +goose Down
ALTER TABLE releases DROP COLUMN removed_at;
