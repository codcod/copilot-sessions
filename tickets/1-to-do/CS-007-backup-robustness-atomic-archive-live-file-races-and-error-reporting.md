---
id: CS-007
title: backup robustness: atomic archive, live-file races and error reporting
project: copilot-sessions
depends-on: []
spawned-by: [CS-003]
impact: medium
complexity: low
cost: S
---

# CS-007 — backup robustness: atomic archive, live-file races and error reporting

## Outcome

A failed `backup` never leaves a half-written archive behind, so `--keep` and `restore latest`
only ever see complete backups. A backup taken while Copilot is running finishes even when
session files change underneath it, and real I/O or permission errors are reported instead of
being treated as "nothing there".

## Description

Hardens the `backup` command shipped in CS-003. Every item below is a non-blocking finding from
the CS-003 review, recorded there as `noted` with its evidence. Most are behaviour the Python
script shares, so fixing them deliberately departs from parity. That is acceptable once CS-005
cuts over from the script, but each departure should be named in the plan.

- **F2 — atomic archive.** `backup.Create` writes straight to the target and leaves the partial
  file on error (CS-003 decision 5). Because the partial file has the newest
  `copilot-sessions-*` name, a later `--keep` keeps it and deletes good backups, and CS-004's
  `restore latest` would pick it. Write to a temp file in the same directory, rename it on
  success, and remove it on error. **This reverses CS-003 decision 5.**
- **F3 — same-second collision.** Two backups in the same second share the default name, and
  the second one truncates the first (`O_TRUNC`). Use `O_EXCL` on the default name, plus a
  suffix or a clear error. An explicit `FILE` keeps overwrite semantics.
- **F4 and F14 — files that change during the walk.** A file or directory deleted mid-walk
  (`fs.ErrNotExist`) aborts the backup. A file that shrinks between stat and copy aborts it with
  an early EOF. Skip vanished entries, and pad a short file with zeros up to the header size, as
  GNU tar does, or skip it with a warning.
- **F5 — absent vs unreadable item.** Any `os.Lstat` failure skips a top-level item silently.
  Skip only `fs.ErrNotExist`; report anything else as an error.
- **F6 — backup dir errors.** `backup.List` maps any `Stat` error to "no backups". Map only a
  missing dir or a non-directory to an empty list; return other errors (EACCES, I/O).
- **F7 — lock-dir exclusion.** The prefix test also drops siblings such as
  `.session-operation-locks-old/`. Exclude exactly `session-state/.session-operation-locks` and
  everything below it, as spec §6.3 says.
- **F8 — retention order.** Sorting by file name puts a `copilot-sessions-<non-timestamp>.tar.gz`
  above every timestamped backup, so `--keep` keeps it. Decide whether to order by
  `backup.Created` instead. This is a spec §6.3 change. It also changes the `--list` order and
  the `backup --list` parity check.
- **F9 — reporting after success.** If `session.Dirs` fails after the archive is written, the
  command exits 1 for a backup that exists, and `--keep` is skipped. Report the backup anyway,
  with a warning, and continue.

Out of scope, and still `noted` on CS-003: F10 (`ExpandUser` location), F11 (PAX sub-second
mtimes), F12/F15 (test error checks) and F13 (name literals).

Soft couplings: builds on CS-003's `internal/backup`, so it cannot start until PR #4 is merged.
CS-004 (restore) benefits from F2, since it then never picks a partial archive, and may want the
same temp-and-rename pattern for its own writes. CS-005's parity check is affected if F8 changes
the `--list` order. Open decisions for refinement: F8 (whether to change it at all) and F14 (pad
or skip a short file).

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-07 — created (TO DO). source: review: CS-003 review findings F2–F9 and F14, batched as one backup-robustness theme at the user's request
