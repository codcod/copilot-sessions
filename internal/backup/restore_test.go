package backup

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

func TestExtractModesAndLinks(t *testing.T) {
	mtime := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	reg := func(name string, mode int64) tar.Header {
		return tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: mode, ModTime: mtime}
	}
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	testfix.Archive(t, archive,
		tar.Header{Typeflag: tar.TypeDir, Name: "session-state/", Mode: 0o2555, ModTime: mtime},
		reg("session-state/a/sub/f", 0o644),
		tar.Header{Typeflag: tar.TypeSymlink, Name: "session-state/a/l", Linkname: "sub/f"},
		tar.Header{Typeflag: tar.TypeLink, Name: "session-state/a/h", Linkname: "session-state/a/sub/f", Mode: 0o644, ModTime: mtime},
		reg("suid", 0o4755),
		reg("wide", 0o666),
		reg("ro", 0o400),
	)
	isBackup, n, err := Inspect(archive)
	if err != nil || !isBackup || n != 1 {
		t.Fatalf("inspect: %v %v %d", err, isBackup, n)
	}

	dest := t.TempDir()
	if err := Extract(archive, dest); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{"suid": 0o755, "wide": 0o644, "ro": 0o600} {
		fi, err := os.Stat(filepath.Join(dest, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode() != want {
			t.Errorf("%s: mode %v, want %v", name, fi.Mode(), want)
		}
		if !fi.ModTime().Equal(mtime) {
			t.Errorf("%s: mtime %v, want %v", name, fi.ModTime(), mtime)
		}
	}
	// Dir modes are ignored (filter="data"): a read-only archive dir stays owner-writable.
	if fi, err := os.Stat(filepath.Join(dest, "session-state")); err != nil || fi.Mode()&(0o700|os.ModeSetgid) != 0o700 || !fi.ModTime().Equal(mtime) {
		t.Errorf("session-state: %v %v", fi.Mode(), err)
	}
	for _, name := range []string{"session-state/a/l", "session-state/a/h"} {
		if b, err := os.ReadFile(filepath.Join(dest, name)); err != nil || string(b) != "data\n" {
			t.Errorf("%s: %q %v", name, b, err)
		}
	}
}
