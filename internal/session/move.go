package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// repointKeys are the workspace.yaml keys that locate a session (CS-017 decision 2).
var repointKeys = map[string]bool{"cwd": true, "git_root": true}

// Repoint rewrites the location of ss to newRoot: the cwd and git_root lines of each
// workspace.yaml and sessions.cwd in the DB, wherever they are under one of oldRoots (the
// folder as given and with symlinks resolved). All roots must be absolute and clean. Like
// Delete it is a point of no return and ignores ctx cancellation. A failed workspace.yaml is
// reported in failed and the others still go ahead; a DB error is returned as err.
func Repoint(ctx context.Context, home string, ss []Session, oldRoots []string, newRoot string) (failed []error, err error) {
	ctx = context.WithoutCancel(ctx)
	for _, s := range ss {
		if s.Dir == "" {
			continue
		}
		if err := repointWorkspace(filepath.Join(s.Dir, "workspace.yaml"), oldRoots, newRoot); err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", s.ID, err))
		}
	}
	return failed, repointDB(ctx, home, ss, oldRoots, newRoot)
}

// RebaseAny is Rebase against the first of oldRoots that path is under.
func RebaseAny(path string, oldRoots []string, newRoot string) (string, bool) {
	for _, r := range oldRoots {
		if p, ok := Rebase(path, r, newRoot); ok {
			return p, true
		}
	}
	return "", false
}

// repointWorkspace rewrites only the matched top-level lines, so every other byte stays.
// A missing workspace.yaml has nothing to rewrite.
func repointWorkspace(path string, oldRoots []string, newRoot string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(data), "\n")
	changed := false
	for i, line := range lines {
		key, after, found := strings.Cut(line, ":")
		if !found || strings.HasPrefix(line, " ") || !repointKeys[strings.TrimSpace(key)] {
			continue
		}
		raw := strings.TrimSpace(after)
		value, quoted, err := unquoteYAML(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", strings.TrimSpace(key), err)
		}
		rebased, ok := RebaseAny(value, oldRoots, newRoot)
		if !ok {
			continue
		}
		at := strings.Index(after, raw)
		lines[i] = key + ":" + after[:at] + quoteYAML(rebased, quoted) + after[at+len(raw):]
		changed = true
	}
	if !changed {
		return nil
	}
	return writeFileAtomic(path, []byte(strings.Join(lines, "")))
}

// unquoteYAML reads a plain, 'single' or "double" quoted YAML scalar. Copilot quotes paths
// that YAML would misread, such as one containing ": " (CS-017 review F2).
func unquoteYAML(v string) (s string, quoted bool, err error) {
	if len(v) < 2 {
		return v, false, nil
	}
	switch {
	case v[0] == '\'' && v[len(v)-1] == '\'':
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'"), true, nil
	case v[0] == '"' && v[len(v)-1] == '"':
		// ponytail: Go escapes cover the ones a path needs (\\, \", \uXXXX). Most YAML-only
		// escapes fail here and are reported; \xNN decodes as a byte in Go, not U+00NN as in
		// YAML, so such a value is not matched (review R3). Decode runes if that shows up.
		s, err := strconv.Unquote(v)
		if err != nil {
			return "", false, fmt.Errorf("unsupported quoted value %s", v)
		}
		return s, true, nil
	}
	return v, false, nil
}

// quoteYAML writes s single-quoted when it was quoted or a plain scalar would misread it.
// Control characters never get here: move refuses them (CS-017 review F3).
func quoteYAML(s string, quoted bool) string {
	if quoted || needsQuote(s) {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return s
}

func needsQuote(s string) bool {
	return s == "" || strings.ContainsRune("-?:,[]{}#&*!|>'\"%@` ", rune(s[0])) ||
		strings.Contains(s, ": ") || strings.Contains(s, " #") ||
		strings.HasSuffix(s, ":") || strings.HasSuffix(s, " ")
}

func repointDB(ctx context.Context, home string, ss []Session, oldRoots []string, newRoot string) error {
	db, err := OpenDB(home)
	if errors.Is(err, ErrNoDB) {
		return nil
	}
	if err != nil {
		return err
	}
	defer db.Close()
	wrap := func(err error) error { return fmt.Errorf("repoint %s: %w", DBFile, err) }
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return wrap(err)
	}
	defer tx.Rollback()
	for _, s := range ss {
		if rebased, ok := RebaseAny(s.DBCwd, oldRoots, newRoot); ok {
			if _, err := tx.ExecContext(ctx, "UPDATE sessions SET cwd = ? WHERE id = ?", rebased, s.ID); err != nil {
				return wrap(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return wrap(err)
	}
	return nil
}
