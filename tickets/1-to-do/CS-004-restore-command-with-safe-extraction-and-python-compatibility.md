---
id: CS-004
title: restore command with safe extraction and Python compatibility
project: copilot-sessions
depends-on: [CS-002, CS-003]
spawned-by: []
impact: high
complexity: high
cost: M
---

# CS-004 — restore command with safe extraction and Python compatibility

## Outcome

After this ships, `copilot-sessions restore [FILE|latest]` replaces all current Copilot
sessions with a backup's content. It refuses archives with unsafe paths. Archives made by
the Go version and by the Python script restore with either tool.

## Description

Implements `development/spec.md` §6.4 and covers §9 cases 11–12.

- **Argument.** A `FILE`, `latest`, or an interactive pick from the backup table
  (`Restore which backup? [1] `; an empty answer means 1). With no argument and no TTY,
  fail with `no backup given; pass FILE or 'latest'`. Other errors: `no backups in <dir>`,
  `backup not found: <path>`, and `does not look like a copilot-sessions backup`.
- **Warning and confirmation.** Print the four-line warning block, then ask `Continue?`.
  `<N>` is the number of distinct non-hidden names directly below `session-state/` in the
  archive.
- **Safe extraction.** Extract into a temp dir inside `COPILOT_HOME`. Match Python
  `tarfile` `filter="data"`: reject absolute paths, `..` components, out-of-tree symlinks
  and hardlinks, and device, FIFO or other special files. Strip setuid and setgid bits.
  This is the security-sensitive part of the ticket.
- **Replace.** For each of the four top-level items, remove the current item (for the DB,
  also remove `-wal` and `-shm`), then rename the extracted item into place. Print
  `Restored <N> session(s) from <path>`.
- **Tests.** A round trip (backup → `clean --all` → restore) gives the same IDs and DB row
  counts. Also test `-wal`/`-shm` removal, `latest`, and rejection of path-traversal
  archives. An optional compatibility test (§9 case 12) runs only if `python3` (3.12+, for
  `tarfile` `filter="data"`) is on PATH. It uses the in-repo reference script
  `development/copilot-sessions.sh`. It checks that Go restores a Python-made archive and that Python
  restores a Go-made one.

Soft couplings: builds on CS-001. Uses the backup listing and archive layout from CS-003,
and `clean --all` from CS-002 for the round-trip test.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md §6.4, §9 cases 11-12 (Go rewrite of copilot-sessions)
