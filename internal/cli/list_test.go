package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

func TestMain(m *testing.M) {
	time.Local = time.FixedZone("XST", 2*3600)
	os.Exit(m.Run())
}

// run executes the CLI in-process against fx and returns stdout, stderr and the exit code.
func run(fx *testfix.Fixture, ancestors map[int]bool, args ...string) (string, string, int) {
	var out, errOut bytes.Buffer
	env := Env{
		Home: fx.Home, BackupDir: fx.BackupDir,
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut,
		IsTTY:        func() bool { return false },
		Now:          func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) },
		AncestorPIDs: func() map[int]bool { return ancestors },
	}
	code := Execute(env, args, "test")
	return out.String(), errOut.String(), code
}

func TestListTable(t *testing.T) {
	fx := testfix.Build(t)
	ongoing := "stale         "
	summary := "6 of 6 sessions (2 stale, 4 closed), 974B.\n"
	if fx.OngoingLive {
		ongoing = "ongoing (busy)"
		summary = "6 of 6 sessions (1 ongoing, 1 stale, 4 closed), 974B.\n"
	}
	want := "" +
		"  UPDATED           STATUS            SIZE  ID                                    NAME\n" +
		"  2026-09-20 09:00  closed               -  " + testfix.DBOnly + "  Only in the DB (db only)\n" +
		"  2026-10-01 10:00  closed            126B  " + testfix.EmptyNoEvents + "  \n" +
		"  2026-10-15 11:30  closed            175B  " + testfix.EmptyStartShutdown + "  \n" +
		"  2026-11-01 12:00  closed            268B  " + testfix.Titled + "  Titled session\n" +
		"  2026-11-04 00:30  stale             107B  " + testfix.Stale + "  Stale from DB (this session)\n" +
		"  2026-11-05 13:00  " + ongoing + "    298B  " + testfix.Ongoing + "  " + string([]rune(testfix.OngoingName)[:60]) + "\n" +
		summary
	out, errOut, code := run(fx, map[int]bool{testfix.StalePID: true}, "list")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}

	out, _, _ = run(fx, nil, "ls", "--status", "stale", "--cwd", "/home/u")
	if !strings.HasSuffix(out, "1 of 6 sessions (1 stale), 107B.\n") || strings.Contains(out, "(this session)") {
		t.Errorf("filtered:\n%s", out)
	}
	out, _, code = run(fx, nil, "list", "--grep", "nothing-matches")
	if code != 0 || out != "No sessions match.\n" {
		t.Errorf("no match: %d %q", code, out)
	}
}

func TestListQuiet(t *testing.T) {
	fx := testfix.Build(t)
	out, _, code := run(fx, nil, "list", "-q", "--since", "2026-10-01", "--until", "2026-11-01")
	want := testfix.EmptyNoEvents + "\n" + testfix.EmptyStartShutdown + "\n" + testfix.Titled + "\n"
	if code != 0 || out != want {
		t.Errorf("got %q", out)
	}
	out, _, _ = run(fx, nil, "list", "-q", "--empty")
	want = testfix.DBOnly + "\n" + testfix.EmptyNoEvents + "\n" + testfix.EmptyStartShutdown + "\n"
	if out != want {
		t.Errorf("--empty got %q", out)
	}
	out, _, _ = run(fx, nil, "list", "-q", "-g", "zzz")
	if out != "" {
		t.Errorf("-q no match got %q", out)
	}
}

func TestListJSON(t *testing.T) {
	fx := testfix.Build(t)
	decode := func(args ...string) []map[string]any {
		t.Helper()
		out, errOut, code := run(fx, map[int]bool{testfix.StalePID: true}, args...)
		if code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
		var v []map[string]any
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return v
	}

	rows := decode("list", "--json")
	if len(rows) != 6 {
		t.Fatalf("got %d rows", len(rows))
	}
	keys := []string{"id", "name", "cwd", "updated_at", "status", "busy", "current", "size", "db_only"}
	for _, r := range rows {
		if len(r) != len(keys) {
			t.Errorf("keys %v", r)
		}
		for _, k := range keys {
			if _, ok := r[k]; !ok {
				t.Errorf("missing %s in %v", k, r)
			}
		}
		if _, ok := r["size"].(float64); !ok {
			t.Errorf("size not a number: %v", r)
		}
	}
	first, stale := rows[0], rows[4]
	if first["id"] != testfix.DBOnly || first["db_only"] != true || first["updated_at"] != "2026-09-20T07:00:00Z" || first["busy"] != nil {
		t.Errorf("db-only row %v", first)
	}
	if stale["id"] != testfix.Stale || stale["current"] != true || stale["status"] != "stale" {
		t.Errorf("stale row %v", stale)
	}
	if fx.OngoingLive && rows[5]["busy"] != true {
		t.Errorf("ongoing busy %v", rows[5])
	}

	for _, r := range decode("list", "--json", "-q") {
		if r["size"] != nil {
			t.Errorf("--json -q size %v", r["size"])
		}
	}
	if out, _, _ := run(fx, nil, "list", "--json", "-g", "zzz"); out != "[]\n" {
		t.Errorf("empty JSON %q", out)
	}
}

func TestExitCodes(t *testing.T) {
	fx := testfix.Build(t)
	tests := []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"list", "--until", "nope"}, 2, "invalid date 'nope'; use YYYY-MM-DD, today, yesterday, Nd, Nw or Nm"},
		{[]string{"list", "-S", "1x"}, 2, "invalid date '1x'"},
		{[]string{"list", "--status", "open"}, 2, "invalid status 'open'; use a comma-separated list of ongoing, stale, closed"},
		{[]string{"list", "--bogus"}, 2, "unknown flag: --bogus"},
		{[]string{}, 2, "required: COMMAND"},
		{[]string{"nope"}, 2, `unknown command "nope"`},
		{[]string{"list", "zzzz"}, 1, "no session matches ID 'zzzz'"},
		{[]string{"list", "aaaa"}, 1, "ID 'aaaa' is ambiguous: " + testfix.Titled + ", " + testfix.EmptyNoEvents},
		{[]string{"list", "aaaa1"}, 0, ""},
	}
	for _, tt := range tests {
		_, errOut, code := run(fx, nil, tt.args...)
		if code != tt.code || !strings.Contains(errOut, tt.msg) || (tt.msg != "" && !strings.HasPrefix(errOut, "error: ")) {
			t.Errorf("%v: exit %d, stderr %q", tt.args, code, errOut)
		}
	}

	os.WriteFile(fx.Home+"/session-store.db", []byte("not a database, definitely not sqlite at all......"), 0o644)
	if _, errOut, code := run(fx, nil, "list"); code != 1 || !strings.HasPrefix(errOut, "error: ") {
		t.Errorf("broken DB: exit %d, %q", code, errOut)
	}
}

func TestHelp(t *testing.T) {
	fx := testfix.Build(t)
	out, _, code := run(fx, nil, "--help")
	for _, want := range []string{
		"List, clean, back up and restore GitHub Copilot CLI sessions.",
		"YYYY-MM-DD, today, yesterday, Nd, Nw, Nm",
		"ongoing  a live copilot process", "stale    the session", "closed   the session",
		"Copilot home: " + fx.Home + " (COPILOT_HOME)", "Backups: " + fx.BackupDir + " (COPILOT_BACKUP_DIR)",
	} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	if out, _, _ := run(fx, nil, "--version"); !strings.Contains(out, "test") {
		t.Errorf("version %q", out)
	}
}
