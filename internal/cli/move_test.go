package cli

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

const (
	moveTop   = "7a000000-0000-4000-8000-000000000001" // cwd = git_root = proj
	moveSub   = "7a000000-0000-4000-8000-000000000002" // cwd proj/sub, git_root proj
	moveSib   = "7a000000-0000-4000-8000-000000000003" // projX: /a/b vs /a/bc
	moveDBOne = "7a000000-0000-4000-8000-000000000004" // DB-only, cwd proj/db
)

// moveFixture adds sessions under root/proj (which exists on disk) to fx.
func moveFixture(t *testing.T, fx *testfix.Fixture) (root string) {
	t.Helper()
	// Real paths, as Copilot records them (macOS t.TempDir is under the /var symlink).
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj, sib := filepath.Join(root, "proj"), filepath.Join(root, "projX")
	for _, d := range []string{filepath.Join(proj, "sub"), sib} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	yaml := map[string][2]string{
		moveTop: {proj, proj},
		moveSub: {filepath.Join(proj, "sub"), proj},
		moveSib: {sib, sib},
	}
	for id, p := range yaml {
		dir := filepath.Join(fx.Home, "session-state", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "id: " + id + "\ncwd: " + p[0] + "\ngit_root: " + p[1] + "\nbranch: main\nname: n\n  cwd: indented is ignored\nupdated_at: 2026-11-01T10:00:00Z\n"
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db := openDB(t, fx)
	defer db.Close()
	for id, cwd := range map[string]string{moveTop: proj, moveSub: filepath.Join(proj, "sub"), moveSib: sib, moveDBOne: filepath.Join(proj, "db")} {
		if _, err := db.Exec("INSERT INTO sessions (id, cwd, summary, updated_at) VALUES (?, ?, 's', '2026-11-01T10:00:00Z')", id, cwd); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func openDB(t *testing.T, fx *testfix.Fixture) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(fx.Home, "session-store.db"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func dbCwd(t *testing.T, fx *testfix.Fixture, id string) string {
	t.Helper()
	db := openDB(t, fx)
	defer db.Close()
	var cwd string
	if err := db.QueryRow("SELECT cwd FROM sessions WHERE id = ?", id).Scan(&cwd); err != nil {
		t.Fatal(err)
	}
	return cwd
}

func readYAML(t *testing.T, fx *testfix.Fixture, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fx.Home, "session-state", id, "workspace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// moveState captures what move may touch, for the "changes nothing" checks.
func moveState(t *testing.T, fx *testfix.Fixture, root string) string {
	t.Helper()
	var b strings.Builder
	for _, d := range []string{"proj", "moved", "projX"} {
		b.WriteString(d + ":" + boolStr(exists(filepath.Join(root, d))) + "\n")
	}
	for _, id := range []string{moveTop, moveSub, moveSib} {
		b.WriteString(readYAML(t, fx, id))
	}
	for _, id := range []string{moveTop, moveSub, moveSib, moveDBOne} {
		b.WriteString(dbCwd(t, fx, id) + "\n")
	}
	return b.String() + snapshot(t, fx)
}

func boolStr(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func checkMoved(t *testing.T, fx *testfix.Fixture, root string) {
	t.Helper()
	proj, moved, sib := filepath.Join(root, "proj"), filepath.Join(root, "moved"), filepath.Join(root, "projX")
	want := map[string][2]string{
		moveTop: {moved, moved},
		moveSub: {filepath.Join(moved, "sub"), moved},
		moveSib: {sib, sib},
	}
	for id, p := range want {
		body := "id: " + id + "\ncwd: " + p[0] + "\ngit_root: " + p[1] + "\nbranch: main\nname: n\n  cwd: indented is ignored\nupdated_at: 2026-11-01T10:00:00Z\n"
		if got := readYAML(t, fx, id); got != body {
			t.Errorf("%s workspace.yaml:\n%s\nwant:\n%s", id, got, body)
		}
	}
	for id, cwd := range map[string]string{moveTop: moved, moveSub: filepath.Join(moved, "sub"), moveSib: sib, moveDBOne: filepath.Join(moved, "db")} {
		if got := dbCwd(t, fx, id); got != cwd {
			t.Errorf("%s DB cwd = %q, want %q", id, got, cwd)
		}
	}
	// The fixture's own sessions are elsewhere and stay put.
	if got := dbCwd(t, fx, testfix.Stale); got != "/home/u/other" {
		t.Errorf("Stale DB cwd = %q", got)
	}
	if exists(proj) || !exists(filepath.Join(moved, "sub")) {
		t.Errorf("folder not moved: proj %v, moved/sub %v", exists(proj), exists(filepath.Join(moved, "sub")))
	}
}

func TestMove(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	out, errOut, code := run(fx, nil, "move", "-y", filepath.Join(root, "proj"), filepath.Join(root, "moved"))
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
	// Top, Sub and the DB-only session; not projX.
	if !strings.Contains(out, "3 sessions to repoint") || !strings.Contains(out, "Moved ") || !strings.HasSuffix(out, "Repointed 3 sessions.\n") || strings.Contains(out, moveSib) {
		t.Errorf("output:\n%s", out)
	}
	checkMoved(t, fx, root)
}

// OLD already moved by hand: only the sessions are repointed (decision 7).
func TestMoveManual(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	if err := os.Rename(filepath.Join(root, "proj"), filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := run(fx, nil, "mv", "-y", filepath.Join(root, "proj"), filepath.Join(root, "moved"))
	if code != 0 || !strings.Contains(out, "already moved") || strings.Contains(out, "Moved ") {
		t.Fatalf("exit %d:\n%s%s", code, out, errOut)
	}
	checkMoved(t, fx, root)
}

func TestMoveDryRun(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	before := moveState(t, fx, root)
	out, errOut, code := run(fx, nil, "move", "-n", filepath.Join(root, "proj"), filepath.Join(root, "moved"))
	if code != 0 || !strings.Contains(out, "3 sessions to repoint") || errOut != "Dry run: nothing moved.\n" {
		t.Errorf("exit %d:\n%s%s", code, out, errOut)
	}
	if moveState(t, fx, root) != before {
		t.Error("dry run changed something")
	}
}

func TestMoveRefused(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	proj := filepath.Join(root, "proj")
	before := moveState(t, fx, root)
	for _, tt := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{proj, filepath.Join(root, "projX")}, 1, "already exists"},
		{[]string{filepath.Join(root, "nope"), filepath.Join(root, "nope2")}, 1, "does not exist"},
		{[]string{proj, filepath.Join(proj, "inner")}, 2, "is OLD or inside it"},
		{[]string{proj, proj}, 2, "is OLD or inside it"},
		{[]string{proj}, 2, "accepts 2 arg(s)"},
		{[]string{proj, filepath.Join(root, "new\nname: x")}, 2, "control character"},
	} {
		_, errOut, code := run(fx, nil, append([]string{"move", "-y"}, tt.args...)...)
		if code != tt.code || !strings.Contains(errOut, tt.msg) {
			t.Errorf("%v: exit %d, stderr %q; want %d, %q", tt.args, code, errOut, tt.code, tt.msg)
		}
	}
	// No TTY and no -y: confirm fails, nothing changes.
	if _, _, code := run(fx, nil, "move", proj, filepath.Join(root, "moved")); code == 0 {
		t.Error("move without -y and without a TTY succeeded")
	}
	if moveState(t, fx, root) != before {
		t.Error("a refused move changed something")
	}
}

// An ongoing session under OLD blocks the whole move (decision 4).
func TestMoveOngoing(t *testing.T) {
	fx := testfix.Build(t)
	if !fx.OngoingLive {
		t.Skip("needs a live ongoing session")
	}
	root := moveFixture(t, fx)
	proj := filepath.Join(root, "proj")
	yaml := filepath.Join(fx.Home, "session-state", testfix.Ongoing, "workspace.yaml")
	b, err := os.ReadFile(yaml)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yaml, []byte(strings.Replace(string(b), "cwd: /srv/work", "cwd: "+proj, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	before := moveState(t, fx, root)
	_, errOut, code := run(fx, nil, "move", "-y", proj, filepath.Join(root, "moved"))
	if code != 1 || !strings.Contains(errOut, "1 ongoing session under ") {
		t.Errorf("exit %d: %s", code, errOut)
	}
	if moveState(t, fx, root) != before {
		t.Error("move with an ongoing session changed something")
	}
}

// With no DB the workspace.yaml files are still repointed.
func TestMoveNoDB(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	for _, f := range []string{"session-store.db", "session-store.db-wal", "session-store.db-shm"} {
		_ = os.Remove(filepath.Join(fx.Home, f))
	}
	if _, errOut, code := run(fx, nil, "move", "-y", filepath.Join(root, "proj"), filepath.Join(root, "moved")); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := readYAML(t, fx, moveSub); !strings.Contains(got, "cwd: "+filepath.Join(root, "moved", "sub")+"\n") {
		t.Errorf("workspace.yaml:\n%s", got)
	}
}

// A half-done repoint (YAML rewritten, DB not) is finished by rerunning the move (decision 9).
func TestMoveRerunFinishesDB(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	proj, moved := filepath.Join(root, "proj"), filepath.Join(root, "moved")
	if _, errOut, code := run(fx, nil, "move", "-y", proj, moved); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	db := openDB(t, fx)
	if _, err := db.Exec("UPDATE sessions SET cwd = ? WHERE id = ?", filepath.Join(proj, "sub"), moveSub); err != nil {
		t.Fatal(err)
	}
	db.Close()
	out, errOut, code := run(fx, nil, "move", "-y", proj, moved)
	if code != 0 || !strings.Contains(out, "already moved") {
		t.Fatalf("exit %d:\n%s%s", code, out, errOut)
	}
	checkMoved(t, fx, root)
}

// OLD given through a symlink still finds the sessions, which record the real path (review F1).
func TestMoveSymlink(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	// NEW through the link too: sessions get the resolved path (review R1).
	out, errOut, code := run(fx, nil, "move", "-y", filepath.Join(link, "proj"), filepath.Join(link, "moved"))
	if code != 0 || !strings.Contains(out, "3 sessions to repoint") {
		t.Fatalf("exit %d:\n%s%s", code, out, errOut)
	}
	checkMoved(t, fx, root)
}

// OLD a symlink is refused: renaming it would move only the link, which may dangle (R2, P1).
func TestMoveSymlinkOld(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	link := filepath.Join(root, "link")
	if err := os.Symlink("proj", link); err != nil {
		t.Skip("no symlinks:", err)
	}
	if err := os.Mkdir(filepath.Join(root, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := moveState(t, fx, root)
	_, errOut, code := run(fx, nil, "move", "-y", link, filepath.Join(root, "other", "link2"))
	if code != 2 || !strings.Contains(errOut, "is a symlink") {
		t.Errorf("exit %d, stderr %q; want 2, is a symlink", code, errOut)
	}
	if moveState(t, fx, root) != before {
		t.Error("a refused move changed something")
	}
}

// A symlink whose target holds a control character is refused before anything moves (G1).
func TestMoveSymlinkControl(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("newline is not allowed in Windows file names")
	}
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	odd := filepath.Join(t.TempDir(), "a\nb")
	if err := os.Mkdir(odd, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "lnk")
	if err := os.Symlink(odd, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	before := moveState(t, fx, root)
	_, errOut, code := run(fx, nil, "move", "-y", filepath.Join(root, "proj"), filepath.Join(link, "moved"))
	if code != 2 || !strings.Contains(errOut, "control character") {
		t.Errorf("exit %d, stderr %q; want 2, control character", code, errOut)
	}
	if moveState(t, fx, root) != before {
		t.Error("a refused move changed something")
	}
}

// Quoted values are read and kept quoted; a NEW that YAML would misread gets quoted (F2, F3).
func TestMoveQuoted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(`":" is not allowed in Windows file names`)
	}
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	odd := filepath.Join(root, "a: b")
	if err := os.Mkdir(odd, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := filepath.Join(fx.Home, "session-state", moveSib, "workspace.yaml")
	body := "id: " + moveSib + "\ncwd: '" + odd + "'\ngit_root: \"" + odd + "\"\nname: 'x: y'\n"
	if err := os.WriteFile(yaml, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "c #d")
	out, errOut, code := run(fx, nil, "move", "-y", odd, dst)
	if code != 0 || !strings.Contains(out, "Repointed 1 session.") {
		t.Fatalf("exit %d:\n%s%s", code, out, errOut)
	}
	want := "id: " + moveSib + "\ncwd: '" + dst + "'\ngit_root: '" + dst + "'\nname: 'x: y'\n"
	if got := readYAML(t, fx, moveSib); got != want {
		t.Errorf("workspace.yaml:\n%s\nwant:\n%s", got, want)
	}
	// list --cwd reads the quoted value too (the DB row of moveSib points elsewhere).
	if out, _, _ := run(fx, nil, "list", "-q", "--cwd", dst); !strings.Contains(out, moveSib) {
		t.Errorf("list --cwd %q misses the quoted session: %q", dst, out)
	}
}

// One unwritable session is reported; the others are still repointed; exit 1 (decision 9).
func TestMoveFailedSession(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs Unix permission bits and a non-root user")
	}
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	dir := filepath.Join(fx.Home, "session-state", moveSub)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	out, errOut, code := run(fx, nil, "move", "-y", filepath.Join(root, "proj"), filepath.Join(root, "moved"))
	if code != 1 || !strings.Contains(errOut, "failed: "+moveSub+": ") || !strings.Contains(errOut, "repointed 2 of 3 sessions") {
		t.Fatalf("exit %d:\n%s%s", code, out, errOut)
	}
	if got := readYAML(t, fx, moveTop); !strings.Contains(got, "cwd: "+filepath.Join(root, "moved")+"\n") {
		t.Errorf("sibling not repointed:\n%s", got)
	}
	// Rerun after fixing the permissions finishes the job.
	_ = os.Chmod(dir, 0o755)
	if _, errOut, code := run(fx, nil, "move", "-y", filepath.Join(root, "proj"), filepath.Join(root, "moved")); code != 0 {
		t.Fatalf("rerun exit %d: %s", code, errOut)
	}
	checkMoved(t, fx, root)
}

// Answering n at the prompt changes nothing (review F12).
func TestMoveConfirmNo(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	before := moveState(t, fx, root)
	_, errOut, code := runIn(fx, nil, "n\n", true, "move", filepath.Join(root, "proj"), filepath.Join(root, "moved"))
	if code != 0 || !strings.HasSuffix(errOut, "Aborted.\n") {
		t.Errorf("exit %d: %q", code, errOut)
	}
	if moveState(t, fx, root) != before {
		t.Error("declined move changed something")
	}
}

// sideEffectReader runs fn on its first Read, then answers like a user typing.
type sideEffectReader struct {
	fn   func()
	r    io.Reader
	done bool
}

func (s *sideEffectReader) Read(p []byte) (int, error) {
	if !s.done {
		s.done = true
		s.fn()
	}
	return s.r.Read(p)
}

// A symlink on NEW's path retargeted during the prompt stops the move (S1).
func TestMoveRecheckNewTarget(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	for _, d := range []string{"safe", "other"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lnk := filepath.Join(root, "lnk")
	if err := os.Symlink(filepath.Join(root, "safe"), lnk); err != nil {
		t.Skip("no symlinks:", err)
	}
	retarget := func() { _ = os.Remove(lnk); _ = os.Symlink(filepath.Join(root, "other"), lnk) }
	stdin := &sideEffectReader{fn: retarget, r: strings.NewReader("y\n")}
	_, errOut, code := runCtx(context.Background(), fx, nil, stdin, true, "move", filepath.Join(root, "proj"), filepath.Join(lnk, "moved"))
	if code != 1 || !strings.Contains(errOut, "changed while confirming") || !exists(filepath.Join(root, "proj")) {
		t.Errorf("exit %d: %s", code, errOut)
	}
}

// A NEW created while the prompt waits is caught before anything moves (review F4).
func TestMoveRecheckAfterConfirm(t *testing.T) {
	fx := testfix.Build(t)
	root := moveFixture(t, fx)
	moved := filepath.Join(root, "moved")
	stdin := &sideEffectReader{fn: func() { _ = os.Mkdir(moved, 0o755) }, r: strings.NewReader("y\n")}
	_, errOut, code := runCtx(context.Background(), fx, nil, stdin, true, "move", filepath.Join(root, "proj"), moved)
	if code != 1 || !strings.Contains(errOut, "changed while confirming") || !exists(filepath.Join(root, "proj")) {
		t.Errorf("exit %d: %s", code, errOut)
	}
	if got := dbCwd(t, fx, moveTop); got != filepath.Join(root, "proj") {
		t.Errorf("DB cwd changed to %q", got)
	}
}

// A session that goes ongoing under OLD while the prompt waits stops the move (review R4).
func TestMoveRecheckOngoing(t *testing.T) {
	fx := testfix.Build(t)
	if !fx.OngoingLive {
		t.Skip("needs a live ongoing session")
	}
	root := moveFixture(t, fx)
	proj := filepath.Join(root, "proj")
	yaml := filepath.Join(fx.Home, "session-state", testfix.Ongoing, "workspace.yaml")
	b, err := os.ReadFile(yaml)
	if err != nil {
		t.Fatal(err)
	}
	stdin := &sideEffectReader{fn: func() {
		_ = os.WriteFile(yaml, []byte(strings.Replace(string(b), "cwd: /srv/work", "cwd: "+proj, 1)), 0o600)
	}, r: strings.NewReader("y\n")}
	_, errOut, code := runCtx(context.Background(), fx, nil, stdin, true, "move", proj, filepath.Join(root, "moved"))
	if code != 1 || !strings.Contains(errOut, "1 ongoing session under ") || !exists(proj) {
		t.Errorf("exit %d: %s", code, errOut)
	}
	if got := dbCwd(t, fx, moveTop); got != proj {
		t.Errorf("DB cwd changed to %q", got)
	}
}
