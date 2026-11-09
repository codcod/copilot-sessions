package backup

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

// readArchive returns the headers by name and the bytes of session-store.db.
func readArchive(t *testing.T, path string) (map[string]*tar.Header, []byte) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	hdrs := map[string]*tar.Header{}
	var db []byte
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return hdrs, db
		}
		if err != nil {
			t.Fatal(err)
		}
		hdrs[h.Name] = h
		if h.Name == "session-store.db" {
			if db, err = io.ReadAll(tr); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Spec §9 case 10: entries, lock dir excluded, DB snapshot includes WAL content.
func TestCreate(t *testing.T) {
	fx := testfix.Build(t)
	// A sibling of the lock dir is archived; only the lock dir itself is excluded.
	oldLocks := filepath.Join(fx.Home, "session-state", ".session-operation-locks-old")
	if err := os.MkdirAll(oldLocks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldLocks, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Leave a committed row in the WAL: no checkpoint, connection held open.
	live, err := sql.Open("sqlite", filepath.Join(fx.Home, "session-store.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	live.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA wal_autocheckpoint=0", "INSERT INTO sessions (id) VALUES ('wal-only-row')"} {
		if _, err := live.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if fi, err := os.Stat(filepath.Join(fx.Home, "session-store.db-wal")); err != nil || fi.Size() == 0 {
		t.Fatalf("WAL not populated: %v", err)
	}

	target := filepath.Join(fx.BackupDir, "nested", "b.tar.gz")
	if err := Create(fx.Home, target, true); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o600 {
		t.Errorf("archive mode %v", fi.Mode().Perm())
	}
	hdrs, dbBytes := readArchive(t, target)

	for _, want := range []string{"session-state/", "session-state/.other/workspace.yaml", "session-state/.session-operation-locks-old/x", "session-store.db", "open-sessions-state.json", "sidebar-sessions-state/a.json"} {
		if hdrs[want] == nil {
			t.Errorf("missing %s", want)
		}
	}
	for name := range hdrs {
		if strings.HasPrefix(name, "session-state/.session-operation-locks/") || strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") {
			t.Errorf("unexpected entry %s", name)
		}
	}
	// Every file and dir under session-state/ (bar the lock dir) and the sidebar dir is
	// there with its mode and mtime.
	for _, item := range []string{"session-state", "sidebar-sessions-state", "open-sessions-state.json"} {
		filepath.WalkDir(filepath.Join(fx.Home, item), func(p string, d fs.DirEntry, _ error) error {
			rel, _ := filepath.Rel(fx.Home, p)
			if rel == filepath.FromSlash("session-state/.session-operation-locks") {
				return fs.SkipDir
			}
			name := filepath.ToSlash(rel)
			if d.IsDir() {
				name += "/"
			}
			fi, _ := d.Info()
			h := hdrs[name]
			switch {
			case h == nil:
				t.Errorf("missing %s", name)
			case fs.FileMode(h.Mode).Perm() != fi.Mode().Perm():
				t.Errorf("%s mode %o, want %o", name, h.Mode, fi.Mode().Perm())
			case !h.ModTime.Equal(fi.ModTime().Round(time.Second)):
				t.Errorf("%s mtime %v, want %v", name, h.ModTime, fi.ModTime())
			}
			return nil
		})
	}

	snap := filepath.Join(t.TempDir(), "snap.db")
	if err := os.WriteFile(snap, dbBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", snap)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM sessions WHERE id = 'wal-only-row'").Scan(&n); err != nil || n != 1 {
		t.Errorf("WAL row in snapshot: %d, %v", n, err)
	}
}

func TestCreateEmptyHome(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "b.tar.gz")
	if err := Create(filepath.Join(dir, "none"), target, true); err != nil {
		t.Fatal(err)
	}
	if hdrs, _ := readArchive(t, target); len(hdrs) != 0 {
		t.Errorf("entries %v", hdrs)
	}
}

// A file a live session appends to during the backup is stored at its stat-ed size.
func TestCreateGrowingFile(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, "session-state", "s", "events.jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	stop, done := make(chan bool), make(chan bool)
	go func() {
		defer close(done)
		for range 1 << 12 { // up to 16 MiB more
			select {
			case <-stop:
				return
			default:
				f.Write(make([]byte, 4096))
			}
		}
	}()
	defer func() { close(stop); <-done; f.Close() }()
	for i := range 5 {
		target := filepath.Join(t.TempDir(), "b.tar.gz")
		if err := Create(home, target, true); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if hdrs, _ := readArchive(t, target); hdrs["session-state/s/events.jsonl"] == nil {
			t.Fatalf("run %d: entry missing", i)
		}
	}
}

// A failed backup leaves neither the target nor its temp file behind.
func TestCreateFailureLeavesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads mode-0 files")
	}
	home := t.TempDir()
	p := filepath.Join(home, "session-state", "s", "events.jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "b.tar.gz")
	if err := Create(home, target, true); err == nil {
		t.Fatal("want error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left behind: %v", entries)
	}
}

func TestCreateNoClobber(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	target := filepath.Join(dir, "b.tar.gz")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Create(home, target, false); err == nil || !strings.Contains(err.Error(), "backup already exists: "+target) {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "old" {
		t.Errorf("target changed: %q", b)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("left behind: %v", entries)
	}
	if err := Create(home, target, true); err != nil {
		t.Fatal(err)
	}
	readArchive(t, target)
}

func TestCopyPadded(t *testing.T) {
	for size, want := range map[int64]string{5: "abc\x00\x00", 2: "ab"} {
		var buf strings.Builder
		if err := copyPadded(&buf, strings.NewReader("abc"), size); err != nil || buf.String() != want {
			t.Errorf("size %d: %q, %v", size, buf.String(), err)
		}
	}
}

func TestListErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	os.WriteFile(file, nil, 0o600)
	for _, p := range []string{filepath.Join(dir, "missing"), file} {
		if got, err := List(p); got != nil || err != nil {
			t.Errorf("%s: %v, %v", p, got, err)
		}
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores dir modes")
	}
	locked := filepath.Join(dir, "p")
	os.Mkdir(locked, 0)
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	if _, err := List(filepath.Join(locked, "b")); err == nil {
		t.Error("want EACCES")
	}
}

// Without hard-link support the no-clobber publish falls back to O_EXCL + rename.
func TestCreateNoHardLinks(t *testing.T) {
	link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EPERM} }
	t.Cleanup(func() { link = os.Link })
	home, dir := t.TempDir(), t.TempDir()
	target := filepath.Join(dir, "b.tar.gz")
	if err := Create(home, target, false); err != nil {
		t.Fatal(err)
	}
	readArchive(t, target)
	if err := Create(home, target, false); err == nil || !strings.Contains(err.Error(), "backup already exists: "+target) {
		t.Fatalf("err %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("left behind: %v", entries)
	}
}
