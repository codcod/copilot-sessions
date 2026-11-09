package session

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A cwd that is neither absolute nor rooted (recorded on another OS) never matches, even
// when it would resolve under root from the process cwd (CS-010 review F1).
func TestUnderRelative(t *testing.T) {
	wd, _ := os.Getwd()
	paths := []string{"proj", ""}
	if runtime.GOOS != "windows" {
		paths = append(paths, `C:\Users\me\proj`) // absolute on Windows itself
	}
	for _, p := range paths {
		if Under(p, filepath.Dir(wd)) {
			t.Errorf("Under(%q, %q) = true", p, filepath.Dir(wd))
		}
	}
}

func TestRebase(t *testing.T) {
	root := t.TempDir()
	old, dst := filepath.Join(root, "a", "b"), filepath.Join(root, "x")
	for _, tt := range []struct {
		path, want string
		ok         bool
	}{
		{old, dst, true},
		{filepath.Join(old, "c", "d"), filepath.Join(dst, "c", "d"), true},
		{old + "c", "", false},
		{filepath.Join(root, "a"), "", false},
		{"b/c", "", false},
	} {
		got, ok := Rebase(tt.path, old, dst)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Rebase(%q) = %q, %v; want %q, %v", tt.path, got, ok, tt.want, tt.ok)
		}
	}
}
