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

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: pickle ticket new
