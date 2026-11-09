package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Under reports whether path is root or below it; /a/b is not below /a/bc. root must be
// absolute and clean. A path that is neither absolute nor rooted (a cwd recorded on another
// OS, like C:\x read on Linux) is never under root. A rooted Windows path (\a\b) goes through
// Abs like root, to get the drive letter. Windows paths compare case-insensitively.
func Under(path, root string) bool {
	if path == "" || !filepath.IsAbs(path) && !os.IsPathSeparator(path[0]) {
		return false
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	sep := string(filepath.Separator)
	prefix := strings.TrimRight(root, sep) + sep
	if runtime.GOOS == "windows" {
		path, root, prefix = strings.ToLower(path), strings.ToLower(root), strings.ToLower(prefix)
	}
	return path == root || strings.HasPrefix(path, prefix)
}

// Rebase returns path with its oldRoot prefix replaced by newRoot, when path is Under
// oldRoot. Both roots must be absolute and clean.
func Rebase(path, oldRoot, newRoot string) (string, bool) {
	if !Under(path, oldRoot) {
		return "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	// ponytail: cuts at len(oldRoot) bytes; on Windows a case fold that changes byte length
	// (non-ASCII) would cut wrong. Compare runes if such paths show up.
	rest := abs[len(strings.TrimRight(oldRoot, string(filepath.Separator))):]
	return filepath.Join(newRoot, rest), true
}
