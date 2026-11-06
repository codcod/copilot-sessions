package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

func TestMain(m *testing.M) {
	time.Local = time.FixedZone("XST", 2*3600)
	os.Exit(m.Run())
}

func byID(t *testing.T, ss []Session) map[string]Session {
	t.Helper()
	m := map[string]Session{}
	for _, s := range ss {
		m[s.ID] = s
	}
	return m
}

func TestLoadFixture(t *testing.T) {
	fx := testfix.Build(t)
	ss, err := Load(fx.Home, map[int]bool{}, true)
	if err != nil {
		t.Fatal(err)
	}
	got := byID(t, ss)
	if len(got) != 6 {
		t.Fatalf("got %d sessions: %v", len(got), got)
	}
	for _, id := range []string{".other", ".session-operation-locks", "plain-file"} {
		if _, ok := got[id]; ok {
			t.Errorf("%s read as a session", id)
		}
	}

	wantStatus := map[string]string{
		testfix.Titled: "closed", testfix.EmptyNoEvents: "closed", testfix.EmptyStartShutdown: "closed",
		testfix.Stale: "stale", testfix.DBOnly: "closed", testfix.Ongoing: "stale",
	}
	if fx.OngoingLive {
		wantStatus[testfix.Ongoing] = "ongoing"
	}
	for id, st := range wantStatus {
		if got[id].Status != st {
			t.Errorf("%s status = %s, want %s", id, got[id].Status, st)
		}
	}

	titled := got[testfix.Titled]
	if titled.Name != "Titled session" || titled.Cwd != "/home/u/proj" || titled.Busy {
		t.Errorf("titled = %+v (DB must not override, indented lines ignored)", titled)
	}
	if !titled.Updated.Equal(time.Date(2026, 11, 1, 10, 0, 0, 5e8, time.UTC)) {
		t.Errorf("titled updated = %v", titled.Updated)
	}
	stale := got[testfix.Stale]
	if stale.Name != "Stale from DB" || stale.Cwd != "/home/u/other" {
		t.Errorf("stale = %+v (DB fills empty fields)", stale)
	}
	quoted := got[testfix.EmptyStartShutdown]
	if quoted.Updated == nil || !quoted.Updated.Equal(time.Date(2026, 10, 15, 9, 30, 0, 123e6, time.UTC)) {
		t.Errorf("quoted created_at = %v", quoted.Updated)
	}
	dbOnly := got[testfix.DBOnly]
	if dbOnly.Dir != "" || dbOnly.Name != "Only in the DB" || *dbOnly.Size != 0 || dbOnly.Updated == nil {
		t.Errorf("db-only = %+v", dbOnly)
	}
	if !got[testfix.Ongoing].Busy {
		t.Error("ongoing not busy")
	}
	if *titled.Size == 0 {
		t.Error("titled size is 0")
	}

	ss, _ = Load(fx.Home, map[int]bool{testfix.StalePID: true}, false)
	for _, s := range ss {
		if s.Size != nil {
			t.Errorf("%s size computed without withSize", s.ID)
		}
		if s.Current != (s.ID == testfix.Stale) {
			t.Errorf("%s current = %v", s.ID, s.Current)
		}
	}
}

func TestLoadOngoingLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	fx := testfix.Build(t)
	ss, err := Load(fx.Home, map[int]bool{fx.OngoingPID: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	s := byID(t, ss)[testfix.Ongoing]
	if s.Status != "ongoing" || !s.Current || !s.Busy {
		t.Errorf("ongoing = %+v", s)
	}
}

func TestDefaultAncestors(t *testing.T) {
	pids := DefaultAncestors()
	if !pids[os.Getpid()] || pids[1] {
		t.Errorf("ancestors = %v", pids)
	}
	if runtime.GOOS != "linux" {
		return
	}
	// The fixture's helper is a child of this process, so the walk must reach us.
	fx := testfix.Build(t)
	pids = ancestorsFrom(fx.OngoingPID)
	if !pids[fx.OngoingPID] || !pids[os.Getpid()] || pids[1] {
		t.Errorf("ancestors of helper = %v, test pid %d", pids, os.Getpid())
	}
}

func TestLoadFallbacks(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, StateDir)
	mk := func(id, yaml string) string {
		d := filepath.Join(state, id)
		os.MkdirAll(d, 0o755)
		if yaml != "" {
			os.WriteFile(filepath.Join(d, "workspace.yaml"), []byte(yaml), 0o644)
		}
		return d
	}
	mtime := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	os.Chtimes(mk("nomtime", "name: x\nupdated_at: garbage\n"), mtime, mtime)
	mk("zoneless", "updated_at: '2026-11-01 10:00:00.25'\r\ncreated_at: 2020-01-01T00:00:00Z\r\n")
	ss, err := Load(home, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got := byID(t, ss)
	if u := got["nomtime"].Updated; u == nil || !u.Equal(mtime) {
		t.Errorf("mtime fallback = %v", u)
	}
	if u := got["zoneless"].Updated; u == nil || !u.Equal(time.Date(2026, 11, 1, 10, 0, 0, 25e7, time.UTC)) {
		t.Errorf("zone-less = %v", u)
	}
	if got["zoneless"].Status != "closed" {
		t.Error("no DB, no locks should be closed")
	}

	// A DB without a sessions table is an error.
	os.WriteFile(filepath.Join(home, DBFile), nil, 0o644)
	if _, err := Load(home, nil, false); err == nil {
		t.Error("missing sessions table: no error")
	}
	if turns, err := HasTurns(home); err != nil || len(turns) != 0 {
		t.Errorf("missing turns table: %v, %v", turns, err)
	}
}

func TestParseTime(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-06T05:20:49.652Z":      "2026-11-06T05:20:49.652Z",
		`"2026-11-06T05:20:49Z"`:        "2026-11-06T05:20:49Z",
		" '2026-11-06T07:20:49+02:00' ": "2026-11-06T05:20:49Z",
		"2026-11-06T05:20:49":           "2026-11-06T05:20:49Z",
		"2026-11-06 05:20:49.1":         "2026-11-06T05:20:49.1Z",
		"2026-11-06 05:20:49-01:00":     "2026-11-06T06:20:49Z",
	} {
		got := ParseTime(in)
		if got == nil || got.UTC().Format(time.RFC3339Nano) != want {
			t.Errorf("ParseTime(%q) = %v, want %s", in, got, want)
		}
	}
	for _, in := range []string{"", "''", "garbage", "2026-11-06"} {
		if got := ParseTime(in); got != nil {
			t.Errorf("ParseTime(%q) = %v, want nil", in, got)
		}
	}
}

func TestIsEmpty(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		d := filepath.Join(dir, name)
		os.MkdirAll(d, 0o755)
		if content != "" {
			os.WriteFile(filepath.Join(d, "events.jsonl"), []byte(content), 0o644)
		}
		return d
	}
	long := strings.Repeat("x", 200<<10)
	tests := []struct {
		name string
		s    Session
		want bool
	}{
		{"no events", Session{ID: "a", Dir: write("none", "")}, true},
		{"start/shutdown", Session{ID: "b", Dir: write("ss", `{"type":"session.start"}`+"\n"+`{"type":"session.shutdown"}`+"\n")}, true},
		{"user message", Session{ID: "c", Dir: write("msg", `{"type":"session.start"}`+"\n"+`{"type":"user.message"}`+"\n")}, false},
		// The pattern straddles the end of the first read (64 KiB + overlap) on a line longer than 64 KiB.
		{"long line", Session{ID: "d", Dir: write("long", long[:64<<10+len(`"type":"user.message"`)-1-5]+`"type":"user.message"`+long)}, false},
		{"long line without", Session{ID: "e", Dir: write("longno", long+"\n"+long)}, true},
		{"turns", Session{ID: "t", Dir: write("turns", "")}, false},
		{"db only", Session{ID: "f"}, true},
	}
	for _, tt := range tests {
		if got := IsEmpty(tt.s, map[string]bool{"t": true}); got != tt.want {
			t.Errorf("%s: IsEmpty = %v", tt.name, got)
		}
	}
}

func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0B", 375: "375B", 1023: "1023B", 1024: "1.0K", 2458: "2.4K",
		1<<20 - 1: "1024.0K", 194 << 20: "194.0M", 1 << 30: "1.0G", 3 << 40: "3072.0G",
	} {
		if got := HumanSize(n); got != want {
			t.Errorf("HumanSize(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestSort(t *testing.T) {
	a, b := time.Unix(100, 0), time.Unix(200, 0)
	ss := []Session{{ID: "z", Updated: &b}, {ID: "y", Updated: &a}, {ID: "x"}, {ID: "w", Updated: &b}}
	Sort(ss)
	var ids []string
	for _, s := range ss {
		ids = append(ids, s.ID)
	}
	if got := strings.Join(ids, ""); got != "xywz" {
		t.Errorf("order = %s", got)
	}
}
