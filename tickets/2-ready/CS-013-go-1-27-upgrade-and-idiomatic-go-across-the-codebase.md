---
id: CS-013
title: Go 1.27 upgrade and idiomatic Go across the codebase
project: copilot-sessions
depends-on: []
spawned-by: []
impact: medium
complexity: high
cost: L
---

# CS-013 — Go 1.27 upgrade and idiomatic Go across the codebase

## Outcome

After this ships, the module builds on Go 1.27 with current dependencies. The code reads like
idiomatic Go: typed options instead of runs of bool parameters, a `Status` type, `context.Context`
on I/O, and modern `slices`/`maps`. Permission errors are reported instead of showing up as
"no sessions" or "backup not found", and Ctrl-C cancels cleanly, rolling back a restore that is
in progress. Wherever idiomatic Go and Python-script parity disagree, idiom wins, and spec §11
records each difference.

## Description

Two parts, one ticket. They touch the same files, and a separate Go-bump ticket would be
mechanical and would not be picked up on its own (rules §3).

**Part 1: Go 1.27.** `go.mod` declares `go 1.26.0`; the dev toolchain is `go1.27.3`. CI
(`.github/workflows/ci.yml`, `release.yml`) and the justfile's `test-linux` read the version from
`go.mod` (`go-version-file`, `go_version`), so they follow automatically; verify this. Bump the
directive to `go 1.27.0`, upgrade the direct dependencies (`cobra`, `pflag`, `modernc.org/sqlite`)
and the indirect ones to their latest versions (`go get -u ./... && go mod tidy`), and run the
`go fix` modernizers. Spec §2 still says "Go 1.22 or later": update it. The local `staticcheck`
and `golangci-lint` were built with go1.23 and cannot load this module; the refinement must decide
how lint runs (rebuild, or `go run` a pinned version from the justfile).

**Part 2: idiomatic Go.** Source: a code review on 2026-11-09 (`go vet`/`gofmt` clean, tests
green). Every finding below is in scope. IDs refer to that review.

*Input parameters*
- P1: `runClean(env, f, all, includeOngoing, dryRun, yes, quiet bool)` (`internal/cli/clean.go`)
  takes five positional bools. Replace them with an options struct that the flags bind into.
- P2: the same pattern, smaller, in `runList`, `createBackup`, `applyRetention`, `session.Load(…,
  withSize)`, `session.Delete(…, everything)`, `backup.Create(…, overwrite)`. Use options structs
  or split functions.
- P3: `Env` is passed by value. Its unexported `in *bufio.Reader` is set only by `Execute`, so
  `readLine` panics on any `Env` built elsewhere. Pass `*Env`, or create the reader lazily.
- P4: statuses are strings (`"ongoing"`, `"stale"`, `"closed"` as literals across session,
  filter and cli). Introduce `type Status string` with constants.
- P5: `Session.Updated *time.Time` and `ParseTime` returning `*time.Time` use a pointer for an
  optional value. Use `time.Time` with `IsZero()`.
- P6: `Session.Size *int64` is dereferenced unconditionally in `runList` and `runClean`. That is
  safe only because `withSize = !quiet` happens to line up. Use `int64`.
- P7: `filter.DateFlag(p **Date, …)` follows from `Filters.Until/Since *Date`. Simplify with P5.

*Correctness hidden by idiom slips*
- C1: `session.Dirs` treats every `os.Stat` error as "no session-state", so an untraversable
  `COPILOT_HOME` reads as zero sessions (restore says "0 will be deleted", backup writes an empty
  archive). Only `fs.ErrNotExist` should mean absent; `backup.List` already does this correctly.
- C2: `session.OpenDB` has the same swallowing, plus a `nil, nil` return that every caller must
  remember to check. Use a sentinel such as `ErrNoDB`, or an equivalent explicit signal.
- C3: `cli/restore.go` reports every `os.Stat` error as "backup not found". Wrap the real error.
- C4: `session.HasTurns` returns a non-nil map together with a non-nil error.
- C5: `ancestorsFrom` slices `b[i+1:]` before it checks `i < 0`.

*Modern stdlib*: replace `sort.SliceStable`/`sort.Slice`/`sort.Strings`/`sort.Reverse` with
`slices.SortStableFunc`/`SortFunc`/`Sort`/`Reverse`, and `sortedKeys` with
`slices.Sorted(maps.Keys(m))`. Consider `strings.Lines` in `readWorkspace`.

*Context and signals*
- X1: `cmd/copilot-sessions/main.go` handles Ctrl-C with `signal.Notify` and `os.Exit(130)`, which
  skips deferred cleanup and can leave a `tmp*` dir in `COPILOT_HOME` holding the only copy of a
  restored item (the `ponytail:` comment there, CS-007). Replace it with `signal.NotifyContext` +
  `root.ExecuteContext`, so Ctrl-C cancels and the restore rolls back through `swapItems`'s
  existing undo. Temp dirs are cleaned up and the exit code stays 130.
- X2: no function takes a `context.Context`. Pass ctx as the first parameter of I/O functions, and
  use `QueryContext`/`ExecContext`/`BeginTx(ctx)` in `session` and `backup`.

*API surface and package boundaries*
- A1: move `filter.ExpandUser` to `cli` (its only users).
- A2: move the `pflag.Value` adapters (`DateFlag`, `StatusFlag`) from `filter` to `cli`, so `filter`
  no longer imports `pflag`.
- A3: the exported mutable slices `filter.Statuses` and `backup.Items`. Protect or unexport them.
- A4: exported identifiers without doc comments (`session.StateDir`, `DBFile`, `OpenState`,
  `SidebarDir`, the `filter.Date` fields).
- A5: rename `main.Version` to `version`, and update the `-ldflags -X` in the justfile and
  `.goreleaser.yaml`.

*Duplication and small points*
- D1: three digit-only checks (`session.isDigits`, `filter.allDigits`, and the `"+-"` guard in
  `cli/restore.go`). Merge them into one.
- D2: the `usageError`-wrapping `Args` func is duplicated in `backup.go` and `restore.go`.
- D3: the JSON encoder setup is repeated three times.
- D4: `Execute` detects unknown commands with `strings.HasPrefix(err.Error(), "unknown command ")`.
  Remove the string match if cobra allows it without losing "Did you mean this?" (CS-012).
- D5: error texts use `'%s'`. Switch to `%q`; this changes visible text and the tests that assert it.
- D6: the test seams `var rename`/`var link` are mutable package globals. Make them
  parallel-safe, or inject them.
- D7: errors that are deliberately ignored should be made explicit (`_ =`) or handled.
- D8: stray blank first line in `pruneUIState`.
- D9: `backup.Create` makes the backup dir `0o755`. Use `0o700`, consistent with home.

**Decisions taken at filing (user, 2026-11-09)**
1. Idiom wins everywhere over Python-script parity, including user-visible text (error quoting,
   permission errors as real errors) and JSON formats (e.g. `backup --list --json` `created`
   moves from Python `isoformat` to RFC 3339). Each change goes into spec §11 (which currently
   says "everything else … must match"). `TestParity` (`internal/cli/parity_test.go`) is narrowed
   or retired to match. The spec is the thing to update, not the reason to keep the old behaviour.
2. Context and signals are in scope (X1 and X2): `signal.NotifyContext`, a restore that rolls back
   on Ctrl-C, and ctx on I/O functions.
3. The Go 1.27 bump includes upgrading all dependencies and running the `go fix` modernizers.
4. This ticket goes before CS-009 and CS-010 (both READY), which now have `depends-on: [CS-013]`.
   Both rewrite process detection in `internal/session/session.go`, which this ticket refactors,
   so their plans need a refresh against the new APIs at pickup.

The questions left open at filing (lint tooling, JSON nullability, P2 shape, D6, `filter.Date`)
were settled with the user at refinement on 2026-11-09. They are decisions 5–12 below. Every
visible change is listed in spec §11 and CHANGELOG.

## Implementation Plan

### 0. Feature branch (mandatory)

```
cd /Users/nka/Projects/codcod/copilot-sessions
git checkout main && git pull --ff-only
git checkout -b feat/CS-013-go-1-27-idiomatic
```

Commit locally as you go, one WIP commit per task at least. Root-path child (`path = "."`): tidy the
WIP commits into atomic ones before presenting, and keep that history by default. Before pushing
(in-tree layout), `git fetch origin main && git diff --name-only origin/main...HEAD | grep
'^tickets/'` must print nothing. No push or MR without the user's approval; merging is the human's.

### Prerequisite gate (hard)

- CS-013 has no `depends-on`. Start from a clean tree on `main`.
- Go toolchain ≥ 1.27 (`go version`; `go1.27.3` on the dev Mac, checked 2026-11-09).
- `python3` ≥ 3.12 on `PATH`, so `TestRestoreCompat` runs instead of skipping (3.15.2, checked
  2026-11-09). `docker` is needed for `just test-linux` (29.9.2, checked 2026-11-09).
- No release tag exists (checked 2026-11-09). If one has been cut since, the CHANGELOG entry must
  name the released behaviour that breaks; ask the user before proceeding.
- CS-009 and CS-010 must not be in development. They depend on this ticket.

### Confirmed design decisions (do not deviate without asking)

1. **Idiomatic Go wins over Python-script parity everywhere**, including user-visible text and
   JSON formats. Each visible change is recorded in spec §11, and the sentence "Everything else …
   must match" is narrowed to the archive format (decision 7). (Filing, 2026-11-09.)
2. **Context and signals are in scope.** `main` uses `signal.NotifyContext(ctx, os.Interrupt)` and
   `cli.Execute(ctx, …)` runs `root.ExecuteContext(ctx)`. I/O functions take `ctx` as their first
   parameter; DB calls use `QueryContext`, `QueryRowContext`, `ExecContext` and `BeginTx(ctx, …)`.
   On cancel, the binary prints `\nAborted.` to stderr and exits with 130, as before. Temp dirs and
   files are removed by their existing `defer`s. (Filing.)
3. **The Go 1.27 bump covers the toolchain, the dependencies and modernizers**: `go 1.27.0` in
   `go.mod`, `go get -u ./... && go mod tidy`, then `go fix ./...`. (Filing.)
4. **CS-013 goes before CS-009 and CS-010**, which have `depends-on: [… CS-013]`. (Filing.)
5. **Lint gains a `go fix -diff ./...` gate** in `just lint` and in CI. No third-party linters.
   (Refinement.)
6. **The `list --json` shape is unchanged.** `updated_at` and `busy` stay `null` when unknown or
   not applicable. The pointers live only in the JSON DTO (`sessionJSON`); domain types use
   values. (Refinement.)
7. **`TestParity` is retired, archive compatibility is kept.** The output comparisons and spec
   §13's parity criterion are removed. `TestRestoreCompat` (Go restores a Python archive, and
   Python restores a Go archive) stays as the compatibility contract (spec §9 case 12). Its
   `pythonRunner` helper moves out of `parity_test.go`, which is deleted. (Refinement.)
8. **`backup --list --json` `created` is RFC 3339 in local time with its offset**, e.g.
   `2026-10-31T12:00:00+02:00` (`time.RFC3339`; the file-name stamp has no sub-second part).
   The `isoformat` helper is deleted. (Refinement.)
9. **The API takes this shape** (accepted with the user at refinement):
   - `session`: `type Status string` with `StatusOngoing`, `StatusStale` and `StatusClosed`.
     `Session.Updated` is a `time.Time` (zero means unknown). `Session.Size` is an `int64` (0
     until sizes are filled). `Load(ctx, home string, ancestors map[int]bool) ([]Session,
     error)` never computes sizes. `FillSizes(ss []Session)` computes them for the given
     (selected) sessions only. `Delete(ctx, home, ss)` and `DeleteAll(ctx, home, ss)` replace the
     `everything` bool; `DeleteAll` also clears the UI state completely. `OpenDB` returns the
     exported sentinel `ErrNoDB` when the DB file is absent, instead of `nil, nil`.
     `ParseTime(v) (time.Time, bool)`.
   - `backup`: `Create(ctx, home, target string, overwrite bool) error`. One bool is acceptable
     here.
   - `filter`: `Date` stays a civil-date value type with `IsZero()`. `Filters.Until` and
     `Filters.Since` are `Date` values. `Filters.Status` is a `map[session.Status]bool`.
     `filter.Statuses` becomes `[]session.Status`, returned by a function or kept unexported
     with an accessor (A3).
   - `cli`: `Execute(ctx, env *Env, args []string, version string) int`, with every command
     taking `*Env`. The opts structs `listOpts{JSON, Quiet}`, `cleanOpts{All, IncludeOngoing,
     DryRun, Yes, Quiet}` and `backupOpts{List, NoCreate, Yes, JSON, Quiet bool; Keep int}` have
     the cobra flags bound straight into their fields, and the `run*` functions take them.
10. **Once deletion starts, `clean` is not interruptible.** ctx is honoured up to and including
    the confirmation prompt. `Delete`/`DeleteAll` run to completion with their own
    `context.WithoutCancel(ctx)`, and cancellation takes effect afterwards. Restore still rolls
    back through `swapItems`'s undo on cancel, and backup still removes its temp file.
    (Refinement.)
11. **Test seams are unexported function parameters, not globals** (D6):
    `swapItems(home, tmp string, rename func(string, string) error)` and `moveNew(tmp, target
    string, link func(string, string) error)`. Production passes `os.Rename` and `os.Link`.
    `var rename` and `var link` are deleted. (Refinement.)
12. **A prompt honours Ctrl-C.** The blocking read in `readLine` runs in a goroutine and is
    selected against `ctx.Done()`, so a first Ctrl-C at `[y/N]` or `Restore which backup?`
    aborts at once instead of waiting for Enter. This follows from decision 2: without it,
    `NotifyContext` would swallow the signal while stdin blocks. (Refinement.)
13. **The archive format is unchanged**: entry names, the DB snapshot, the lock-dir exclusion and
    modes. Only the Go code around it changes.

### Tasks

#### Task 1 — Go 1.27, dependencies, modernizers (decisions 3, 5)

- `go mod edit -go=1.27.0`, then `go get -u ./... && go mod tidy`. At refinement this lists
  `pflag` v1.1.0, `modernc.org/sqlite` v1.62.0 and `libc` v1.80.1, among others.
- `go fix ./...`. The dry run at refinement rewrote `strings.Split` → `SplitSeq` (filter), a
  loop → `slices.ContainsFunc` (session), a reverse loop → `slices.Backward` (cli/restore) and
  3-clause loops → range-over-int (backup tests).
- `justfile` `lint` recipe: add `go fix -diff ./...` after `go vet`. In
  `.github/workflows/ci.yml`, add a step `- name: go fix\n  run: go fix -diff ./...`.
- Check that `ci.yml` and `release.yml` (`go-version-file: go.mod`) and the justfile's
  `go_version` (now `1.27`, image `golang:1.27`) pick up the change. Nothing is pinned elsewhere.
- Run the tests at this point; commit before refactoring.

#### Task 2 — `session` package (P4–P6, C1, C2, C4, C5, A4, D1, D7, D8; decision 9)

Files: `internal/session/session.go`, `internal/session/delete.go`.
- `Status` type and constants. Every literal `"ongoing"`, `"stale"` or `"closed"` in non-test code
  across all packages uses them.
- `Updated time.Time`; `ParseTime` returns `(time.Time, bool)`. `Sort` uses
  `slices.SortStableFunc` with zero-first, then `Time.Compare`, then `strings.Compare` on the ID.
- `Size int64`. Remove sizing from `Load` and add `FillSizes(ss []Session)`, which leaves DB-only
  rows (`Dir == ""`) at 0.
- C1: `Dirs` returns `nil, nil` only for `errors.Is(err, fs.ErrNotExist)` or a non-dir. Any other
  Stat error is returned.
- C2: `OpenDB` returns `ErrNoDB` when `errors.Is(err, fs.ErrNotExist)` or the file is not
  regular, and the real error otherwise. Callers (`dbSessions`, `HasTurns`, `pruneDB`,
  `backup.snapshotDB`) treat `errors.Is(err, ErrNoDB)` as "no DB".
- C4: `HasTurns` returns `nil, err` on every error path.
- C5: in `ancestorsFrom`, check `i < 0` before slicing.
- ctx is the first parameter of `Load`, `HasTurns`, `Delete`, `DeleteAll` and the internal DB
  helpers, using the `*Context` DB methods (decision 2).
- `Delete`/`DeleteAll` share one unexported implementation. Its ctx is
  `context.WithoutCancel(ctx)` (decision 10).
- A4: doc comments on `StateDir`, `DBFile`, `OpenState` and `SidebarDir`.
- D1: one digit check. Keep `isDigits` in `session` if `filter` and `cli` can do without one
  (`filter` can use `strconv.ParseUint`, since it rejects a sign), or put a single helper
  wherever the remaining callers can import it. The implementer chooses, as long as only one
  hand-written loop remains.
- D7: ignored errors are deliberate (`os.RemoveAll` in delete, `filepath.WalkDir` in
  `dirSize`, `os.ReadDir` in `lockPIDs`). Make each one explicit with `_ =` and keep the existing
  comment.
- D8: drop the blank first line of `pruneUIState`.

#### Task 3 — `filter` package (P7, A1–A3, D5, sort modernisation; decision 9)

File: `internal/filter/filter.go`.
- `Filters.Until` and `Since` become `Date` values, and `Date` gains `IsZero()`. `Any`,
  `Describe` and `Apply` use `IsZero()`. `Apply` uses `Updated.IsZero()` for unknown times.
- `Filters.Status map[session.Status]bool`; `ParseStatuses` returns that map type.
- A1: move `ExpandUser` to `internal/cli`, unexported (`expandUser`).
- A2: move `DateFlag`/`StatusFlag` and their types to `internal/cli` (a new file `flags.go`).
  `filter` no longer imports `pflag`.
- A3: replace the exported mutable `Statuses` with `func Statuses() []session.Status` (a fresh
  slice each call), or move it to `session` as such. Same treatment for `backup.Items` in Task 4.
- D5: `%q` in every error text (`invalid date`, `invalid status`, `no session matches ID`, `ID …
  is ambiguous`). Update the tests that assert them.
- Replace `sortedKeys` with `slices.Sorted(maps.Keys(m))` and `sort.Strings` with `slices.Sort`.

#### Task 4 — `backup` package (A3, D6, D9, X2; decisions 11, 13)

Files: `internal/backup/backup.go`, `internal/backup/restore.go`.
- `Create(ctx, …)`: check `ctx.Err()` in the `addTree` walk callback, so a cancelled backup
  returns and the existing `defer`s remove the temp file and temp dir. `snapshotDB` uses
  `ExecContext`.
- D6: `moveNew(tmp, target, link)`. `Create` passes `os.Link`, and the test passes its failing
  stub. Delete `var link`.
- D9: `os.MkdirAll(filepath.Dir(target), 0o700)`.
- A3: `Items` becomes `func Items() []string`, or unexported with an accessor. `cli/restore.go`
  is its only external user.
- `List`: `slices.Sort(out); slices.Reverse(out)`. `Extract`'s dir sort: `slices.SortFunc` with
  descending `strings.Compare`.
- `Extract(ctx, …)` and `Inspect`: `walk` checks `ctx.Err()` per header, so a cancelled extract
  stops and `runRestore`'s defer removes tmp.

#### Task 5 — `cli` package (P1–P3, C3, D2–D6, X1, X2; decisions 2, 6–12)

Files: `internal/cli/*.go`, `cmd/copilot-sessions/main.go`.
- `Execute(ctx, env *Env, args, version)` calls `root.ExecuteContext(ctx)`. Commands read
  `cmd.Context()`. `DefaultEnv()` returns `*Env`, and `newList(env *Env)` etc.
- P3: keep the reader in `Env.in`, but create it lazily in `readLine` if it is nil, so a
  hand-built `*Env` cannot panic.
- Decision 12: `readLine(ctx, env) (string, error)` runs `env.in.ReadString('\n')` in a goroutine
  and selects on `ctx.Done()`. `confirm` and `restoreSource` propagate `ctx.Err()`.
- P1/P2: `listOpts`, `cleanOpts` and `backupOpts`, with flags bound to their fields; the `run*`
  functions take `(ctx, env, f, opts)`. `runList`/`runClean` call `session.FillSizes(selected)`
  unless `-q` (as today, `--json` always sizes). Delete the unconditional `*s.Size` derefs.
  `clean` calls `DeleteAll` when every session is selected and `Delete` otherwise (the current
  `len(kept) == len(sessions)` rule).
- P5 boundary: `printTable` shows `unknown` for `Updated.IsZero()`. `writeJSON` builds the
  `*string`/`*bool` DTO fields from the zero value and the status (decision 6).
- Decision 8: `runBackupList` JSON `created` uses `backup.Created(b).Format(time.RFC3339)`.
  Delete `isoformat`.
- C3: `runRestore` reports `backup not found: <path>` only for `fs.ErrNotExist` or a non-regular
  file, and otherwise `fmt.Errorf("backup %s: %w", source, err)`.
- D2: `func usageArgs(v cobra.PositionalArgs) cobra.PositionalArgs` wraps errors in `usageError`.
  `backup` and `restore` use `usageArgs(cobra.MaximumNArgs(1))`.
- D3: one `newJSONEncoder(w io.Writer) *json.Encoder` (2-space indent, no HTML escaping), used by
  `writeJSON` and `runBackupList`. `session.writeJSONFile` keeps its own: it is a different
  package and a single use.
- D4: try replacing the `strings.HasPrefix(err.Error(), "unknown command ")` match with a typed
  path. If cobra offers none that keeps the "Did you mean this?" behaviour CS-012 requires, keep
  the string match with a one-line comment saying why. Either outcome is acceptable; record which
  in the summary.
- D5: `%q` in `invalid choice`.
- D6/decision 11: `swapItems(home, tmp, rename)`. `runRestore` passes `os.Rename`, and
  `TestRestoreRollback` calls `swapItems` (or `runRestore` with an injectable seam) with a failing
  stub. Delete `var rename`.
- Decision 10: `runClean` passes ctx to `Delete`/`DeleteAll`, which detach from cancellation
  internally. After deletion returns, `runClean` returns `ctx.Err()` if ctx was cancelled during
  it.
- X1: `main.go` becomes `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt);
  code := cli.Execute(ctx, …); stop()`. If `ctx.Err() != nil` and the run ended with an error,
  print `\nAborted.` and exit 130; otherwise exit with `code`. Remove the `ponytail:` comment it
  resolves. A5: rename `Version` to `version` in `main.go`, the `justfile` `build` recipe and
  `.goreleaser.yaml` (`-X main.version=…`).
- Replace the cli's `sort` uses with `slices`.

#### Task 6 — Tests (decisions 7, 11)

- Delete `internal/cli/parity_test.go`. Move `pythonRunner` (and any helper `TestRestoreCompat`
  needs) into `internal/cli/restore_test.go` or a `compat_test.go`. `TestRestoreCompat` must still
  run, not skip, with `python3` present.
- Update every test for the new signatures: `session.Load`/`FillSizes`/`Delete`/`DeleteAll`,
  `backup.Create`, `cli.Execute(ctx, *Env, …)`, `Status` constants, `time.Time` `Updated`, `%q`
  error texts, and RFC 3339 `created`.
- New tests:
  - (a) C1/C2/C3: a home whose `session-state` cannot be traversed (chmod 000 on a parent; skip
    when running as root) makes `list` exit 1 with the permission error, not "No sessions match.".
    An unreadable backup path makes `restore` report the wrapped error, not "backup not found".
  - (b) cancellation: `backup.Create` with an already-cancelled ctx returns `context.Canceled`
    and leaves no `.copilot-sessions-*.tmp` in the target dir. `Extract` with a cancelled ctx
    returns `context.Canceled`. `runRestore` with a ctx cancelled at the confirmation prompt (a
    stdin that never sends a line) returns promptly with `context.Canceled`, and home is unchanged.
  - (c) decision 10: `Delete` with a cancelled ctx still deletes everything it was given (dirs, DB
    rows, UI entries).
  - (d) `FillSizes` leaves DB-only rows at 0 and sizes the others.
  - (e) a zero-value `Env` with only `Stdin` set does not panic in `readLine` (P3).
- `go test -race ./...` must pass. The removed globals make `t.Parallel()` safe in the affected
  tests, but adding it is optional.

#### Task 7 — Spec (`development/spec.md`; decisions 1, 6–8, 10, 12)

- §2: "Go 1.27 or later".
- §6 *Streams* (l. ~172): Ctrl-C still prints `\nAborted.` and exits 130. Add: a restore in
  progress is rolled back, temp files are removed, and once `clean` starts deleting it finishes
  first.
- §6.3 (l. ~381): `created` is RFC 3339 local time with offset.
- §6.1 JSON: unchanged (decision 6). Re-read it and adjust only if wording referenced Python.
- §9: drop the parity requirement from the test list, keep case 12, and add the cancellation and
  permission-error cases.
- §11: a "Go idioms (CS-013)" sub-list. Error texts use Go quoting (`"…"`, not `'…'`).
  Permission errors on `COPILOT_HOME`, the DB or a backup file are reported, not treated as
  absent. `created` is RFC 3339 with offset. Ctrl-C rolls back a restore and aborts prompts at
  once. Replace "Everything else (flags, aliases, the archive format, and message texts) must
  match" with "The archive format stays compatible both ways (§9 case 12); flags and aliases are
  unchanged."
- §13: replace the parity bullet with "`go fix -diff ./...` is clean", and keep case 12.

### Acceptance test

From the repo root on the feature branch:

1. `grep '^go ' go.mod` prints `go 1.27.0`, and `go list -m -u github.com/spf13/cobra
   github.com/spf13/pflag modernc.org/sqlite` shows no `[…]` upgrade for any of the three.
2. `just lint && just test && just test-linux` are all green. `just lint` now includes
   `go fix -diff ./...`. `go test -race ./...` is green. `go test ./internal/cli -run
   TestRestoreCompat -v` shows `--- PASS` (not SKIP). `test ! -e internal/cli/parity_test.go`.
3. Code checks (each prints nothing):
   ```sh
   grep -rn '"sort"' --include='*.go' internal cmd
   grep -rn 'var \(rename\|link\) = ' --include='*.go' internal
   grep -rn 'pflag' internal/filter
   grep -rnE '"(ongoing|stale|closed)"' --include='*.go' internal | grep -v _test.go | grep -v 'Status = '
   grep -rn "'%s'" --include='*.go' internal
   grep -rn 'signal.Notify(' cmd
   ```
4. CLI probes (`just build` first):
   ```sh
   export COPILOT_HOME=$(mktemp -d) COPILOT_BACKUP_DIR=$(mktemp -d); c=./copilot-sessions
   $c list --until bogus 2>&1 | grep -q 'invalid date "bogus"' && echo quote-ok
   $c backup -q >/dev/null && $c backup --list --json | grep -qE '"created": "[0-9T:-]+[+-][0-9]{2}:[0-9]{2}"' && echo created-ok
   mkdir -p "$COPILOT_HOME/session-state" && chmod 000 "$COPILOT_HOME"; $c list; echo "perm=$?"; chmod 700 "$COPILOT_HOME"
   $c restore "$COPILOT_BACKUP_DIR"/copilot-sessions-*.tar.gz </dev/null; echo "notty=$?"
   ```
   Expected: `quote-ok`, `created-ok`, a permission-denied error with `perm=1`, and `notty=1`
   (`confirmation required; rerun with --yes`).
5. Ctrl-C (manual, on a TTY): `$c restore latest`, then Ctrl-C at `Continue? [y/N]` prints
   `Aborted.` immediately and exits 130 (`echo $?`), and `ls -a "$COPILOT_HOME"` shows no `tmp*`
   dir.
6. `ls tickets/2-ready/` still lists CS-009 and CS-010 with `depends-on` containing CS-013 (no
   change; they get refreshed at their pickup).

### Docs update (mandatory when user-facing)

- `development/spec.md` per Task 7.
- `README.md`: next to the `go install …@latest` line (l. ~141), state that it needs Go 1.27 or
  later. README does not describe the JSON formats or Python parity (checked at refinement), so
  nothing else changes.
- `CHANGELOG.md` `[Unreleased]` → `### Changed`: "Built with Go 1.27 and current dependencies.
  Error messages quote values Go-style (`"…"`). Permission errors on the Copilot home, its DB or a
  backup file are reported instead of read as 'no sessions' / 'backup not found'. `backup --list
  --json` `created` is RFC 3339 with the local offset. Ctrl-C aborts prompts immediately and rolls
  back an in-progress restore (CS-013)." Mark it breaking for scripts that parse error text or
  `created`.
- `RELEASING.md`: no change. It names neither `main.Version` nor a Go version (checked at
  refinement).

### Finish (mandatory)

1. Acceptance tests green; `just lint`, `just test`, `just test-linux` and `go test -race ./...`
   clean.
2. Docs updated (spec, README, CHANGELOG).
3. Summary: files touched, decisions honoured, which D4 outcome was chosen, anything deferred.
4. Suggested commits (root-path child, tidied history kept by default), e.g.:
   ```
   build: require Go 1.27, update dependencies, gate go fix in lint (CS-013)
   refactor(session): Status type, value times and sizes, explicit DB absence (CS-013)
   refactor(filter,backup): value dates, modern slices, no flag adapters (CS-013)
   feat(cli)!: context cancellation, opts structs, Go-style errors (CS-013)

   BREAKING CHANGE: error text quoting, backup --list --json created format,
   and permission errors that used to read as empty.
   test: retire Python output parity, keep archive compatibility (CS-013)
   docs(spec): record CS-013's intended differences (CS-013)
   ```
5. Tidy WIP commits, commit locally, and do not push. Move to `4-in-review/`.

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: chat: Go 1.27 upgrade plus whole-codebase idiomatic-Go pass from the 2026-11-09 code review, idiom over Python parity
- 2026-11-09 — TO DO → READY: plan complete
