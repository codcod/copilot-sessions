---
id: CS-003
title: "backup command: archive, list and retention"
project: copilot-sessions
depends-on: [CS-001]
spawned-by: []
impact: high
complexity: medium
cost: M
---

# CS-003 — backup command: archive, list and retention

## Outcome

After this ships, `copilot-sessions backup` writes a `.tar.gz` of the Copilot session data
that the Python script can read. The archive holds a consistent DB snapshot that includes
data still in the WAL. `backup --list` and `--keep N` manage the backup directory.

## Description

Implements `development/spec.md` §6.3.

- **Flags.** `FILE`, `-l/--list`, `-k/--keep N`, `--no-create`, `-y/--yes`, `--json` (only
  with `--list`), `-q/--quiet`. Use the four validation messages from §6.3 (exit 1).
- **Archive format.** It must stay compatible with the Python version in both directions.
  - gzip tar with mode `0600`. Create parent directories as needed.
  - The top-level entries are `session-state/`, `session-store.db`,
    `open-sessions-state.json` and `sidebar-sessions-state/`, each only if it exists.
  - Exclude `.session-operation-locks`.
  - The DB entry is a snapshot made with `VACUUM INTO` or the online backup API into a temp
    file. Never include the `-wal` or `-shm` files.
  - Keep file modes and mtimes in the tar headers.
- **Default path.** `$COPILOT_BACKUP_DIR/copilot-sessions-YYYYmmdd-HHMMSS.tar.gz` in local
  time. On success, print `Backup created: <path> (<N> session(s), <size>)`. With `-q`,
  print only the path.
- **Retention.** Only `copilot-sessions-*.tar.gz` files count. Sort them by file name,
  newest first. Print the "nothing to remove" message, or the table followed by a
  confirmation (the helper from CS-002) and `Deleted <N> old backup(s).`
- **Backup table and `--json`.** `CREATED` comes from the timestamp in the file name, or
  the mtime if that does not parse. With no backups, print `No backups in <dir>`. CS-004's
  interactive picker and CS-005's restore completion reuse the backup listing.
- **Tests.** §9 case 10.

Soft couplings: builds on CS-001. Reuses the confirmation helper from CS-002; if CS-002 has
not landed yet, this ticket adds the helper.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md §6.3 (Go rewrite of copilot-sessions)
