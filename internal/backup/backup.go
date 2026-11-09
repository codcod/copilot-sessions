// Package backup writes and lists the .tar.gz backups of a Copilot home (spec §6.3).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codcod/copilot-sessions/internal/session"
)

// Items returns the top-level archive entries, each stored only if it exists.
func Items() []string {
	return []string{session.StateDir, session.DBFile, session.OpenState, session.SidebarDir}
}

// Pattern matches the backup files that --list and --keep consider.
const Pattern = "copilot-sessions-*.tar.gz"

const locks = session.StateDir + "/.session-operation-locks"

// Create writes a gzip tar of home to target with mode 0600. The DB entry is a
// VACUUM INTO snapshot, so committed WAL content is included and -wal/-shm never are.
// The archive is built in a hidden temp file next to target and moved into place only
// when complete, so a failure leaves nothing behind. With overwrite false an existing
// target is never replaced ("backup already exists"). A cancelled ctx stops the walk
// and leaves nothing behind.
func Create(ctx context.Context, home, target string, overwrite bool) (err error) {
	tmp, err := os.MkdirTemp("", "copilot-sessions-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	snapshot := filepath.Join(tmp, session.DBFile)
	if err := snapshotDB(ctx, home, snapshot); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".copilot-sessions-*.tmp")
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close() // already failing; the temp file is removed next
		}
		if err != nil {
			_ = os.Remove(f.Name()) // best effort: a leftover never matches Pattern
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, item := range Items() {
		src := filepath.Join(home, item)
		if item == session.DBFile {
			src = snapshot
		}
		if _, err := os.Lstat(src); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := addTree(ctx, tw, src, item); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	closed = true
	if err := f.Close(); err != nil {
		return err
	}
	// Last chance to cancel: publishing may replace an explicit FILE.
	if err := ctx.Err(); err != nil {
		return err
	}
	if overwrite {
		return os.Rename(f.Name(), target)
	}
	if err = moveNew(f.Name(), target, os.Link); errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("backup already exists: %s", target)
	}
	return err
}

// moveNew moves tmp to target only if target does not exist yet (fs.ErrExist otherwise).
// link is os.Link; tests pass a failing one to simulate a filesystem without hard links.
func moveNew(tmp, target string, link func(string, string) error) error {
	err := link(tmp, target)
	if err == nil {
		_ = os.Remove(tmp) // a leftover never matches Pattern
		return nil
	}
	if errors.Is(err, fs.ErrExist) {
		return err
	}
	// No hard links here (exFAT, some network mounts): claim the name with O_EXCL, then
	// rename over it. The empty placeholder is visible to --list for one rename.
	c, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_ = c.Close() // empty placeholder, nothing written to lose
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(target) // best effort: drop the placeholder
		return err
	}
	return nil
}

func snapshotDB(ctx context.Context, home, snapshot string) error {
	db, err := session.OpenDB(home)
	if errors.Is(err, session.ErrNoDB) {
		return nil
	}
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", snapshot); err != nil {
		return fmt.Errorf("snapshot %s: %w", session.DBFile, err)
	}
	return nil
}

// addTree adds root as name, recursively, the way Python's tarfile.add does:
// symlinks are stored, never followed; types tar cannot hold are skipped.
func addTree(ctx context.Context, tw *tar.Writer, root, name string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return skipVanished(err)
		}
		rel, _ := filepath.Rel(root, p)
		entry := name
		if rel != "." {
			entry += "/" + filepath.ToSlash(rel)
		}
		if entry == locks || strings.HasPrefix(entry, locks+"/") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return skipVanished(err)
		}
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return skipVanished(err)
			}
		}
		var f *os.File
		if info.Mode().IsRegular() {
			// Open before writing the header, so a vanished file leaves no header without data.
			if f, err = os.Open(p); err != nil {
				return skipVanished(err)
			}
			defer f.Close()
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return nil // socket or similar: tarfile skips these too
		}
		hdr.Name = entry
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if f == nil {
			return nil
		}
		// Write exactly the stat-ed size: a live session may append meanwhile (cut, as
		// tarfile tolerates) or truncate (zero-padded, as GNU tar does).
		return copyPadded(tw, f, hdr.Size)
	})
}

// skipVanished drops an entry deleted while the walk ran; any other error stands.
func skipVanished(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// copyPadded copies exactly size bytes from r, padding with zeros if r ends early.
func copyPadded(w io.Writer, r io.Reader, size int64) error {
	n, err := io.CopyN(w, r, size)
	if err == io.EOF {
		_, err = io.CopyN(w, zeros{}, size-n)
	}
	return err
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// List returns the backup files in dir, newest (highest name) first. A missing dir, or
// a path that is not a dir, has none; any other error (EACCES, I/O) is returned.
func List(dir string) ([]string, error) {
	fi, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !fi.IsDir() {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if ok, _ := filepath.Match(Pattern, e.Name()); ok && !e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	slices.Sort(out)
	slices.Reverse(out)
	return out, nil
}

// Created is the local time in the file name, or the file mtime if the name has none.
func Created(path string) time.Time {
	stamp := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "copilot-sessions-"), ".tar.gz")
	if t, err := time.ParseInLocation("20060102-150405", stamp, time.Local); err == nil {
		return t
	}
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime().Local()
}
