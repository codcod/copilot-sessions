---
id: CS-002
title: "clean command: filtered deletion of sessions, DB rows and UI state"
project: copilot-sessions
depends-on: [CS-001]
spawned-by: []
impact: high
complexity: medium
cost: M
---

# CS-002 — clean command: filtered deletion of sessions, DB rows and UI state

## Outcome

After this ships, `copilot-sessions clean` (alias `rm`) deletes the selected sessions. It
removes their directories, their DB rows (including FTS search) and their UI-state entries,
the same way the Python script does. It never deletes the session that runs the command.

## Description

Implements `development/spec.md` §6.2, using the filters and session model from CS-001.

- **Flags.** `-a/--all`, `--include-ongoing`, `-n/--dry-run`, `-y/--yes`, `-q/--quiet`,
  plus the shared filters from §5.2.
- **Validation (exit 1).** `--all` combined with IDs or filters fails, and so does a call
  with no `--all`, no ID and no filter. Use the exact messages from §6.2.
- **Safety rules, in order.** Apply the filters. Always drop the current session. Drop
  ongoing sessions unless `--include-ongoing` is set or `--status` contains `ongoing`. If
  nothing remains, print `No sessions match.` and exit 0. Print the skip notes with the
  plural rule from §6.5.
- **Output.** The skip notes, then the table (not with `-q`), then
  `<N> of <total> sessions selected (<scope>[, <size>]).` with the scope parts in the
  spec's order. `-n` prints `Dry run: nothing deleted.`
- **Confirmation helper (§6).** `--yes` continues. With no TTY, fail with
  `confirmation required; rerun with --yes`. Otherwise prompt `[y/N]`. The helper is shared
  with CS-003 and CS-004.
- **Deletion.**
  1. Remove the session dirs recursively.
  2. Delete the DB rows in one transaction with a 30 s busy timeout, using bound
     parameters or a temp table. Delete only from tables that exist in `sqlite_master`
     (`search_index`, `turns`, `checkpoints`, `session_files`, `session_refs`,
     `forge_trajectory_events`, `assistant_usage_events`), then from `sessions`. Never
     create or migrate tables.
  3. Prune `open-sessions-state.json` and `sidebar-sessions-state/*.json`. Clear them fully
     when every known session is deleted. Write a file only if it changed, atomically, with
     the original mode kept, 2-space indent, a trailing newline and no HTML escaping.
- **Tests.** §9 cases 5–9. Case 7 injects the ancestor-PID provider.

Soft coupling: builds on CS-001 (the session model, filters, fixture and seams). CS-001 review findings F5 (unsanitised names/IDs) and F11 (an unstattable `session-state/` reads as empty, like the script) bear on `clean`'s "all known sessions" full clear; decide whether that needs a stricter view. CS-004's
round-trip test uses `clean --all`.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md §6.2 (Go rewrite of copilot-sessions)
- 2026-11-06 — impact sweep from CS-001 review: added pointer to CS-001 findings F5 and F11.
