-- +goose Up
-- Lengths summed from Discogs tracklists left out suites listed as one "index" track with its parts, so they are
-- looked up again. The old length stays until the new one is in.
UPDATE albums SET duration_status = 'pending'
WHERE duration_status = 'resolved' AND duration_source IN ('discogs_master', 'discogs_release');

-- +goose Down
SELECT 1;
