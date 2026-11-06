---
id: CS-001
title: Go scaffold, session model and list command
project: copilot-sessions
depends-on: []
spawned-by: []
impact: critical
complexity: high
cost: L
---

# CS-001 — Go scaffold, session model and list command

## Outcome

After this ships, `copilot-sessions list` (alias `ls`) from the Go binary prints the same
table, `-q` IDs and `--json` output as the Python script for the same `COPILOT_HOME`. It
supports every filter. Later commands build on its session model.

## Description

This ticket is the foundation of the Go rewrite in `development/spec.md`. The spec is the
contract, and the Python script `~/.local/bin/copilot-sessions` is the reference where the
spec is unclear.

In scope:

- **Scaffold (spec §2).** Go ≥ 1.22, one module, one binary `copilot-sessions`, cobra, and
  `modernc.org/sqlite` (no CGO). A `main.Version` variable feeds cobra's `--version`. The
  `justfile` (build, test, install) is CS-006's job.
- **Root command and help (§6 global, §7).** The root description. The help also shows the
  DATE formats, the current `COPILOT_HOME`/`COPILOT_BACKUP_DIR` values with their names, and
  the three status definitions. Errors print as `error: <msg>` and exit 1. Usage errors exit
  2. Ctrl-C prints `\nAborted.` and exits 130.
- **Paths (§3).** Read `COPILOT_HOME` and `COPILOT_BACKUP_DIR` with their defaults. A
  session dir is any non-hidden directory under `session-state/`. `.session-operation-locks`
  is never a session.
- **Data model (§4).** Line-based `workspace.yaml` parsing with no YAML library. Merge with
  the `sessions` table, which also yields DB-only sessions. Timestamp parsing strips quotes
  and treats a timestamp without a zone as UTC.
- **Status (§4.2).** `ongoing`, `stale` and `closed` from `inuse.<pid>.lock` and
  `/proc/<pid>/cmdline`. `busy` comes from `open-sessions-state.json`. `current` comes from
  the ancestor-PID set (`/proc/<pid>/stat`, the field after the last `)`).
- **Empty (§4.3)** and **size (§4.4)**, each computed only when needed.
- **DATE and filter parsing (§5).** Includes the month-end clamp, ID-prefix errors, and the
  exact error texts and exit codes.
- **`list` (§6.1).** Sorting, row format, summary line, `-q`, `--json`, `--json -q` (size
  null), and human-readable sizes.
- **Injection seams (§9).** The TTY check, the stdin reader, the clock and the ancestor-PID
  provider. Later tickets reuse them.
- **Test fixture (§9).** Built in code: a WAL-mode DB with the §10 schema including FTS5
  `search_index`, all the listed session kinds (one ongoing via a copied `sleep` binary
  named `copilot-test`), the lock dir, and the UI state files. Tests always point both env
  vars at `t.TempDir()`. This ticket covers §9 cases 1–4.

Soft couplings: CS-002, CS-003, CS-004 and CS-005 all build on this module, its fixture and
its seams.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md (Go rewrite of copilot-sessions); foundation ticket
