-- +goose Up
CREATE TABLE device_pairings (
    code_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    expires_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE device_pairings;
