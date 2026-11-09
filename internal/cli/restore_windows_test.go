package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

// Windows refuses to rename a directory holding a file open without share-delete (os.Open
// opens that way), as Copilot's own files may be. sidebar-sessions-state is the last item
// swapItems moves, so the earlier moves really roll back and home is left as it was.
func TestRestoreOpenFile(t *testing.T) {
	fx := testfix.Build(t)
	mustRun(t, fx, "backup")
	// Home now differs from the backup in session-state, the first item swapped, so a swap
	// left in place shows in the tree comparison.
	if err := os.WriteFile(filepath.Join(fx.Home, "session-state", testfix.Titled, "events.jsonl"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(fx.Home, "sidebar-sessions-state", "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before := tree(t, fx.Home)

	env, _, _ := testEnv(fx, nil, strings.NewReader(""), false)
	err = restore(t.Context(), env, []string{"latest"}, true, os.Rename)
	if err == nil || !strings.HasPrefix(err.Error(), "restore failed, nothing changed") {
		t.Fatalf("err %v", err)
	}
	if got := tree(t, fx.Home); got != before {
		t.Errorf("home changed:\n%s\nwant:\n%s", got, before)
	}
}
