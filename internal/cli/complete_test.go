package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

// complete runs cobra's hidden __complete command and returns the candidates and directive.
func complete(t *testing.T, fx *testfix.Fixture, args ...string) ([]string, cobra.ShellCompDirective) {
	t.Helper()
	out, _, code := run(fx, nil, append([]string{"__complete"}, args...)...)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	last := lines[len(lines)-1]
	d, err := strconv.Atoi(strings.TrimPrefix(last, ":"))
	if code != 0 || !strings.HasPrefix(last, ":") || err != nil {
		t.Fatalf("__complete %q: code %d, output %q", args, code, out)
	}
	return lines[:len(lines)-1], cobra.ShellCompDirective(d)
}

func TestCompleteIDs(t *testing.T) {
	fx := testfix.Build(t)
	got, d := complete(t, fx, "list", "")
	if d != cobra.ShellCompDirectiveNoFileComp|cobra.ShellCompDirectiveKeepOrder || len(got) != 6 {
		t.Fatalf("got %q, directive %d", got, d)
	}
	ids := []string{testfix.Titled, testfix.EmptyNoEvents, testfix.EmptyStartShutdown, testfix.Stale, testfix.DBOnly, testfix.Ongoing}
	for _, c := range got {
		id, desc, ok := strings.Cut(c, "\t")
		status, _, _ := strings.Cut(desc, " ")
		if !ok || !slices.Contains(ids, id) || !slices.Contains([]string{"ongoing", "stale", "closed"}, status) {
			t.Errorf("bad candidate %q", c)
		}
	}
	for _, want := range []string{testfix.Titled + "\tclosed Titled session", testfix.EmptyNoEvents + "\tclosed -"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestCompleteFlags(t *testing.T) {
	fx := testfix.Build(t)
	got, d := complete(t, fx, "clean", "--status", "closed,")
	if !slices.Equal(got, []string{"closed,ongoing", "closed,stale"}) || d != cobra.ShellCompDirectiveNoSpace|cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("--status: %q, directive %d", got, d)
	}
	got, d = complete(t, fx, "list", "--until", "")
	if len(got) != 6 || got[2] != "7d\t7 days ago" || d != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("--until: %q, directive %d", got, d)
	}
	if _, d = complete(t, fx, "list", "--cwd", ""); d != cobra.ShellCompDirectiveFilterDirs {
		t.Errorf("--cwd directive %d", d)
	}
	for _, args := range [][]string{{"list", "--grep", ""}, {"backup", "--keep", ""}} {
		if got, d = complete(t, fx, args...); len(got) != 0 || d != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("%q: %q, directive %d", args, got, d)
		}
	}
	got, d = complete(t, fx, "backup", "")
	if !slices.Equal(got, []string{"tar.gz"}) || d != cobra.ShellCompDirectiveFilterFileExt {
		t.Errorf("backup FILE: %q, directive %d", got, d)
	}
}

func TestCompleteRestore(t *testing.T) {
	fx := testfix.Build(t)
	os.MkdirAll(fx.BackupDir, 0o755)
	older := filepath.Join(fx.BackupDir, "copilot-sessions-20261101-120000.tar.gz")
	newer := filepath.Join(fx.BackupDir, "copilot-sessions-20261105-120000.tar.gz")
	for _, p := range []string{older, newer} {
		os.WriteFile(p, nil, 0o644)
	}
	t.Chdir(t.TempDir())
	os.WriteFile("x.tar.gz", nil, 0o644)

	got, d := complete(t, fx, "restore", "")
	if !slices.Equal(got, []string{"latest", newer, older, "x.tar.gz"}) || d != cobra.ShellCompDirectiveNoFileComp|cobra.ShellCompDirectiveKeepOrder {
		t.Errorf("restore: %q, directive %d", got, d)
	}
	// A path with a "/" goes to the shell's file completion; cobra's scripts have already
	// expanded "~" by then (CS-005 R2-F1).
	for _, typed := range []string{"./x", "/home/u/", "sub/"} {
		if got, d = complete(t, fx, "restore", typed); !slices.Equal(got, []string{"tar.gz"}) || d != cobra.ShellCompDirectiveFilterFileExt {
			t.Errorf("restore %q: %q, directive %d", typed, got, d)
		}
	}
	if got, _ = complete(t, fx, "restore", "latest", ""); len(got) != 0 {
		t.Errorf("second arg: %q", got)
	}
}
