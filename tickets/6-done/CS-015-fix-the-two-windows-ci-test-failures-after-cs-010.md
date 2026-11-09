---
id: CS-015
title: Fix the two Windows CI test failures after CS-010
project: copilot-sessions
depends-on: []
spawned-by: [CS-010]
impact: high
complexity: low
cost: S
---

# CS-015 — Fix the two Windows CI test failures after CS-010

## Outcome

CI's `test-windows` job passes on `main`. It has failed since CS-010 merged (PR #13, run 42650761905). The failures are two wrong test expectations; the product code is fine.

## Description

The `test-windows` run on PR #13 was CS-010's first run on real Windows, and it ended after the merge. Process detection, symlinks and the open-file restore all passed (`internal/session` ok; `TestRestoreOpenFile` passed). Two tests failed:

1. **`TestExtractUnsafeWindowsPaths`** (`internal/backup/restore_windows_test.go:18`): `nul.txt: err <nil>`. Windows 11, and Go's `filepath.IsLocal` along with it, no longer treats a reserved name with an extension as a device. `nul.txt` is therefore an ordinary local file, and extracting it is safe. The test's expectation is wrong.
2. **`TestRestoreCompat`** (`internal/cli/restore_test.go:210`): the expected and actual ID lists look identical. Python on Windows writes `\r\n` in text mode, so `pythonRunner`'s output (`restore_test.go:238`) carries CRs that the Go side doesn't.

## Implementation Plan

### 0. Feature branch (mandatory)

```
git checkout main && git pull --ff-only && git checkout -b feat/CS-015-windows-ci-tests
```
Commit locally; no push or PR without explicit user approval. Before pushing, `git diff --name-only origin/main...HEAD | grep '^tickets/'` must print nothing.

### Prerequisite gate (hard)

CS-010 is in `6-done/` and merged (PR #13, bb149b3).

### Confirmed design decisions (do not deviate without asking)

1. Test-only changes; no product code changes.
2. `nul.txt` leaves the unsafe-name table. A bare reserved name with no extension (`NUL`) stays, alongside `CON` and `s/AUX`.
3. `pythonRunner` normalises `\r\n` to `\n` in what it returns, so every Python comparison is line-ending-agnostic.

### Tasks

#### Task 1 — unsafe-name table
In `internal/backup/restore_windows_test.go`, replace `nul.txt` with `NUL`.

#### Task 2 — Python output line endings
In `internal/cli/restore_test.go` `pythonRunner`, return `strings.ReplaceAll(string(out), "\r\n", "\n")`.

### Acceptance test

`just lint && just test && just test-linux`, plus `GOOS=windows go vet ./...`. After the push, CI `test-windows` is green on the PR.

### Docs update (mandatory when user-facing)

None: test-only change, not user-facing.

### Finish (mandatory)

Commit `test: fix Windows reserved-name and Python line-ending expectations (CS-015)`. Then `pickle ticket move CS-015 in-review --reason "acceptance green"`.

## Review

2026-11-09 — delegated review (independent reviewer sub-agent, hunt: one pass), branch `feat/CS-015-windows-ci-tests` @ fd8fdee.

- [x] Reviewer independence (step 0): delegated to a fresh sub-agent; no delegated findings discarded
- [x] In-tree stale-branch check (step 0a): branch rebased onto main; `pickle doctor` 0 errors, 0 warnings
- [x] Implementation audit (steps 1, 2): tasks 1–2 and decisions 1–3 verified; `just lint`, `just test`, `just test-linux`, `GOOS=windows go vet ./...` green. CI `test-windows` is still pending until the push
- [x] Correctness hunt (step 3): one pass over all 8 angles by the delegated reviewer, no host tool and no further sub-agents; nothing found. The root cause was confirmed in Go 1.27 `filepathlite.isReservedName`
- [x] Consistency audit (step 4): no stale references to `nul.txt` or line endings
- [x] Documentation audit (step 4a): test-only, no docs or CHANGELOG entry needed
- [x] Docs-readability pass (step 4b): conscious skip, no `.md`/`.adoc` in the diff
- [x] Findings recorded (step 5)
- [x] Ticket moved to `6-done/` (step 6)
- [x] References and governing docs (step 7): none affected
- [x] Impact sweep (step 8): no TO DO/READY ticket depends on or references CS-015
- [ ] Commit message & MR presented for approval (step 9)

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| F1 | non-blocking | test-gap | noted | No test now covers a reserved name with an extension; whether one is rejected depends on the OS | `restore_windows_test.go:14`; Go 1.27 `filepathlite.isReservedName` defers to `RtlIsDosDeviceName_U` | none: the product delegates to `filepath.IsLocal` by design, so an OS-dependent assertion would be fragile |

dispositions: 1 noted (F1); 0 folded, 0 new ticket, 0 absorbed.
cost: estimated S, actual S

## History

- 2026-11-09 — created (TO DO). source: pickle ticket new
- 2026-11-09 — TO DO → READY: plan complete
- 2026-11-09 — READY → IN DEVELOPMENT: picked up; applicability gate: no blocking findings (Description typo fixed)
- 2026-11-09 — IN DEVELOPMENT → IN REVIEW: acceptance green (lint, test, test-linux, GOOS=windows vet); CI test-windows pending push
- 2026-11-09 — IN REVIEW → DONE: review: no blocking findings; 1 noted (F1)
