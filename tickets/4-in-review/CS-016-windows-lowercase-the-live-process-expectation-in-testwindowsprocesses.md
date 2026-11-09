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

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: review: CS-014 review finding N1, promoted at the user's request
- 2026-11-09 — TO DO → READY: plan complete
- 2026-11-09 — READY → IN DEVELOPMENT: picked up; applicability gate: no findings
- 2026-11-09 — IN DEVELOPMENT → IN REVIEW: acceptance green (lint, test, test-linux, GOOS=windows vet + test build); CI test-windows pending push
