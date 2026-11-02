// Package backup copies Spindle's database and covers into one archive and puts them back from one.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

const (
	prefix   = "spindle-backup-"
	suffix   = ".tar.gz"
	stamp    = "20060102-150405"
	database = "spindle.db"
	covers   = "covers"
)

// maxRestoreBytes caps what a restore unpacks, so a crafted archive can't fill the disk.
var maxRestoreBytes int64 = 4 << 30

var errTooLarge = errors.New("the backup is too large to restore: over 4 GiB unpacked")

var ErrServerRunning = errors.New("spindle.db-wal is there, so the server looks like it is still running; stop it first")

type Store struct {
	DB      *db.DB
	DataDir string
	Dir     string
	Keep    int
	Now     func() time.Time
}

// Create writes a backup while the server keeps running and drops the oldest ones beyond Keep. It returns its path.
func (s *Store) Create(ctx context.Context) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return "", err
	}
	name := prefix + s.Now().Format(stamp) + suffix
	snapshot := filepath.Join(s.Dir, "."+name+".db")
	os.Remove(snapshot)
	defer os.Remove(snapshot)
	// VACUUM INTO reads one consistent view of the database, however many writes are going on.
	if _, err := s.DB.W.ExecContext(ctx, `VACUUM INTO ?`, snapshot); err != nil {
		return "", fmt.Errorf("snapshot the database: %w", err)
	}
	tmp := filepath.Join(s.Dir, "."+name+".tmp")
	if err := writeArchive(tmp, snapshot, filepath.Join(s.DataDir, covers)); err != nil {
		os.Remove(tmp)
		return "", err
	}
	final := filepath.Join(s.Dir, name)
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return final, prune(s.Dir, s.Keep)
}

// Run makes a backup every interval until ctx ends, starting at once when the last one is already that old, so
// restarts don't keep pushing it back. Each result goes to report; after a failure the next try is a minute later.
func (s *Store) Run(ctx context.Context, every time.Duration, report func(path string, err error)) {
	for {
		t := time.NewTimer(s.untilNext(every))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		path, err := s.Create(ctx)
		if ctx.Err() != nil {
			return
		}
		report(path, err)
		if err != nil {
			// don't retry in a tight loop when, say, the backup folder is full or gone
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
		}
	}
}

func (s *Store) untilNext(every time.Duration) time.Duration {
	last, ok := s.newest()
	if !ok {
		return 0
	}
	return max(0, last.Add(every).Sub(s.Now()))
}

func (s *Store) newest() (time.Time, bool) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return time.Time{}, false
	}
	var latest time.Time
	for _, e := range entries {
		ts, ok := strings.CutPrefix(e.Name(), prefix)
		ts, ok2 := strings.CutSuffix(ts, suffix)
		if !ok || !ok2 {
			continue
		}
		if t, err := time.ParseInLocation(stamp, ts, s.Now().Location()); err == nil && t.After(latest) {
			latest = t
		}
	}
	return latest, !latest.IsZero()
}

func writeArchive(target, snapshot, coverDir string) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := addFile(tw, snapshot, database); err != nil {
		return err
	}
	if err := addCovers(tw, coverDir); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Sync()
}

func addCovers(tw *tar.Writer, coverDir string) error {
	entries, err := os.ReadDir(coverDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if err := addFile(tw, filepath.Join(coverDir, e.Name()), path.Join(covers, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func addFile(tw *tar.Writer, from, name string) error {
	f, err := os.Open(from)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o640, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// prune keeps the newest keep backups; the timestamp in their names sorts them. Other files are left alone.
func prune(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), suffix) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	var errs []error
	for _, n := range names[:max(0, len(names)-keep)] {
		errs = append(errs, os.Remove(filepath.Join(dir, n)))
	}
	return errors.Join(errs...)
}

// Restore puts a backup's database and covers into dataDir. The server must be stopped. Whatever was there before is
// moved into a before-restore-* folder next to it rather than deleted; Restore returns that folder, or "" if there was
// nothing to move.
func Restore(archive, dataDir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dataDir, database+"-wal")); err == nil {
		return "", ErrServerRunning
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(dataDir, ".restore-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := unpack(archive, tmp); err != nil {
		return "", err
	}
	aside, err := moveAside(dataDir)
	if err != nil {
		return "", err
	}
	return aside, moveIn(tmp, dataDir)
}

// unpack extracts the archive and checks that what came out is a healthy Spindle database.
func unpack(archive, dir string) error {
	if err := extract(archive, dir); err != nil {
		return err
	}
	return check(filepath.Join(dir, database))
}

func moveIn(from, dataDir string) error {
	if err := os.Rename(filepath.Join(from, database), filepath.Join(dataDir, database)); err != nil {
		return err
	}
	err := os.Rename(filepath.Join(from, covers), filepath.Join(dataDir, covers))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// extract only takes the database and flat cover files, so an archive can't write anywhere else.
func extract(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("not a Spindle backup: %w", err)
	}
	tr := tar.NewReader(gz)
	left := maxRestoreBytes
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("not a Spindle backup: %w", err)
		}
		if left, err = unpackEntry(tr, h, dir, left); err != nil {
			return err
		}
	}
}

// unpackEntry writes one entry and returns how much may still be unpacked.
func unpackEntry(r io.Reader, h *tar.Header, dir string, left int64) (int64, error) {
	if !allowed(h) {
		return 0, fmt.Errorf("not a Spindle backup: unexpected entry %q", h.Name)
	}
	n, err := writeEntry(io.LimitReader(r, left+1), filepath.Join(dir, filepath.FromSlash(h.Name)))
	if err != nil {
		return 0, err
	}
	if left -= n; left < 0 {
		return 0, errTooLarge
	}
	return left, nil
}

func writeEntry(r io.Reader, target string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return 0, err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, r)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}

func allowed(h *tar.Header) bool {
	if h.Typeflag != tar.TypeReg {
		return false
	}
	dir, base := path.Split(h.Name)
	return h.Name == database || (dir == covers+"/" && base != "" && base != "." && base != "..")
}

func check(dbPath string) error {
	if _, err := os.Stat(dbPath); err != nil {
		return errors.New("not a Spindle backup: it has no spindle.db")
	}
	d, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return err
	}
	defer d.Close()
	var result string
	if err := d.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil || result != "ok" {
		return fmt.Errorf("the backup's database is damaged: %v %s", err, result)
	}
	return nil
}

func moveAside(dataDir string) (string, error) {
	var present []string
	for _, n := range []string{database, database + "-shm", covers} {
		if _, err := os.Stat(filepath.Join(dataDir, n)); err == nil {
			present = append(present, n)
		}
	}
	if len(present) == 0 {
		return "", nil
	}
	aside := filepath.Join(dataDir, "before-restore-"+time.Now().Format(stamp))
	if err := os.Mkdir(aside, 0o750); err != nil {
		return "", err
	}
	for _, n := range present {
		if err := os.Rename(filepath.Join(dataDir, n), filepath.Join(aside, n)); err != nil {
			return "", err
		}
	}
	return aside, nil
}
