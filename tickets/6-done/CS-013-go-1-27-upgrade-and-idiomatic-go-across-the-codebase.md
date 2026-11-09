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
"no sessions" or "backup not found". `/dev/null` on stdin no longer counts as a terminal. Ctrl-C
cancels cleanly before any irreversible step, aborts prompts at once and leaves no temp files. Wherever idiomatic Go and Python-script parity disagree, idiom wins, and spec §11
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
- C6 (found by the plan challenge, F14): `DefaultEnv().IsTTY` checks only `ModeCharDevice`,
  which is true for `/dev/null`. A cron `clean` with `</dev/null` and no `-y` prints a prompt and
  exits 0 with `Aborted.`, instead of failing with `confirmation required` and exit 1.

*Modern stdlib*: replace `sort.SliceStable`/`sort.Slice`/`sort.Strings`/`sort.Reverse` with
`slices.SortStableFunc`/`SortFunc`/`Sort`/`Reverse`, and `sortedKeys` with
`slices.Sorted(maps.Keys(m))`. `strings.Lines` in `readWorkspace` is dropped: `FieldsFunc` also
splits on a bare `\r` and skips blank lines, so swapping it in would change parsing.

*Context and signals*
- X1: `cmd/copilot-sessions/main.go` handles Ctrl-C with `signal.Notify` and `os.Exit(130)`, which
  skips deferred cleanup and can leave a `tmp*` dir in `COPILOT_HOME` holding the only copy of a
  restored item (the `ponytail:` comment there, CS-007). Replace it with `signal.NotifyContext` +
  `root.ExecuteContext`, so Ctrl-C cancels before any irreversible step and the deferred cleanup
  runs. The exit code stays 130.
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
2. Context and signals are in scope (X1 and X2): `signal.NotifyContext`, cancellation honoured
   before every irreversible step, and ctx on I/O functions. Amended after the plan challenge:
   the earlier "a restore that rolls back on Ctrl-C" was not achievable as planned (decision 10).
3. The Go 1.27 bump includes upgrading all dependencies and running the `go fix` modernizers.
4. This ticket goes before CS-009 and CS-010 (both READY), which now have `depends-on: [CS-013]`.
   Both rewrite process detection in `internal/session/session.go`, which this ticket refactors,
   so their plans need a refresh against the new APIs at pickup.

The questions left open at filing (lint tooling, JSON nullability, P2 shape, D6, `filter.Date`)
were settled with the user at refinement on 2026-11-09. They are decisions 5–13 below. An
independent plan challenge the same day found F1–F16. The user approved amending decisions 2, 8,
10 and 12 and adding decisions 14 and 15, and the other findings are folded into the tasks.
Every visible change is listed in spec §11 and CHANGELOG.

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
   On cancel, the binary prints only `\nAborted.` to stderr (no `error:` line) and exits with 130,
   as before. Temp dirs and files are removed by their existing `defer`s. After the first signal,
   default handling is restored, so a second Ctrl-C kills the process at once, as today.
   (Filing; amended after the plan challenge, F1/F2.)
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
8. **`backup --list --json` `created` is RFC 3339 in local time with a numeric offset**, e.g.
   `2026-10-31T12:00:00+02:00`, and `+00:00` in UTC, never `Z`. The layout is
   `"2006-01-02T15:04:05-07:00"`, not `time.RFC3339`; the file-name stamp has no sub-second part.
   The `isoformat` helper is deleted. (Refinement; amended after the plan challenge, F5.)
9. **The API takes this shape** (accepted with the user at refinement):
   - `session`: `type Status string` with `StatusOngoing`, `StatusStale` and `StatusClosed`.
     `Session.Updated` is a `time.Time` (zero means unknown). `Session.Size` is an `int64` (0
     until sizes are filled). `Load(ctx, home string, ancestors map[int]bool) ([]Session,
     error)` never computes sizes, and checks `ctx.Err()` once per session dir.
     `FillSizes(ctx, ss []Session) error` computes them for the given (selected) sessions only,
     checking `ctx.Err()` per session. `Delete(ctx, home, ss)` and `DeleteAll(ctx, home, ss)` replace the
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
10. **Cancellation is checked right before each irreversible step, and never after it.** Each
    command checks `if err := ctx.Err(); err != nil { return err }` immediately before its point of
    no return: `session.Delete`/`DeleteAll` in `runClean`, `swapItems` in `runRestore`, and the
    `os.Remove` loop in `applyRetention`. This holds with `-y` too, where no prompt intervenes.
    Past that point the command runs to completion, prints its normal output (`Deleted N
    sessions.`, the `-q` IDs, `Restored …`) and exits 0. `Delete`/`DeleteAll` use
    `context.WithoutCancel(ctx)` internally. `swapItems` takes no ctx: it is a few fast renames,
    and its existing undo still runs on a rename *failure*. Ctrl-C before the swap leaves home
    untouched and removes the temp dir. Backup removes its temp file on cancel. (Refinement;
    amended after the plan challenge, F1/F3, overturning "rollback on Ctrl-C" and "return
    ctx.Err() after deletion".)
11. **Test seams are unexported function parameters, not globals** (D6):
    `swapItems(home, tmp string, rename func(string, string) error)` and `moveNew(tmp, target
    string, link func(string, string) error)`. Production passes `os.Rename` and `os.Link`.
    `var rename` and `var link` are deleted. (Refinement.)
12. **A prompt honours Ctrl-C.** `readLine(ctx, env) (string, error)` aborts at once on a first
    Ctrl-C at `[y/N]` or `Restore which backup?`, instead of waiting for Enter. Without this,
    `NotifyContext` would swallow the signal while stdin blocks. The contract:
    - it creates `env.in` lazily, before the goroutine starts;
    - it returns `ctx.Err()` if ctx is already done, before reading (a `select` with both cases
      ready picks at random);
    - it runs `ReadString('\n')` in a goroutine that sends on a 1-buffered channel, and selects
      that channel against `ctx.Done()`;
    - EOF is not an error: a partial line or `""` is returned as today, so `y` with no newline
      and an empty pick (which defaults to 1) keep working;
    - only `ctx.Err()` is returned as an error.

    The goroutine left behind after a cancel is harmless, because the run ends. (Refinement;
    amended after the plan challenge, F9.)
13. **The archive format is unchanged**: entry names, the DB snapshot, the lock-dir exclusion and
    modes. Only the Go code around it changes.
14. **TTY detection uses a terminal check, not `ModeCharDevice`** (C6). `DefaultEnv().IsTTY` is
    `term.IsTerminal(int(os.Stdin.Fd()))` from `golang.org/x/term`. That is a new direct
    dependency, but `golang.org/x/sys` is already in the graph, and `x/term` is portable to the
    macOS and Windows work in CS-009/CS-010. `/dev/null` and pipes are then non-TTY, and a prompt
    without `-y` fails with `confirmation required; rerun with --yes`, exit 1. (Plan challenge
    F14, user 2026-11-09.)
15. **`Describe` uses Go quoting too**: `name contains "…"` via `%q`, consistent with the error
    texts (D5). (Plan challenge F11, user 2026-11-09.)

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
- `Size int64`. Remove sizing from `Load` and add `FillSizes(ctx, ss []Session) error`, which
  leaves DB-only rows (`Dir == ""`) at 0 and checks `ctx.Err()` per session. `Load` checks
  `ctx.Err()` once per session dir (decision 9, F1).
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
  `context.WithoutCancel(ctx)`, so once started it runs to completion (decision 10). The caller
  checks ctx before calling.
- A4: doc comments on `StateDir`, `DBFile`, `OpenState` and `SidebarDir`.
- D1: one digit check. Keep `isDigits` in `session` if `filter` and `cli` can do without one
  (`filter` can use `strconv.ParseUint`, since it rejects a sign), or put a single helper
  wherever the remaining callers can import it. The implementer chooses, as long as only one
  hand-written loop remains.
- D7: these ignored errors are deliberate and become explicit with `_ =` (or are handled where
  the comment says so), each with its reason as a comment:
  - session: `os.RemoveAll` in delete, `filepath.WalkDir` in `dirSize`, `os.ReadDir` in
    `lockPIDs`, `filepath.Glob` in `pruneUIState`.
  - backup: `os.Remove(f.Name())`, `os.Remove(tmp)` and `c.Close()` in `backup.go`;
    `root.Remove(name)` in `restore.go`.
  - cli: the deferred `os.RemoveAll(tmp)` in `restore.go`; the 7× `RegisterFlagCompletionFunc`,
    `filepath.Glob` and `backup.List` in `complete.go`.
- D8: drop the blank first line of `pruneUIState`.

#### Task 3 — `filter` package (P7, A1–A3, D5, sort modernisation; decision 9)

File: `internal/filter/filter.go`.
- `Filters.Until` and `Since` become `Date` values, and `Date` gains `IsZero()`. `Any`,
  `Describe` and `Apply` use `IsZero()`. `Apply` uses `Updated.IsZero()` for unknown times.
- `Filters.Status map[session.Status]bool`; `ParseStatuses` returns that map type.
- A1: move `ExpandUser` to `internal/cli`, unexported (`expandUser`).
- A2: move `DateFlag`/`StatusFlag` and their types to `internal/cli` (a new file `flags.go`).
  `filter` no longer imports `pflag`. `dateFlag.String()` returns `""` when the date
  `IsZero()`, so help shows no `(default 0000-00-00)` (F8).
- A4: doc comments on the `Date` fields `Y`, `M` and `D`.
- A3: replace the exported mutable `Statuses` with `func Statuses() []session.Status` (a fresh
  slice each call), or move it to `session` as such. Same treatment for `backup.Items` in Task 4.
- D5: `%q` in every error text (`invalid date`, `invalid status`, `no session matches ID`, `ID …
  is ambiguous`), and in `Describe`'s `name contains %q` (decision 15). Update the tests that
  assert them.
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

#### Task 5 — `cli` package (P1–P3, C3, C6, D2–D6, X1, X2; decisions 2, 6–12, 14)

Files: `internal/cli/*.go`, `cmd/copilot-sessions/main.go`.
- `Execute(ctx, env *Env, args, version)` calls `root.ExecuteContext(ctx)`. Commands read
  `cmd.Context()`. `DefaultEnv()` returns `*Env`, and `newList(env *Env)` etc.
- P3: keep the reader in `Env.in`, but create it lazily in `readLine` if it is nil, so a
  hand-built `*Env` cannot panic.
- Decision 12: implement `readLine(ctx, env) (string, error)` to the contract given there.
  `confirm` and `restoreSource` propagate `ctx.Err()`.
- Decision 10: add `if err := ctx.Err(); err != nil { return err }` immediately before
  `session.Delete/DeleteAll` in `runClean`, before `swapItems` in `runRestore`, and before the
  `os.Remove` loop in `applyRetention`. Past those points, return the normal result.
- Decision 14 (C6): `DefaultEnv().IsTTY` uses `term.IsTerminal(int(os.Stdin.Fd()))`. Add
  `golang.org/x/term` with `go get golang.org/x/term@latest`.
- F15: remove `printTable`'s `showSize` parameter; both callers pass `true`.
- P1/P2: `listOpts`, `cleanOpts` and `backupOpts`, with flags bound to their fields; the `run*`
  functions take `(ctx, env, f, opts)`. `runList`/`runClean` call `session.FillSizes(selected)`
  unless `-q` (as today, `--json` always sizes), and return its error. Delete the unconditional `*s.Size` derefs.
  `clean` calls `DeleteAll` when every session is selected and `Delete` otherwise (the current
  `len(kept) == len(sessions)` rule).
- P5 boundary: `printTable` shows `unknown` for `Updated.IsZero()`. `writeJSON` builds the
  `*string`/`*bool` DTO fields from the zero value and the status (decision 6).
- Decision 8: `runBackupList` JSON `created` uses
  `backup.Created(b).Format("2006-01-02T15:04:05-07:00")`. Delete `isoformat`.
- C3: `runRestore` reports `backup not found: <path>` only for `fs.ErrNotExist` or a non-regular
  file, and otherwise `fmt.Errorf("backup %s: %w", source, err)`.
- D2: `func usageArgs(v cobra.PositionalArgs) cobra.PositionalArgs` wraps errors in `usageError`.
  `backup` and `restore` use `usageArgs(cobra.MaximumNArgs(1))`.
- D3: one `newJSONEncoder(w io.Writer) *json.Encoder` (2-space indent, no HTML escaping), used by
  `writeJSON` and `runBackupList`. `session.writeJSONFile` keeps its own: it is a different
  package and a single use.
- D4: keep the `strings.HasPrefix(err.Error(), "unknown command ")` match, with a one-line comment
  saying why. The plan challenge found that cobra 1.10.2 has no typed unknown-command error, and
  that a custom `root.Args` is never reached for a non-runnable root. Re-check after the cobra
  upgrade in Task 1, and switch only if a typed path now exists and keeps "Did you mean this?".
- D5: `%q` in `invalid choice`.
- D6/decision 11: `swapItems(home, tmp, rename)`. Split `runRestore` into a thin wrapper around
  an unexported `restore(ctx, env, args, yes, rename)`, which `runRestore` calls with `os.Rename`.
  `TestRestoreRollback` drives `restore` end to end with a failing stub, keeping both its
  error-text and its `keepTmp` assertions (F12). Delete `var rename`.
- X1/F2: in `Execute`, if `err != nil && ctx.Err() != nil`, print only `\nAborted.` to stderr and
  return 130, with no `error:` line. Test `ctx.Err()`, not `errors.Is(err, context.Canceled)`,
  because modernc's interrupt error is not `context.Canceled`. Every other error keeps the
  `error: …` line and exit 1 or 2.
- X1: `main.go` becomes `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt);
  defer stop(); go func() { <-ctx.Done(); stop() }()` (a second Ctrl-C then kills at once), then
  `os.Exit(cli.Execute(ctx, cli.DefaultEnv(), os.Args[1:], resolveVersion()))`. Remove the
  `ponytail:` comment it resolves. A5: rename `Version` to `version` in `main.go`, the `justfile` `build` recipe and
  `.goreleaser.yaml` (`-X main.version=…`).
- Replace the cli's `sort` uses with `slices`.

#### Task 6 — Tests (decisions 2, 7, 10–12, 14)

- Delete `internal/cli/parity_test.go`. Move `pythonRunner` (and any helper `TestRestoreCompat`
  needs) into `internal/cli/restore_test.go` or a `compat_test.go`. `TestRestoreCompat` must still
  run, not skip, with `python3` present.
- Update every test for the new signatures: `session.Load`/`FillSizes`/`Delete`/`DeleteAll`,
  `backup.Create`, `cli.Execute(ctx, *Env, …)`, `Status` constants, `time.Time` `Updated`, `%q`
  error texts, and RFC 3339 `created`.
- New tests:
  - (a) C1/C2/C3:
    - A home whose `session-state` cannot be traversed (mode 000 on its parent) makes `list` exit
      1 with the permission error, not "No sessions match.".
    - A backup whose *parent directory* is mode 000 makes `restore` report the wrapped
      permission error, not "backup not found". A mode-000 *file* already reports `permission
      denied` today, so it does not test the fix (F7).
    - Restore 0700 in `t.Cleanup`, and skip when running as root.
  - (b) cancellation:
    - `backup.Create` with an already-cancelled ctx returns an error and leaves no
      `.copilot-sessions-*.tmp` in the target dir.
    - `Extract` with a cancelled ctx returns an error.
  - (c) decision 10:
    - `session.Delete` with a cancelled ctx still deletes everything it was given (dirs, DB rows,
      UI entries).
    - `clean -y` with a ctx cancelled before the delete deletes nothing, exits 130 and prints
      `Aborted.`.
    - `restore -y` with a ctx cancelled after extraction leaves home unchanged and no `tmp*` dir.
    - `backup --keep 1 -y` with a cancelled ctx removes no old backup.
  - (f) F2: `Execute` with ctx cancelled at the `[y/N]` prompt (a stdin that never sends a line)
    returns 130 promptly, and stderr ends in exactly `[y/N] \nAborted.\n`, with no `error:` line.
    Home is unchanged.
  - (g) F8: `clean --help` output contains no `0000-00-00`.
  - (d) `FillSizes` leaves DB-only rows at 0 and sizes the others.
  - (e) a zero-value `Env` with only `Stdin` set does not panic in `readLine` (P3).
- `go test -race ./...` must pass. The removed globals make `t.Parallel()` safe in the affected
  tests, but adding it is optional.

#### Task 7 — Spec (`development/spec.md`; decisions 1, 6–8, 10, 12, 14, 15)

- §2: "Go 1.27 or later".
- §6 *Streams* (l. ~172): Ctrl-C still prints `\nAborted.` and exits 130. Add:
  - Ctrl-C aborts a prompt at once.
  - Cancellation is honoured up to the irreversible step of each command (deleting sessions,
    swapping restored items in, removing old backups). Past it, the command finishes, reports
    normally and exits 0.
  - Ctrl-C before the swap leaves home untouched, and temp files are removed.
  - A second Ctrl-C kills at once.
  - Stdin that is not a terminal (including `/dev/null`) counts as no TTY.
- §6.3 (l. ~381): `created` is RFC 3339 local time with a numeric offset (`+00:00` in UTC). The
  backup dir is created with mode 0700.
- §6.1 JSON: unchanged (decision 6). Re-read it and adjust only if wording referenced Python.
- §9: drop the parity requirement from the test list, keep case 12, and add the cancellation and
  permission-error cases.
- §11: a "Go idioms (CS-013)" sub-list. Error texts use Go quoting (`"…"`, not `'…'`).
  Permission errors on `COPILOT_HOME`, the DB or a backup file are reported, not treated as
  absent. `created` is RFC 3339 with a numeric offset. Ctrl-C aborts prompts at once and is
  honoured before irreversible steps only. `/dev/null` stdin is not a TTY. The backup dir is
  created 0700, not 0755. `Describe` quotes with `"…"`. Replace "Everything else (flags, aliases, the archive format, and message texts) must
  match" with "The archive format stays compatible both ways (§9 case 12); flags and aliases are
  unchanged."
- §13: replace the parity bullet with "`go fix -diff ./...` is clean", and keep case 12.

### Acceptance test

From the repo root on the feature branch:

1. `grep '^go ' go.mod` prints `go 1.27.0`. At implementation time, `go list -m -u
   github.com/spf13/cobra github.com/spf13/pflag modernc.org/sqlite` showed no `[…]` upgrade;
   record its output in the summary, because the check goes stale as soon as upstream releases.
   The sqlite/libc coupling holds:
   ```sh
   [ "$(go mod graph | awk '$1 ~ /^modernc.org\/sqlite@/ && $2 ~ /^modernc.org\/libc@/ {print $2}')" = "modernc.org/libc@$(go list -m -f '{{.Version}}' modernc.org/libc)" ] && echo libc-ok
   ```
   prints `libc-ok`.
2. `just lint && just test && just test-linux` are all green. `just lint` now includes
   `go fix -diff ./...`. `go test -race ./...` is green. `go test ./internal/cli -run
   TestRestoreCompat -v` shows `--- PASS` (not SKIP). `test ! -e internal/cli/parity_test.go`.
3. Code checks (each prints nothing):
   ```sh
   grep -rn '"sort"' --include='*.go' internal cmd
   grep -rn 'var \(rename\|link\) = ' --include='*.go' internal
   grep -rn 'pflag' internal/filter
   grep -rnE '"(ongoing|stale|closed)"' --include='*.go' internal | grep -v _test.go | grep -vE 'Status(Ongoing|Stale|Closed) +Status = '
   grep -rn "'%s'" --include='*.go' internal
   grep -rn 'signal.Notify(' cmd
   grep -rn "contains '" internal/filter
   ```
4. CLI probes (`just build` first):
   ```sh
   export COPILOT_HOME=$(mktemp -d) COPILOT_BACKUP_DIR=$(mktemp -d); c=./copilot-sessions
   $c list --until bogus 2>&1 | grep -q 'invalid date "bogus"' && echo quote-ok
   mkdir -p "$COPILOT_HOME/session-state/abc"
   $c backup -q >/dev/null && $c backup --list --json | grep -qE '"created": "[0-9T:-]+[+-][0-9]{2}:[0-9]{2}"' && echo created-ok
   TZ=UTC $c backup --list --json | grep -q '+00:00"' && echo utc-offset-ok
   : | $c restore "$COPILOT_BACKUP_DIR"/copilot-sessions-*.tar.gz 2>&1 | grep -q 'confirmation required' && echo notty-ok
   $c restore "$COPILOT_BACKUP_DIR"/copilot-sessions-*.tar.gz </dev/null 2>&1 | grep -q 'confirmation required' && echo devnull-ok
   $c clean --help | grep -q '0000-00-00' || echo no-zero-default-ok
   chmod 000 "$COPILOT_HOME"; $c list; echo "perm=$?"; chmod 700 "$COPILOT_HOME"
   ```
   Expected: `quote-ok`, `created-ok`, `utc-offset-ok`, `notty-ok`, `devnull-ok`,
   `no-zero-default-ok`, then a permission-denied error with `perm=1`. Skip the `chmod` line as
   root.
5. Ctrl-C (manual, on a TTY): `$c restore latest`, then Ctrl-C at `Continue? [y/N]`. It prints
   `Aborted.` immediately, with no `error:` line, and exits 130 (`echo $?`).
   `ls -a "$COPILOT_HOME"` shows no `tmp*` dir. Test (f) covers the same path automatically.
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
  --json` `created` is RFC 3339 with a numeric local offset. Ctrl-C aborts prompts immediately and
  is honoured before any irreversible step; a second Ctrl-C kills at once. `/dev/null` on stdin no
  longer counts as a terminal, so an unattended `clean`/`restore` without `-y` fails instead of
  aborting silently. The backup directory is created with mode 0700 (CS-013)." Mark it breaking
  for scripts that parse error text or `created`, or that relied on `</dev/null` exiting 0.
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
   permission errors that used to read as empty, and </dev/null no longer
   counts as a TTY.
   test: retire Python output parity, keep archive compatibility (CS-013)
   docs(spec): record CS-013's intended differences (CS-013)
   ```
5. Tidy WIP commits, commit locally, and do not push. Move to `4-in-review/`.

## Review

### Review 1 — 2026-11-09 (branch `feat/CS-013-go-1-27-idiomatic` @ 2e9d5e3)

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| R1 | blocking | correctness | — | Once ctx is done, `Execute` replaces *every* error with `Aborted.`/130, including errors from steps that run to completion past the point of no return | Reproduced (delegated reviewer): `Execute(cancelledCtx, env, ["restore","-y","/nonexistent.tar.gz"])` → 130, stderr `"\nAborted.\n"`; the same call via `restore()` returns `backup not found: …`. Traced: Ctrl-C during `swapItems` + rename failure → `restore failed and rollback incomplete; old data kept in <tmp>/old` is hidden; a `pruneDB` failure inside `Delete` after dirs are gone reads as `Aborted.`. `internal/cli/cli.go` `Execute` (CS-013 decision 2, Task 5 X1/F2) | Classify cancellation at the source: DB helpers map a driver error to `ctx.Err()` when ctx is done; `Execute` prints `Aborted.`/130 only for `errors.Is(err, context.Canceled)` |
| R2 | blocking | correctness | — | `backup.Create` never re-checks ctx after the walk, so a Ctrl-C after the last walk callback still publishes, and with an explicit FILE replaces the existing file, then reports `Aborted.` | unreproduced: timing window between the last `WalkDir` callback and `os.Rename`; traced `internal/backup/backup.go` `Create` → `tw.Close`/`gz.Close`/`f.Close` → `os.Rename(f.Name(), target)` with no ctx check; contradicts spec §6 "honoured up to each command's irreversible step" (CS-013 decision 10) | `if err := ctx.Err(); err != nil { return err }` immediately before the publish |
| R3 | non-blocking | test-gap | noted | The `ctx.Err()` check right before `swapItems` has no test; `TestRestoreCancelled` cancels before `Extract`, so the extract-cancel path is what it exercises (Task 6 (c) partially met) | Mutation (delegated reviewer): deleting the check leaves `go test ./internal/cli -run 'Cancel\|Restore'` green | No seam between `Extract` returning and the check without a counting-ctx contortion; the check is a two-line guard verified by reading |
| R4 | non-blocking | other | fixed inline | `go.mod` not tidy: `x/term` marked indirect though imported, `pflag` listed direct though no longer imported | `go mod tidy -diff` exits 1 | `go mod tidy` |
| R5 | non-blocking | stale-xref | fixed inline | spec §1 still says the Go version must keep the Python behaviour and output and that the script is the tiebreaker, contradicting decision 1 and the narrowed §11 | `development/spec.md:8-10` | Narrow §1 to archive compatibility, the spec as tiebreaker |
| R6 | non-blocking | stale-xref | fixed inline | Test renamed to `TestResolveversion` by the `Version`→`version` replace | `cmd/copilot-sessions/main_test.go:5` | Rename back to `TestResolveVersion` |
| R7 | non-blocking | design | fixed inline | `defer stop()` in `main` never runs: `main` ends in `os.Exit` after an explicit `stop()` | `cmd/copilot-sessions/main.go` | Drop the defer |
| R8 | non-blocking | design | noted | C3's wrapped error repeats the path (`backup /x: stat /x: permission denied`) | CLI probe; wording is Task 5's `fmt.Errorf("backup %s: %w", source, err)` | Changing it is a visible-text change; leave |
| R9 | non-blocking | design | noted | `session.ParseUint` is an exported generic helper in the domain package; `ParseStatuses` builds its own name list | `internal/session/session.go`, `internal/filter/filter.go` | Within Task 2 D1's latitude |
| R10 | non-blocking | design | noted | `env.in` is cached on a reused `*Env`; a second `Execute` on the same Env with a swapped `Stdin` would read the old reader | `internal/cli/confirm.go` `readLine` | Production builds one Env per process; tests build one per run |
| R11 | non-blocking | design | noted | `golang.org/x/term` added although `mattn/go-isatty` is already in the graph | `go.mod` | Locked by CS-013 decision 14 (user, 2026-11-09) |

Disposition summary: 2 blocking (R1, R2) → rework; 9 non-blocking — 4 fixed inline (R4–R7, applied in the rework round), 5 noted (R3, R8–R11); 0 new tickets.

cost: estimated L, actual L

Also observed, outside the diff: pre-existing mojibake at `development/spec.md` §9 case 11 (`Ôćĺ`, on `main` before this branch); SIGTERM/SIGHUP still exit without cleanup (only `os.Interrupt` is in scope).

Checklist:
- [x] Reviewer independence (step 0): delegated — fresh general-purpose reviewer ran steps 1–4a with `hunt: angle 7 only`; orchestrator authored the branch. All 7 delegated findings re-verified by hand; 0 discarded (its F1 = R1, F2 = R3, F3 = R4, F4 = R8, F5 = R5, F6 = R6, F7 = R9).
- [x] Stale-branch check (step 0a): `pickle doctor` warned (branch had the ticket in 3-in-development); rebased onto main, re-run clean.
- [x] Implementation audit (step 2): Tasks 1–7 and decisions 1–15 met, except Task 6 (c)'s restore case partially met (R3). Acceptance steps 1–4 and 6 re-run green by the delegate and by the implementer; step 5 run by the implementer through a pty (`^C` at `Continue? [y/N]` → 130, `Aborted.`, no `tmp*`).
- [x] Correctness hunt (step 3): host tool path — `/code-review high` on `main...feat/CS-013-go-1-27-idiomatic`, 8 findings, 0 discarded (R1, R2, R4, R6, R7, R9, R10, R11); angle 7 by the delegate, nothing found.
- [x] Consistency audit (step 4): delegate searched the whole tree for stale names; R4, R5.
- [x] Docs audit (step 4a): spec, README, CHANGELOG cover the visible changes; no docs build configured.
- [x] Docs-readability (step 4b): skipped — optional; deferred to the re-review round.

Verdict: rework (R1, R2).

### Rework fix record — round 1 (commits 08cb9f2..8ee65d6)

- R1: `Execute` prints `Aborted.`/130 only for `errors.Is(err, context.Canceled)`; every other error is reported even after a cancel. `session.dbSessions`/`HasTurns` (new `ctxErr`) and `backup.snapshotDB` return `ctx.Err()` in place of the driver's interrupt error once ctx is done, so a cancelled query still classifies as a cancel. Spec §6 gains the rule. Regression: `TestCancelDoesNotHideErrors` (cancelled `restore` of a missing file → `error: backup not found`, exit 1; cancel during the swap with a failing rollback → the `old data kept in …` error, not a cancel). Fails with the old `ctx.Err() != nil` check.
- R2: `backup.Create` checks `ctx.Err()` immediately before publishing. Regression: `TestCreateCancelledBeforePublish` (cancelled ctx, empty home, existing FILE → `context.Canceled`, FILE unchanged, no temp file). Fails without the check.
- R4: `go mod tidy` (`x/term` direct, `pflag` indirect); `go mod tidy -diff` clean.
- R5: spec §1 narrowed to flags, aliases and archive compatibility; idiom wins, spec is the tiebreaker.
- R6: `TestResolveVersion` restored.
- R7: dead `defer stop()` removed from `main`.

Gate re-run on 8ee65d6 (rebased onto main after the record move; same tree as 64f1d73): `just lint`, `go mod tidy -diff`, `go test -race ./...`, `just test-linux`, `TestRestoreCompat` PASS, acceptance greps clean, pty `^C` at `Continue? [y/N]` → 130 `Aborted.` with no `tmp*`.

### Review 2 — scoped re-review, 2026-11-09 (fix range 08cb9f2..8ee65d6)

R1, R2, R4–R7 verified closed by a fresh delegated reviewer; the R1 and R2 regression tests fail under mutation. New rows:

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| N1a | non-blocking | stale-xref | fixed inline | Three comments added by the R1 fix claim the sqlite driver reports a cancel as an interrupt rather than `context.Canceled` | modernc.org/sqlite v1.62.0 returns `ctx.Err()` on cancel (`stmt.go:113,309`, `tx.go:104`, `conn.go:1264`); delegate probe: cancelled `QueryRowContext`, `ExecContext` and a `rows.Next` loop all give `errors.Is(err, context.Canceled)` | Correct the comments — done in 76bc102 |
| N1b | non-blocking | design | fixed inline | The R1 fix's `session.ctxErr` defers and the `snapshotDB` cancel branch are redundant (mutation: removing them leaves `go test ./...` green); as a side effect they turned a real DB error coinciding with a cancel into `Aborted.` | delegate mutation + driver source above | Deleted in 76bc102; only the corrupt-DB-during-cancel edge changes, now reported as `error: …` |
| N2 | non-blocking | plan-wrong | noted | Task 5 X1/F2's instruction to test `ctx.Err()` "because modernc's interrupt error is not `context.Canceled`" rested on a false premise; following it produced R1 | N1 probe | The code now uses `errors.Is(err, context.Canceled)`; this row and R1 record the override |

Disposition summary (review 2): 0 blocking; 3 non-blocking — 2 fixed inline (N1a, N1b), 1 noted (N2); 0 new tickets.

Checklist (review 2):
- [x] Reviewer independence: delegated, fresh reviewer, `hunt: one pass` over the fix diff; 2 findings re-verified by hand against the driver source, 0 discarded.
- [x] Gates on 8ee65d6 (delegate) and on 76bc102 (orchestrator): `just lint`, `go mod tidy -diff`, `go test -race ./...`, `just test-linux`, `TestRestoreCompat` PASS, acceptance steps 1, 3, 4 green.
- [x] Docs-readability (step 4b): skipped — optional; prose changes are small and were read by both reviewers.
- [x] Governing docs (step 7): spec §1, §2, §6, §6.3, §9, §11, §13 reconciled on the branch.
- [x] Impact sweep (step 8): CS-009 and CS-010 name `pidIsCopilot`, `ancestorsFrom`, `DefaultAncestors` — signatures unchanged; only their line anchors drifted. No patch; refreshed at their pickup (CS-013 decision 4).

Verdict: approve. Ticket → done; publish pending the user's approval of the commit messages.

## History

- 2026-11-09 — created (TO DO). source: chat: Go 1.27 upgrade plus whole-codebase idiomatic-Go pass from the 2026-11-09 code review, idiom over Python parity
- 2026-11-09 — TO DO → READY: plan complete
- 2026-11-09 — plan amended inline: an independent plan challenge (F1–F16) found that Ctrl-C could still lead to deletion, the cancel output and exit logic were wrong, the restore-rollback claim was false, and a probe passed falsely; user approved amending decisions 2/8/10/12, adding 14 (x/term TTY check) and 15 (Describe %q), and folding the rest into the tasks
- 2026-11-09 — plan amended inline: pickup applicability gate F1 — acceptance step 3 gains a `contains '` grep so decision 15 (Describe %q) is probed; F2–F4 noted and closed (cli has no sort imports; notty/no-zero-default probes are regression guards only; C5 is a logic slip, not a panic)
- 2026-11-09 — READY → IN DEVELOPMENT: picked up
- 2026-11-09 — IN DEVELOPMENT → IN REVIEW: acceptance green
- 2026-11-09 — IN REVIEW → REWORK: review 1: blocking R1, R2
- 2026-11-09 — REWORK → IN REVIEW: findings fixed
- 2026-11-09 — IN REVIEW → DONE: review 2 approved: 0 blocking; R4-R7, N1a, N1b fixed inline; R3, R8-R11, N2 noted; 0 new tickets
- 2026-11-09 — published: user approved; tidied to 4 commits (248bbd4, 11ea07f, 6b74024, dd5663a), pushed, PR #11 https://github.com/codcod/copilot-sessions/pull/11; merge pending (human)
