-- +goose Up
ALTER TABLE devices ADD COLUMN update_target TEXT;
ALTER TABLE devices ADD COLUMN update_state TEXT;

-- +goose Down
ALTER TABLE devices DROP COLUMN update_state;
ALTER TABLE devices DROP COLUMN update_target;
