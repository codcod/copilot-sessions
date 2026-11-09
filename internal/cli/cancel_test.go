package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

// cancelOn is a writer that cancels once the text written so far contains trigger.
type cancelOn struct {
	bytes.Buffer
	trigger string
	cancel  func()
}

func (w *cancelOn) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if strings.Contains(w.String(), w.trigger) {
		w.cancel()
	}
	return n, err
}

func sessionDirs(t *testing.T, fx *testfix.Fixture) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fx.Home, "session-state"))
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// Decision 10: clean -y cancelled after selecting (while printing the table) deletes nothing.
func TestCleanCancelledBeforeDelete(t *testing.T) {
	fx := testfix.Build(t)
	before := sessionDirs(t, fx)
	ctx, cancel := context.WithCancel(t.Context())
	env, _, errOut := testEnv(fx, nil, strings.NewReader(""), false)
	env.Stdout = &cancelOn{trigger: "UPDATED", cancel: cancel}
	if code := Execute(ctx, env, []string{"clean", "--all", "--include-ongoing", "-y"}, "test"); code != 130 || errOut.String() != "\nAborted.\n" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if got := sessionDirs(t, fx); got != before {
		t.Errorf("%d session dirs, want %d", got, before)
	}
}

// Decision 10: backup --keep 1 -y cancelled before rotation removes no old backup.
func TestRetentionCancelled(t *testing.T) {
	fx := testfix.Build(t)
	seedBackups(t, fx.BackupDir, false)
	before, _ := filepath.Glob(filepath.Join(fx.BackupDir, "*.tar.gz"))
	ctx, cancel := context.WithCancel(t.Context())
	env, _, errOut := testEnv(fx, nil, strings.NewReader(""), false)
	env.Stdout = &cancelOn{trigger: "Backup created", cancel: cancel}
	if code := Execute(ctx, env, []string{"backup", "--keep", "1", "-y"}, "test"); code != 130 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if after, _ := filepath.Glob(filepath.Join(fx.BackupDir, "*.tar.gz")); len(after) != len(before)+1 {
		t.Errorf("backups %v, want the %d old ones plus the new one", after, len(before))
	}
}

// restore -y cancelled before the swap leaves home unchanged and no tmp* dir.
func TestRestoreCancelled(t *testing.T) {
	fx := testfix.Build(t)
	file := filepath.Join(t.TempDir(), "b.tar.gz")
	mustRun(t, fx, "backup", file)
	mustRun(t, fx, "clean", "--all", "--include-ongoing", "-y")
	before := tree(t, fx.Home)
	ctx, cancel := context.WithCancel(t.Context())
	env, _, _ := testEnv(fx, nil, strings.NewReader(""), false)
	errOut := &cancelOn{trigger: "Close all running", cancel: cancel}
	env.Stderr = errOut
	if code := Execute(ctx, env, []string{"restore", file, "-y"}, "test"); code != 130 || !strings.HasSuffix(errOut.String(), "\nAborted.\n") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if got := tree(t, fx.Home); got != before {
		t.Errorf("home changed:\n%s\nwant:\n%s", got, before)
	}
}

// F2/decision 12: Ctrl-C at [y/N] aborts at once, even though stdin never sends a line.
func TestPromptCancelled(t *testing.T) {
	fx := testfix.Build(t)
	before := sessionDirs(t, fx)
	ctx, cancel := context.WithCancel(t.Context())
	stdin, w := io.Pipe()
	t.Cleanup(func() { w.Close() })
	env, _, _ := testEnv(fx, nil, stdin, true)
	// Cancel a little later, so readLine is already blocked in its select.
	errOut := &cancelOn{trigger: "[y/N] ", cancel: func() { time.AfterFunc(20*time.Millisecond, cancel) }}
	env.Stderr = errOut
	done := make(chan int)
	go func() { done <- Execute(ctx, env, []string{"clean", "--all"}, "test") }()
	select {
	case code := <-done:
		if code != 130 || !strings.HasSuffix(errOut.String(), "[y/N] \nAborted.\n") || strings.Contains(errOut.String(), "error:") {
			t.Fatalf("exit %d, stderr %q", code, errOut)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("prompt did not abort")
	}
	if got := sessionDirs(t, fx); got != before {
		t.Errorf("%d session dirs, want %d", got, before)
	}
}

// C1: an untraversable COPILOT_HOME is an error, not "No sessions match.".
// C3: an unreadable backup path is reported as such, not as "backup not found".
func TestPermissionErrors(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores permissions")
	}
	fx := testfix.Build(t)
	os.Chmod(fx.Home, 0)
	t.Cleanup(func() { os.Chmod(fx.Home, 0o700) })
	if out, errOut, code := run(fx, nil, "list"); code != 1 || !strings.Contains(errOut, "permission denied") {
		t.Errorf("list: exit %d, %q %q", code, out, errOut)
	}
	os.Chmod(fx.Home, 0o700)

	dir := filepath.Join(t.TempDir(), "locked")
	os.MkdirAll(dir, 0o700)
	file := filepath.Join(dir, "b.tar.gz")
	mustRun(t, fx, "backup", file)
	os.Chmod(dir, 0)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	_, errOut, code := run(fx, nil, "restore", file, "-y")
	if code != 1 || !strings.Contains(errOut, "error: backup "+file+": ") || !strings.Contains(errOut, "permission denied") {
		t.Errorf("restore: exit %d, %q", code, errOut)
	}
}

// F8: an unset DATE flag shows no "(default 0000-00-00)" in help.
func TestHelpNoZeroDate(t *testing.T) {
	fx := testfix.Build(t)
	if out, _, _ := run(fx, nil, "clean", "--help"); strings.Contains(out, "0000-00-00") || !strings.Contains(out, "--until DATE") {
		t.Errorf("help:\n%s", out)
	}
}

// P3: readLine works on an Env built without Execute.
func TestReadLineZeroEnv(t *testing.T) {
	if got, err := readLine(t.Context(), &Env{Stdin: strings.NewReader("y")}); got != "y" || err != nil {
		t.Errorf("got %q, %v", got, err)
	}
}

// --cwd ~ is expanded by the cli, now that filter no longer does it.
func TestCwdTilde(t *testing.T) {
	fx := testfix.Build(t)
	t.Setenv("HOME", "/home/u")
	if got := mustRun(t, fx, "list", "-q", "--cwd", "~/proj"); got != testfix.DBOnly+"\n"+testfix.EmptyStartShutdown+"\n"+testfix.Titled+"\n" {
		t.Errorf("got %q", got)
	}
}

// R1: after a cancel, an error that is not the cancel itself is still reported.
func TestCancelDoesNotHideErrors(t *testing.T) {
	fx := testfix.Build(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, errOut, code := runCtx(ctx, fx, nil, strings.NewReader(""), false, "restore", "-y", "/nonexistent.tar.gz"); code != 1 || errOut != "error: backup not found: /nonexistent.tar.gz\n" {
		t.Errorf("exit %d, %q", code, errOut)
	}

	// Ctrl-C during the swap, then a failing rename both ways: the error naming where the
	// old data was kept must surface, not read as a cancel.
	file := filepath.Join(t.TempDir(), "b.tar.gz")
	mustRun(t, fx, "backup", file)
	ctx, cancel = context.WithCancel(t.Context())
	sidebar := filepath.Join(fx.Home, "sidebar-sessions-state")
	rename := func(from, to string) error {
		cancel()
		if to == sidebar {
			return errors.New("boom")
		}
		return os.Rename(from, to)
	}
	env, _, _ := testEnv(fx, nil, strings.NewReader(""), false)
	err := restore(ctx, env, []string{file}, true, rename)
	if err == nil || errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), "restore failed and rollback incomplete; old data kept in ") {
		t.Errorf("err %v", err)
	}
}
