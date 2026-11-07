---
id: CS-003
title: "backup command: archive, list and retention"
project: copilot-sessions
depends-on: [CS-001]
spawned-by: []
impact: high
complexity: medium
cost: M
---

# CS-003 — backup command: archive, list and retention

## Outcome

After this ships, `copilot-sessions backup` writes a `.tar.gz` of the Copilot session data
that the Python script can read. The archive holds a consistent DB snapshot that includes
data still in the WAL. `backup --list` and `--keep N` manage the backup directory.

## Description

Implements `development/spec.md` §6.3.

- **Flags.** `FILE`, `-l/--list`, `-k/--keep N`, `--no-create`, `-y/--yes`, `--json` (only
  with `--list`), `-q/--quiet`. Use the four validation messages from §6.3 (exit 1).
- **Archive format.** It must stay compatible with the Python version in both directions.
  - gzip tar with mode `0600`. Create parent directories as needed.
  - The top-level entries are `session-state/`, `session-store.db`,
    `open-sessions-state.json` and `sidebar-sessions-state/`, each only if it exists.
  - Exclude `.session-operation-locks`.
  - The DB entry is a snapshot made with `VACUUM INTO` or the online backup API into a temp
    file. Never include the `-wal` or `-shm` files.
  - Keep file modes and mtimes in the tar headers.
- **Default path.** `$COPILOT_BACKUP_DIR/copilot-sessions-YYYYmmdd-HHMMSS.tar.gz` in local
  time. On success, print `Backup created: <path> (<N> session(s), <size>)`. With `-q`,
  print only the path.
- **Retention.** Only `copilot-sessions-*.tar.gz` files count. Sort them by file name,
  newest first. Print the "nothing to remove" message, or the table followed by a
  confirmation (the helper from CS-002) and `Deleted <N> old backup(s).`
- **Backup table and `--json`.** `CREATED` comes from the timestamp in the file name, or
  the mtime if that does not parse. With no backups, print `No backups in <dir>`. CS-004's
  interactive picker and CS-005's restore completion reuse the backup listing.
- **Tests.** §9 case 10.

Soft couplings: builds on CS-001. Reuses the confirmation helper from CS-002; if CS-002 has
not landed yet, this ticket adds the helper.

## Implementation Plan

### 0. Feature branch (mandatory)

Before any change, create the feature branch. The child is this repository (`path = "."`):

```
git checkout main && git pull --ff-only
git checkout -b feat/CS-003-backup-command
```

Do all work on this branch and commit locally as you go; WIP commits are encouraged. Do not push
or open a merge request without explicit user approval. This is a root-path child, so before
presenting, tidy the WIP commits into atomic Conventional Commits; the default is to keep that
history rather than squash. Ticket and board bookkeeping is never committed on this branch
(in-tree layout, rules §0). Before pushing, check that the remote base is not behind:
`git fetch origin main && git diff --name-only origin/main...HEAD | grep '^tickets/'` must print
nothing. Merging is always the human's.

> If a dependency's branch is not merged to `main`, stop and tell the human rather than
> building on top of it.

### Prerequisite gate (hard)

- CS-001 is in `6-done/` and merged to `main` (PR #1, `533c9c0`). Holds as of 2026-11-07.
- The working tree is clean, and `go test ./...` is green on `main`.
- CS-002 merged (PR #3, `309e7ee`): reuse `confirm`/`readLine` (`internal/cli/confirm.go`)
  and `plural` (`internal/cli/cli.go`).

### Confirmed design decisions (do not deviate without asking)

1. **The Python script is the reference where the spec is silent.** That means
   `create_backup`, `apply_retention`, `cmd_backup`, `list_backups`, `backup_time` and
   `print_backups` (lines 512–603).
2. **The archive logic lives in a new package `internal/backup`;** the command lives in
   `internal/cli/backup.go`. CS-004 adds restore's extraction to the same package.
3. **The DB snapshot uses `VACUUM INTO ?` with the temp path as a bound parameter,** run on
   the connection from `session.OpenDB(home)` (busy timeout 30 s). The temp path is
   `<os.MkdirTemp("", "copilot-sessions-")>/session-store.db`, removed afterwards. With no DB,
   there is no DB entry, as in the script. `VACUUM INTO` reads committed WAL content. The
   copy's journal mode may differ from the live DB's; that is accepted, since content and
   schema are identical and Copilot sets its own mode.
4. **The tar walk mirrors `tarfile.add`.** For each item of
   `[session-state, session-store.db, open-sessions-state.json, sidebar-sessions-state]`
   that exists (`os.Lstat`), walk it with `filepath.WalkDir`. Symlinks are stored as symlink
   entries and never followed; that includes a symlinked session dir. Entry names are
   `<item>/<rel>` with forward slashes. Directory entries get a trailing `/`, the
   `tar.FileInfoHeader` convention; Python strips it on read. Any name with the prefix
   `session-state/.session-operation-locks` is skipped (`fs.SkipDir` for the dir), matching
   the script's prefix test. Headers come from `tar.FileInfoHeader`, which keeps mode and
   mtime. Sockets and other types `FileInfoHeader` refuses are skipped silently, as
   `tarfile` does. Hard links are stored as regular files, which is accepted; both tools
   restore either way. Leave the tar format at Go's default; Python reads USTAR and PAX.
5. **The target file is created with `os.OpenFile(…, O_CREATE|O_TRUNC|O_WRONLY, 0o600)`
   followed by `Chmod(0o600)`,** so a pre-existing file with other bits also ends at `0600`.
   Its parent is created with `os.MkdirAll(dir, 0o755)`. On error the partial target is
   left, as in the script.
6. **Backups are listed with `os.ReadDir(dir)` plus `filepath.Match("copilot-sessions-*.tar.gz",
   name)`, sorted by name descending.** `filepath.Glob` is not used, because a `[` in
   `COPILOT_BACKUP_DIR` would be read as a pattern. A missing dir means no backups.
   Directories that match the pattern are skipped.
7. **`CREATED` is `time.ParseInLocation("20060102-150405", stamp, time.Local)`; if that
   fails, it is the file mtime in local time.** The table shows `2006-01-02 15:04`. The
   JSON `created` is `2006-01-02T15:04:05`, plus `.000000` microseconds only when they are
   non-zero (Python `isoformat`). JSON is written with a 2-space indent and no HTML
   escaping; non-ASCII paths are raw UTF-8, an accepted text difference.
8. **The flags mirror the script.** `FILE` is an optional single positional
   (`cobra.MaximumNArgs(1)`, usage error exit 2 beyond that) and goes through
   `filter.ExpandUser`. Whether `--keep` was given is `cmd.Flags().Changed("keep")`.
   Validation runs in the script's order, and each error is a plain error (exit 1):
   - `--list` with FILE, `--keep` or `--no-create`;
   - `--json` without `--list`;
   - `--no-create` without `--keep`;
   - `--keep` < 1.
9. **The default file name uses `env.Now().Local()`,** so tests are deterministic.
10. **Exports needed from CS-001's packages are renames, not new code:**
    `session.sessionDirs` → `session.Dirs`, `session.openDB` → `session.OpenDB`,
    `filter.expandUser` → `filter.ExpandUser`. Update their callers.

### Tasks

#### Task 1 — exports

Apply decision 10's renames in `internal/session/session.go`, `internal/filter/filter.go`
and their callers (session.go, `internal/session/delete.go`, filter.go) and tests.

#### Task 2 — `internal/backup/backup.go`

- `var Items = []string{session.StateDir, session.DBFile, session.OpenState, session.SidebarDir}`.
- `func Create(home, target string) error` implements decisions 3–5.
- `func List(dir string) ([]string, error)` implements decision 6 and returns full paths.
- `func Created(path string) time.Time` implements decision 7.

#### Task 3 — `backup` command

Create `internal/cli/backup.go` with `newBackup(env Env)`: `Use: "backup [FILE]"`,
`Short: "create, list or rotate backups"`. Port the flag help from the script (lines
705–712). Register it in `Execute`. Its run function:

1. Validation (decision 8).
2. `--list` prints JSON (`[]` when empty), or `No backups in <dir>`, or `printBackups`.
   `printBackups(w, paths)` goes in `backup.go` for CS-004 to reuse. Header
   `fmt.Sprintf("  %2s  %-16s  %6s  FILE", "#", "CREATED", "SIZE")`, rows
   `"  %2d  %s  %6s  %s"` with `session.HumanSize`.
3. Unless `--no-create`: target is FILE or
   `<BackupDir>/copilot-sessions-<Now: 20060102-150405>.tar.gz`. Call `backup.Create`. Then
   print the path with `-q`, otherwise
   `Backup created: <path> (<plural(len(Dirs), "session")>, <HumanSize(target size)>)`.
4. If `--keep` is set: `old := List(dir)[keep:]`. If there are none, print (unless `-q`)
   `Nothing to remove: at most <N> backups in <dir>.`. Otherwise print (unless `-q`)
   `Backups to remove (keeping the newest <N>):` and the table, then
   `confirm(env, "Delete <plural(len(old), "old backup")>?", yes)`. On false print
   `Aborted.`. Otherwise remove each file and print `Deleted <plural(…)>.`

#### Task 4 — tests

Create `internal/backup/backup_test.go` and `internal/cli/backup_test.go` (spec §9 case 10).

- **Entries.** Read the archive back with `archive/tar`. It contains the dirs and files of
  every fixture session, `session-store.db`, `open-sessions-state.json` and
  `sidebar-sessions-state/a.json`. It also contains `session-state/.other/workspace.yaml`:
  only the lock dir is excluded. It has no `.session-operation-locks` entry and no `-wal` or
  `-shm` entry. Each header's mode and mtime equal the source file's (excluding `session-store.db`, which
  is the snapshot; compare mtime with `src.ModTime().Round(time.Second)`). The archive file
  mode is `0600`.
- **WAL snapshot.** Before the backup, open the fixture DB in the test with
  `db.SetMaxOpenConns(1)` (the pragma is per connection), run `PRAGMA wal_autocheckpoint=0`, insert a `sessions` row, and keep that connection open.
  Assert `session-store.db-wal` has a non-zero size, so the test proves something. Run the
  backup, extract `session-store.db` from the archive to a temp file, and query the new row:
  it must be found.
- **Output.** `Backup created: <path> (5 sessions, <size>)` (five dirs; the DB-only session
  has none), and `-q` prints just the path. The default name comes from the injected clock.
- **Retention.** Create four empty files `copilot-sessions-2026110{1..4}-120000.tar.gz` and a
  non-matching `other.tar.gz`. Then:
  - `--keep 2 --no-create -y` deletes the 01 and 02 files and keeps `other.tar.gz`;
  - `--keep 5 --no-create` prints the nothing-to-remove message;
  - `--keep 1 --no-create` with no TTY fails with the confirmation error, exit 1.
- **`--list --json`.** `file`/`created`/`size` per backup, newest first. A
  `copilot-sessions-bad.tar.gz` falls back to its mtime, set with `os.Chtimes` to a known
  instant.
- **Validation.** All four messages, exit 1.

Extend `TestParity`: with the four timestamped files and the `bad` one in `fx.BackupDir`
(mtime pinned), `backup --list` output must equal Python's byte for byte. The test runs both
with `TZ=XST-2`, as the existing parity test does. This is the spec §13 `backup --list`
criterion. Also have Python read a Go-made archive: run
`python3 -I -c 'import tarfile,sys; tarfile.open(sys.argv[1]).getnames()'` on it as a cheap
format check. The full cross-restore is CS-004's case 12.

### Acceptance test

```
gofmt -l . | wc -l       # 0
go vet ./...
go test -count=1 ./...   # backup tests and TestParity pass on macOS
just test-linux          # if CS-006 is merged; else the docker run in README § Development
CGO_ENABLED=0 go build ./cmd/copilot-sessions
H=$(mktemp -d); COPILOT_HOME=$H/none COPILOT_BACKUP_DIR=$H/b ./copilot-sessions backup -l   # "No backups in $H/b"
COPILOT_BACKUP_DIR=$H/b ./copilot-sessions backup --json; echo $?                          # "error: --json only works with --list", 1
COPILOT_HOME=$H/none COPILOT_BACKUP_DIR=$H/b ./copilot-sessions backup -q && tar tzf $H/b/*.tar.gz   # empty home: archive created, no entries
```

### Docs update (mandatory when user-facing)

The user-facing surface is `backup --help`, with flag help ported from the script (Task 3).
The README is unchanged here; CS-005 writes the usage section. No changelog.

### Finish (mandatory)

1. Acceptance test green on macOS and Linux (Docker).
2. Help strings in place.
3. Write a summary: files touched, decisions made, anything deferred.
4. Suggested commit message:

   ```
   feat(backup): archive, list and rotate session backups (CS-003)

   Adds the backup command: a Python-compatible .tar.gz with a VACUUM INTO DB
   snapshot (WAL content included, no -wal/-shm), --list/--json, and --keep
   retention with confirmation.
   ```

5. Tidy the WIP commits into atomic ones (root-path child).
6. Commit locally; do not push. `pickle ticket move CS-003 in-review --reason "acceptance green"`.

## Review

### Review — round 1 (2026-11-07)

Checklist:

- [x] Reviewer independence (step 0): **delegated**. The branch was written in this session, so a fresh reviewer ran steps 1–4a with `hunt: angle 7 only`. It discarded nothing.
- [x] Stale-branch check (step 0a): `pickle doctor` warned that the branch had CS-003 in `3-in-development`. The branch was rebased onto `main`, and the warning cleared (0/0).
- [x] Implementation audit (step 2): Tasks 1–4 and decisions 1–10 are met. The acceptance test was re-run verbatim and is green: gofmt 0, vet clean, `go test` passes on macOS with `TestParity` run, `just test-linux` passes, and the build plus the three binary checks behave as expected.
- [x] Correctness hunt (step 3): **host tool path**. `/code-review medium` ran over `main...feat/CS-003-backup-command` and returned 10 findings. F1 was reproduced. None were discarded; the other 9 were triaged as non-blocking. The tool does not cover security, so the delegated reviewer ran angle 7 and found nothing (snapshot path bound as a parameter, temp dir 0700 and removed, archive 0600 before any write, symlinks stored and never followed, retention limited to regular files that match the pattern).
- [x] Quality and consistency audits (steps 3–4): the delegated reviewer raised F11–F13.
- [x] Docs audit (step 4a): `backup --help` matches script lines 705–712. The README is out of scope (CS-005). There is no docs build.
- [x] Docs readability (step 4b): skipped, because no `.md`/`.adoc` changed.

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| F1 | blocking | correctness | — | `addTree` copies with an unbounded `io.Copy`. A file that grows after it is stat'ed (a live session appending to `events.jsonl`) makes `tar.Writer` fail, so the backup aborts. | Repro: one goroutine appends to a 1 MiB `session-state/s/events.jsonl` while `Create` runs. Result: `run 0: archive/tar: write too long`. Python's `addfile` copies exactly `tarinfo.size` bytes. | `io.CopyN(tw, f, hdr.Size)`, plus a regression test. |
| F2 | non-blocking | correctness | noted | On error, `Create` leaves the partial target, which has the newest `copilot-sessions-*` name. A later `--keep` would keep it and drop the good backups. | `backup.go` Create. **CS-003 decision 5** ("On error the partial target is left, as in the script"). Python does the same. Once F1 is fixed, the trigger is rare (disk full, a file vanishing mid-walk). | Write to a temp file in the same dir and rename it on success. This needs user sign-off to change decision 5. |
| F3 | non-blocking | correctness | noted | `O_TRUNC` on the default name: two backups in the same second overwrite each other. | `backup.go` Create. Python does the same (`tarfile.open(…, "w:gz")`). | `O_EXCL` or a suffix. |
| F4 | non-blocking | correctness | noted | A file or dir deleted mid-walk (ErrNotExist) aborts the backup. | `addTree` returns the walk error. Python's `tarfile.add` also raises. | Skip `fs.ErrNotExist` in the walk. |
| F5 | non-blocking | correctness | noted | `os.Lstat` treats any error (including EACCES) as "item absent", so the item is skipped silently. | `backup.go` Create item loop. | Skip only `fs.ErrNotExist`. |
| F6 | non-blocking | correctness | noted | `List` maps any Stat error on the backup dir to "no backups". | `backup.go` List. Python's `is_dir()` also swallows errors. | Map only ErrNotExist and not-a-dir. |
| F7 | non-blocking | design | noted | The lock exclusion is a prefix match, so it also drops `.session-operation-locks*` siblings. | **CS-003 decision 4** mirrors the script's prefix test on purpose. | `== locks` or a `locks+"/"` prefix, in both tools. |
| F8 | non-blocking | design | noted | A `copilot-sessions-<non-timestamp>.tar.gz` sorts above every timestamped backup, so `--keep` would keep it. | Spec §6.3 and Python: sort by name. | Sort by `Created` (spec change). |
| F9 | non-blocking | correctness | noted | When `session.Dirs` fails after the archive is written, the command exits 1 for a backup that succeeded. | `cli/backup.go` createBackup. Python also raises. | Print the line with 0 sessions, or warn. |
| F10 | non-blocking | design | noted | `ExpandUser` is a generic path helper exported from `filter`. | **CS-003 decision 10** mandated this export. | Move it if a third user appears. |
| F11 | non-blocking | design | noted | Go's default tar format rounds mtime to the second, while Python's PAX keeps sub-second precision. | **CS-003 decision 4** ("leave the tar format at Go's default"). The Task 4 test rounds to match. | `hdr.Format = tar.FormatPAX` if fidelity matters (CS-004 case 12). |
| F12 | non-blocking | test-gap | noted | `seedBackups` ignores the `MkdirAll` and `Chtimes` errors. | `cli/backup_test.go`. | `t.Fatal` on error. |
| F13 | non-blocking | design | noted | The `copilot-sessions-` and `.tar.gz` literals are repeated in three places. | `backup.Pattern`, `backup.Created`, the default name in `cli/backup.go`. | A `backup.Name(t)` helper. |

Disposition summary: 1 blocking (F1, rework). 12 noted (F2–F13). None fixed inline, folded or turned into tickets. F2–F9 are robustness gaps that the Python reference shares. They are candidates for a "backup robustness" follow-up if the user wants to depart from parity.

### Rework fix record — round 1 (commit e239695)

- **F1 fixed.** `addTree` now copies `io.CopyN(tw, f, hdr.Size)`, which is the size `tarfile` copies, so a file that grows during the backup is stored at the size it had when stat'ed. The regression test `TestCreateGrowingFile` (`internal/backup/backup_test.go`) appends up to 16 MiB to a 1 MiB `events.jsonl` while `Create` runs. It fails with `archive/tar: write too long` when the fix is reverted and passes with it. gofmt and vet are clean, and `go test -count=1 ./...` and `just test-linux` are green.

### Review — round 2, scoped re-review (2026-11-07)

Checklist:

- [x] Reviewer independence (step 0): **delegated** to a fresh reviewer. Nothing was discarded.
- [x] Stale-branch check (step 0a): `pickle doctor` reported 0/0.
- [x] Implementation audit (steps 1–2): fix diff `e239695` read for new defects. The acceptance test was re-run verbatim and is green on macOS and Linux. **F1 closed**: with the fix reverted, `TestCreateGrowingFile` failed 30/30. With the fix, `-race -count=30` passes.
- [x] Correctness hunt (step 3): **angle hunt, one pass, no sub-agents**, over the fix diff with all eight angles, including 7. F14 is unreproduced, with its traced path below. I verified it by hand: `io.CopyN` returns `io.EOF` on a short source.
- [x] Consistency audit (step 4): no other unbounded copy site, and no stale prose.
- [x] Docs audit (step 4a): n/a, since the fix changes no user-facing surface.
- [x] Docs readability (step 4b): skipped, because no `.md`/`.adoc` changed.
- [x] Moved to `6-done/` with History appended (step 6).
- [x] References and governing docs (step 7): spec §6.3 still matches the shipped behaviour, and the board was regenerated by the move.
- [x] Impact sweep (step 8): CS-004 and CS-005 cite `internal/backup` (`Items`, `Create`, `List`, `Created`), `printBackups`, `session.Dirs` and `filter.ExpandUser`. All of them shipped under those names, so neither ticket needed a patch. F11 (mtime rounded to the second) is relevant to CS-004 case 12, and CS-004 already compares file mtimes to the header's.

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| F14 | non-blocking | correctness | noted | A file that shrinks or is truncated between stat and copy still aborts the backup, because `io.CopyN` hits an early EOF. | unreproduced: narrow race. Traced path: `addTree` writes the header with the stat'ed size, `io.CopyN` returns `io.EOF`, `WalkDir` returns it, `Create` fails. Python raises `OSError("unexpected end of data")` here too. `events.jsonl` is append-only. | Pad with zeros or skip the file, as part of a backup-robustness follow-up (with F2–F9). |
| F15 | non-blocking | test-gap | noted | `TestCreateGrowingFile` ignores the `os.MkdirAll` error. | `internal/backup/backup_test.go`. Same pattern as F12. | `t.Fatal` on the error. |

Disposition summary (rounds 1–2): 1 blocking (F1, fixed in round 1), 14 noted (F2–F15), none fixed inline, folded or turned into tickets.

cost: estimated M, actual M

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md §6.3 (Go rewrite of copilot-sessions)
- 2026-11-07 — TO DO → READY: plan complete
- 2026-11-07 — plan amended inline: applicability gate (independent sub-agent, no blocking findings) — CS-002 helpers and `session.SidebarDir` now exist, so the fallback/conditional is dropped; Task 1 lists the `delete.go` caller; Task 4 excludes the snapshot DB from the mode/mtime check, rounds mtime to the second, and pins the WAL test to one connection.
- 2026-11-07 — READY → IN DEVELOPMENT: picked up
- 2026-11-07 — IN DEVELOPMENT → IN REVIEW: acceptance green
- 2026-11-07 — IN REVIEW → REWORK: F1 blocking: backup fails on a growing live file
- 2026-11-07 — REWORK → IN REVIEW: findings fixed
- 2026-11-07 — IN REVIEW → DONE: review round 2 clean: F1 fixed; 14 noted (F2–F15)
- 2026-11-07 — published: branch feat/CS-003-backup-command pushed (2 commits, history kept), PR #4 https://github.com/codcod/copilot-sessions/pull/4 opened against main; awaiting human merge
- 2026-11-07 — follow-up filed: noted findings F2–F9 and F14 promoted to CS-007 (backup robustness) at the user's request
