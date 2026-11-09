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

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: review: CS-014 review finding N1, promoted at the user's request
