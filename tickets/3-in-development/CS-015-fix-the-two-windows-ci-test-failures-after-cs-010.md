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

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: pickle ticket new
- 2026-11-09 — TO DO → READY: plan complete
- 2026-11-09 — READY → IN DEVELOPMENT: picked up; applicability gate: no blocking findings (Description typo fixed)
