// Package filter parses DATE and status values and applies the session filters (spec §5).
package filter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/codcod/copilot-sessions/internal/session"
)

// Statuses lists the session statuses in display order.
var Statuses = []string{"ongoing", "stale", "closed"}

// Date is a calendar date in the local timezone.
type Date struct {
	Y int
	M time.Month
	D int
}

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
	if len(v) > 1 && strings.IndexByte("dwm", v[len(v)-1]) >= 0 && allDigits(v[:len(v)-1]) {
		if n, err := strconv.Atoi(v[:len(v)-1]); err == nil {
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
	return Date{}, fmt.Errorf("invalid date '%s'; use YYYY-MM-DD, today, yesterday, Nd, Nw or Nm", value)
}

func allDigits(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// ParseStatuses parses a comma-separated subset of ongoing, stale, closed.
func ParseStatuses(value string) (map[string]bool, error) {
	set := map[string]bool{}
	ok := true
	for _, item := range strings.Split(value, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		set[item] = true
		ok = ok && (item == "ongoing" || item == "stale" || item == "closed")
	}
	if !ok || len(set) == 0 {
		return nil, fmt.Errorf("invalid status '%s'; use a comma-separated list of %s", value, strings.Join(Statuses, ", "))
	}
	return set, nil
}

type dateFlag struct {
	p   **Date
	now func() time.Time
}

// DateFlag is a pflag.Value that parses into *p, using now for "today".
func DateFlag(p **Date, now func() time.Time) pflag.Value { return dateFlag{p, now} }

func (f dateFlag) Set(s string) error {
	d, err := ParseDate(s, f.now())
	if err == nil {
		*f.p = &d
	}
	return err
}

func (f dateFlag) String() string {
	if f.p == nil || *f.p == nil {
		return ""
	}
	return (*f.p).String()
}

func (dateFlag) Type() string { return "DATE" }

type statusFlag struct{ p *map[string]bool }

// StatusFlag is a pflag.Value that parses a status list into *p.
func StatusFlag(p *map[string]bool) pflag.Value { return statusFlag{p} }

func (f statusFlag) Set(s string) error {
	set, err := ParseStatuses(s)
	if err == nil {
		*f.p = set
	}
	return err
}

func (f statusFlag) String() string {
	if f.p == nil {
		return ""
	}
	return strings.Join(sortedKeys(*f.p), ",")
}

func (statusFlag) Type() string { return "LIST" }

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Filters holds the list/clean filters; they combine with AND.
type Filters struct {
	IDs          []string
	Until, Since *Date
	Status       map[string]bool
	Cwd, Grep    string
	Empty        bool
}

// Any reports whether any filter or ID is set.
func (f Filters) Any() bool {
	return len(f.IDs) > 0 || f.Until != nil || f.Since != nil || len(f.Status) > 0 || f.Cwd != "" || f.Grep != "" || f.Empty
}

// Describe returns the active filters joined with "; ", or "all sessions" (spec §6.2).
func (f Filters) Describe() string {
	var parts []string
	if len(f.IDs) > 0 {
		parts = append(parts, "IDs "+strings.Join(f.IDs, ", "))
	}
	if f.Since != nil {
		parts = append(parts, "updated on or after "+f.Since.String())
	}
	if f.Until != nil {
		parts = append(parts, "updated on or before "+f.Until.String())
	}
	if len(f.Status) > 0 {
		parts = append(parts, "status "+strings.Join(sortedKeys(f.Status), "/"))
	}
	if f.Cwd != "" {
		parts = append(parts, "cwd under "+f.Cwd)
	}
	if f.Grep != "" {
		parts = append(parts, "name contains '"+f.Grep+"'")
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
// against all sessions. hasTurns is called only for --empty.
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
		if root, err = filepath.Abs(expandUser(f.Cwd)); err != nil {
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
		if s.Updated != nil {
			day = DateOf(s.Updated.Local()).key()
		}
		switch {
		case wanted != nil && !wanted[s.ID],
			f.Until != nil && (s.Updated == nil || day > f.Until.key()),
			f.Since != nil && (s.Updated == nil || day < f.Since.key()),
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
			return nil, fmt.Errorf("no session matches ID '%s'", p)
		case len(matches) > 1:
			sort.Strings(matches)
			return nil, fmt.Errorf("ID '%s' is ambiguous: %s", p, strings.Join(matches[:min(5, len(matches))], ", "))
		}
		ids[matches[0]] = true
	}
	return ids, nil
}

func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

// under reports whether path is root or below it; /a/b is not below /a/bc.
func under(path, root string) bool {
	path = filepath.Clean(path)
	return path == root || strings.HasPrefix(path, strings.TrimRight(root, "/")+"/")
}
