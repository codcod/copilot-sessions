package cli

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

var pruneTables = []string{"search_index", "turns", "checkpoints", "session_files", "session_refs", "forge_trajectory_events", "assistant_usage_events"}

// rowCount counts the rows of table, optionally only those whose col equals id.
func rowCount(t *testing.T, fx *testfix.Fixture, table, col, id string) int {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(fx.Home, "session-store.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q, args := "SELECT count(*) FROM "+table, []any{}
	if col != "" {
		q, args = q+" WHERE "+col+" = ?", []any{id}
	}
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// snapshot captures everything clean may touch: dirs, DB row counts and UI-state bytes.
func snapshot(t *testing.T, fx *testfix.Fixture) string {
	t.Helper()
	var b strings.Builder
	entries, _ := os.ReadDir(filepath.Join(fx.Home, "session-state"))
	for _, e := range entries {
		b.WriteString(e.Name() + "\n")
	}
	for _, tb := range append(pruneTables, "sessions") {
		b.WriteString(tb + ":" + strconv.Itoa(rowCount(t, fx, tb, "", "")) + "\n")
	}
	for _, f := range []string{"open-sessions-state.json", "sidebar-sessions-state/a.json"} {
		data, _ := os.ReadFile(filepath.Join(fx.Home, f))
		b.Write(data)
	}
	return b.String()
}

// Case 5: validation.
func TestCleanValidation(t *testing.T) {
	fx := testfix.Build(t)
	for _, args := range [][]string{
		{"clean"},
		{"rm", "-y"},
		{"clean", "--all", "--grep", "x"},
		{"clean", "--all", "aaaa1"},
	} {
		_, errOut, code := run(fx, nil, args...)
		want := "error: --all cannot be combined with IDs or filters\n"
		if len(args) < 3 {
			want = "error: specify session IDs, a filter (--until, --since, --status, --cwd, --grep, --empty) or --all\n"
		}
		if code != 1 || errOut != want {
			t.Errorf("%v: exit %d, %q", args, code, errOut)
		}
	}
}

// Case 6: ongoing sessions are skipped unless asked for.
func TestCleanOngoing(t *testing.T) {
	fx := testfix.Build(t)
	if !fx.OngoingLive {
		t.Skip("needs /proc for a live ongoing session")
	}
	note := "Skipped 1 ongoing session (use --include-ongoing).\n"
	tests := []struct {
		args    []string
		note    bool
		summary string
	}{
		{[]string{"--all"}, true, "5 of 6 sessions selected (all sessions, "},
		{[]string{"--status", "closed"}, false, "4 of 6 sessions selected (status closed, "},
		{[]string{"--all", "--include-ongoing"}, false, "6 of 6 sessions selected (all sessions, "},
		{[]string{"--status", "ongoing"}, false, "1 of 6 sessions selected (status ongoing, "},
	}
	for _, tt := range tests {
		out, errOut, code := run(fx, nil, append([]string{"clean", "-n"}, tt.args...)...)
		if code != 0 {
			t.Fatalf("%v: exit %d: %s", tt.args, code, errOut)
		}
		if strings.HasPrefix(out, note) != tt.note || !strings.Contains(out, "\n"+tt.summary) ||
			!strings.HasSuffix(out, "Dry run: nothing deleted.\n") {
			t.Errorf("%v:\n%s", tt.args, out)
		}
	}
}

// Case 7: the current session is never deleted.
func TestCleanCurrent(t *testing.T) {
	fx := testfix.Build(t)
	out, errOut, code := run(fx, map[int]bool{testfix.StalePID: true}, "clean", "--all", "--include-ongoing", "-y")
	if code != 0 || !strings.HasPrefix(out, "Skipped this session (it runs this command).\n") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if !exists(filepath.Join(fx.Home, "session-state", testfix.Stale)) {
		t.Error("current session dir deleted")
	}
	if n := rowCount(t, fx, "sessions", "id", testfix.Stale); n != 1 {
		t.Errorf("current session row gone: %d", n)
	}
	if n := rowCount(t, fx, "turns", "session_id", testfix.Stale); n != 1 {
		t.Errorf("current session turns gone: %d", n)
	}
	// Not every known session went, so no full clear: the extra key stays.
	if _, ok := readJSONMap(t, filepath.Join(fx.Home, "open-sessions-state.json"))["extra-key-id"]; !ok {
		t.Error("partial clean cleared open state")
	}
}

// Case 8: deletion of dirs, DB rows and UI state; full clear.
func TestCleanDelete(t *testing.T) {
	fx := testfix.Build(t)
	sidebar := filepath.Join(fx.Home, "sidebar-sessions-state", "a.json")
	open := filepath.Join(fx.Home, "open-sessions-state.json")
	os.Chmod(sidebar, 0o600)
	before := map[string]int{}
	for _, tb := range []string{"forge_skill_proposals", "schema_version"} {
		before[tb] = rowCount(t, fx, tb, "", "")
	}

	out, errOut, code := run(fx, nil, "clean", "aaaa1", "-y")
	if code != 0 || !strings.HasSuffix(out, "Deleted 1 session.\n") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	if exists(filepath.Join(fx.Home, "session-state", testfix.Titled)) {
		t.Error("dir not removed")
	}
	for _, tb := range pruneTables {
		if n := rowCount(t, fx, tb, "session_id", testfix.Titled); n != 0 {
			t.Errorf("%s: %d rows left", tb, n)
		}
		if tb != "search_index" && rowCount(t, fx, tb, "session_id", testfix.Stale) != 1 {
			t.Errorf("%s: Stale rows touched", tb)
		}
	}
	if rowCount(t, fx, "sessions", "id", testfix.Titled) != 0 || rowCount(t, fx, "sessions", "id", testfix.Stale) != 1 {
		t.Error("sessions table wrong")
	}
	for tb, n := range before {
		if rowCount(t, fx, tb, "", "") != n {
			t.Errorf("%s touched", tb)
		}
	}
	om := readJSONMap(t, open)
	if _, ok := om[testfix.Titled]; ok {
		t.Error("Titled still in open state")
	}
	if _, ok := om["extra-key-id"]; !ok {
		t.Error("extra key dropped")
	}
	sm := readJSONMap(t, sidebar)
	if ids, _ := json.Marshal(sm["sessionIds"]); string(ids) != `["`+testfix.Stale+`","`+testfix.EmptyNoEvents+`"]` || sm["collapsed"] != false {
		t.Errorf("sidebar %v", sm)
	}
	if fi, _ := os.Stat(sidebar); fi.Mode().Perm() != 0o600 {
		t.Errorf("sidebar mode %v", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(sidebar); !strings.HasSuffix(string(b), "]\n}\n") || !strings.Contains(string(b), "\n  \"collapsed\": false") {
		t.Errorf("sidebar format %q", b)
	}

	fx = testfix.Build(t)
	if _, errOut, code := run(fx, nil, "clean", "--all", "--include-ongoing", "-y"); code != 0 {
		t.Fatalf("full clean: exit %d: %s", code, errOut)
	}
	if b, _ := os.ReadFile(filepath.Join(fx.Home, "open-sessions-state.json")); string(b) != "{}\n" {
		t.Errorf("open state %q", b)
	}
	if ids, _ := json.Marshal(readJSONMap(t, filepath.Join(fx.Home, "sidebar-sessions-state", "a.json"))["sessionIds"]); string(ids) != "[]" {
		t.Errorf("sidebar ids %s", ids)
	}
	if rowCount(t, fx, "sessions", "", "") != 0 {
		t.Error("sessions left")
	}
}

// Case 9: dry run and confirmation.
func TestCleanConfirm(t *testing.T) {
	fx := testfix.Build(t)
	want := snapshot(t, fx)
	if out, _, code := run(fx, nil, "clean", "--all", "-n"); code != 0 || !strings.HasSuffix(out, "Dry run: nothing deleted.\n") {
		t.Errorf("dry run: %d %s", code, out)
	}
	if got := snapshot(t, fx); got != want {
		t.Errorf("dry run changed state:\n%s\nwant:\n%s", got, want)
	}
	if _, errOut, code := run(fx, nil, "clean", "--all"); code != 1 || errOut != "error: confirmation required; rerun with --yes\n" {
		t.Errorf("no TTY: %d %q", code, errOut)
	}
	out, _, code := runIn(fx, nil, "n\n", true, "clean", "--all")
	if code != 0 || !strings.HasSuffix(out, "Permanently delete these sessions? [y/N] Aborted.\n") || snapshot(t, fx) != want {
		t.Errorf("abort: %d %q", code, out)
	}
	out, _, code = runIn(fx, nil, " Yes\n", true, "clean", "aaaa1")
	if code != 0 || !strings.HasSuffix(out, "Deleted 1 session.\n") || exists(filepath.Join(fx.Home, "session-state", testfix.Titled)) {
		t.Errorf("yes: %d %q", code, out)
	}
}
