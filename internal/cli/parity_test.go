package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codcod/copilot-sessions/internal/session"
	"github.com/codcod/copilot-sessions/internal/testfix"
)

// TestParity compares list and clean -n output with the Python reference script on the same fixture.
// It skips unless python3 is on PATH.
func TestParity(t *testing.T) {
	fx := testfix.Build(t)
	py := pythonRunner(t, fx)
	ancestors := session.DefaultAncestors()

	goRun := func(args ...string) string {
		t.Helper()
		out, errOut, code := run(fx, ancestors, args...)
		if code != 0 {
			t.Fatalf("go %v: exit %d: %s", args, code, errOut)
		}
		return out
	}

	filters := [][]string{
		nil,
		{"aaaa1"},
		{"--until", "2026-11-01"},
		{"--since", "2026-11-04"},
		{"--status", "stale,closed"},
		{"--cwd", "/home/u/proj"},
		{"--grep", "SESSION"},
		{"--empty"},
		{"-s", "closed", "-c", "/home/u", "-S", "2026-10-02"},
		{"--grep", "no-such-name"},
	}
	for _, f := range filters {
		args := append([]string{"list"}, f...)
		if got, want := withoutSize(goRun(args...)), withoutSize(py(args...)); got != want {
			t.Errorf("%v:\ngo:\n%s\npython:\n%s", args, got, want)
		}
		cargs := append([]string{"clean", "-n"}, f...)
		if f == nil {
			cargs = append(cargs, "--all")
		}
		if got, want := withoutSize(goRun(cargs...)), withoutSize(py(cargs...)); got != want {
			t.Errorf("%v:\ngo:\n%s\npython:\n%s", cargs, got, want)
		}
		args = append(args, "-q")
		if got, want := goRun(args...), py(args...); got != want {
			t.Errorf("%v:\ngo:\n%s\npython:\n%s", args, got, want)
		}
		args = append(args, "--json")
		if got, want := normJSON(t, goRun(args...)), normJSON(t, py(args...)); !reflect.DeepEqual(got, want) {
			t.Errorf("%v:\ngo:     %v\npython: %v", args, got, want)
		}
	}

	// backup --list (spec §13), and Python can read a Go-made archive.
	seedBackups(t, fx.BackupDir, true)
	if got, want := goRun("backup", "--list"), py("backup", "--list"); got != want {
		t.Errorf("backup --list:\ngo:\n%s\npython:\n%s", got, want)
	}
	archive := filepath.Join(t.TempDir(), "go.tar.gz")
	goRun("backup", "-q", archive)
	if out, err := exec.Command("python3", "-I", "-c", "import tarfile,sys; print(len(tarfile.open(sys.argv[1]).getnames()))", archive).CombinedOutput(); err != nil || strings.TrimSpace(string(out)) == "0" {
		t.Errorf("python read archive: %v %s", err, out)
	}
}

// pythonRunner returns a runner for the reference script against fx, with -I and the
// test zone. It skips the test unless python3 3.12+ (tarfile filter="data") and the
// script are available.
func pythonRunner(t *testing.T, fx *testfix.Fixture) func(args ...string) string {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	if exec.Command(python, "-c", "import sys; sys.exit(sys.version_info < (3, 12))").Run() != nil {
		t.Skip("python3 older than 3.12")
	}
	script, _ := filepath.Abs("../../development/copilot-sessions.sh")
	if _, err := os.Stat(script); err != nil {
		t.Skip("reference script missing")
	}
	return func(args ...string) string {
		t.Helper()
		cmd := exec.Command(python, append([]string{"-I", script}, args...)...)
		cmd.Env = append(os.Environ(), "TZ=XST-2", "COPILOT_HOME="+fx.Home, "COPILOT_BACKUP_DIR="+fx.BackupDir)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("python %v: %v", args, err)
		}
		return string(out)
	}
}

// withoutSize drops the SIZE column and the summary's total size.
func withoutSize(out string) string {
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		switch {
		case strings.Contains(l, " sessions (") || strings.Contains(l, " sessions selected ("):
			lines[i] = l[:strings.LastIndex(l, ", ")]
		case len(l) >= 44 && strings.HasPrefix(l, "  "):
			lines[i] = l[:36] + l[44:]
		}
	}
	return strings.Join(lines, "\n")
}

// normJSON parses the JSON output and replaces updated_at with its instant.
func normJSON(t *testing.T, out string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, r := range rows {
		if s, ok := r["updated_at"].(string); ok {
			ts, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				t.Fatalf("updated_at %q: %v", s, err)
			}
			r["updated_at"] = ts.UnixNano()
		}
	}
	return rows
}
