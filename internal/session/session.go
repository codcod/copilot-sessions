// Package session reads Copilot CLI sessions from COPILOT_HOME (spec §3, §4).
package session

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite", no CGO
)

// Names of the entries in COPILOT_HOME that hold session data (spec §3).
const (
	// StateDir holds one directory per session.
	StateDir = "session-state"
	// DBFile is the SQLite session store.
	DBFile = "session-store.db"
	// OpenState is the JSON object of open sessions, keyed by session ID.
	OpenState = "open-sessions-state.json"
	// SidebarDir holds the sidebar JSON files that list session IDs.
	SidebarDir = "sidebar-sessions-state"
)

// Status is a session's liveness (spec §4.2).
type Status string

// The session statuses, in display order.
const (
	StatusOngoing Status = "ongoing"
	StatusStale   Status = "stale"
	StatusClosed  Status = "closed"
)

// Statuses returns the session statuses in display order.
func Statuses() []Status { return []Status{StatusOngoing, StatusStale, StatusClosed} }

// ErrNoDB reports that COPILOT_HOME has no session-store.db.
var ErrNoDB = errors.New("no " + DBFile)

// Session is one Copilot session, from its directory, the DB or both.
type Session struct {
	ID, Name, Cwd string
	Updated       time.Time // zero when unknown (DB-only sessions only)
	Dir           string    // "" for DB-only sessions
	Status        Status
	Busy, Current bool
	Size          int64 // 0 until FillSizes, and always for DB-only sessions
}

// Load reads every session under home, without sizes (see FillSizes).
func Load(ctx context.Context, home string, ancestors map[int]bool) ([]Session, error) {
	var out []Session
	index := map[string]int{}
	dirs, err := Dirs(home)
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		meta := readWorkspace(dir)
		updated, ok := ParseTime(meta["updated_at"])
		if !ok {
			updated, ok = ParseTime(meta["created_at"])
		}
		if !ok {
			if fi, err := os.Stat(dir); err == nil {
				updated = fi.ModTime().UTC()
			}
		}
		id := filepath.Base(dir)
		index[id] = len(out)
		out = append(out, Session{ID: id, Name: meta["name"], Cwd: meta["cwd"], Updated: updated, Dir: dir})
	}

	rows, err := dbSessions(ctx, home)
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
		if s.Updated.IsZero() {
			var ok bool
			if s.Updated, ok = ParseTime(r.updated); !ok {
				s.Updated, _ = ParseTime(r.created)
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
		s.Status = StatusClosed
		if locked {
			s.Status = StatusStale
			if slices.ContainsFunc(pids, pidIsCopilot) {
				s.Status = StatusOngoing
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
	}
	return out, nil
}

// FillSizes sets Size for each session with a directory; DB-only sessions stay at 0.
func FillSizes(ctx context.Context, ss []Session) error {
	for i := range ss {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ss[i].Dir != "" {
			ss[i].Size = dirSize(ss[i].Dir)
		}
	}
	return nil
}

// Dirs lists the non-hidden directories (symlinks followed) in session-state/.
// A missing session-state/, or one that is not a dir, has no sessions; any other error
// (EACCES on COPILOT_HOME, I/O) is returned.
func Dirs(home string) ([]string, error) {
	state := filepath.Join(home, StateDir)
	fi, err := os.Stat(state)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !fi.IsDir() {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(state)
	if err != nil {
		return nil, err
	}
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
	return dirs, nil
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
// Fractional seconds are accepted by every layout. ok is false when nothing matches.
func ParseTime(v string) (t time.Time, ok bool) {
	v = strings.Trim(strings.TrimSpace(v), `'"`)
	if v == "" {
		return time.Time{}, false
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// OpenDB opens session-store.db if it is a regular file, so the driver never creates
// one. A missing or non-regular file returns ErrNoDB; any other Stat error is returned.
func OpenDB(home string) (*sql.DB, error) {
	p := filepath.Join(home, DBFile)
	fi, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !fi.Mode().IsRegular() {
		return nil, ErrNoDB
	}
	if err != nil {
		return nil, err
	}
	// Escape the path: a raw '?', '#' or '%' would be read as URI syntax and open another file.
	dsn := url.URL{Scheme: "file", OmitHost: true, Path: p, RawQuery: "_pragma=busy_timeout(30000)"}
	return sql.Open("sqlite", dsn.String())
}

type dbRow struct{ id, summary, cwd, created, updated string }

func dbSessions(ctx context.Context, home string) ([]dbRow, error) {
	db, err := OpenDB(home)
	if errors.Is(err, ErrNoDB) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT id, summary, cwd, created_at, updated_at FROM sessions")
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
func HasTurns(ctx context.Context, home string) (map[string]bool, error) {
	ids := map[string]bool{}
	db, err := OpenDB(home)
	if errors.Is(err, ErrNoDB) {
		return ids, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'turns'").Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return ids, nil
	}
	rows, err := db.QueryContext(ctx, "SELECT DISTINCT session_id FROM turns")
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
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
	entries, _ := os.ReadDir(dir) // unreadable dir: no locks visible, reads as closed
	var pids []int
	locked := false
	for _, e := range entries {
		if ok, _ := filepath.Match("inuse.*.lock", e.Name()); !ok {
			continue
		}
		locked = true
		part := strings.Split(e.Name(), ".")[1]
		if pid, ok := ParseUint(part); ok {
			pids = append(pids, pid)
		}
	}
	return pids, locked
}

// ParseUint parses s as plain decimal digits (no sign, no spaces) that fit an int.
func ParseUint(s string) (int, bool) {
	n, err := strconv.ParseUint(s, 10, strconv.IntSize-1)
	return int(n), err == nil
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
		if i < 0 {
			break
		}
		fields := strings.Fields(string(b[i+1:]))
		if len(fields) < 2 {
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
// A symlinked dir is resolved first, because WalkDir does not follow a symlinked root.
func dirSize(dir string) int64 {
	var total int64
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	// The callback never returns an error, so neither does WalkDir.
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

// Sort orders sessions by Updated ascending, unknown (zero) first, ties by ID.
func Sort(ss []Session) {
	slices.SortStableFunc(ss, func(a, b Session) int {
		if c := a.Updated.Compare(b.Updated); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
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
