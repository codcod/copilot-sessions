// Package testfix builds a COPILOT_HOME fixture in a temp dir. Only tests import it.
package testfix

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	_ "modernc.org/sqlite"
)

// Session IDs by role. Titled and EmptyNoEvents share the prefix "aaaa".
const (
	Titled             = "aaaa1111-1111-4111-8111-111111111111"
	EmptyNoEvents      = "aaaa2222-2222-4222-8222-222222222222"
	EmptyStartShutdown = "bbbb3333-3333-4333-8333-333333333333"
	Stale              = "cccc4444-4444-4444-8444-444444444444"
	DBOnly             = "dddd6666-6666-4666-8666-666666666666"
	Ongoing            = "eeee5555-5555-4555-8555-555555555555"

	StalePID = 999999999
	// OngoingName is longer than 60 runes and has multibyte runes.
	OngoingName = "Ongoing session with a long name: ünïcödé ✓ that runs past sixty runes"
)

// Fixture is a built COPILOT_HOME. Every timestamp has a zone and every updated value is
// distinct, so the Python parity run neither crashes nor reorders ties.
type Fixture struct {
	Home, BackupDir string
	OngoingPID      int
	OngoingLive     bool // the ongoing lock PID is a live copilot-test process (Linux and macOS)
}

// Build creates the fixture under t.TempDir() and points COPILOT_HOME and
// COPILOT_BACKUP_DIR at it.
func Build(t testing.TB) *Fixture {
	t.Helper()
	root := t.TempDir()
	fx := &Fixture{Home: filepath.Join(root, "copilot"), BackupDir: filepath.Join(root, "backups"), OngoingPID: 999999998}
	t.Setenv("COPILOT_HOME", fx.Home)
	t.Setenv("COPILOT_BACKUP_DIR", fx.BackupDir)

	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		fx.OngoingPID = startCopilotHelper(t, root)
		fx.OngoingLive = true
	}

	state := filepath.Join(fx.Home, "session-state")
	userMsg := `{"type":"session.start","data":{}}` + "\n" + `{"type":"user.message","data":{"content":"hi"}}` + "\n"

	write(t, filepath.Join(state, Titled, "workspace.yaml"), "id: "+Titled+"\ncwd: /home/u/proj\nname: Titled session\n  name: indented lines are ignored\ncreated_at: 2026-11-01T09:00:00Z\nupdated_at: 2026-11-01T10:00:00.500Z\n")
	write(t, filepath.Join(state, Titled, "events.jsonl"), userMsg)

	write(t, filepath.Join(state, EmptyNoEvents, "workspace.yaml"), "id: "+EmptyNoEvents+"\ncwd: /home/u/projX\ncreated_at: 2026-10-01T07:00:00Z\nupdated_at: 2026-10-01T08:00:00Z\n")

	write(t, filepath.Join(state, EmptyStartShutdown, "workspace.yaml"), "id: "+EmptyStartShutdown+"\ncwd: /home/u/proj/sub\ncreated_at: \"2026-10-15T09:30:00.123Z\"\n")
	write(t, filepath.Join(state, EmptyStartShutdown, "events.jsonl"), `{"type":"session.start","data":{}}`+"\n"+`{"type":"session.shutdown","data":{}}`+"\n")

	// No name or cwd in the YAML: the DB fills them. No events, but DB turns.
	write(t, filepath.Join(state, Stale, "workspace.yaml"), "id: "+Stale+"\ncreated_at: 2026-11-03T20:00:00Z\nupdated_at: 2026-11-03T22:30:00Z\n")
	write(t, filepath.Join(state, Stale, "inuse."+strconv.Itoa(StalePID)+".lock"), "")

	write(t, filepath.Join(state, Ongoing, "workspace.yaml"), "id: "+Ongoing+"\ncwd: /srv/work\nname: "+OngoingName+"\ncreated_at: 2026-11-05T10:00:00+01:00\nupdated_at: 2026-11-05T12:00:00+01:00\n")
	write(t, filepath.Join(state, Ongoing, "events.jsonl"), userMsg)
	write(t, filepath.Join(state, Ongoing, "inuse."+strconv.Itoa(fx.OngoingPID)+".lock"), "")

	// Entries that are not sessions.
	write(t, filepath.Join(state, ".session-operation-locks", "x.lock"), "")
	write(t, filepath.Join(state, ".other", "workspace.yaml"), "id: hidden\n")
	write(t, filepath.Join(state, "plain-file"), "not a session\n")

	writeJSON(t, filepath.Join(fx.Home, "open-sessions-state.json"), map[string]any{
		Ongoing:        map[string]any{"working": true},
		Titled:         map[string]any{"working": false},
		"extra-key-id": map[string]any{"working": true, "other": 1},
	})
	writeJSON(t, filepath.Join(fx.Home, "sidebar-sessions-state", "a.json"), map[string]any{
		"sessionIds": []string{Titled, Stale, EmptyNoEvents},
		"collapsed":  false,
	})

	buildDB(t, filepath.Join(fx.Home, "session-store.db"))
	return fx
}

// startCopilotHelper runs sleep through a symlink in a directory named copilot-test, so its
// argv contains "copilot", and returns its PID. A symlink, not a copy: macOS kills a copied
// platform binary (its code signature no longer matches). The link keeps the name sleep
// because a multicall coreutils (uutils, Ubuntu 26.04+) picks the applet from argv[0]'s
// basename.
func startCopilotHelper(t testing.TB, dir string) int {
	src, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "copilot-test", "sleep")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, bin); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "300")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	return cmd.Process.Pid
}

func buildDB(t testing.TB, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		"PRAGMA journal_mode=WAL",
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, cwd TEXT, repository TEXT, host_type TEXT,
		  branch TEXT, summary TEXT, created_at TEXT, updated_at TEXT)`,
		`CREATE TABLE turns (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL REFERENCES sessions(id),
		  turn_index INTEGER NOT NULL, user_message TEXT, assistant_response TEXT, timestamp TEXT,
		  UNIQUE(session_id, turn_index))`,
		`CREATE VIRTUAL TABLE search_index USING fts5(content, session_id UNINDEXED,
		  source_type UNINDEXED, source_id UNINDEXED)`,
		"CREATE TABLE forge_skill_proposals (id INTEGER PRIMARY KEY, data TEXT)",
		"CREATE TABLE dynamic_context_items (id INTEGER PRIMARY KEY, data TEXT)",
		"CREATE TABLE schema_version (version INTEGER)",
		"INSERT INTO schema_version VALUES (1)",
	}
	for _, table := range []string{"checkpoints", "session_files", "session_refs", "forge_trajectory_events", "assistant_usage_events"} {
		stmts = append(stmts,
			"CREATE TABLE "+table+" (id INTEGER PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id))",
			"INSERT INTO "+table+" (session_id) VALUES ('"+Titled+"'), ('"+Stale+"')")
	}
	stmts = append(stmts,
		`INSERT INTO sessions (id, cwd, summary, created_at, updated_at) VALUES
		  ('`+Titled+`', '/db/cwd/ignored', 'DB summary ignored', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		  ('`+Stale+`', '/home/u/other', 'Stale from DB', NULL, NULL),
		  ('`+DBOnly+`', '/home/u/proj/db', 'Only in the DB', '2026-09-20T06:00:00Z', '2026-09-20T07:00:00Z')`,
		`INSERT INTO turns (session_id, turn_index, user_message) VALUES
		  ('`+Titled+`', 0, 'hi'), ('`+Titled+`', 1, 'again'), ('`+Stale+`', 0, 'hello')`,
		`INSERT INTO search_index (content, session_id, source_type, source_id) VALUES
		  ('hi', '`+Titled+`', 'turn', '0'), ('again', '`+Titled+`', 'turn', '1')`,
	)
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func write(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t testing.TB, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(b)+"\n")
}

// Archive writes a gzip tar of hdrs to path. Each regular file gets the body "data\n".
func Archive(t testing.TB, path string, hdrs ...tar.Header) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, h := range hdrs {
		if h.Typeflag == tar.TypeReg {
			h.Size = 5
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte("data\n"))
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}
