-- +goose Up
ALTER TABLE albums ADD COLUMN imported_at INTEGER;

-- +goose Down
ALTER TABLE albums DROP COLUMN imported_at;
