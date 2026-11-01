package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mslaby96/spindle/server/internal/db"
)

func dataDir(t *testing.T) (string, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "spindle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('maciej', 'h', 1)`)
	os.MkdirAll(filepath.Join(dir, "covers"), 0o750)
	os.WriteFile(filepath.Join(dir, "covers", "1.jpeg"), []byte("cover one"), 0o640)
	return dir, d
}

func users(t *testing.T, path string) []string {
	t.Helper()
	d, err := db.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	rows, err := d.R.Query(`SELECT username FROM users ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		rows.Scan(&u)
		out = append(out, u)
	}
	return out
}

func at(ts string) func() time.Time {
	return func() time.Time { t, _ := time.Parse("2006-01-02 15:04:05", ts); return t }
}

func TestBackupOfARunningServerHasTheDataAndCovers(t *testing.T) {
	dir, d := dataDir(t)
	backups := t.TempDir()
	s := &Store{DB: d, DataDir: dir, Dir: backups, Keep: 7, Now: at("2026-11-01 20:24:00")}

	path, err := s.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "spindle-backup-20261101-202400.tar.gz" {
		t.Fatalf("named %s", path)
	}
	restored := t.TempDir()
	aside, err := Restore(path, restored)
	if err != nil || aside != "" {
		t.Fatalf("%q %v", aside, err)
	}
	if got := users(t, filepath.Join(restored, "spindle.db")); !slices.Equal(got, []string{"maciej"}) {
		t.Fatalf("users %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(restored, "covers", "1.jpeg")); string(b) != "cover one" {
		t.Fatalf("cover %q", b)
	}
	if left, _ := filepath.Glob(filepath.Join(backups, "*")); len(left) != 1 {
		t.Fatalf("left in the backup dir: %v", left)
	}
}

func TestOnlyTheNewestBackupsAreKept(t *testing.T) {
	_, d := dataDir(t)
	backups := t.TempDir()
	os.WriteFile(filepath.Join(backups, "notes.txt"), []byte("mine"), 0o640)
	for _, ts := range []string{"2026-10-28 03:00:00", "2026-10-29 03:00:00", "2026-10-30 03:00:00", "2026-10-31 03:00:00"} {
		s := &Store{DB: d, DataDir: t.TempDir(), Dir: backups, Keep: 2, Now: at(ts)}
		if _, err := s.Create(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	names, _ := filepath.Glob(filepath.Join(backups, "*"))
	for i := range names {
		names[i] = filepath.Base(names[i])
	}
	want := []string{"notes.txt", "spindle-backup-20261030-030000.tar.gz", "spindle-backup-20261031-030000.tar.gz"}
	if !slices.Equal(names, want) {
		t.Fatalf("kept %v", names)
	}
}

func TestRestoreKeepsTheCurrentDataAside(t *testing.T) {
	dir, d := dataDir(t)
	s := &Store{DB: d, DataDir: dir, Dir: t.TempDir(), Keep: 7, Now: at("2026-11-01 20:24:00")}
	path, _ := s.Create(context.Background())
	d.W.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES ('later', 'h', 2)`)
	d.Close()

	aside, err := Restore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := users(t, filepath.Join(dir, "spindle.db")); !slices.Equal(got, []string{"maciej"}) {
		t.Fatalf("restored users %v", got)
	}
	if !slices.Equal(users(t, filepath.Join(aside, "spindle.db")), []string{"maciej", "later"}) {
		t.Fatalf("previous data not kept in %q", aside)
	}
}

func TestRestoreRefusesWhileTheServerRuns(t *testing.T) {
	dir, d := dataDir(t)
	s := &Store{DB: d, DataDir: dir, Dir: t.TempDir(), Keep: 7, Now: time.Now}
	path, _ := s.Create(context.Background())
	if _, err := Restore(path, dir); !errors.Is(err, ErrServerRunning) {
		t.Fatalf("got %v", err)
	}
}

func craftedArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spindle-backup-x.tar.gz")
	f, _ := os.Create(path)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o640, Size: int64(len(body))})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	f.Close()
	return path
}

func TestRestoreRejectsBrokenOrUnsafeArchives(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no database":         {"covers/1.jpeg": "x"},
		"not a database":      {"spindle.db": "hello"},
		"path out of the dir": {"spindle.db": "x", "../../evil": "x"},
		"absolute path":       {"spindle.db": "x", "/etc/evil": "x"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := Restore(craftedArchive(t, files), dir); err == nil {
				t.Fatal("accepted")
			}
			if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
				t.Fatalf("left behind %v", left)
			}
		})
	}
}

func TestTheNextBackupIsDueAnIntervalAfterTheLastOne(t *testing.T) {
	_, d := dataDir(t)
	dir := t.TempDir()
	s := &Store{DB: d, DataDir: t.TempDir(), Dir: dir, Keep: 7, Now: at("2026-11-01 03:00:00")}
	if wait := s.untilNext(24 * time.Hour); wait != 0 {
		t.Fatalf("with no backup yet: wait %v", wait)
	}
	s.Create(context.Background())
	s.Now = at("2026-11-01 20:00:00")
	if wait := s.untilNext(24 * time.Hour); wait != 7*time.Hour {
		t.Fatalf("17 h after the last one: wait %v", wait)
	}
	s.Now = at("2026-11-03 20:00:00")
	if wait := s.untilNext(24 * time.Hour); wait != 0 {
		t.Fatalf("long overdue: wait %v", wait)
	}
}

func TestRunMakesBackupsUntilStopped(t *testing.T) {
	_, d := dataDir(t)
	dir := t.TempDir()
	s := &Store{DB: d, DataDir: t.TempDir(), Dir: dir, Keep: 7, Now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, time.Hour, func(string, error) {}); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if found, _ := filepath.Glob(filepath.Join(dir, prefix+"*"+suffix)); len(found) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no backup made on start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestEveryCoverGoesIntoTheBackup(t *testing.T) {
	dir, d := dataDir(t)
	os.WriteFile(filepath.Join(dir, "covers", "2.jpeg"), []byte("cover two"), 0o640)
	s := &Store{DB: d, DataDir: dir, Dir: t.TempDir(), Keep: 7, Now: time.Now}
	path, _ := s.Create(context.Background())
	restored := t.TempDir()
	if _, err := Restore(path, restored); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"1.jpeg": "cover one", "2.jpeg": "cover two"} {
		if b, _ := os.ReadFile(filepath.Join(restored, "covers", name)); string(b) != want {
			t.Errorf("%s: %q", name, b)
		}
	}
}

func TestPruningLeavesOtherArchivesAlone(t *testing.T) {
	_, d := dataDir(t)
	backups := t.TempDir()
	os.WriteFile(filepath.Join(backups, "photos.tar.gz"), []byte("mine"), 0o640)
	os.WriteFile(filepath.Join(backups, "spindle-backup-notes.txt"), []byte("mine"), 0o640)
	for _, ts := range []string{"2026-10-30 03:00:00", "2026-10-31 03:00:00"} {
		s := &Store{DB: d, DataDir: t.TempDir(), Dir: backups, Keep: 1, Now: at(ts)}
		s.Create(context.Background())
	}
	for _, name := range []string{"photos.tar.gz", "spindle-backup-notes.txt", "spindle-backup-20261031-030000.tar.gz"} {
		if _, err := os.Stat(filepath.Join(backups, name)); err != nil {
			t.Errorf("%s was removed", name)
		}
	}
}

func TestAStrangelyNamedFileDoesNotMakeABackupDue(t *testing.T) {
	_, d := dataDir(t)
	dir := t.TempDir()
	s := &Store{DB: d, DataDir: t.TempDir(), Dir: dir, Keep: 7, Now: at("2026-11-01 03:00:00")}
	s.Create(context.Background())
	os.WriteFile(filepath.Join(dir, "spindle-backup-garbage.tar.gz"), []byte("x"), 0o640)
	s.Now = at("2026-11-01 20:00:00")
	if wait := s.untilNext(24 * time.Hour); wait != 7*time.Hour {
		t.Fatalf("wait %v", wait)
	}
}

func TestRestoreKeepsADatabaseWithoutCoversAside(t *testing.T) {
	dir, d := dataDir(t)
	s := &Store{DB: d, DataDir: dir, Dir: t.TempDir(), Keep: 7, Now: time.Now}
	path, _ := s.Create(context.Background())
	d.Close()
	os.RemoveAll(filepath.Join(dir, "covers"))
	if _, err := Restore(path, dir); err != nil {
		t.Fatal(err)
	}
	if aside, _ := filepath.Glob(filepath.Join(dir, "before-restore-*", "spindle.db")); len(aside) != 1 {
		t.Fatal("the database that was there is gone")
	}
}

func TestRunKeepsMakingBackups(t *testing.T) {
	_, d := dataDir(t)
	dir := t.TempDir()
	clock := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	s := &Store{DB: d, DataDir: t.TempDir(), Dir: dir, Keep: 7, Now: func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(24 * time.Hour)
		return clock
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, time.Hour, func(string, error) {}); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if found, _ := filepath.Glob(filepath.Join(dir, prefix+"*"+suffix)); len(found) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stopped after the first backup")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}
