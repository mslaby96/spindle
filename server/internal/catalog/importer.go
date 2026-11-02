package catalog

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/mslaby96/spindle/server/internal/metadata/discogs"
)

const (
	ImportIdle    = "idle"
	ImportRunning = "running"
	ImportDone    = "done"
	ImportFailed  = "failed"
)

// EventImport carries an ImportProgress whenever an import moves along.
const EventImport = "discogs_import"

var ErrImportRunning = errors.New("an import is already running")

// CollectionSource is where a user's records are listed: Discogs, through the token's account.
type CollectionSource interface {
	Identity(ctx context.Context) (string, error)
	Collection(ctx context.Context, username string) ([]discogs.CollectionItem, error)
}

// Importer brings a Discogs collection into the library in the background, one user at a time.
type Importer struct {
	Service *Service
	Source  CollectionSource
	// Enqueue has an album's cover and length looked up later.
	Enqueue func(userID, albumID int64)
	// Backup, when set, runs before anything is written.
	Backup func(ctx context.Context) error
	// Publish, when set, gets every change of an import's progress.
	Publish func(userID int64, p ImportProgress)

	mu       sync.Mutex
	progress map[int64]ImportProgress
}

// ImportProgress is what the app shows about an import. Done and Total count new albums, which are what takes time.
type ImportProgress struct {
	State   string         `json:"state"`
	Done    int            `json:"done"`
	Total   int            `json:"total"`
	Summary *ImportSummary `json:"summary,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// library is what an import needs to know about the albums already there.
type library struct {
	// masters and standalone hold albums by Discogs master id, and by release id for albums without a master.
	masters    map[int64]libAlbum
	standalone map[int64]libAlbum
	// releases are the Discogs pressings already in the library, removed the ones taken out of the collection.
	releases map[int64]bool
	removed  map[int64]bool
}

type libAlbum struct {
	id      int64
	title   string
	removed bool
}

type newAlbum struct {
	// masterID is 0 for a release without a master, which becomes an album of its own.
	masterID int64
	items    []discogs.CollectionItem
}

type pressing struct {
	albumID int64
	item    discogs.CollectionItem
}

type importPlan struct {
	found     int
	newAlbums []newAlbum
	pressings []pressing
	have      int
	removed   []string
	// removedPressings counts pressings taken out of the collection in Spindle that Discogs still lists.
	removedPressings int
}

// ImportSummary says what an import does, or did.
type ImportSummary struct {
	Found            int      `json:"found"`
	NewAlbums        int      `json:"new_albums"`
	NewPressings     int      `json:"new_pressings"`
	AlreadyInLibrary int      `json:"already_in_library"`
	SkippedRemoved   []string `json:"skipped_removed"`
	// SkippedRemovedPressings are pressings removed in Spindle that stay out.
	SkippedRemovedPressings int `json:"skipped_removed_pressings"`
}

// plan decides what a collection adds to the library. It only ever adds: pressings already there are counted, and
// albums the user removed from their collection in Spindle stay out, even though Discogs still lists them.
func plan(items []discogs.CollectionItem, lib library) importPlan {
	p := importPlan{found: len(items), removed: []string{}}
	seen := map[int64]bool{}
	grouped := map[int64]int{}
	skipped := map[int64]bool{}
	for _, it := range items {
		if lib.removed[it.ReleaseID] {
			p.removedPressings++
			continue
		}
		if seen[it.ReleaseID] || lib.releases[it.ReleaseID] {
			p.have++
			continue
		}
		seen[it.ReleaseID] = true
		key, existing, ok := lib.albumFor(it)
		switch {
		case ok && existing.removed:
			if !skipped[existing.id] {
				skipped[existing.id] = true
				p.removed = append(p.removed, existing.title)
			}
		case ok:
			p.pressings = append(p.pressings, pressing{albumID: existing.id, item: it})
		default:
			p.group(key, it, grouped)
		}
	}
	return p
}

// albumFor finds the album a pressing belongs to, and the key that groups new pressings of the same album: the
// master id, or the negated release id for a release without a master.
func (lib library) albumFor(it discogs.CollectionItem) (int64, libAlbum, bool) {
	if it.MasterID != 0 {
		a, ok := lib.masters[it.MasterID]
		return it.MasterID, a, ok
	}
	a, ok := lib.standalone[it.ReleaseID]
	return -it.ReleaseID, a, ok
}

func (p *importPlan) group(key int64, it discogs.CollectionItem, grouped map[int64]int) {
	if i, ok := grouped[key]; ok {
		p.newAlbums[i].items = append(p.newAlbums[i].items, it)
		return
	}
	grouped[key] = len(p.newAlbums)
	p.newAlbums = append(p.newAlbums, newAlbum{masterID: it.MasterID, items: []discogs.CollectionItem{it}})
}

func (p importPlan) summary() ImportSummary {
	s := ImportSummary{Found: p.found, NewAlbums: len(p.newAlbums), NewPressings: len(p.pressings),
		AlreadyInLibrary: p.have, SkippedRemoved: p.removed, SkippedRemovedPressings: p.removedPressings}
	for _, a := range p.newAlbums {
		s.NewPressings += len(a.items)
	}
	return s
}

// Preview says what an import would do, without changing anything.
func (im *Importer) Preview(ctx context.Context, userID int64) (ImportSummary, error) {
	p, err := im.prepare(ctx, userID)
	if err != nil {
		return ImportSummary{}, err
	}
	return p.summary(), nil
}

func (im *Importer) Status(userID int64) ImportProgress {
	im.mu.Lock()
	defer im.mu.Unlock()
	if p, ok := im.progress[userID]; ok {
		return p
	}
	return ImportProgress{State: ImportIdle}
}

// Start begins an import and returns at once; progress arrives through Publish and Status.
func (im *Importer) Start(userID int64) error {
	im.mu.Lock()
	if im.progress == nil {
		im.progress = map[int64]ImportProgress{}
	}
	if im.progress[userID].State == ImportRunning {
		im.mu.Unlock()
		return ErrImportRunning
	}
	im.progress[userID] = ImportProgress{State: ImportRunning}
	im.mu.Unlock()
	go im.background(userID)
	return nil
}

func (im *Importer) background(userID int64) {
	ctx := context.Background()
	im.report(userID, ImportProgress{State: ImportRunning})
	sum, err := im.backupAndRun(ctx, userID)
	if err != nil {
		im.report(userID, ImportProgress{State: ImportFailed, Error: err.Error()})
		return
	}
	im.report(userID, ImportProgress{State: ImportDone, Done: sum.NewAlbums, Total: sum.NewAlbums, Summary: &sum})
}

func (im *Importer) backupAndRun(ctx context.Context, userID int64) (ImportSummary, error) {
	if im.Backup != nil {
		if err := im.Backup(ctx); err != nil {
			return ImportSummary{}, fmt.Errorf("backup before the import failed: %w", err)
		}
	}
	return im.run(ctx, userID, func(done, total int) {
		im.report(userID, ImportProgress{State: ImportRunning, Done: done, Total: total})
	})
}

func (im *Importer) report(userID int64, p ImportProgress) {
	im.mu.Lock()
	im.progress[userID] = p
	im.mu.Unlock()
	if im.Publish != nil {
		im.Publish(userID, p)
	}
}

func (im *Importer) prepare(ctx context.Context, userID int64) (importPlan, error) {
	username, err := im.Source.Identity(ctx)
	if err != nil {
		return importPlan{}, err
	}
	items, err := im.Source.Collection(ctx, username)
	if err != nil {
		return importPlan{}, err
	}
	lib, err := im.Service.library(ctx, userID)
	if err != nil {
		return importPlan{}, err
	}
	return plan(items, lib), nil
}

// run imports the collection. It can be run again at any point, also after it stopped halfway: what is already in the
// library is skipped.
func (im *Importer) run(ctx context.Context, userID int64, progress func(done, total int)) (ImportSummary, error) {
	p, err := im.prepare(ctx, userID)
	if err != nil {
		return ImportSummary{}, err
	}
	progress(0, len(p.newAlbums))
	for i, a := range p.newAlbums {
		id, err := im.Service.importAlbum(ctx, userID, a)
		if err != nil {
			return ImportSummary{}, err
		}
		im.Enqueue(userID, id)
		progress(i+1, len(p.newAlbums))
	}
	for _, pr := range p.pressings {
		if err := im.Service.insertPressing(ctx, userID, pr.albumID, pr.item); err != nil {
			return ImportSummary{}, err
		}
	}
	return p.summary(), nil
}

// importAlbum stores a new album with its pressings. An album with a master takes the master's original year and
// cover, like one added by hand; if Discogs no longer has the master, the collection's own details stand in.
func (s *Service) importAlbum(ctx context.Context, userID int64, a newAlbum) (int64, error) {
	first := a.items[0]
	var master, release *int64
	title, artist, year, genres, styles, cover := first.Title, first.Artist, first.Year, first.Genres, first.Styles, first.CoverURL
	if a.masterID != 0 {
		master = &a.masterID
		m, err := s.Discogs.Master(ctx, a.masterID)
		switch {
		case err == nil:
			title, artist, year, genres, styles = m.Title, discogs.ArtistNames(m.Artists), m.Year, m.Genres, m.Styles
			cover = cmp.Or(discogs.PrimaryImage(m.Images), cover)
		case !errors.Is(err, discogs.ErrNotFound):
			return 0, err
		}
	} else {
		release = &first.ReleaseID
	}
	id, err := s.insertAlbum(ctx, userID, master, release, title, artist, year, genres, styles, cover)
	if err != nil {
		return 0, err
	}
	for _, it := range a.items {
		if err := s.insertPressing(ctx, userID, id, it); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// insertPressing adds a pressing from the collection and marks its album as part of the collection, so it stays in
// the library even with no sleeve or listen pointing at it.
func (s *Service) insertPressing(ctx context.Context, userID, albumID int64, it discogs.CollectionItem) error {
	_, err := s.DB.W.ExecContext(ctx,
		`INSERT INTO releases (user_id, album_id, discogs_release_id, title, year, label, catno, format, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, albumID, it.ReleaseID, it.Title, nullYear(it.Year), nullString(it.Label), nullString(it.CatNo),
		nullString(it.Format), s.now())
	if err != nil {
		return err
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE albums SET imported_at = coalesce(imported_at, ?) WHERE id = ? AND user_id = ?`,
		s.now(), albumID, userID)
	return err
}

// library reads what an import needs to know about the user's albums.
func (s *Service) library(ctx context.Context, userID int64) (library, error) {
	lib := library{masters: map[int64]libAlbum{}, standalone: map[int64]libAlbum{}, releases: map[int64]bool{},
		removed: map[int64]bool{}}
	if err := s.libraryAlbums(ctx, userID, lib); err != nil {
		return library{}, err
	}
	ids, err := s.DB.R.QueryContext(ctx,
		`SELECT discogs_release_id, removed_at IS NOT NULL FROM releases WHERE user_id = ?`, userID)
	if err != nil {
		return library{}, err
	}
	defer ids.Close()
	for ids.Next() {
		var id int64
		var removed bool
		if err := ids.Scan(&id, &removed); err != nil {
			return library{}, err
		}
		if removed {
			lib.removed[id] = true
		} else {
			lib.releases[id] = true
		}
	}
	return lib, ids.Err()
}

func (s *Service) libraryAlbums(ctx context.Context, userID int64, lib library) error {
	rows, err := s.DB.R.QueryContext(ctx,
		`SELECT id, discogs_master_id, discogs_release_id, title, removed_at IS NOT NULL FROM albums WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a libAlbum
		var master, release sql.NullInt64
		if err := rows.Scan(&a.id, &master, &release, &a.title, &a.removed); err != nil {
			return err
		}
		if master.Valid {
			lib.masters[master.Int64] = a
		} else if release.Valid {
			lib.standalone[release.Int64] = a
		}
	}
	return rows.Err()
}

// UntaggedAlbums are the albums in the collection that no sleeve points at yet, so the app can offer them first when
// a new sleeve is put on the stand.
func (s *Service) UntaggedAlbums(ctx context.Context, userID int64) ([]Album, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+albumColumns+` FROM albums a
		WHERE user_id = ? AND removed_at IS NULL AND NOT EXISTS (SELECT 1 FROM tags t WHERE t.album_id = a.id)
		ORDER BY lower(artist), lower(title), id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Album{}
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
