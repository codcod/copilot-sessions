package backup

import (
	"archive/tar"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

// Drive letters, backslash traversal and reserved device names never land outside dest.
func TestExtractUnsafeWindowsPaths(t *testing.T) {
	for _, name := range []string{"C:/x", "C:x", `..\x`, "CON", "NUL", "s/AUX"} {
		archive := filepath.Join(t.TempDir(), "a.tar.gz")
		testfix.Archive(t, archive, tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644})
		if err := Extract(t.Context(), archive, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unsafe path") {
			t.Errorf("%s: err %v", name, err)
		}
	}
	// A backslash ".." in a symlink target climbs out through an "s -> ." link like "/" does.
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	testfix.Archive(t, archive,
		tar.Header{Typeflag: tar.TypeSymlink, Name: "d/s", Linkname: "."},
		tar.Header{Typeflag: tar.TypeSymlink, Name: "d/x", Linkname: `s\..\y`},
	)
	if err := Extract(t.Context(), archive, t.TempDir()); err == nil || !strings.Contains(err.Error(), "points outside") {
		t.Errorf("backslash symlink: err %v", err)
	}
}
