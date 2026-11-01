-- +goose Up
ALTER TABLE devices ADD COLUMN wifi_change_requested_at INTEGER;

-- +goose Down
ALTER TABLE devices DROP COLUMN wifi_change_requested_at;
