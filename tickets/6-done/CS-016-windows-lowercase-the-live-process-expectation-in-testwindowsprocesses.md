---
id: CS-016
title: Windows: lowercase the live-process expectation in TestWindowsProcesses
project: copilot-sessions
depends-on: []
spawned-by: [CS-014]
impact: low
complexity: low
cost: S
---

# CS-016 — Windows: lowercase the live-process expectation in TestWindowsProcesses

## Outcome

`TestWindowsProcesses` no longer fails spuriously when `go test -run` is given a pattern containing `Copilot` (any case). Test-only: product behaviour is unchanged.

## Description

Spawned by CS-014's review (finding N1). The bug dates from CS-010.

The Windows matcher (`internal/session/session_windows.go:29`) lowercases the command line before looking for `copilot`. The live-process check in `TestWindowsProcesses` (`internal/session/session_windows_test.go:22`) computes its expected value with a case-sensitive `strings.Contains(a, "copilot")`. When an argv string contains `Copilot` but not `copilot` (for example `go test -run 'TestWindowsProcesses|Copilot' ./internal/session`), the matcher says true, `want` says false, and the test fails.

Fix: `strings.Contains(strings.ToLower(a), "copilot")` at line 22. That mirrors what CS-014 did for darwin at `session_darwin_test.go:62`. The reviewer reproduced the same mismatch on darwin. It is unverified on Windows, but the code path is identical. Soft coupling: CS-014 (the darwin twin).

## Implementation Plan

### 0. Feature branch (mandatory)

```
git checkout main && git pull --ff-only && git checkout -b feat/CS-016-windows-test-case
```
Commit locally; no push or PR without explicit user approval. Before pushing, `git diff --name-only origin/main...HEAD | grep '^tickets/'` must print nothing.

### Prerequisite gate (hard)

None: `depends-on: []`. CS-014 (soft coupling, the darwin twin) is done and merged (PR #15, 16da929).

### Confirmed design decisions (do not deviate without asking)

1. Test-only change; the matcher in `session_windows.go` stays as is.
2. The expectation mirrors the matcher exactly (`strings.ToLower` then `Contains`), as CS-014 did at `session_darwin_test.go:62`.

### Tasks

#### Task 1 — expectation
In `internal/session/session_windows_test.go:22`, change `strings.Contains(a, "copilot")` to `strings.Contains(strings.ToLower(a), "copilot")`.

### Acceptance test

`just lint && just test && just test-linux`, plus `GOOS=windows go vet ./...` and `GOOS=windows go test -c -o /dev/null ./internal/session`. After the push, CI `test-windows` is green on the PR. The spurious failure can't be reproduced locally on macOS, since the test is Windows-only, so CI is the real check.

### Docs update (mandatory when user-facing)

None: test-only change, not user-facing.

### Finish (mandatory)

Commit `test(session): match copilot case-insensitively in the Windows live check (CS-016)`. Then `pickle ticket move CS-016 in-review --reason "acceptance green"`.

## Review

2026-11-09 — delegated review (independent reviewer sub-agent, hunt: one pass), branch `feat/CS-016-windows-test-case` @ 3fc97c8.

- [x] Reviewer independence (step 0): delegated to a fresh sub-agent; no delegated findings discarded
- [x] In-tree stale-branch check (step 0a): branch rebased onto main; `pickle doctor` 0 errors, 0 warnings
- [x] Implementation audit (steps 1, 2): Task 1 and decisions 1–2 verified. `just lint`, `just test`, `just test-linux`, `GOOS=windows go vet ./...` and the Windows test build are green. CI `test-windows` is still pending until the push
- [x] Correctness hunt (step 3): one pass over all 8 angles by the delegated reviewer, no host tool and no further sub-agents; nothing found
- [x] Consistency audit (step 4): the Windows and darwin live checks are now identical; Linux stays case-sensitive by CS-014 decision 2
- [x] Documentation audit (step 4a): test-only, nothing user-facing
- [x] Docs-readability pass (step 4b): conscious skip, no docs in the diff
- [x] Findings recorded (step 5)
- [x] Ticket moved to `6-done/` (step 6)
- [x] References and governing docs (step 7): none affected
- [x] Impact sweep (step 8): no TO DO/READY tickets remain
- [x] Commit message & MR approved (step 9); main pushed first, `origin/main...HEAD` carried no `tickets/` path; PR #16 opened

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|

No findings.

dispositions: none (0 findings).
cost: estimated S, actual S

## History

- 2026-11-09 — created (TO DO). source: review: CS-014 review finding N1, promoted at the user's request
- 2026-11-09 — TO DO → READY: plan complete
- 2026-11-09 — READY → IN DEVELOPMENT: picked up; applicability gate: no findings
- 2026-11-09 — IN DEVELOPMENT → IN REVIEW: acceptance green (lint, test, test-linux, GOOS=windows vet + test build); CI test-windows pending push
- 2026-11-09 — IN REVIEW → DONE: review: no findings
- 2026-11-09 — published: pushed feat/CS-016-windows-test-case, opened PR #16 (awaiting human merge)
