-- +goose Up
ALTER TABLE devices ADD COLUMN rssi INTEGER;
ALTER TABLE devices ADD COLUMN reader_resets INTEGER;
ALTER TABLE devices ADD COLUMN uptime_sec INTEGER;

-- +goose Down
ALTER TABLE devices DROP COLUMN uptime_sec;
ALTER TABLE devices DROP COLUMN reader_resets;
ALTER TABLE devices DROP COLUMN rssi;
