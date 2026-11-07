package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

// badMtime is the pinned mtime of copilot-sessions-bad.tar.gz.
var badMtime = time.Date(2026, 10, 30, 8, 15, 30, 0, time.UTC)

// seedBackups creates four timestamped backups, a non-matching file and, if bad, one
// whose name has no timestamp.
func seedBackups(t *testing.T, dir string, bad bool) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	names := []string{"other.tar.gz"}
	for _, d := range "1234" {
		names = append(names, "copilot-sessions-2026110"+string(d)+"-120000.tar.gz")
	}
	if bad {
		names = append(names, "copilot-sessions-bad.tar.gz")
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if bad {
		os.Chtimes(filepath.Join(dir, "copilot-sessions-bad.tar.gz"), badMtime, badMtime)
	}
}

func TestBackupCreate(t *testing.T) {
	fx := testfix.Build(t)
	target := filepath.Join(fx.BackupDir, "copilot-sessions-20261106-140000.tar.gz")
	out, errOut, code := run(fx, nil, "backup")
	if code != 0 || !regexp.MustCompile(`^Backup created: `+regexp.QuoteMeta(target)+` \(5 sessions, \d+(\.\d)?[BKM]\)\n$`).MatchString(out) {
		t.Fatalf("exit %d: %q %s", code, out, errOut)
	}
	file := filepath.Join(t.TempDir(), "x.tar.gz")
	if out, _, code := run(fx, nil, "backup", "-q", file); code != 0 || out != file+"\n" || !exists(file) {
		t.Errorf("quiet: %d %q", code, out)
	}
}

func TestBackupRetention(t *testing.T) {
	fx := testfix.Build(t)
	seedBackups(t, fx.BackupDir, false)
	p := func(d string) string {
		return filepath.Join(fx.BackupDir, "copilot-sessions-2026110"+d+"-120000.tar.gz")
	}

	if out, _, code := run(fx, nil, "backup", "--keep", "5", "--no-create"); code != 0 || out != "Nothing to remove: at most 5 backups in "+fx.BackupDir+".\n" {
		t.Errorf("keep 5: %d %q", code, out)
	}
	if _, errOut, code := run(fx, nil, "backup", "--keep", "1", "--no-create"); code != 1 || errOut != "error: confirmation required; rerun with --yes\n" {
		t.Errorf("no TTY: %d %q", code, errOut)
	}
	out, errOut, code := run(fx, nil, "backup", "--keep", "2", "--no-create", "-y")
	want := "Backups to remove (keeping the newest 2):\n" +
		"   #  CREATED             SIZE  FILE\n" +
		"   1  2026-11-02 12:00      0B  " + p("2") + "\n" +
		"   2  2026-11-01 12:00      0B  " + p("1") + "\n" +
		"Deleted 2 old backups.\n"
	if code != 0 || out != want {
		t.Errorf("keep 2: exit %d %s\ngot:\n%s\nwant:\n%s", code, errOut, out, want)
	}
	if exists(p("1")) || exists(p("2")) || !exists(p("3")) || !exists(p("4")) || !exists(filepath.Join(fx.BackupDir, "other.tar.gz")) {
		t.Error("wrong files deleted")
	}
}

func TestBackupList(t *testing.T) {
	fx := testfix.Build(t)
	if out, _, _ := run(fx, nil, "backup", "-l"); out != "No backups in "+fx.BackupDir+"\n" {
		t.Errorf("empty: %q", out)
	}
	if out, _, _ := run(fx, nil, "backup", "-l", "--json"); out != "[]\n" {
		t.Errorf("empty json: %q", out)
	}
	seedBackups(t, fx.BackupDir, true)
	os.WriteFile(filepath.Join(fx.BackupDir, "copilot-sessions-20261104-120000.tar.gz"), []byte("abc"), 0o644)
	out, _, code := run(fx, nil, "backup", "--list", "--json")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || code != 0 {
		t.Fatalf("%d %v %s", code, err, out)
	}
	var got []string
	for _, r := range rows {
		got = append(got, filepath.Base(r["file"].(string))+" "+r["created"].(string)+" "+string(must(json.Marshal(r["size"]))))
	}
	want := []string{
		"copilot-sessions-bad.tar.gz 2026-10-30T10:15:30 0", // mtime in XST (UTC+2)
		"copilot-sessions-20261104-120000.tar.gz 2026-11-04T12:00:00 3",
		"copilot-sessions-20261103-120000.tar.gz 2026-11-03T12:00:00 0",
		"copilot-sessions-20261102-120000.tar.gz 2026-11-02T12:00:00 0",
		"copilot-sessions-20261101-120000.tar.gz 2026-11-01T12:00:00 0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s", strings.Join(got, "\n"))
	}
}

func must(b []byte, _ error) []byte { return b }

func TestBackupValidation(t *testing.T) {
	fx := testfix.Build(t)
	for _, tt := range []struct {
		args []string
		msg  string
	}{
		{[]string{"-l", "f.tar.gz"}, "--list cannot be combined with FILE, --keep or --no-create"},
		{[]string{"-l", "-k", "1"}, "--list cannot be combined with FILE, --keep or --no-create"},
		{[]string{"-l", "--no-create"}, "--list cannot be combined with FILE, --keep or --no-create"},
		{[]string{"--json"}, "--json only works with --list"},
		{[]string{"--no-create"}, "--no-create requires --keep N"},
		{[]string{"-k", "0"}, "--keep must be at least 1"},
	} {
		if _, errOut, code := run(fx, nil, append([]string{"backup"}, tt.args...)...); code != 1 || errOut != "error: "+tt.msg+"\n" {
			t.Errorf("%v: %d %q", tt.args, code, errOut)
		}
	}
	if _, _, code := run(fx, nil, "backup", "a", "b"); code != 2 {
		t.Errorf("two args: exit %d", code)
	}
	if exists(fx.BackupDir) {
		t.Error("validation created a backup")
	}
}

// Two default-named backups in the same second: the second fails, the first survives.
func TestBackupSameSecond(t *testing.T) {
	fx := testfix.Build(t)
	mustRun(t, fx, "backup")
	target := filepath.Join(fx.BackupDir, "copilot-sessions-20261106-140000.tar.gz")
	if _, errOut, code := run(fx, nil, "backup"); code != 1 || errOut != "error: backup already exists: "+target+"\n" {
		t.Fatalf("exit %d: %q", code, errOut)
	}
	if out := mustRun(t, fx, "backup", "--list"); strings.Count(out, "copilot-sessions-") != 1 {
		t.Errorf("list:\n%s", out)
	}
	if l := listing(fx.BackupDir); l != filepath.Base(target) {
		t.Errorf("backup dir: %s", l)
	}
	if out := mustRun(t, fx, "restore", target, "-y"); !strings.Contains(out, "Restored ") {
		t.Errorf("restore:\n%s", out)
	}
}
