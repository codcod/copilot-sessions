---
id: CS-017
title: move command: relocate a project directory and repoint its sessions
project: copilot-sessions
depends-on: []
spawned-by: []
impact: medium
complexity: medium
cost: M
---

# CS-017 — move command: relocate a project directory and repoint its sessions

## Outcome

After this ships, you can move a project folder with `copilot-sessions move OLD NEW`. The
folder is moved on disk, and every session started in or under it shows the new location in
Copilot's `--resume` picker, ranks first there again when you run Copilot from the new
folder, and is found by `copilot-sessions list --cwd NEW`.

## Description

### Why

Copilot CLI never updates a session's recorded directory. After `mv`, resuming by ID still
works, but the `--resume` picker shows the old Location. It also loses the session's
relevance ranking for the new folder, and our own `list --cwd` filter misses the session.

### How Copilot stores the path (observed 2026-11-09, Copilot CLI 1.0.108, Linux container)

Probe: three sessions in `/work/proj-a`, `/work/proj-a/sub` and `/work/proj-b`. Then
`mv /work/proj-a /work/moved/proj-a`, followed by `--resume`, `--continue` and the interactive
picker run from the new location.

| Location | Field(s) | Role |
|---|---|---|
| `session-state/<id>/workspace.yaml` | `cwd`, `git_root` | **Read by the `--resume` picker** for its Location column and its relevance ranking. Rewriting only these two lines fixed both. |
| `session-store.db` | `sessions.cwd` (indexed) | Not used by the picker. Our `list` falls back to it when the YAML has no cwd. |
| `session-store.db` | `session_files.file_path` | Absolute paths of the files a session touched; `UNIQUE(session_id, file_path)`. |
| `session-store.db` | `turns.assistant_response`, `search_index.content` | Free-text history. |
| `events.jsonl` | ~20 fields (`session.start.context.cwd/gitRoot`, tool args/results, `fileEdits`, `codeChanges.filesModified`, reasoning text) | Append-only history. A resume adds a `session.resume` event with the new `context.cwd`. |
| `rewind-file-snapshots/index.json` | `path`, `resolvedPath`, `encodedPath.data` (hex of the path) | Files are keyed by a hash that is not an MD5 of the path. |
| `config.json` | trusted folders | Written only when you choose "remember this folder". |

A session started in a subdirectory has `cwd: /a/b/sub` and `git_root: /a/b`.
`--continue` ignores cwd and always resumes the most recent session.

### Spec (decisions confirmed with the user, 2026-11-09)

1. **The command moves the folder itself, then rewrites sessions.** It does not only rewrite
   sessions after a manual `mv`.
2. **It rewrites only the fields that locate a session:** `cwd` and `git_root` in
   `workspace.yaml`, and `sessions.cwd`. It does not touch `session_files`, `events.jsonl`,
   `turns`, `search_index` or rewind snapshots, which are history, and rewriting the snapshots
   risks breaking their hash keys.
3. **Prefix match, as `--cwd` does.** OLD matches `OLD` and `OLD/...`, not `OLDx`. A value is
   rewritten as `NEW + rest`. `cwd` and `git_root` are matched independently.
4. **Safety matches `clean`.** It refuses when a matched session is `ongoing`. `-n` gives a dry
   run, there is a `[y/N]` prompt, and `-y` skips it. There is no automatic backup; the help
   text points to `backup`.
5. **Copilot's `config.json` (trusted folders) is left alone.** Copilot asks for trust again
   in the new folder.

### Open for refinement

- Command name (`move`, maybe with an `mv` alias) and argument form.
- Behaviour when NEW exists, or when OLD is missing (for example, already moved by hand: should
  the command still rewrite sessions?).
- A move across filesystems: `os.Rename` fails with EXDEV. Refuse, or fall back to copy and
  delete?
- Order of operations and failure handling: if the folder moves and the session rewrite then
  fails, should the command roll back or report?
- How DB-only sessions (no directory) are handled, and how paths are compared on Windows
  (case-insensitive, `\` separators, as in `internal/filter`).
- Whether the `workspace.yaml` rewrite must preserve every other line byte for byte. It
  should: rewrite only the matched lines, then replace the file atomically.

Soft couplings: reuses the prefix matching in `internal/filter` (`--cwd`), and the
ongoing/confirm/dry-run flow and DB handling of `clean` (CS-002).

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: chat: user asked for a way to move a project folder Copilot was used in; findings from a Copilot CLI 1.0.108 probe in Docker
