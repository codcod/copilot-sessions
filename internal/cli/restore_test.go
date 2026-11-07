package cli

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

// mustRun runs the CLI against fx and fails the test on a non-zero exit.
func mustRun(t *testing.T, fx *testfix.Fixture, args ...string) string {
	t.Helper()
	out, errOut, code := run(fx, nil, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d: %s", args, code, errOut)
	}
	return out
}

// listing is the sorted names in dir, to spot strays.
func listing(dir string) string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return strings.Join(names, " ")
}

// Case 11: backup → clean --all → restore gives the same sessions and DB rows back.
func TestRestoreRoundTrip(t *testing.T) {
	fx := testfix.Build(t)
	ids := mustRun(t, fx, "list", "-q")
	// Backups leave out the operation locks by design (spec §6.3).
	before := strings.Replace(snapshot(t, fx), ".session-operation-locks\n", "", 1)
	file := filepath.Join(t.TempDir(), "b.tar.gz")
	mustRun(t, fx, "backup", file)
	mustRun(t, fx, "clean", "--all", "--include-ongoing", "-y")
	db := filepath.Join(fx.Home, "session-store.db")
	os.WriteFile(db+"-wal", []byte("junk"), 0o644)
	os.WriteFile(db+"-shm", []byte("junk"), 0o644)

	out := mustRun(t, fx, "restore", file, "-y")
	if !strings.HasSuffix(out, "Restored 6 sessions from "+file+"\n") {
		t.Errorf("output:\n%s", out)
	}
	for _, s := range []string{"-wal", "-shm"} {
		if b, err := os.ReadFile(db + s); err == nil && string(b) == "junk" {
			t.Errorf("%s survived", s)
		}
	}
	if got := mustRun(t, fx, "list", "-q"); got != ids {
		t.Errorf("ids:\n%s\nwant:\n%s", got, ids)
	}
	if got := snapshot(t, fx); got != before {
		t.Errorf("snapshot:\n%s\nwant:\n%s", got, before)
	}
	if l := listing(fx.Home); strings.Contains(l, "tmp") {
		t.Errorf("temp dir left behind: %s", l)
	}
}

func TestRestorePick(t *testing.T) {
	fx := testfix.Build(t)
	newest := filepath.Join(fx.BackupDir, "copilot-sessions-20261102-120000.tar.gz")
	if _, errOut, code := run(fx, nil, "restore"); code != 1 || errOut != "error: no backups in "+fx.BackupDir+"\n" {
		t.Errorf("empty dir: %d %q", code, errOut)
	}
	if _, errOut, code := run(fx, nil, "restore", "latest"); code != 1 || errOut != "error: no backups in "+fx.BackupDir+"\n" {
		t.Errorf("latest, empty dir: %d %q", code, errOut)
	}
	mustRun(t, fx, "backup", newest)
	testfix.Archive(t, filepath.Join(fx.BackupDir, "copilot-sessions-20261101-120000.tar.gz"),
		tar.Header{Typeflag: tar.TypeDir, Name: "session-state/", Mode: 0o755})

	if out := mustRun(t, fx, "restore", "latest", "-y"); !strings.HasSuffix(out, "Restored 6 sessions from "+newest+"\n") {
		t.Errorf("latest:\n%s", out)
	}
	if _, errOut, code := run(fx, nil, "restore"); code != 1 || errOut != "error: no backup given; pass FILE or 'latest'\n" {
		t.Errorf("no TTY: %d %q", code, errOut)
	}
	out, errOut, code := runIn(fx, nil, "\ny\n", true, "restore")
	if code != 0 || !strings.Contains(out, "Restore which backup? [1] WARNING") || !strings.HasSuffix(out, "Restored 6 sessions from "+newest+"\n") {
		t.Errorf("pick 1: %d %s\n%s", code, errOut, out)
	}
	for _, bad := range []string{"9", "0", "x", "+1"} {
		if _, errOut, code := runIn(fx, nil, bad+"\n", true, "restore"); code != 1 || errOut != "error: invalid choice '"+bad+"'\n" {
			t.Errorf("pick %s: %d %q", bad, code, errOut)
		}
	}
}

func TestRestoreErrors(t *testing.T) {
	fx := testfix.Build(t)
	before := snapshot(t, fx)
	missing := filepath.Join(t.TempDir(), "nope.tar.gz")
	if _, errOut, code := run(fx, nil, "restore", missing); code != 1 || errOut != "error: backup not found: "+missing+"\n" {
		t.Errorf("missing: %d %q", code, errOut)
	}
	foo := filepath.Join(t.TempDir(), "foo.tar.gz")
	testfix.Archive(t, foo, tar.Header{Typeflag: tar.TypeReg, Name: "foo", Mode: 0o644})
	if _, errOut, code := run(fx, nil, "restore", foo); code != 1 || errOut != "error: "+foo+" does not look like a copilot-sessions backup\n" {
		t.Errorf("not a backup: %d %q", code, errOut)
	}
	file := filepath.Join(t.TempDir(), "b.tar.gz")
	mustRun(t, fx, "backup", file)
	out, _, code := runIn(fx, nil, "n\n", true, "restore", file)
	want := "WARNING: restore REPLACES all current Copilot sessions.\n" +
		"  Backup:  " + file + " (6 sessions)\n" +
		"  Current: " + fx.Home + " (5 sessions will be deleted)\n" +
		"  Close all running Copilot CLI sessions first; they may overwrite restored data.\n" +
		"Continue? [y/N] Aborted.\n"
	if code != 0 || out != want {
		t.Errorf("abort: %d\ngot:\n%s\nwant:\n%s", code, out, want)
	}
	if got := snapshot(t, fx); got != before {
		t.Errorf("changed:\n%s\nwant:\n%s", got, before)
	}
}

func TestRestoreRejectsUnsafeArchives(t *testing.T) {
	dot := tar.Header{Typeflag: tar.TypeSymlink, Name: "session-state/s", Linkname: "."}
	cases := map[string][]tar.Header{
		"dotdot":       {{Typeflag: tar.TypeReg, Name: "session-state/../../evil", Mode: 0o644}},
		"absolute":     {{Typeflag: tar.TypeReg, Name: "/abs/evil", Mode: 0o644}},
		"symlink up":   {{Typeflag: tar.TypeSymlink, Name: "session-state/s1/l", Linkname: "../x"}},
		"symlink abs":  {{Typeflag: tar.TypeSymlink, Name: "session-state/s1/l", Linkname: "/etc"}},
		"symlink loop": {dot, {Typeflag: tar.TypeSymlink, Name: "session-state/evil", Linkname: "s/s/../../x"}},
		"top symlink":  {{Typeflag: tar.TypeSymlink, Name: "session-store.db", Linkname: "session-state/s1/workspace.yaml"}},
		"top dot":      {{Typeflag: tar.TypeSymlink, Name: "./session-store.db", Linkname: "x"}},
		"top updown":   {{Typeflag: tar.TypeSymlink, Name: "session-state/../session-store.db", Linkname: "x"}},
		"top hardlink": { // x exists at the top, so the hard-linked symlink resolves there
			{Typeflag: tar.TypeReg, Name: "x", Mode: 0o644},
			{Typeflag: tar.TypeSymlink, Name: "session-state/l", Linkname: "x"},
			{Typeflag: tar.TypeLink, Name: "session-store.db", Linkname: "session-state/l"},
		},
		"hardlink up":  {{Typeflag: tar.TypeLink, Name: "session-state/s1/h", Linkname: "../x"}},
		"fifo":         {{Typeflag: tar.TypeFifo, Name: "session-state/s1/p", Mode: 0o644}},
		"char device":  {{Typeflag: tar.TypeChar, Name: "session-state/s1/c", Mode: 0o644, Devmajor: 1, Devminor: 3}},
		"block device": {{Typeflag: tar.TypeBlock, Name: "session-state/s1/b", Mode: 0o644, Devmajor: 8}},
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			fx := testfix.Build(t)
			parent := filepath.Dir(fx.Home)
			before, ids, outside, home := snapshot(t, fx), mustRun(t, fx, "list", "-q"), listing(parent), listing(fx.Home)
			file := filepath.Join(t.TempDir(), "bad.tar.gz")
			testfix.Archive(t, file, append([]tar.Header{
				{Typeflag: tar.TypeDir, Name: "session-state/", Mode: 0o755},
				{Typeflag: tar.TypeReg, Name: "session-state/s1/workspace.yaml", Mode: 0o644},
			}, bad...)...)
			if _, errOut, code := run(fx, nil, "restore", file, "-y"); code != 1 || errOut == "" {
				t.Errorf("accepted: %d %q", code, errOut)
			}
			if got := snapshot(t, fx); got != before {
				t.Errorf("fixture changed:\n%s\nwant:\n%s", got, before)
			}
			if got := mustRun(t, fx, "list", "-q"); got != ids {
				t.Errorf("ids changed:\n%s", got)
			}
			if got := listing(parent); got != outside {
				t.Errorf("stray outside home: %s, want %s", got, outside)
			}
			if got := listing(fx.Home); got != home {
				t.Errorf("stray in home: %s, want %s", got, home)
			}
		})
	}
}

func TestRestoreFreshHome(t *testing.T) {
	fx := testfix.Build(t)
	file := filepath.Join(t.TempDir(), "b.tar.gz")
	mustRun(t, fx, "backup", file)
	fresh := &testfix.Fixture{Home: filepath.Join(t.TempDir(), "new", "copilot"), BackupDir: fx.BackupDir}
	if out := mustRun(t, fresh, "restore", file, "-y"); !strings.HasSuffix(out, "Restored 6 sessions from "+file+"\n") {
		t.Errorf("output:\n%s", out)
	}
	if got, want := mustRun(t, fresh, "list", "-q"), mustRun(t, fx, "list", "-q"); got != want {
		t.Errorf("ids:\n%s\nwant:\n%s", got, want)
	}
}

// Case 12: each tool restores the other's archive.
func TestRestoreCompat(t *testing.T) {
	fx := testfix.Build(t)
	py := pythonRunner(t, fx)
	ids := mustRun(t, fx, "list", "-q")
	dir := t.TempDir()

	f1 := filepath.Join(dir, "py.tar.gz")
	py("backup", f1)
	mustRun(t, fx, "clean", "--all", "--include-ongoing", "-y")
	mustRun(t, fx, "restore", f1, "-y")
	if got := mustRun(t, fx, "list", "-q"); got != ids {
		t.Errorf("go restore of python archive:\n%s\nwant:\n%s", got, ids)
	}

	f2 := filepath.Join(dir, "go.tar.gz")
	mustRun(t, fx, "backup", f2)
	mustRun(t, fx, "clean", "--all", "--include-ongoing", "-y")
	py("restore", f2, "-y")
	if got := py("list", "-q"); got != ids {
		t.Errorf("python restore of go archive:\n%s\nwant:\n%s", got, ids)
	}
}
