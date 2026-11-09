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

**Open for refinement (ask, do not assume)**: the lint tooling question in Part 1; whether the
JSON `updated_at`/`busy` nullability in `list --json` changes along with P5 (a JSON schema
change); options struct vs split functions for each P2 site; how D6 is solved; whether `filter.Date`
survives P5/P7 or becomes a `time.Time`. Every visible change must be listed in spec §11 and
CHANGELOG.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: chat: Go 1.27 upgrade plus whole-codebase idiomatic-Go pass from the 2026-11-09 code review, idiom over Python parity
