---
id: CS-007
title: backup robustness: atomic archive, live-file races and error reporting
project: copilot-sessions
depends-on: []
spawned-by: [CS-003, CS-004]
impact: medium
complexity: medium
cost: S
---

# CS-007 — backup robustness: atomic archive, live-file races and error reporting

## Outcome

A failed `backup` never leaves a half-written archive behind, so `--keep` and `restore latest`
only ever see complete backups. A backup taken while Copilot is running finishes even when
session files change underneath it, and real I/O or permission errors are reported instead of
being treated as "nothing there". A second default-named backup in the same second fails with a
clear error instead of overwriting the first, and a `restore` that fails mid-way leaves
`COPILOT_HOME` exactly as it was.

## Description

Hardens the `backup` command shipped in CS-003. Every item below is a non-blocking finding from
the CS-003 review, recorded there as `noted` with its evidence. Most are behaviour the Python
script shares, so fixing them deliberately departs from parity. That is acceptable once CS-005
cuts over from the script, but each departure should be named in the plan.

- **F2 — atomic archive.** `backup.Create` writes straight to the target and leaves the partial
  file on error (CS-003 decision 5). Because the partial file has the newest
  `copilot-sessions-*` name, a later `--keep` keeps it and deletes good backups, and CS-004's
  `restore latest` would pick it. Write to a temp file in the same directory, rename it on
  success, and remove it on error. **This reverses CS-003 decision 5.**
- **F3 — same-second collision.** Two backups in the same second share the default name, and
  the second one truncates the first (`O_TRUNC`). Use `O_EXCL` on the default name, plus a
  suffix or a clear error. An explicit `FILE` keeps overwrite semantics.
- **F4 and F14 — files that change during the walk.** A file or directory deleted mid-walk
  (`fs.ErrNotExist`) aborts the backup. A file that shrinks between stat and copy aborts it with
  an early EOF. Skip vanished entries, and pad a short file with zeros up to the header size, as
  GNU tar does, or skip it with a warning.
- **F5 — absent vs unreadable item.** Any `os.Lstat` failure skips a top-level item silently.
  Skip only `fs.ErrNotExist`; report anything else as an error.
- **F6 — backup dir errors.** `backup.List` maps any `Stat` error to "no backups". Map only a
  missing dir or a non-directory to an empty list; return other errors (EACCES, I/O).
- **F7 — lock-dir exclusion.** The prefix test also drops siblings such as
  `.session-operation-locks-old/`. Exclude exactly `session-state/.session-operation-locks` and
  everything below it, as spec §6.3 says.
- **F8 — retention order.** Sorting by file name puts a `copilot-sessions-<non-timestamp>.tar.gz`
  above every timestamped backup, so `--keep` keeps it. **Refinement: not changed** (note and
  close). Only a hand-named file triggers it, F3's fix never creates such a name, and changing it
  would rewrite spec §6.3 and break the `backup --list` parity check, whose fixture seeds
  `copilot-sessions-bad.tar.gz`.
- **F9 — reporting after success.** If `session.Dirs` fails after the archive is written, the
  command exits 1 for a backup that exists, and `--keep` is skipped. Report the backup anyway,
  with a warning, and continue.
- **CS-004 F4 — restore replace window.** `restore` deletes each current item before renaming
  its replacement in; if a rename fails mid-way, the deferred `RemoveAll(tmp)` also deletes the
  restored data not yet moved. Move old items aside and roll back on failure, or keep `tmp` and
  name it in the error.

Out of scope, and still `noted` on CS-003: F10 (`ExpandUser` location), F11 (PAX sub-second
mtimes), F12/F15 (test error checks) and F13 (name literals).

Soft couplings: builds on CS-003's `internal/backup` (PR #4) and CS-004's restore (PR #5), both
merged to `main`. CS-005 (done, not yet merged) touches only `internal/cli/cli.go` (one line),
`complete*.go` and the README, so it does not overlap. Its WSL cutover has already replaced the
Python script, so departing from script parity here is acceptable. The parity test stays green
because F8 is left unchanged.

## Implementation Plan

### 0. Feature branch (mandatory)

Before any change, create the feature branch. The child is this repository (`path = "."`):

```
git checkout main && git pull --ff-only
git checkout -b feat/CS-007-backup-robustness
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

- CS-003 and CS-004 are merged to `main` (their History has `merged to main` lines). There is no
  `depends-on:`, because both are already merged.
- On `main`: `internal/backup/backup.go` (`Create`, `addTree`, `List`, `Created`),
  `internal/cli/backup.go` (`createBackup`, `applyRetention`), and `internal/cli/restore.go`
  (`runRestore` with its delete-then-rename loop).
- Clean working tree; `go test ./...` green on `main`.

### Confirmed design decisions (do not deviate without asking)

1. **`backup.Create` writes to a hidden temp file in the target's directory and only renames it
   onto the target once the archive is complete.** The temp file comes from
   `os.CreateTemp(filepath.Dir(target), ".copilot-sessions-*.tmp")`. It never matches
   `Pattern`, so `--list`, `--keep` and `restore latest` cannot see it. On any error it is
   closed and removed. This reverses CS-003 decision 5.
2. **A default-named backup never overwrites; an explicit `FILE` still does.** `Create` takes a
   third parameter, `overwrite bool`. With `overwrite`, it finishes with `os.Rename(tmp, target)`.
   Without it, it finishes with `os.Link(tmp, target)` followed by `os.Remove(tmp)`, an atomic
   no-clobber step. `EEXIST` becomes `backup already exists: <target>`. There is no suffix
   scheme, because `…-1.tar.gz` sorts below `….tar.gz` and fails `Created`'s parse. The CLI
   passes `overwrite = len(args) > 0`.
   `// ponytail: os.Link needs hard-link support in the backup dir; fall back to O_EXCL+rename if a filesystem refuses it`.
3. **Entries that vanish mid-walk are skipped silently.** In `addTree`, an `fs.ErrNotExist`
   from the walk error, `d.Info()`, `os.Readlink` or `os.Open` returns `nil`. Each regular
   file is opened *before* its header is written, so a file that vanished never leaves a header
   with no data.
4. **A file that shrank after stat is padded with zeros up to the header size, silently** (GNU
   tar behaviour). A helper `copyPadded(w io.Writer, r io.Reader, size int64) error` does
   `io.CopyN`, and on `io.EOF` copies the rest from a zero reader (no allocation of the gap).
   Files that grow are still cut at the stat-ed size, as now.
5. **A top-level item is skipped only when it is absent.** In `Create`, an `os.Lstat` error
   other than `fs.ErrNotExist` is returned.
6. **`backup.List` returns `nil, nil` only for a missing dir or a non-directory.** Any other
   `Stat` error (EACCES, I/O) is returned.
7. **The lock exclusion is exact:** `entry == locks || strings.HasPrefix(entry, locks+"/")`.
   Siblings such as `.session-operation-locks-old/` are archived.
8. **F8 is not changed.** Retention and `--list` still sort by file name (spec §6.3 unchanged).
9. **A session-count failure after a successful write does not fail the backup.**
   `createBackup` prints `warning: <err>` to `env.Stderr` and then
   `Backup created: <path> (<size>)` without the count. It returns nil, so `--keep` still runs.
10. **`restore` moves current items aside and rolls back on failure.** Each existing
    `COPILOT_HOME/<item>` (and, for the DB, its `-wal`/`-shm`) is renamed into `<tmp>/old/`
    before the extracted item is renamed in. Every successful rename is recorded. On the first
    failure the recorded renames are undone in reverse, and the error is returned as
    `restore failed, nothing changed: <err>`. If a rollback rename also fails, `tmp` is **not**
    removed, and the error names it: `restore failed and rollback incomplete; old data kept in <tmp>/old: <err>`.
    On success the deferred `RemoveAll(tmp)` drops the old items. Renames go through a
    package-level `var rename = os.Rename` in `internal/cli`, which is the test seam.

### Tasks

#### Task 1 — `internal/backup/backup.go`: atomic, no-clobber `Create`

Decisions 1, 2 and 5. Signature `Create(home, target string, overwrite bool) (err error)`. Keep
the `0o600` chmod and parent `MkdirAll`. A single deferred cleanup closes the temp file if it is
still open and removes it whenever `err != nil` (after a successful `Link` it is removed
explicitly). Close the file (and check the error) before rename/link. Update the doc comment.

#### Task 2 — `internal/backup/backup.go`: walk robustness and lock exclusion

Decisions 3, 4 and 7 in `addTree`, plus the `copyPadded` helper and a two-line `zeros` reader.
Replace the `io.CopyN` comment so it describes both directions (growing files are cut, shrinking
files are padded).

#### Task 3 — `internal/backup/backup.go`: `List` errors

Decision 6:
`fi, err := os.Stat(dir); if errors.Is(err, fs.ErrNotExist) || err == nil && !fi.IsDir() { return nil, nil }; if err != nil { return nil, err }`.
Update the doc comment.

#### Task 4 — `internal/cli/backup.go`

Pass `overwrite` (decision 2). Make `createBackup`'s `session.Dirs` failure non-fatal
(decision 9).

#### Task 5 — `internal/cli/restore.go`: move-aside with rollback

Decision 10. Replace the delete-then-rename loop. `os.MkdirAll(<tmp>/old)`, then for each item:
move the existing dest (plus `-wal`/`-shm` for the DB) into `old/`, then move the extracted item
in. Keep the "skip if absent" checks. Replace the unconditional `defer os.RemoveAll(tmp)` with
one guarded by a `keepTmp` flag. Update the comment so it names this as a departure from the
script.

#### Task 6 — tests

- `internal/backup/backup_test.go`:
  - `TestCreateFailureLeavesNothing`: a session file with mode `0` makes `Create` fail. Assert
    an error, no file at `target`, and the target dir empty (no `.copilot-sessions-*.tmp`).
    `t.Skip` when `os.Geteuid() == 0`, since root reads it anyway and Docker runs as root.
  - `TestCreateNoClobber`: `Create(…, false)` onto an existing file gives an `already exists`
    error, the existing file is byte-for-byte intact, and no temp file is left. `Create(…, true)`
    onto it succeeds.
  - `TestCopyPadded`: `copyPadded(&buf, strings.NewReader("abc"), 5)` gives `"abc\x00\x00"`.
    With size 2 it gives `"ab"`.
  - Extend `TestCreate`: add `session-state/.session-operation-locks-old/x` to the home. It must
    be in the archive, and nothing under `.session-operation-locks/` may be.
  - `TestListErrors`: a missing dir and a regular file both give `nil, nil`. `<p>/b` where `<p>` has
    mode `0` (Stat gives EACCES) gives an error; skip as root; `t.Cleanup` restores `<p>`'s mode so TempDir cleanup works.
- `internal/cli/backup_test.go`: `TestBackupSameSecond`: run `backup` twice with the fixed
  `env.Now`. The second exits 1 with `backup already exists:`, and `backup --list` still shows
  exactly one readable backup.
- `internal/cli/restore_test.go`: `TestRestoreRollback`: swap `rename` for one that fails when
  the *source* is `<tmp>/sidebar-sessions-state` (the extracted last item, after the others
  were swapped; matching on the destination would also fail the rollback rename back into
  `<home>/sidebar-sessions-state`). Restore exits 1 with `restore failed, nothing changed`. Every top-level item in
  home has the same contents as before the restore (compare a recursive listing + file bytes
  taken beforehand), and no `tmp*` dir is left in home. Restore `rename` with `t.Cleanup`.
  A second case additionally fails the rollback rename back into `<home>/sidebar-sessions-state`:
  the error says `rollback incomplete` and names `<tmp>/old`, which still exists.

F4's vanish guards (decision 3) and F5's `Lstat` guard are one-line `errors.Is` checks with no
deterministic trigger, so the review covers them by reading the code. No race tests.

### Acceptance test

```
gofmt -l . | wc -l       # 0
go vet ./...
go test -count=1 ./...   # all green; TestParity runs (not skipped) on the dev Mac and passes unchanged
go test -count=1 -v -run 'CreateFailure|NoClobber|CopyPadded|TestCreate$|ListErrors|SameSecond|RestoreRollback' ./internal/... | grep -E '^(--- (PASS|FAIL|SKIP))'
                         # every listed test PASS on macOS (none SKIP)
just test-linux          # green (root-only skips allowed: CreateFailureLeavesNothing, ListErrors)
CGO_ENABLED=0 go build ./cmd/copilot-sessions
H=$(mktemp -d); mkdir -p $H/h/session-state/s
COPILOT_HOME=$H/h COPILOT_BACKUP_DIR=$H/b ./copilot-sessions backup -q && COPILOT_HOME=$H/h COPILOT_BACKUP_DIR=$H/b ./copilot-sessions backup -q; echo $?
                         # the second run within the same second prints "error: backup already exists: …" and exits 1
                         # (if the clock ticks between the two runs, both succeed — rerun)
ls -A $H/b               # only copilot-sessions-*.tar.gz files, no .copilot-sessions-*.tmp
```

### Docs update (mandatory when user-facing)

`development/spec.md`:
- §6.3 gets a short "Deliberate departures from the script" note: the archive is written to a
  hidden temp file and renamed into place; a default-named backup never overwrites
  (`backup already exists: <path>`); entries that vanish mid-walk are skipped and files that
  shrink are zero-padded; only `.session-operation-locks` itself is excluded; a backup whose
  session count cannot be read is still reported, with a warning.
- §6.4 gets one line: a restore that fails while swapping items rolls back and leaves
  `COPILOT_HOME` unchanged.

There are no help-text changes, the README is unchanged (CS-005 owns its usage section), and
there is no changelog.

### Finish (mandatory)

1. Acceptance test green on macOS (TestParity executed) and Linux (Docker).
2. Spec notes in place.
3. Write a summary: files touched, decisions made, anything deferred.
4. Suggested commits (root-path child, atomic history kept):

   ```
   fix(backup): write archives atomically and tolerate live session changes (CS-007)

   Create writes to a hidden temp file and renames it into place, so a failed
   backup leaves nothing --keep or restore latest could pick. A default-named
   backup never overwrites one from the same second. Vanished entries are
   skipped, shrunk files zero-padded, real Lstat/Stat errors reported, the lock
   dir excluded exactly, and a failed session count no longer fails a written
   backup.
   ```

   ```
   fix(restore): roll back when replacing items fails (CS-007)

   Current items are moved aside before the restored ones are moved in; on a
   failure every move is undone, so COPILOT_HOME is left as it was.
   ```

5. Tidy the WIP commits into these atomic ones (root-path child).
6. Commit locally; do not push. `pickle ticket move CS-007 in-review --reason "acceptance green"`.

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-07 — created (TO DO). source: review: CS-003 review findings F2–F9 and F14, batched as one backup-robustness theme at the user's request
- 2026-11-07 — folded: CS-004 review F4 (restore replace window) added to the Description.
- 2026-11-07 — TO DO → READY: plan complete; decisions: F8 left as is, F14 zero-pad, F3 error on collision, restore move-aside rollback
- 2026-11-07 — READY → IN DEVELOPMENT: picked up; applicability gate: 1 blocking test-design finding amended inline, 3 non-blocking (1 inline, 2 noted)
- 2026-11-07 — plan amended inline: applicability gate — TestRestoreRollback seam keyed on the extracted source, not the destination (the rollback rename shares it), plus a rollback-also-fails case; TestListErrors restores the dir mode in t.Cleanup. Noted and closed: a leftover hidden .tmp when Remove fails after Link (never matches Pattern); WalkDir's second dir-read-error callback is covered by decision 3.
