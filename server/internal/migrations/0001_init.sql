-- +goose Up
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    catalog_mode_until INTEGER,
    created_at INTEGER NOT NULL
);

CREATE TABLE login_sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    persistent INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX login_sessions_user ON login_sessions(user_id);

CREATE TABLE devices (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    fw_version TEXT,
    last_seen_at INTEGER,
    revoked_at INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX devices_user ON devices(user_id);

CREATE TABLE albums (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    discogs_master_id INTEGER,
    discogs_release_id INTEGER,
    title TEXT NOT NULL,
    artist TEXT NOT NULL,
    year INTEGER,
    genres TEXT NOT NULL DEFAULT '[]',
    styles TEXT NOT NULL DEFAULT '[]',
    cover_url TEXT,
    cover_path TEXT,
    cover_color TEXT,
    cover_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (cover_status IN ('pending', 'resolved', 'no_data', 'failed')),
    duration_sec INTEGER,
    duration_source TEXT,
    duration_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (duration_status IN ('pending', 'resolved', 'no_data', 'failed', 'manual')),
    created_at INTEGER NOT NULL,
    CHECK (discogs_master_id IS NOT NULL OR discogs_release_id IS NOT NULL)
);
CREATE INDEX albums_user ON albums(user_id);
CREATE UNIQUE INDEX albums_user_master ON albums(user_id, discogs_master_id) WHERE discogs_master_id IS NOT NULL;
CREATE UNIQUE INDEX albums_user_standalone ON albums(user_id, discogs_release_id)
    WHERE discogs_master_id IS NULL;

CREATE TABLE releases (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    album_id INTEGER NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    discogs_release_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    country TEXT,
    year INTEGER,
    label TEXT,
    catno TEXT,
    format TEXT,
    created_at INTEGER NOT NULL,
    UNIQUE (user_id, discogs_release_id)
);
CREATE INDEX releases_album ON releases(album_id);

CREATE TABLE tags (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    uid TEXT NOT NULL,
    album_id INTEGER REFERENCES albums(id) ON DELETE SET NULL,
    release_id INTEGER REFERENCES releases(id) ON DELETE SET NULL,
    assigned_at INTEGER,
    created_at INTEGER NOT NULL,
    UNIQUE (user_id, uid)
);

CREATE TABLE sessions (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    album_id INTEGER REFERENCES albums(id) ON DELETE SET NULL,
    release_id INTEGER REFERENCES releases(id) ON DELETE SET NULL,
    state TEXT NOT NULL
        CHECK (state IN ('active', 'unclassified', 'discarded', 'counted', 'pending_confirmation')),
    started_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    ended_at INTEGER,
    classified_at INTEGER,
    play_count INTEGER NOT NULL DEFAULT 0,
    listened_sec INTEGER NOT NULL DEFAULT 0,
    duration_confirmed INTEGER NOT NULL DEFAULT 1,
    resolution TEXT CHECK (resolution IN ('forgot', 'repeated', 'longer', 'auto')),
    rating INTEGER CHECK (rating BETWEEN 1 AND 10),
    rated_at INTEGER,
    note TEXT,
    inbox_state TEXT NOT NULL DEFAULT 'open' CHECK (inbox_state IN ('open', 'done', 'dismissed'))
);
CREATE INDEX sessions_user_state ON sessions(user_id, state);
CREATE INDEX sessions_user_album ON sessions(user_id, album_id);
CREATE INDEX sessions_device_active ON sessions(device_id) WHERE state = 'active';

-- +goose Down
DROP TABLE sessions;
DROP TABLE tags;
DROP TABLE releases;
DROP TABLE albums;
DROP TABLE devices;
DROP TABLE login_sessions;
DROP TABLE users;
