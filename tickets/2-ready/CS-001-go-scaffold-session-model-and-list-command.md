---
id: CS-001
title: Go scaffold, session model and list command
project: copilot-sessions
depends-on: []
spawned-by: []
impact: critical
complexity: high
cost: L
---

# CS-001 — Go scaffold, session model and list command

## Outcome

After this ships, `copilot-sessions list` (alias `ls`) from the Go binary prints the same
table, `-q` IDs and `--json` output as the Python script for the same `COPILOT_HOME`. It
supports every filter. Later commands build on its session model.

## Description

This ticket is the foundation of the Go rewrite in `development/spec.md`. The spec is the
contract. The reference implementation is the Python script, copied into this repo as
`development/copilot-sessions.sh` (a Python 3 file despite the `.sh` name). It is the
reference where the spec is unclear.

In scope:

- **Scaffold (spec §2).** One module and one binary `copilot-sessions`, using cobra and
  `modernc.org/sqlite` (no CGO). The `go` directive is 1.26, because
  `modernc.org/sqlite` v1.61.1 needs it; this still meets the spec's "Go 1.22 or later". The
  layout is `cmd/copilot-sessions/` plus `internal/` packages. A `main.Version` variable
  feeds cobra's `--version`. The `justfile` (build, test, install) is CS-006's job.
- **Root command and help (§6 global, §7).** The root description. The help also shows the
  DATE formats, the current `COPILOT_HOME`/`COPILOT_BACKUP_DIR` values with their names, and
  the three status definitions. Errors print as `error: <msg>` and exit 1. Usage errors exit
  2. Ctrl-C prints `\nAborted.` and exits 130.
- **Paths (§3).** Read `COPILOT_HOME` and `COPILOT_BACKUP_DIR` with their defaults. A
  session dir is any non-hidden directory under `session-state/`. `.session-operation-locks`
  is never a session.
- **Data model (§4).** Line-based `workspace.yaml` parsing with no YAML library. Merge with
  the `sessions` table, which also yields DB-only sessions. Timestamp parsing strips quotes
  and treats a timestamp without a zone as UTC.
- **Status (§4.2).** `ongoing`, `stale` and `closed` from `inuse.<pid>.lock` and
  `/proc/<pid>/cmdline`. `busy` comes from `open-sessions-state.json`. `current` comes from
  the ancestor-PID set (`/proc/<pid>/stat`, the field after the last `)`).
- **Empty (§4.3)** and **size (§4.4)**, each computed only when needed.
- **DATE and filter parsing (§5).** Includes the month-end clamp, ID-prefix errors, and the
  exact error texts and exit codes.
- **`list` (§6.1).** Sorting, row format, summary line, `-q`, `--json`, `--json -q` (size
  null), and human-readable sizes.
- **Injection seams (§9).** The TTY check, the stdin reader, the clock and the ancestor-PID
  provider. Later tickets reuse them.
- **Test fixture (§9).** Built in code: a WAL-mode DB with the §10 schema including FTS5
  `search_index`, all the listed session kinds (one ongoing via a copied `sleep` binary
  named `copilot-test`), the lock dir, and the UI state files. Tests always point both env
  vars at `t.TempDir()`. This ticket covers §9 cases 1–4.

Soft couplings: CS-002, CS-003, CS-004 and CS-005 all build on this module, its fixture and
its seams.

## Implementation Plan

### 0. Feature branch (mandatory)

```
cd /Users/nka/Projects/codcod/copilot-sessions
git checkout main && git pull --ff-only
git checkout -b feat/CS-001-go-scaffold-session-model-and-list
```

Commit WIP locally as you go. Do not push and do not open a merge request; that happens
at the end of the review after user approval. This is a root-path child (`path = "."`), so
tidy the WIP commits into atomic ones before review. Before any push, this check must print
nothing: `git fetch origin main && git diff --name-only origin/main...HEAD | grep
'^tickets/'`.

### Prerequisite gate (hard)

- `development/copilot-sessions.sh` (the reference script) is committed on `main`.
- The working tree is clean on `main`.
- Docker is available for the Linux run in the acceptance test. CS-006's `just test-linux`
  doesn't exist yet, so use the raw `docker run` command below.

### Confirmed design decisions (do not deviate without asking)

1. **Package layout is `cmd/` plus `internal/`.** The packages are
   `cmd/copilot-sessions/main.go`, `internal/cli` (cobra commands, output, exit codes),
   `internal/session` (the model, workspace parsing, DB read, status, size, empty),
   `internal/filter` (DATE, status list and the filters) and `internal/testfix` (the
   fixture builder, imported only by tests). Don't add more packages. CS-006's `build`
   recipe targets `./cmd/copilot-sessions`.
2. **The spec wins, then the script.** Spec text is binding. Where the spec is silent, match
   `development/copilot-sessions.sh` line for line. Where they conflict, follow the spec and
   note the difference in the finish summary. Known conflicts:
   - A timestamp without a zone is UTC everywhere. The script treats such timestamps as
     local time when it prints them.
   - DATE accepts only `YYYY-MM-DD`. Python's `fromisoformat` also accepts other forms such
     as `20261106`.
3. **The seams live in one struct.** `cli.Env{Home, BackupDir string; Stdin io.Reader;
   Stdout, Stderr io.Writer; IsTTY func() bool; Now func() time.Time; AncestorPIDs
   func() map[int]bool}`. `cli.DefaultEnv()` fills it from the process environment.
   - `IsTTY` checks `os.Stdin.Stat()` for `os.ModeCharDevice`, so no `x/term` dependency is
     needed.
   - `session.Load` takes the ancestor set and a `withSize bool` as arguments and has no
     globals.
   - The `IsTTY` and `Stdin` fields are added now even though `list` doesn't use them,
     because CS-002, CS-003 and CS-004 depend on them.
4. **Exit codes come from error types.**
   - `cli.Execute(env, args) int` returns the exit code, and `main` calls `os.Exit` with it.
   - Usage errors are a `usageError` type and exit 2. These are flag parse errors
     (through `SetFlagErrorFunc`), invalid DATE or `--status` values, bad argument counts,
     unknown commands, and running the root with no command (argparse requires a command).
   - Every other error exits 1.
   - Set `SilenceErrors` and `SilenceUsage`, and print every error as `error: <msg>` to
     stderr. The message must contain the exact spec text, such as `invalid date 'x'; use
     ...`. Text cobra adds around it is allowed (spec §11).
5. **DATE and status are `pflag.Value` types.** Parsing happens in `Set`, so invalid input
   fails during flag parsing. The `today` the parser uses comes from `Env.Now` in the local
   timezone, converted to a date.
6. **Ctrl-C is handled in `main`.** A `signal.Notify` goroutine prints `\nAborted.` to
   stdout and calls `os.Exit(130)`. It's a ponytail-level shortcut, and its ceiling is that
   deferred cleanup doesn't run. CS-003 and CS-004 must switch to context cancellation if
   they leave temp files behind.
7. **The DB is opened only if it exists, and is never written.** Check that
   `session-store.db` is a regular file before `sql.Open`, so the driver doesn't create
   one. Use the DSN `file:<path>?_pragma=busy_timeout(30000)`. If the `turns` table is
   missing, the "has turns" set is empty; check `sqlite_master` for it. A missing
   `sessions` table is an error, as in the script.
8. **Merge, status and fallback follow the script.**
   - A directory session's `updated` is `updated_at`, else `created_at`, else the
     directory mtime, so it is never unknown. A DB-only session's `updated` can be unknown.
   - DB values fill only empty fields.
   - Lock files are `inuse.*.lock`. A name whose middle part isn't a number gives no PID.
     If lock files exist but none gives a running copilot PID, the status is `stale`.
   - `busy` is true when `open-sessions-state.json[id].working` is truthy. If the file is
     missing or broken, treat it as `{}`.
   - The ancestor walk starts at `os.Getpid()` and stops before PID 1, or at the first
     read error. On macOS it returns only the process's own PID.
9. **Timestamps are parsed by trying a fixed list of layouts.** Strip quotes and spaces,
   then try RFC 3339 with an optional fraction, the same without a zone (as UTC), and both
   with a space instead of `T`. If nothing matches, the value is unknown.
10. **The empty check scans in chunks.** Stream `events.jsonl` in 64 KiB chunks and keep a
    `len(pattern)-1` overlap between chunks, so line length doesn't matter. Don't use
    `bufio.Scanner`, which has a 64 KiB line limit, and don't read the whole file.
11. **JSON output.**
    - Use a `json.Encoder` with `SetIndent("", "  ")` and `SetEscapeHTML(false)`.
    - Always pass a non-nil slice, so an empty result prints `[]`, not `null`.
    - `updated_at` is `t.UTC().Format(time.RFC3339Nano)`, or null.
    - `busy` and `size` are pointers, so they can be null.
    - Python prints non-ASCII as `\uXXXX`; Go prints UTF-8. Parity checks compare the
      parsed JSON, not the bytes.
12. **Size counts regular files only.** Use `filepath.WalkDir`, sum
    `Lstat().Size()` of `d.Type().IsRegular()` entries, and skip entries that error.
13. **Names are truncated by rune.** Use `[]rune(name)[:60]`. Pad columns with `fmt`
    widths (`%-16s`, `%-14s`, `%6s`, `%-36s`), as in the script's `print_sessions`. The
    `SIZE` column appears only when sizes were computed. `list` always computes them unless
    `-q` is set.
14. **Tests that need `/proc` skip outside Linux** (`AGENTS.md` Platform note). These are
    the ongoing status, the real ancestor walk and the `(this session)` lock match against
    the helper process. Everything else runs on macOS.
15. **Help strings are ported verbatim from `add_filters` and the subparser definitions in
    the script.** The root `Long` text has the one-line description, the DATE formats, the
    three status lines from the script's docstring, and `Copilot home: <path>
    (COPILOT_HOME). Backups: <path> (COPILOT_BACKUP_DIR).`

### Tasks

#### Task 1 — Module and entry point

- `go mod init github.com/codcod/copilot-sessions`, then `go get
  github.com/spf13/cobra@v1.10.2 modernc.org/sqlite@v1.61.1`. Run `go mod tidy`; the `go`
  directive ends up at 1.26.
- `cmd/copilot-sessions/main.go` defines `var Version = "dev"`, installs the SIGINT
  handler (decision 6) and calls `os.Exit(cli.Execute(cli.DefaultEnv(), os.Args[1:],
  Version))`.

#### Task 2 — `internal/filter`

- `ParseDate(s string, today time.Time) (civil date, error)`. Represent the date as
  `time.Time` at local midnight, or as a small `Date{Y, M, D}`; choose one and use it
  everywhere.
  - `Nm` clamps to the last day of the month, like the script's `months_before`. Use
    `time.Date` normalization plus a check.
- `ParseStatuses(s) (map[string]bool, error)`. Trim and lowercase each item. An empty
  result is an error.
- The `pflag.Value` wrappers `DateFlag` and `StatusFlag` (decision 5).
- `Filters{IDs []string; Until, Since *Date; Status map[string]bool; Cwd, Grep string;
  Empty bool}`.
  - `Filters.Any()` reports whether any filter is set.
  - `Filters.Describe()` builds the scope string in the spec §6.2 order. CS-002 uses it;
    port it now because it only reads the struct.
  - `Apply(all []session.Session, f, hasTurns func() map[string]bool)
    ([]session.Session, error)`. ID prefixes resolve against **all** sessions. The errors
    are `no session matches ID '<p>'` and `ID '<p>' is ambiguous: <≤5 sorted>`. `--cwd`
    expands `~`, then applies `filepath.Abs` and `Clean`. A session's cwd matches if it
    equals the root or has the prefix `root + "/"`, and an empty cwd never matches.
    `hasTurns` is called only for `--empty`.

#### Task 3 — `internal/session`

- `type Session struct{ID, Name, Cwd string; Updated *time.Time; Dir string /* "" for
  DB-only */; Status string; Busy, Current bool; Size *int64}`.
- `Load(home string, ancestors map[int]bool, withSize bool) ([]Session, error)`:
  1. List the directories (non-hidden, directories only).
  2. Parse `workspace.yaml` line by line (spec §4.1).
  3. Read the DB (decision 7) and merge (decision 8).
  4. Read the lock PIDs and check them against `/proc/<pid>/cmdline` for the bytes
     `copilot`.
  5. Read `open-sessions-state.json`.
  6. Compute sizes if `withSize` is set.
- `DefaultAncestors()` walks `/proc/<pid>/stat`. The parent PID is
  `strings.Fields(after last ")")[1]`.
- `HasTurns(home) (map[string]bool, error)` and `IsEmpty(s, turns) bool`, using the
  chunked scan from decision 10.
- `Sort(sessions)` sorts by `Updated` ascending, with unknown first. Make it stable, with
  the ID as tie-breaker so output is deterministic. Python's sort is stable on dict
  order, but Go map order is random.
- `HumanSize(n int64) string` matches the script's `human_size` exactly, including
  `1023B`, `1.0K` and values above 1024G staying in `G`.

#### Task 4 — `internal/cli`

- `Env` and `DefaultEnv` (decision 3).
- `Execute`: the root command (decisions 4 and 15), plus `--version` from the version
  argument.
- `list` with the `ls` alias. Its flags are the filters plus `--json` and `-q/--quiet`;
  `--json` wins over `-q`.
  - With `-q`, sizes aren't computed.
  - The no-match text is `No sessions match.`, printed only by the table form.
  - The summary line is `<n> of <total> sessions (<counts>), <size>.`. Counts appear in
    the order ongoing, stale, closed, and zero counts are left out.
  - The row suffixes are ` (this session)` and ` (db only)`.
- Put the table printer in its own function. CS-002 reuses it with `showSize=false` for
  `clean -q`.

#### Task 5 — `internal/testfix`

`Build(t) *Fixture` creates `t.TempDir()` homes, calls `t.Setenv` for `COPILOT_HOME` and
`COPILOT_BACKUP_DIR`, and returns the session IDs by role. Contents:

- **DB.** `session-store.db` in WAL mode (`PRAGMA journal_mode=WAL`) with the spec §10
  schema.
  - Tables the spec elides get `(id INTEGER PRIMARY KEY, session_id TEXT NOT NULL
    REFERENCES sessions(id))`.
  - Add the untouched tables `forge_skill_proposals`, `dynamic_context_items` and
    `schema_version`.
  - Add `search_index` rows for the titled session.
- **Sessions.** Fixed IDs. Two of them share the prefix `aaaa`, for the ambiguous-prefix
  test.
  - **titled:** a `name`, cwd `/home/u/proj`, `updated_at` with `Z`, `events.jsonl` with
    `"type":"user.message"`, and DB turns.
  - **empty-no-events:** cwd `/home/u/projX`, which must not match `--cwd /home/u/proj`.
    It has no `events.jsonl`.
  - **empty-start-shutdown:** cwd `/home/u/proj/sub`. Only a quoted `created_at`.
    `events.jsonl` holds only `session.start` and `session.shutdown`.
  - **stale:** no `name` in the YAML but a DB `summary`, to test that the DB fills empty
    fields. Lock file `inuse.999999999.lock`.
  - **ongoing:** on Linux, the lock PID is a helper process: `sleep` is copied to
    `<tmp>/copilot-test` and started with `300`, and `t.Cleanup` kills it. On other systems
    the lock PID is a dead PID, so the session reads as stale and tests skip
    (decision 14). `open-sessions-state.json` marks it `working: true`. Give it a name
    longer than 60 runes that contains a multibyte rune.
  - **DB-only:** in `sessions` only, with a `summary`, a `cwd` and an `updated_at`.
- **Spread of dates.** The sessions fall on at least three different local dates.
- **Ignored entries.** `session-state/.session-operation-locks/x.lock`, a hidden
  `.other/` directory and a plain file in `session-state/` must not be read as sessions.
- **UI state.** `open-sessions-state.json` also holds an extra key, and
  `sidebar-sessions-state/a.json` holds `sessionIds` plus another key. CS-002 needs both.

#### Task 6 — Tests (spec §9 cases 1–4)

Write table-driven tests next to each package, plus `internal/cli/list_test.go`, which
runs `Execute` in-process with an `Env` built on the fixture. In `TestMain`, set
`time.Local = time.FixedZone("XST", 2*3600)` so that local-date logic is exercised and
the output is deterministic.

1. `filter`:
   - every DATE form
   - `2026-03-31` minus `1m` gives `2026-02-28`, and a leap year gives `-29`
   - case and spaces are ignored
   - invalid input gives the spec text, and exits 2 through `Execute`
   - `--status` parsing and its error
2. `session`:
   - closed, stale, ongoing (Linux only) and DB-only sessions
   - hidden entries and the lock dir are ignored
   - the DB fills empty fields
   - mtime fallback and quoted, zone-less timestamps
   - the ancestor walk on Linux includes `os.Getpid()`
   - `HumanSize` table
   - `IsEmpty` for all three event cases, including one line longer than 64 KiB
3. `filter.Apply`:
   - each filter alone, and AND combinations
   - the `/a/b` vs `/a/bc` cwd boundary and `~` expansion
   - an unknown `updated` never matches `--until` or `--since`
   - prefix errors for no match and an ambiguous match (exit 1 through `Execute`)
4. `cli list`:
   - the exact header and row strings
   - the ` (this session)` suffix, by injecting `AncestorPIDs` with the stale session's
     lock PID; the match uses the PID, not the status, so this runs on any OS
   - the ` (db only)` suffix and the `-` size
   - the summary counts
   - `No sessions match.`
   - `-q` IDs in sorted order, with a check that no size is computed (`Size == nil`)
   - `--json` keys and types
   - `--json -q` with `size: null`
   - `--json` with no match prints `[]`
5. **Optional parity test** (`internal/cli/parity_test.go`). It skips unless `python3` is
   on PATH. It runs `python3 -I development/copilot-sessions.sh` with `TZ=XST-2` and the
   fixture's env vars. Compare `list` byte for byte except the `SIZE` column, with stale
   and ongoing sessions treated alike on macOS. Compare `list --json -q` as parsed JSON,
   with `updated_at` compared as instants. Also run `list` with each filter, using
   absolute dates only.

### Acceptance test

From the repo root:

```
gofmt -l . | (! grep .)                       # prints nothing
go vet ./...                                   # exit 0
go test ./...                                  # exit 0 on macOS (Linux-only tests skip)
docker run --rm -v "$PWD":/src -w /src \
  -v go-mod:/go/pkg/mod -v go-build:/root/.cache/go-build \
  golang:1.26 go test -count=1 ./...           # exit 0, ongoing + ancestor tests run
CGO_ENABLED=0 go build -o copilot-sessions ./cmd/copilot-sessions   # exit 0
./copilot-sessions --help                      # description, DATE formats, statuses, both env vars
./copilot-sessions list --until nope; echo $?  # stderr has "invalid date 'nope'; use ...", prints 2
COPILOT_HOME=$(mktemp -d) ./copilot-sessions list   # "No sessions match."
```

`go test -run Parity -v ./internal/cli` must report PASS, not SKIP, on a machine that has
`python3`.

### Docs update (mandatory when user-facing)

- Create `README.md` with one paragraph saying what the tool is (from the root
  description) and a **Development** section: `go test ./...`, the `docker run` command
  above for Linux tests, and a pointer to the `AGENTS.md` Platform note.
- Install and completion instructions are CS-005's job. CS-006 replaces the raw commands
  with `just` recipes.
- No changelog is kept.

### Finish (mandatory)

1. The acceptance test is green.
2. `README.md` is created.
3. Write a summary: files touched, decisions applied, every spec-over-script difference
   found (decision 2), and anything deferred.
4. Tidy the WIP commits into atomic Conventional Commits. This is a root-path child, so
   keep that history rather than squashing. A suggested split:
   - `build: scaffold Go module and entry point (CS-001)`
   - `feat(session): read sessions, status and size (CS-001)`
   - `feat(filter): parse dates and session filters (CS-001)`
   - `feat(list): add list command (CS-001)`
   - `test: add fixture and list tests (CS-001)`
5. Commit locally, without pushing. `pickle ticket move CS-001 in-review --reason
   "acceptance green"`.

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md (Go rewrite of copilot-sessions); foundation ticket
- 2026-11-06 — TO DO → READY: plan complete
