---
id: CS-014
title: macOS: match copilot case-insensitively in process detection
project: copilot-sessions
depends-on: []
spawned-by: [CS-010]
impact: medium
complexity: low
cost: S
---

# CS-014 — macOS: match copilot case-insensitively in process detection

## Outcome

A Copilot CLI started on macOS as `Copilot` or `COPILOT` shows as `ongoing`, not `stale`. `clean` no longer deletes it unless `--include-ongoing` is passed.

## Description

Spawned by CS-010's re-review (finding N1). This predates CS-010: it shipped with CS-009.

On darwin, `argvHasCopilot` (`internal/session/session_darwin.go`) matches `copilot`
case-sensitively. APFS is case-insensitive by default, so `COPILOT` typed in a shell runs
Copilot, and argv keeps the typed case. The session then reads as `stale`. Stale sessions are
cleanable without `--include-ongoing`, so `clean` can delete a running session.

The reviewer reproduced it on macOS: `zsh -c 'SLEEP 3 & ps -o args= -p $!'` prints `SLEEP 3`.

Fix: `bytes.Contains(bytes.ToLower(a), []byte("copilot"))`. The environment is already
excluded, so this can't match on `COPILOT_HOME`. Add a `TestProcargsMatch` row with an
upper-case argv, and update `development/spec.md` §4.2, which CS-010 changed to say Windows
matches case-insensitively. Linux stays case-sensitive (case-sensitive filesystems). Soft
coupling: CS-010 (the Windows equivalent).

## Implementation Plan

### 0. Feature branch (mandatory)

```
git checkout main && git pull --ff-only && git checkout -b feat/CS-014-darwin-case-insensitive
```
Commit locally; no push or PR without explicit user approval. Before pushing, `git diff --name-only origin/main...HEAD | grep '^tickets/'` must print nothing.

### Prerequisite gate (hard)

None: `depends-on: []`. CS-010 (soft coupling, the Windows equivalent) is done and merged (PR #13).

### Confirmed design decisions (do not deviate without asking)

1. darwin matches `copilot` in argv case-insensitively, the same way Windows does (`session_windows.go:29`). The environment stays excluded.
2. Linux stays case-sensitive, because Linux filesystems are case-sensitive.
3. No `CHANGELOG.md` entry. macOS support (CS-009) is still under `[Unreleased]`, so this bug never shipped in a release.

### Tasks

#### Task 1 — matcher
In `internal/session/session_darwin.go` `argvHasCopilot`, use `bytes.Contains(bytes.ToLower(a), []byte("copilot"))`. Update the comments on `pidIsCopilot`/`argvHasCopilot` to say "case-insensitively (APFS is case-insensitive by default, and argv keeps the typed case)".

#### Task 2 — tests
In `internal/session/session_darwin_test.go` `TestProcargsMatch`:
- add a row that expects a match for an upper-case argv: `procargs(1, "/usr/local/bin/COPILOT", []string{"COPILOT"}, nil)`;
- change the live-process `want` (line 59) to `strings.Contains(strings.ToLower(a), "copilot")` so it agrees with the matcher.

#### Task 3 — spec
In `development/spec.md` §4.2 (line 96), move "matched case-insensitively" so it covers both macOS and Windows. Example: "(on macOS, the argv from `kern.procargs2`, not its environment; on Windows, the command line from `NtQueryInformationProcess`; on both, matched case-insensitively)".

### Acceptance test

`just lint && just test && just test-linux`, plus `GOOS=windows go vet ./...`. `go test -run TestProcargsMatch -v ./internal/session` passes on macOS with the new upper-case row. Manual check: before the fix, the new row fails (revert Task 1 locally to confirm).

### Docs update (mandatory when user-facing)

`development/spec.md` §4.2 (Task 3). No README or CHANGELOG change (decision 3).

### Finish (mandatory)

Commit `fix(session): match copilot case-insensitively on macOS (CS-014)`. Then `pickle ticket move CS-014 in-review --reason "acceptance green"`.

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: pickle ticket new
- 2026-11-09 — TO DO → READY: plan complete
- 2026-11-09 — READY → IN DEVELOPMENT: picked up; applicability gate: no findings
- 2026-11-09 — plan amended inline: Task 2 row argc 3 → 1. `procargs`'s first argument is argc, not a row id, so argc 3 with one argv string reads as a truncated buffer and the row could never match.
- 2026-11-09 — IN DEVELOPMENT → IN REVIEW: acceptance green (lint, test, test-linux, GOOS=windows vet; new row red without fix)
