package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codcod/copilot-sessions/internal/session"
)

// Inspect reads the archive headers once. isBackup is true if it holds session-state
// or session-store.db; sessions counts the distinct non-hidden names directly below
// session-state/ (spec §6.4).
func Inspect(ctx context.Context, path string) (isBackup bool, sessions int, err error) {
	names := map[string]bool{}
	err = walk(ctx, path, func(hdr *tar.Header, _ *tar.Reader) error {
		name := strings.TrimSuffix(hdr.Name, "/")
		if name == session.StateDir || name == session.DBFile {
			isBackup = true
		}
		if rest, ok := strings.CutPrefix(name, session.StateDir+"/"); ok {
			if first, _, _ := strings.Cut(rest, "/"); first != "" && !strings.HasPrefix(first, ".") {
				names[first] = true
			}
		}
		return nil
	})
	return isBackup, len(names), err
}

// Extract unpacks archive into dest the way Python's tarfile filter="data" does: it
// rejects the whole archive on the first unsafe member, strips setuid/setgid and
// group/other write from files, ignores directory modes, and never changes ownership.
// Stricter than Python, symlinks must point downward and no top-level item may end up a
// symlink. Every write goes through os.Root, so nothing can land outside dest even if a
// check were missed.
func Extract(ctx context.Context, archive, dest string) error {
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	type dirAttr struct {
		name  string
		mtime time.Time
	}
	var dirs []dirAttr
	err = walk(ctx, archive, func(hdr *tar.Header, tr *tar.Reader) error {
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			return nil
		}
		name := strings.TrimSuffix(hdr.Name, "/")
		if filepath.IsAbs(hdr.Name) || !filepath.IsLocal(name) {
			return fmt.Errorf("%s: unsafe path", hdr.Name)
		}
		mode := os.FileMode(hdr.Mode) & 0o777 &^ 0o022
		if hdr.Typeflag == tar.TypeDir {
			// The archive's dir mode is ignored, as filter="data" does: a dir without owner
			// write would make the rename into COPILOT_HOME fail after the old item is gone.
			if err := root.MkdirAll(name, 0o777); err != nil {
				return err
			}
			dirs = append(dirs, dirAttr{name, hdr.ModTime})
			return nil
		}
		if err := root.MkdirAll(filepath.Dir(name), 0o777); err != nil {
			return err
		}
		_ = root.Remove(name) // replace an earlier member of the same name, as tarfile does; usually absent
		switch hdr.Typeflag {
		case tar.TypeSymlink:
			// ponytail: downward-only symlinks; resolve against the extracted tree if a real backup ever needs ../ links
			// IsLocal alone is lexical: "s/../.." passes, and through an "s -> ." link it climbs out.
			if !filepath.IsLocal(hdr.Linkname) || slices.Contains(strings.Split(hdr.Linkname, "/"), "..") {
				return fmt.Errorf("%s: symlink to %s points outside the archive", hdr.Name, hdr.Linkname)
			}
			return root.Symlink(hdr.Linkname, name)
		case tar.TypeLink:
			if !filepath.IsLocal(hdr.Linkname) {
				return fmt.Errorf("%s: hard link to %s points outside the archive", hdr.Name, hdr.Linkname)
			}
			if err := root.Link(hdr.Linkname, name); err != nil {
				return err
			}
		case tar.TypeReg:
			f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: unsupported file type", hdr.Name)
		}
		mode |= 0o600
		if mode&0o100 == 0 {
			mode &^= 0o011
		}
		if err := root.Chmod(name, mode); err != nil {
			return err
		}
		return root.Chtimes(name, hdr.ModTime, hdr.ModTime)
	})
	if err != nil {
		return err
	}
	// A top-level item is renamed into COPILOT_HOME, where a link would resolve against
	// siblings outside the restored tree. Checked on the result, so ./x, a/../x and a hard
	// link to a symlink are all caught.
	for _, item := range Items() {
		if fi, err := root.Lstat(item); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: top-level item is a symlink", item)
		}
	}
	// Directory mtimes last, deepest first, as tarfile.extractall does.
	slices.SortFunc(dirs, func(a, b dirAttr) int { return strings.Compare(b.name, a.name) })
	for _, d := range dirs {
		if err := root.Chtimes(d.name, d.mtime, d.mtime); err != nil {
			return err
		}
	}
	return nil
}

// walk calls fn for every header of the gzip tar at path, stopping once ctx is done.
func walk(ctx context.Context, path string, fn func(*tar.Header, *tar.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	tr := tar.NewReader(gz)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := fn(hdr, tr); err != nil {
			return err
		}
	}
}
