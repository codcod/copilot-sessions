// Package backup writes and lists the .tar.gz backups of a Copilot home (spec §6.3).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codcod/copilot-sessions/internal/session"
)

// Items are the top-level archive entries, each stored only if it exists.
var Items = []string{session.StateDir, session.DBFile, session.OpenState, session.SidebarDir}

// Pattern matches the backup files that --list and --keep consider.
const Pattern = "copilot-sessions-*.tar.gz"

const locks = session.StateDir + "/.session-operation-locks"

// Create writes a gzip tar of home to target with mode 0600. The DB entry is a
// VACUUM INTO snapshot, so committed WAL content is included and -wal/-shm never are.
func Create(home, target string) (err error) {
	tmp, err := os.MkdirTemp("", "copilot-sessions-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	snapshot := filepath.Join(tmp, session.DBFile)
	if err := snapshotDB(home, snapshot); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, item := range Items {
		src := filepath.Join(home, item)
		if item == session.DBFile {
			src = snapshot
		}
		if _, err := os.Lstat(src); err != nil {
			continue
		}
		if err := addTree(tw, src, item); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func snapshotDB(home, snapshot string) error {
	db, err := session.OpenDB(home)
	if err != nil || db == nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("VACUUM INTO ?", snapshot); err != nil {
		return fmt.Errorf("snapshot %s: %w", session.DBFile, err)
	}
	return nil
}

// addTree adds root as name, recursively, the way Python's tarfile.add does:
// symlinks are stored, never followed; types tar cannot hold are skipped.
func addTree(tw *tar.Writer, root, name string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		entry := name
		if rel != "." {
			entry += "/" + filepath.ToSlash(rel)
		}
		if strings.HasPrefix(entry, locks) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return err
			}
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
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		// Copy exactly the stat-ed size: a live session may append meanwhile, as tarfile tolerates.
		_, err = io.CopyN(tw, f, hdr.Size)
		return err
	})
}

// List returns the backup files in dir, newest (highest name) first. A missing dir has none.
func List(dir string) ([]string, error) {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, nil
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
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
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
