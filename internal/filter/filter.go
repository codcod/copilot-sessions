// Package filter parses DATE and status values and applies the session filters (spec §5).
package filter

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codcod/copilot-sessions/internal/session"
)

// Date is a calendar date in the local timezone; the zero Date means "not set".
type Date struct {
	Y int        // year
	M time.Month // month, 1–12
	D int        // day of the month, 1–31
}

// IsZero reports whether d is the zero Date.
func (d Date) IsZero() bool { return d == Date{} }

// DateOf returns the date of t in t's location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Y, d.M, d.D) }

func (d Date) key() int { return d.Y*10000 + int(d.M)*100 + d.D }

func (d Date) addDays(n int) Date { return DateOf(time.Date(d.Y, d.M, d.D+n, 0, 0, 0, 0, time.UTC)) }

// monthsBefore goes back n calendar months, clamping to the month's last day.
func monthsBefore(d Date, n int) Date {
	idx := d.Y*12 + int(d.M) - 1 - n
	y := idx / 12
	if idx%12 < 0 {
		y--
	}
	m := time.Month(idx - y*12 + 1)
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return Date{y, m, min(d.D, last)}
}

// ParseDate parses YYYY-MM-DD, today, yesterday, Nd, Nw or Nm relative to now's local date.
func ParseDate(value string, now time.Time) (Date, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	today := DateOf(now.Local())
	switch v {
	case "today":
		return today, nil
	case "yesterday":
		return today.addDays(-1), nil
	}
	if len(v) > 1 && strings.IndexByte("dwm", v[len(v)-1]) >= 0 {
		if n, ok := session.ParseUint(v[:len(v)-1]); ok {
			switch v[len(v)-1] {
			case 'd':
				return today.addDays(-n), nil
			case 'w':
				return today.addDays(-7 * n), nil
			default:
				return monthsBefore(today, n), nil
			}
		}
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return DateOf(t), nil
	}
	return Date{}, fmt.Errorf("invalid date %q; use YYYY-MM-DD, today, yesterday, Nd, Nw or Nm", value)
}

// ParseStatuses parses a comma-separated subset of ongoing, stale, closed.
func ParseStatuses(value string) (map[session.Status]bool, error) {
	set := map[session.Status]bool{}
	ok := true
	for item := range strings.SplitSeq(value, ",") {
		st := session.Status(strings.ToLower(strings.TrimSpace(item)))
		if st == "" {
			continue
		}
		set[st] = true
		ok = ok && slices.Contains(session.Statuses(), st)
	}
	if !ok || len(set) == 0 {
		names := make([]string, 0, 3)
		for _, st := range session.Statuses() {
			names = append(names, string(st))
		}
		return nil, fmt.Errorf("invalid status %q; use a comma-separated list of %s", value, strings.Join(names, ", "))
	}
	return set, nil
}

// StatusList returns the statuses in set, sorted, joined with sep.
func StatusList(set map[session.Status]bool, sep string) string {
	var b strings.Builder
	for i, st := range slices.Sorted(maps.Keys(set)) {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(string(st))
	}
	return b.String()
}

// Filters holds the list/clean filters; they combine with AND.
type Filters struct {
	IDs          []string
	Until, Since Date // zero when unset
	Status       map[session.Status]bool
	Cwd, Grep    string
	Empty        bool
}

// Any reports whether any filter or ID is set.
func (f Filters) Any() bool {
	return len(f.IDs) > 0 || !f.Until.IsZero() || !f.Since.IsZero() || len(f.Status) > 0 || f.Cwd != "" || f.Grep != "" || f.Empty
}

// Describe returns the active filters joined with "; ", or "all sessions" (spec §6.2).
func (f Filters) Describe() string {
	var parts []string
	if len(f.IDs) > 0 {
		parts = append(parts, "IDs "+strings.Join(f.IDs, ", "))
	}
	if !f.Since.IsZero() {
		parts = append(parts, "updated on or after "+f.Since.String())
	}
	if !f.Until.IsZero() {
		parts = append(parts, "updated on or before "+f.Until.String())
	}
	if len(f.Status) > 0 {
		parts = append(parts, "status "+StatusList(f.Status, "/"))
	}
	if f.Cwd != "" {
		parts = append(parts, "cwd under "+f.Cwd)
	}
	if f.Grep != "" {
		parts = append(parts, fmt.Sprintf("name contains %q", f.Grep))
	}
	if f.Empty {
		parts = append(parts, "empty")
	}
	if len(parts) == 0 {
		return "all sessions"
	}
	return strings.Join(parts, "; ")
}

// Apply returns the sessions of all that match f, in input order. ID prefixes resolve
// against all sessions. Cwd must already be ~-expanded. hasTurns is called only for --empty.
func Apply(all []session.Session, f Filters, hasTurns func() (map[string]bool, error)) ([]session.Session, error) {
	var wanted map[string]bool
	if len(f.IDs) > 0 {
		var err error
		if wanted, err = resolveIDs(all, f.IDs); err != nil {
			return nil, err
		}
	}
	var root string
	if f.Cwd != "" {
		var err error
		if root, err = filepath.Abs(f.Cwd); err != nil {
			return nil, err
		}
	}
	var turns map[string]bool
	if f.Empty {
		var err error
		if turns, err = hasTurns(); err != nil {
			return nil, err
		}
	}
	needle := strings.ToLower(f.Grep)
	out := []session.Session{}
	for _, s := range all {
		var day int
		unknown := s.Updated.IsZero()
		if !unknown {
			day = DateOf(s.Updated.Local()).key()
		}
		switch {
		case wanted != nil && !wanted[s.ID],
			!f.Until.IsZero() && (unknown || day > f.Until.key()),
			!f.Since.IsZero() && (unknown || day < f.Since.key()),
			len(f.Status) > 0 && !f.Status[s.Status],
			f.Cwd != "" && (s.Cwd == "" || !under(s.Cwd, root)),
			f.Grep != "" && !strings.Contains(strings.ToLower(s.Name), needle),
			f.Empty && !session.IsEmpty(s, turns):
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func resolveIDs(all []session.Session, prefixes []string) (map[string]bool, error) {
	ids := map[string]bool{}
	for _, p := range prefixes {
		var matches []string
		for _, s := range all {
			if strings.HasPrefix(s.ID, p) {
				matches = append(matches, s.ID)
			}
		}
		switch {
		case len(matches) == 0:
			return nil, fmt.Errorf("no session matches ID %q", p)
		case len(matches) > 1:
			slices.Sort(matches)
			return nil, fmt.Errorf("ID %q is ambiguous: %s", p, strings.Join(matches[:min(5, len(matches))], ", "))
		}
		ids[matches[0]] = true
	}
	return ids, nil
}

// under reports whether path is root or below it; /a/b is not below /a/bc.
func under(path, root string) bool {
	path = filepath.Clean(path)
	return path == root || strings.HasPrefix(path, strings.TrimRight(root, "/")+"/")
}
