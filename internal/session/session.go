// Package session reads Copilot CLI sessions from COPILOT_HOME (spec §3, §4).
package session

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite", no CGO
)

const (
	StateDir  = "session-state"
	DBFile    = "session-store.db"
	OpenState = "open-sessions-state.json"
)

// Session is one Copilot session, from its directory, the DB or both.
type Session struct {
	ID, Name, Cwd string
	Updated       *time.Time // nil when unknown (DB-only sessions only)
	Dir           string     // "" for DB-only sessions
	Status        string     // ongoing, stale or closed
	Busy, Current bool
	Size          *int64 // nil unless sizes were computed
}

// Load reads every session under home. Sizes are computed only when withSize is set.
func Load(home string, ancestors map[int]bool, withSize bool) ([]Session, error) {
	var out []Session
	index := map[string]int{}
	for _, dir := range sessionDirs(home) {
		meta := readWorkspace(dir)
		updated := ParseTime(meta["updated_at"])
		if updated == nil {
			updated = ParseTime(meta["created_at"])
		}
		if updated == nil {
			if fi, err := os.Stat(dir); err == nil {
				t := fi.ModTime().UTC()
				updated = &t
			}
		}
		id := filepath.Base(dir)
		index[id] = len(out)
		out = append(out, Session{ID: id, Name: meta["name"], Cwd: meta["cwd"], Updated: updated, Dir: dir})
	}

	rows, err := dbSessions(home)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		i, ok := index[r.id]
		if !ok {
			i = len(out)
			index[r.id] = i
			out = append(out, Session{ID: r.id})
		}
		s := &out[i]
		if s.Updated == nil {
			s.Updated = ParseTime(r.updated)
			if s.Updated == nil {
				s.Updated = ParseTime(r.created)
			}
		}
		if s.Name == "" {
			s.Name = r.summary
		}
		if s.Cwd == "" {
			s.Cwd = r.cwd
		}
	}

	open := readOpenState(home)
	for i := range out {
		s := &out[i]
		pids, locked := lockPIDs(s.Dir)
		s.Status = "closed"
		if locked {
			s.Status = "stale"
			for _, p := range pids {
				if pidIsCopilot(p) {
					s.Status = "ongoing"
					break
				}
			}
		}
		if entry, ok := open[s.ID].(map[string]any); ok {
			s.Busy = truthy(entry["working"])
		}
		for _, p := range pids {
			if ancestors[p] {
				s.Current = true
			}
		}
		if withSize {
			var n int64
			if s.Dir != "" {
				n = dirSize(s.Dir)
			}
			s.Size = &n
		}
	}
	return out, nil
}

// sessionDirs lists the non-hidden directories (symlinks followed) in session-state/.
func sessionDirs(home string) []string {
	state := filepath.Join(home, StateDir)
	entries, _ := os.ReadDir(state)
	var dirs []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(state, e.Name())
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			dirs = append(dirs, p)
		}
	}
	return dirs
}

// readWorkspace parses workspace.yaml line by line, without a YAML library (spec §4.1).
func readWorkspace(dir string) map[string]string {
	meta := map[string]string{}
	data, err := os.ReadFile(filepath.Join(dir, "workspace.yaml"))
	if err != nil {
		return meta
	}
	lines := strings.FieldsFunc(string(data), func(r rune) bool { return r == '\n' || r == '\r' })
	for _, line := range lines {
		key, value, found := strings.Cut(line, ":")
		if found && !strings.HasPrefix(line, " ") {
			meta[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return meta
}

var timeLayouts = []string{
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05",
}

// ParseTime parses an ISO 8601 timestamp, possibly quoted. No zone means UTC.
// Fractional seconds are accepted by every layout. It returns nil when nothing matches.
func ParseTime(v string) *time.Time {
	v = strings.Trim(strings.TrimSpace(v), `'"`)
	if v == "" {
		return nil
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return &t
		}
	}
	return nil
}

// openDB opens session-store.db if it is a regular file; it returns nil, nil otherwise,
// so the driver never creates one.
func openDB(home string) (*sql.DB, error) {
	p := filepath.Join(home, DBFile)
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return nil, nil
	}
	return sql.Open("sqlite", "file:"+p+"?_pragma=busy_timeout(30000)")
}

type dbRow struct{ id, summary, cwd, created, updated string }

func dbSessions(home string) ([]dbRow, error) {
	db, err := openDB(home)
	if db == nil || err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT id, summary, cwd, created_at, updated_at FROM sessions")
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", DBFile, err)
	}
	defer rows.Close()
	var out []dbRow
	for rows.Next() {
		var id, summary, cwd, created, updated sql.NullString
		if err := rows.Scan(&id, &summary, &cwd, &created, &updated); err != nil {
			return nil, fmt.Errorf("read %s: %w", DBFile, err)
		}
		out = append(out, dbRow{id.String, summary.String, cwd.String, created.String, updated.String})
	}
	return out, rows.Err()
}

// HasTurns returns the IDs of sessions with rows in the turns table.
func HasTurns(home string) (map[string]bool, error) {
	ids := map[string]bool{}
	db, err := openDB(home)
	if db == nil || err != nil {
		return ids, err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'turns'").Scan(&n); err != nil || n == 0 {
		return ids, err
	}
	rows, err := db.Query("SELECT DISTINCT session_id FROM turns")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id sql.NullString
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id.String] = true
	}
	return ids, rows.Err()
}

// IsEmpty reports whether s has no turns and no user message in events.jsonl (spec §4.3).
func IsEmpty(s Session, turns map[string]bool) bool {
	return !turns[s.ID] && !hasUserMessage(s.Dir)
}

// hasUserMessage streams events.jsonl in chunks, keeping a pattern-length overlap, so
// neither line length nor file size matters.
func hasUserMessage(dir string) bool {
	if dir == "" {
		return false
	}
	f, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		return false
	}
	defer f.Close()
	pat := []byte(`"type":"user.message"`)
	buf := make([]byte, 64<<10+len(pat)-1)
	keep := 0
	for {
		n, err := f.Read(buf[keep:])
		data := buf[:keep+n]
		if bytes.Contains(data, pat) {
			return true
		}
		if err != nil {
			return false
		}
		keep = min(len(pat)-1, len(data))
		copy(buf, data[len(data)-keep:])
	}
}

// lockPIDs returns the PIDs of inuse.<pid>.lock files and whether any such file exists.
func lockPIDs(dir string) ([]int, bool) {
	if dir == "" {
		return nil, false
	}
	entries, _ := os.ReadDir(dir)
	var pids []int
	locked := false
	for _, e := range entries {
		if ok, _ := filepath.Match("inuse.*.lock", e.Name()); !ok {
			continue
		}
		locked = true
		part := strings.Split(e.Name(), ".")[1]
		if isDigits(part) {
			if pid, err := strconv.Atoi(part); err == nil {
				pids = append(pids, pid)
			}
		}
	}
	return pids, locked
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func pidIsCopilot(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return err == nil && bytes.Contains(b, []byte("copilot"))
}

// DefaultAncestors returns this process and its ancestors up to (not including) PID 1.
// Without /proc (macOS) it returns only the process's own PID.
func DefaultAncestors() map[int]bool { return ancestorsFrom(os.Getpid()) }

func ancestorsFrom(pid int) map[int]bool {
	pids := map[int]bool{}
	for pid > 1 {
		pids[pid] = true
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			break
		}
		i := bytes.LastIndexByte(b, ')')
		fields := strings.Fields(string(b[i+1:]))
		if i < 0 || len(fields) < 2 {
			break
		}
		if pid, err = strconv.Atoi(fields[1]); err != nil {
			break
		}
	}
	return pids
}

// readOpenState reads open-sessions-state.json; a missing or broken file reads as {}.
func readOpenState(home string) map[string]any {
	m := map[string]any{}
	if b, err := os.ReadFile(filepath.Join(home, OpenState)); err == nil {
		if json.Unmarshal(b, &m) != nil || m == nil {
			m = map[string]any{}
		}
	}
	return m
}

// truthy follows Python's bool() for decoded JSON values.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// dirSize sums the lstat sizes of regular files under dir; unreadable entries are skipped.
func dirSize(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

// Sort orders sessions by Updated ascending, unknown first, ties by ID.
func Sort(ss []Session) {
	sort.SliceStable(ss, func(i, j int) bool {
		a, b := ss[i].Updated, ss[j].Updated
		switch {
		case a == nil && b != nil:
			return true
		case a != nil && b == nil:
			return false
		case a != nil && !a.Equal(*b):
			return a.Before(*b)
		}
		return ss[i].ID < ss[j].ID
	})
}

// HumanSize formats n bytes in base 1024 with units B, K, M, G (spec §6.1).
func HumanSize(n int64) string {
	v := float64(n)
	for _, unit := range []string{"B", "K", "M"} {
		if v < 1024 {
			if unit == "B" {
				return fmt.Sprintf("%.0fB", v)
			}
			return fmt.Sprintf("%.1f%s", v, unit)
		}
		v /= 1024
	}
	return fmt.Sprintf("%.1fG", v)
}
