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

2026-11-09 — delegated review (independent reviewer sub-agent, hunt: one pass), branch `feat/CS-014-darwin-case-insensitive` @ 7b69fa1.

- [x] Reviewer independence (step 0): delegated to a fresh sub-agent; no delegated findings discarded
- [x] In-tree stale-branch check (step 0a): branch rebased onto main; `pickle doctor` 0 errors, 0 warnings
- [x] Implementation audit (steps 1, 2): tasks 1–3 (amended Task 2 row) and decisions 1–3 verified. `just lint`, `just test`, `just test-linux`, `GOOS=windows go vet ./...` green. With Task 1 reverted, the new row fails
- [x] Correctness hunt (step 3): one pass over all 8 angles by the delegated reviewer, no host tool and no further sub-agents. No defect in the diff. Unicode folding can only produce a false "ongoing", the safe direction (same as Windows)
- [x] Consistency audit (step 4): repo-wide grep of case-matching claims; only N1
- [x] Documentation audit (step 4a): spec §4.2 updated; README makes no case claim; no docs build
- [x] Docs-readability pass (step 4b): conscious skip, a one-clause spec edit
- [x] Findings recorded (step 5)
- [x] Ticket moved to `6-done/` (step 6)
- [x] References and governing docs (step 7): spec §4.2 reconciled on the branch
- [x] Impact sweep (step 8): no TO DO/READY tickets remain
- [x] Commit message & MR approved (step 9); main pushed first, `origin/main...HEAD` carried no `tickets/` path; PR #15 opened

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| N1 | non-blocking | correctness | noted | Windows `TestWindowsProcesses` live check computes `want` case-sensitively while the Windows matcher lowercases; a `-run` pattern containing `Copilot` fails spuriously (test-only, pre-existing since CS-010) | `internal/session/session_windows_test.go:22`; same mismatch reproduced on darwin in a scratch copy with `-run 'TestProcargsMatch\|NoSuchCopilot'` | one-line `strings.ToLower(a)` there; it predates this branch, so it is not fixed inline (rules §5 causation bar) |

dispositions: 1 noted (N1); 0 fixed inline, 0 folded, 0 new ticket.
cost: estimated S, actual S

## History

- 2026-11-09 — created (TO DO). source: pickle ticket new
- 2026-11-09 — TO DO → READY: plan complete
- 2026-11-09 — READY → IN DEVELOPMENT: picked up; applicability gate: no findings
- 2026-11-09 — plan amended inline: Task 2 row argc 3 → 1. `procargs`'s first argument is argc, not a row id, so argc 3 with one argv string reads as a truncated buffer and the row could never match.
- 2026-11-09 — IN DEVELOPMENT → IN REVIEW: acceptance green (lint, test, test-linux, GOOS=windows vet; new row red without fix)
- 2026-11-09 — IN REVIEW → DONE: review: no blocking findings; 1 noted (N1)
- 2026-11-09 — published: pushed feat/CS-014-darwin-case-insensitive, opened PR #15 (awaiting human merge)
- 2026-11-09 — merged to main (PR #15, 16da929, https://github.com/codcod/copilot-sessions/commit/16da929)
