---
id: CS-004
title: restore command with safe extraction and Python compatibility
project: copilot-sessions
depends-on: [CS-002, CS-003]
spawned-by: []
impact: high
complexity: high
cost: M
---

# CS-004 — restore command with safe extraction and Python compatibility

## Outcome

After this ships, `copilot-sessions restore [FILE|latest]` replaces all current Copilot
sessions with a backup's content. It refuses archives with unsafe paths. Archives made by
the Go version and by the Python script restore with either tool.

## Description

Implements `development/spec.md` §6.4 and covers §9 cases 11–12.

- **Argument.** A `FILE`, `latest`, or an interactive pick from the backup table
  (`Restore which backup? [1] `; an empty answer means 1). With no argument and no TTY,
  fail with `no backup given; pass FILE or 'latest'`. Other errors: `no backups in <dir>`,
  `backup not found: <path>`, and `does not look like a copilot-sessions backup`.
- **Warning and confirmation.** Print the four-line warning block, then ask `Continue?`.
  `<N>` is the number of distinct non-hidden names directly below `session-state/` in the
  archive.
- **Safe extraction.** Extract into a temp dir inside `COPILOT_HOME`. Match Python
  `tarfile` `filter="data"`: reject absolute paths, `..` components, out-of-tree symlinks
  and hardlinks, and device, FIFO or other special files. Strip setuid and setgid bits.
  This is the security-sensitive part of the ticket. Two deliberate departures were
  decided at refinement (2026-11-07, user): symlinks must point downward (no absolute
  target, no `..`), which is stricter than Python; and a missing `COPILOT_HOME` is created
  rather than failing.
- **Replace.** For each of the four top-level items, remove the current item (for the DB,
  also remove `-wal` and `-shm`), then rename the extracted item into place. Print
  `Restored <N> session(s) from <path>`.
- **Tests.** A round trip (backup → `clean --all` → restore) gives the same IDs and DB row
  counts. Also test `-wal`/`-shm` removal, `latest`, and rejection of path-traversal
  archives. An optional compatibility test (§9 case 12) runs only if `python3` (3.12+, for
  `tarfile` `filter="data"`) is on PATH. It uses the in-repo reference script
  `development/copilot-sessions.sh`. It checks that Go restores a Python-made archive and that Python
  restores a Go-made one.

Soft couplings: builds on CS-001. Uses the backup listing and archive layout from CS-003,
and `clean --all` from CS-002 for the round-trip test.

## Implementation Plan

### 0. Feature branch (mandatory)

Before any change, create the feature branch. The child is this repository (`path = "."`):

```
git checkout main && git pull --ff-only
git checkout -b feat/CS-004-restore-command
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

- CS-002 and CS-003 are both in `6-done/` and merged to `main`; check each one's History for
  the `merged to main` line. If either is not merged, stop and ask the human.
- On `main`, the following exist: `internal/cli/confirm.go` (`confirm`, `readLine`),
  `plural` (in `internal/cli/cli.go`), `internal/backup` (`Items`, `Create`, `List`, `Created`), `printBackups`,
  `session.Dirs` and `filter.ExpandUser`.
- The working tree is clean, and `go test ./...` is green on `main`.

### Confirmed design decisions (do not deviate without asking)

1. **The Python script is the reference where the spec is silent.** That means `pick_backup`
   and `cmd_restore` (lines 606–666). The order of steps matches it:
   1. resolve the source;
   2. `backup not found` if it is not a regular file;
   3. inspect;
   4. not-a-backup check;
   5. warning;
   6. confirm;
   7. extract;
   8. replace;
   9. print.
2. **Source resolution follows the script.** With no argument: `backup.List` empty →
   `no backups in <dir>`, checked *before* the TTY check. No TTY →
   `no backup given; pass FILE or 'latest'`. Otherwise `printBackups`, then
   `Restore which backup? [1] ` via `readLine`. Empty means `1`; anything that is not
   all-digits in `1..len` → `invalid choice '<x>'`. `latest` means `List[0]`, or
   `no backups in <dir>`. Any other argument goes through `filter.ExpandUser`. Every error
   here is a plain error (exit 1).
3. **Inspection is one pass over the headers.** A name counts after trimming one trailing
   `/` (Go writes dir entries as `session-state/`; Python stores `session-state`). It is a
   backup if any trimmed name equals `session-state` or `session-store.db`. `<N>` is the
   number of distinct second path components of names starting with `session-state/` whose
   component is non-empty and not hidden. `<M>` is `len(session.Dirs(home))`.
4. **Extraction is confined with `os.Root`** (Go 1.24+; `MkdirAll`, `Symlink`, `Link`,
   `Chmod` and `Chtimes` on `Root` need 1.25+, and `go.mod` is at 1.26). Every write goes
   through `root := os.OpenRoot(tmp)`, so no path, whether `..` or through an extracted
   symlink, can leave `tmp`. On top of that, the member checks below reject the whole
   archive on the first bad member, before anything is replaced, as Python's
   `FilterError` does:
   - **Names.** Reject an absolute name, or one that is not `filepath.IsLocal` after
     trimming the trailing `/`. Rejecting absolute names follows the spec; Python strips
     the slash instead, but its own archives never carry one.
   - **Symlinks are downward-only (user decision 2026-11-07).** Reject if the link target is
     absolute or not `filepath.IsLocal`, that is, if it has any `..`. A chain of
     downward-only links cannot escape. This is stricter than Python's realpath check: an
     in-tree `../sibling` link fails the restore. Mark it with a
     `ponytail: downward-only symlinks; resolve against the extracted tree if a real backup ever needs ../ links`
     comment.
   - **Hard links.** The link target is an archive path. Reject if it is not
     `filepath.IsLocal`; create with `root.Link`.
   - **Types.** Only `TypeDir`, `TypeReg` (and the legacy `TypeRegA`), `TypeSymlink` and
     `TypeLink` are accepted. Char, block, FIFO, contiguous and anything else reject the
     archive with `<name>: unsupported file type`. PAX/GNU long-name headers are handled
     by `archive/tar` itself.
5. **Modes follow Python's `filter="data"`.** `m := Mode & 0o777 &^ 0o022` drops setuid,
   setgid, sticky and group/other write. Regular files and hard links also get `m |= 0o600`,
   and if `m&0o100 == 0` then `m &^= 0o011`. Directories ignore the archive mode, as
   `filter="data"` does (`mode = None` for dirs): they are created with the umask default, and
   only their mtime is recorded and applied after all members, deepest first, as
   `tarfile.extractall` does (amended 2026-11-07, user, review F2). Files get `root.Chmod` and `root.Chtimes`
   with the header mtime right after writing. Ownership is never changed.
6. **A missing `COPILOT_HOME` is created (user decision 2026-11-07).** Run
   `os.MkdirAll(home, 0o700)` before `os.MkdirTemp(home, "tmp")`. This is a deliberate
   departure from the script, which fails there; it makes restoring on a fresh machine work.
7. **Replace follows the script.** For each item in `backup.Items`: if the destination is a
   dir, `os.RemoveAll`; else if it exists (`Lstat`), `os.Remove`. For `session-store.db`,
   also remove `-wal` and `-shm` (ignore not-exist). Then `os.Rename` the extracted item in
   if it exists. The temp dir is removed by `defer os.RemoveAll`. A rejected or failed
   extraction returns before step 7, so current data is untouched.
8. **Ctrl-C is not specially handled.** A Ctrl-C mid-restore can leave a `tmp*` dir in
   `COPILOT_HOME`. It is not a session (it is not under `session-state/`) and not a backup
   item, so the existing `ponytail` comment in `cmd/copilot-sessions/main.go` stays; update
   its wording to name this ceiling. The script's mid-replace window is the same.
9. **The compatibility test (spec §9 case 12) is optional.** It runs only if `python3` is on
   `PATH`, reports 3.12 or later (`filter="data"`), and `development/copilot-sessions.sh`
   exists. It runs the script with `-I` and `TZ=XST-2`, as `TestParity` does. Factor the
   `py` closure out of `TestParity` into a shared test helper
   `pythonRunner(t, fx) func(args ...string) string` that skips when unavailable.

### Tasks

#### Task 1 — `internal/backup/restore.go`

- `func Inspect(path string) (isBackup bool, sessions int, err error)` implements decision 3.
- `func Extract(archive, dest string) error` implements decisions 4–5. It opens the gzip
  stream and uses `os.OpenRoot(dest)`.

#### Task 2 — `restore` command

Create `internal/cli/restore.go` with `newRestore(env Env)`: `Use: "restore [FILE|latest]"`,
`Args: cobra.MaximumNArgs(1)`, `Short: "replace current sessions with a backup"`, and the
`-y/--yes` flag. Port the help strings from the script (lines 717–719). Register it in
`Execute`. Its run function follows decisions 1, 2, 6 and 7. The warning block is exactly:

```
WARNING: restore REPLACES all current Copilot sessions.
  Backup:  <path> (<plural(N, "session")>)
  Current: <home> (<plural(M, "session")> will be deleted)
  Close all running Copilot CLI sessions first; they may overwrite restored data.
```

Then `confirm(env, "Continue?", yes)`. False → `Aborted.`. Finish with
`Restored <plural(N, "session")> from <path>`.

#### Task 3 — `main.go` comment

Reword the `ponytail:` comment in `cmd/copilot-sessions/main.go` per decision 8. There is no
code change.

#### Task 4 — tests

Create `internal/backup/restore_test.go` (extraction safety) and `internal/cli/restore_test.go`
(command), covering spec §9 cases 11–12.

- **Round trip.**
  1. Record the fixture's `list -q` and the DB row count of `sessions`, `turns`,
     `search_index` and the five other tables.
  2. `backup <file>`.
  3. `clean --all --include-ongoing -y` with `nil` ancestors.
  4. Write junk `session-store.db-wal` and `session-store.db-shm`.
  5. `restore <file> -y`.

  Afterwards the same IDs and row counts are back, the junk `-wal`/`-shm` are gone, and the
  output ends with `Restored 6 sessions from <file>`. That is the five session dirs plus the
  fixture's `plain-file`, because the script counts any non-hidden name under
  `session-state/`, files included; keep that parity.
- **`latest`.** It picks the newest of two backups in `fx.BackupDir`.
- **Picker.** With TTY and stdin `"\ny\n"` it restores backup 1. Stdin `"9\n"` gives
  `invalid choice '9'`. No TTY and no argument gives the no-backup-given error. An empty
  backup dir gives `no backups in <dir>` even without a TTY.
- **Errors.** `backup not found: <path>`. An archive with only `foo` gives
  `<path> does not look like a copilot-sessions backup`. `n` at the prompt prints `Aborted.`
  and changes nothing.
- **Rejection, table-driven.** Each archive is built in the test with `archive/tar` and
  holds a valid `session-state/` plus one bad member:
  - `session-state/../../evil`;
  - `/abs/evil`;
  - a symlink to `../x`;
  - a symlink to `/etc`;
  - a hard link to `../x`;
  - a FIFO;
  - a char device.

  Each one fails, writes nothing outside the temp dir (check the parent of `COPILOT_HOME`
  for strays), and leaves the current fixture byte-identical (`list -q`, DB row counts).
  A downward symlink `session-state/a/l -> sub/f` is accepted.
- **Modes.** A member with mode `04755` extracts as `0755`; `0666` extracts as `0644`; a
  `0400` file extracts as `0600`. File mtimes equal the header's.
- **Fresh home.** Restore into a `COPILOT_HOME` that does not exist succeeds (decision 6).
- **Compatibility (case 12), via `pythonRunner`.**
  1. Python `backup <f1>`, then Go `restore <f1> -y`; `list -q` matches the original.
  2. Go `backup <f2>`, then Python `restore <f2> -y`; Python's `list -q` matches the original.

### Acceptance test

```
gofmt -l . | wc -l       # 0
go vet ./...
go test -count=1 ./...   # restore tests pass; the compat test runs (python3 is 3.15 on the dev Mac), not skipped
go test -count=1 -run 'Restore|Compat' -v ./internal/... | grep -E '^(=== RUN|--- (PASS|FAIL|SKIP))'
just test-linux          # green; ongoing-related round-trip parts run
CGO_ENABLED=0 go build ./cmd/copilot-sessions
H=$(mktemp -d); COPILOT_HOME=$H/h COPILOT_BACKUP_DIR=$H/b ./copilot-sessions restore < /dev/null; echo $?   # "error: no backups in $H/b", 1
```

### Docs update (mandatory when user-facing)

The user-facing surface is `restore --help`, with help ported from the script (Task 2).
`development/spec.md` §6.4 gets a one-line note naming the two deliberate departures: the
downward-only symlink rule and the created `COPILOT_HOME`. The README is unchanged here;
CS-005 writes the usage section. No changelog.

### Finish (mandatory)

1. Acceptance test green on macOS (compat test executed) and Linux (Docker).
2. Help strings and the spec note in place.
3. Write a summary: files touched, decisions made, anything deferred.
4. Suggested commit message:

   ```
   feat(restore): replace sessions from a backup with safe extraction (CS-004)

   Adds the restore command (FILE, latest or an interactive pick) with
   os.Root-confined extraction that rejects unsafe names, escaping links and
   special files, strips setuid/setgid, and round-trips archives with the
   Python script.
   ```

5. Tidy the WIP commits into atomic ones (root-path child).
6. Commit locally; do not push. `pickle ticket move CS-004 in-review --reason "acceptance green"`.

## Review

### Review round 1 — 2026-11-07 (branch `feat/CS-004-restore-command` @ 260b5ef)

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| F1 | blocking | correctness | — | A symlink target with balanced `..` escapes the tree: `filepath.IsLocal` is lexical, and the kernel resolves `..` after following an allowed `s -> .` link. Contradicts CS-004 decision 4 ("if it has any `..`"), spec §6.4 step 2 and the Outcome. Python `filter="data"` normalises the same archive into the tree, so Go is *less* strict than the reference. | Reproduced: archive `session-state/`, `session-state/s -> .`, `session-state/evil -> s/s/s/s/s/s/s/s/../../../../../../../../etc`; `restore -y` exits 0 ("Restored 2 sessions"), `readlink -f $H/session-state/evil` → `/private/etc`. `internal/backup/restore.go:77` | Reject a link target that has any `..` element (as decision 4 says), not just `!IsLocal`; add the `s -> .` + balanced-`..` case to the rejection table. |
| F2 | blocking | correctness | — | Directory modes from the archive are applied (CS-004 decision 5). A dir without owner write (e.g. `session-state` `0555`) makes the rename into `COPILOT_HOME` fail *after* the current `session-state` was deleted: the user loses the current sessions and the restored copy is stranded in an undeletable `tmp*` dir. Python `filter="data"` ignores directory modes (`mode = None` for dirs), so it succeeds. | Reproduced (macOS): Python-built archive with `session-state` mode `0555` → `error: rename …/tmp1910346953/session-state …/session-state: permission denied`, exit 1; home left with only `config` and `tmp1910346953`; `rm -rf` of it fails with EACCES. `internal/backup/restore.go:67,113-121` | Follow Python: do not chmod directories (create them with the umask default), keep only the mtime. Needs user sign-off, since it departs from CS-004 decision 5. |
| F3 | blocking | correctness | — | A top-level item may be extracted as a symlink (`session-state -> config`); after the rename it redirects `COPILOT_HOME/session-state` to a sibling dir outside the restored tree, which a later `clean --all` would empty. | Reproduced: archive with member `session-state` symlink → `config`; `restore -y` exits 0, `ls -la $H` shows `session-state -> config`. `internal/backup/restore.go:73-78` | Reject a symlink or hard link whose name is a top-level item (no `/`); the four items must be a real dir or file. |
| F4 | non-blocking | design | folded | Replace deletes each current item before renaming its replacement in; if a rename fails mid-way, the deferred `RemoveAll(tmp)` also deletes the not-yet-moved restored data. Same window as the script (CS-004 decision 8); with F2 fixed no known trigger remains. | Traced: `internal/cli/restore.go:86-112` → return → `defer os.RemoveAll(tmp)`; F2's repro hits this path. | Folded into CS-007 (backup/restore robustness): move old items aside and roll back, or keep `tmp` and name it on failure. |
| F5 | non-blocking | stale-xref | fixed inline | The reworded `ponytail:` comment in `cmd/copilot-sessions/main.go` says a Ctrl-C mid-restore only leaves a harmless `tmp*` dir; a Ctrl-C between delete and rename leaves the restored data only in that dir (decision 8's mid-replace window). | `cmd/copilot-sessions/main.go:16-18` | Name the mid-replace window in the comment (rework pass, no behaviour change). |
| F6 | non-blocking | test-gap | noted | `pythonRunner` now skips `TestParity` on python3 < 3.12, though list/clean parity needs no `filter="data"`, and `data_filter` was backported to 3.8.17/3.9.17/3.10.12/3.11.4. | `internal/cli/parity_test.go` `pythonRunner` | Gate on `hasattr(tarfile, "data_filter")`, and only in `TestRestoreCompat`. |
| F7 | non-blocking | spec-unclear | noted | Spec §6.4 step 2 says "reject `..` components" for names; the code (and Python) accepts balanced names like `session-state/x/../y`, confined by `os.Root`. | `internal/backup/restore.go:59`; repro `session-state/x/../../evil` fails only because `x` is absent | Reword the spec to "names that escape the target", or reject any `..` element in names as well. |
| F8 | non-blocking | correctness | noted | Archive-supplied member names and link targets are printed raw (`%s`) in errors, so a crafted archive can send terminal escape sequences. | `internal/backup/restore.go:60,78,83,100` (code reading) | Use `%q`. |
| F9 | non-blocking | test-gap | noted | `TestRestoreRejectsUnsafeArchives` asserts only exit 1 + non-empty stderr; a case could pass on an unrelated error. | `internal/cli/restore_test.go` rejection table | Assert the expected message per case. |
| F10 | non-blocking | design | noted | The replace loop's Lstat/IsDir/RemoveAll-or-Remove branch and the `-wal`/`-shm` ErrNotExist loop re-implement `os.RemoveAll`. | `internal/cli/restore.go:86-104` | `os.RemoveAll` on each of `dest`, `dest-wal`, `dest-shm`. |
| F11 | non-blocking | design | noted | `filepath.IsAbs` before `!filepath.IsLocal` is redundant (IsLocal is false for absolute paths); the hard-link branch already omits it. | `internal/backup/restore.go:59,77` | Drop the `IsAbs` checks. |
| F12 | non-blocking | design | noted | The archive is gunzipped twice (Inspect, then Extract). The script does the same (`getnames` then `extractall`). | `internal/cli/restore.go:47,81` | None now. |

Disposition summary: 3 blocking (F1–F3, → rework); 9 non-blocking: 1 fixed inline (F5), 1 folded (F4 → CS-007), 7 noted (F6–F12), 0 new tickets.
cost: estimated M, actual M

**Checklist**

- [x] Reviewer independence settled (step 0): delegated to an independent sub-agent (the orchestrator wrote the branch). Delegated findings discarded on re-verification: none. The delegated reviewer itself dropped four candidates (hardlink to symlink, `TypeRegA`, top-level downward symlink, gzip bomb). The orchestrator re-raised the top-level symlink as F3 from the host tool's finding: it redirects into a sibling of the restored tree.
- [x] In-tree stale-branch check (step 0a): `pickle doctor` warned that the ticket was stale on the branch; rebased onto `main`; re-run clean.
- [x] Implementation audit (steps 1, 2): acceptance re-run by the delegated reviewer (gofmt 0, vet clean, `go test` green, compat test executed on python3 3.15.2, `just test-linux` green, build + empty-home smoke exit 1). Tasks 1–4 met; decision 4 not met (F1).
- [x] Correctness hunt (step 3): host tool path, `/code-review high` run by the top-level session on `main...feat/CS-004-restore-command`. 9 candidates; 0 discarded (tool #1 → F2, #2 → F4, #3 → F5, #4 → F3, #5 → F6, #6 → F7, #7 → F10, #8 → F11, #9 → F12). Angle 7 ran separately (one pass, delegated reviewer): F1, F8. F1–F3 reproduced; F4 traced.
- [x] Consistency audit (step 4): F1 and F7 are contradictions with decision 4 and the spec.
- [x] Documentation audit (step 4a): no shipped docs tree; `restore --help` and the spec §6.4 note are present; README deferred to CS-005 by plan.
- [ ] Docs-readability pass (step 4b): conscious skip; the only changed prose is a two-line spec note.
- [x] Findings recorded with severity, class and disposition; summary + cost lines present (step 5).
- [x] Ticket moved to `tickets/5-rework/` (step 6).
- [x] Other references: CS-007 gets F4's item; the spec note will be revisited by the F2 fix (step 7).
- [x] Impact sweep (step 8): CS-005 (README usage) and CS-007 reference restore; no assumption invalidated beyond F4's fold.
- [ ] Summary + commit message presented: deferred until the rework re-review concludes (step 9).

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md §6.4, §9 cases 11-12 (Go rewrite of copilot-sessions)
- 2026-11-07 — TO DO → READY: plan complete
- 2026-11-07 — impact sweep from CS-002 review: `plural` lives in `internal/cli/cli.go`, not `confirm.go`; prerequisite line corrected.
- 2026-11-07 — applicability gate (independent sub-agent): no blocking findings. G1 (non-blocking, stale-reference): script help range was 715–717 → plan amended inline: Task 2 help-string range corrected to 717–719. G2 (non-blocking, sequencing): overlap with CS-007 F2 (partial newest archive picked by `latest`) and F8 (sort order) — noted and closed; decision 7 already leaves current data untouched on a failed extraction.
- 2026-11-07 — READY → IN DEVELOPMENT: picked up
- 2026-11-07 — IN DEVELOPMENT → IN REVIEW: acceptance green
- 2026-11-07 — IN REVIEW → REWORK: review round 1: F1–F3 blocking; F5 fixed inline, F4 folded into CS-007, F6–F12 noted
- 2026-11-07 — plan amended inline: decision 5 no longer applies archive directory modes (Python `filter="data"` ignores them); user decision on review F2.
