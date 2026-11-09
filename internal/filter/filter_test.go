package filter

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codcod/copilot-sessions/internal/session"
)

func TestMain(m *testing.M) {
	time.Local = time.FixedZone("XST", 2*3600)
	os.Exit(m.Run())
}

func TestParseDate(t *testing.T) {
	// 23:30 UTC on Mar 30 is already Mar 31 in XST (+2).
	now := time.Date(2026, 3, 30, 23, 30, 0, 0, time.UTC)
	tests := []struct{ in, want string }{
		{"2026-11-06", "2026-11-06"},
		{"today", "2026-03-31"},
		{"  ToDay ", "2026-03-31"},
		{"yesterday", "2026-03-30"},
		{"0d", "2026-03-31"},
		{"10d", "2026-03-21"},
		{"2W", "2026-03-17"},
		{"1m", "2026-02-28"},
		{"13m", "2025-02-28"},
		{"3m", "2025-12-31"},
		{"25m", "2024-02-29"},
	}
	for _, tt := range tests {
		got, err := ParseDate(tt.in, now)
		if err != nil || got.String() != tt.want {
			t.Errorf("ParseDate(%q) = %v, %v; want %s", tt.in, got, err, tt.want)
		}
	}
	leap := time.Date(2024, 3, 31, 12, 0, 0, 0, time.Local)
	if got, _ := ParseDate("1m", leap); got.String() != "2024-02-29" {
		t.Errorf("leap 1m = %v", got)
	}
	for _, bad := range []string{"nope", "", "d", "-1d", "1y", "2026-02-30", "20261106", "1.5d"} {
		_, err := ParseDate(bad, now)
		want := `invalid date "` + bad + `"; use YYYY-MM-DD, today, yesterday, Nd, Nw or Nm`
		if err == nil || err.Error() != want {
			t.Errorf("ParseDate(%q) err = %v", bad, err)
		}
	}
}

func TestParseStatuses(t *testing.T) {
	got, err := ParseStatuses(" Ongoing, closed,,")
	if err != nil || !reflect.DeepEqual(got, map[session.Status]bool{"ongoing": true, "closed": true}) {
		t.Errorf("got %v, %v", got, err)
	}
	for _, bad := range []string{"", ",", "open", "closed,open"} {
		want := `invalid status "` + bad + `"; use a comma-separated list of ongoing, stale, closed`
		if _, err := ParseStatuses(bad); err == nil || err.Error() != want {
			t.Errorf("ParseStatuses(%q) err = %v", bad, err)
		}
	}
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestApply(t *testing.T) {
	dir := t.TempDir()
	withMsg := filepath.Join(dir, "with-msg")
	os.MkdirAll(withMsg, 0o755)
	os.WriteFile(filepath.Join(withMsg, "events.jsonl"), []byte(`{"type":"user.message"}`+"\n"), 0o644)
	home, _ := os.UserHomeDir()

	all := []session.Session{
		{ID: "a1", Name: "Fix Bug", Cwd: "/a/b", Updated: at("2026-11-01T21:59:00Z"), Status: "closed", Dir: withMsg},
		{ID: "a2", Name: "other", Cwd: "/a/bc", Updated: at("2026-11-01T22:00:00Z"), Status: "stale"},
		{ID: "b1", Name: "bugfix", Cwd: "/a/b/c/", Updated: at("2026-11-03T10:00:00Z"), Status: "ongoing"},
		{ID: "c1", Name: "", Cwd: "", Status: "closed"},
		{ID: "d1", Name: "home", Cwd: home + "/x", Updated: at("2026-11-04T10:00:00Z"), Status: "closed"},
	}
	day := func(s string) Date { d, _ := ParseDate(s, time.Now()); return d }
	turns := func() (map[string]bool, error) { return map[string]bool{"b1": true}, nil }
	tests := []struct {
		name string
		f    Filters
		want string
	}{
		{"none", Filters{}, "a1 a2 b1 c1 d1"},
		{"ids", Filters{IDs: []string{"b", "c1"}}, "b1 c1"},
		// a1 is Nov 1 23:59 local, a2 is Nov 2 00:00 local; unknown never matches.
		{"until", Filters{Until: day("2026-11-01")}, "a1"},
		{"since", Filters{Since: day("2026-11-02")}, "a2 b1 d1"},
		{"status", Filters{Status: map[session.Status]bool{"closed": true, "stale": true}}, "a1 a2 c1 d1"},
		{"cwd boundary", Filters{Cwd: "/a/b"}, "a1 b1"},
		{"cwd trailing slash", Filters{Cwd: "/a/b/"}, "a1 b1"},
		{"cwd home", Filters{Cwd: home}, "d1"},
		{"grep", Filters{Grep: "BUG"}, "a1 b1"},
		{"empty", Filters{Empty: true}, "a2 c1 d1"},
		{"and", Filters{Grep: "bug", Since: day("2026-11-02")}, "b1"},
		{"and ids", Filters{IDs: []string{"a1", "b1"}, Status: map[session.Status]bool{"closed": true}}, "a1"},
	}
	for _, tt := range tests {
		got, err := Apply(all, tt.f, turns)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		var ids []string
		for _, s := range got {
			ids = append(ids, s.ID)
		}
		if strings.Join(ids, " ") != tt.want {
			t.Errorf("%s: got %v, want %s", tt.name, ids, tt.want)
		}
	}

	noTurns := func() (map[string]bool, error) { t.Fatal("hasTurns called without --empty"); return nil, nil }
	if got, _ := Apply(nil, Filters{}, noTurns); got == nil {
		t.Error("Apply returned nil slice")
	}
	if _, err := Apply(all, Filters{IDs: []string{"z"}}, noTurns); err == nil || err.Error() != `no session matches ID "z"` {
		t.Errorf("no match err = %v", err)
	}
	if _, err := Apply(all, Filters{IDs: []string{"a"}}, noTurns); err == nil || err.Error() != `ID "a" is ambiguous: a1, a2` {
		t.Errorf("ambiguous err = %v", err)
	}
	var many []session.Session
	for _, id := range []string{"x7", "x3", "x6", "x1", "x5", "x2", "x4"} {
		many = append(many, session.Session{ID: id})
	}
	if _, err := Apply(many, Filters{IDs: []string{"x"}}, noTurns); err == nil || err.Error() != `ID "x" is ambiguous: x1, x2, x3, x4, x5` {
		t.Errorf("ambiguous >5 err = %v", err)
	}
}

func TestDescribe(t *testing.T) {
	d := Date{2026, 11, 6}
	f := Filters{IDs: []string{"ab", "cd"}, Until: d, Since: d, Status: map[session.Status]bool{"stale": true, "closed": true}, Cwd: "~/x", Grep: "bug", Empty: true}
	want := "IDs ab, cd; updated on or after 2026-11-06; updated on or before 2026-11-06; status closed/stale; cwd under ~/x; name contains \"bug\"; empty"
	if got := f.Describe(); got != want {
		t.Errorf("got %q", got)
	}
	if !f.Any() || (Filters{}).Any() || (Filters{}).Describe() != "all sessions" {
		t.Error("Any/Describe on empty filters")
	}
}
