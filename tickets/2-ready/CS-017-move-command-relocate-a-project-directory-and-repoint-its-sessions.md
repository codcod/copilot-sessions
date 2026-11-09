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

Further decisions (2026-11-09): the command is `move` with the alias `mv`. It refuses when
NEW exists, rewrites sessions only when OLD is already gone (a manual move), refuses a move
across filesystems, and reports, rather than rolls back, a rewrite that fails after the folder
has moved. The plan's decisions list holds the full set.

Soft couplings: reuses the prefix matching in `internal/filter` (`--cwd`), and the
ongoing/confirm/dry-run flow and DB handling of `clean` (CS-002).

## Implementation Plan

### 0. Feature branch (mandatory)

```
cd /Users/nka/Projects/codcod/copilot-sessions
git checkout main
git checkout -b feat/CS-017-move-command
```

Commit locally as you go. No push or merge request without explicit user approval (AGENTS.md
commit policy). Ticket and board bookkeeping stay on `main`.

### Prerequisite gate (hard)

none

### Confirmed design decisions (do not deviate without asking)

1. **The command is `copilot-sessions move OLD NEW`, alias `mv`.** It moves the folder on
   disk, then repoints the sessions.
2. **Only the fields that locate a session are rewritten:** the top-level `cwd:` and
   `git_root:` lines of `workspace.yaml`, and `sessions.cwd` in `session-store.db`. History
   (`events.jsonl`, `turns`, `search_index`, `session_files`, `rewind-file-snapshots/`) is
   never touched.
3. **Matching is by path prefix, as `--cwd` does.** A value is rewritten when it is OLD or
   below it (`/a/b` matches `/a/b/c`, not `/a/bc`; case-insensitive on Windows) and becomes
   `NEW + rest`. A session is selected when its `cwd` (YAML, else DB) **or** its `git_root`
   is under OLD. Each field is then rewritten only if it is under OLD itself.
4. **Safety follows `clean`.** `-n` is a dry run, `[y/N]` asks for confirmation (`-y` skips
   it), and there is no automatic backup (the help text points to `backup`). The command
   refuses and changes nothing when a selected session is `ongoing`, including the session
   running the command. `stale` and `closed` sessions are repointed.
5. **Copilot's `config.json` (trusted folders) is never touched.**
6. **NEW already existing is refused.** The command does not move OLD *into* it.
7. **OLD missing with NEW present means a manual move.** The command skips the folder move
   and only repoints sessions. With both OLD and NEW missing, it fails with an error.
8. **There is no copy fallback.** A failing `os.Rename` (across filesystems, or a folder held
   open on Windows) is reported as `move OLD to NEW: <err>`, and nothing is changed.
9. **A rewrite failure after the folder has moved is reported, not rolled back.** The command
   repoints the remaining sessions, lists those that failed, and exits 1. Running it again
   finishes the job through decision 7.
10. **Each `workspace.yaml` is rewritten atomically, and only on the matched lines.** It goes
    through a temp file in the same directory, then a rename, keeping the file's permissions.
    Every other byte stays the same. The DB update is a single transaction.

### Tasks

#### Task 1 — move prefix matching into `session` (`internal/session/path.go`, new)
- `filter` imports `session`, so the shared helpers live in `session`. Move `under` from
  `internal/filter/filter.go` to `session.Under`, unchanged, and have `filter.Apply` call
  it.
- Add `Rebase(path, old, new string) (string, bool)`. When `Under(path, old)` holds, it
  returns `new` plus the remainder of `path` after `old`, joined with `filepath.Join`;
  otherwise it returns `("", false)`.
  `// ponytail:` note: on Windows the remainder is cut at `len(old)` of the Abs'd path, which
  is wrong only for case folds that change byte length (non-ASCII).
- Move the `under` tests in `internal/filter/filter_test.go` (and `filter_windows_test.go`, if
  it has any) to `internal/session` and add `Rebase` cases: equal path, a subpath, the
  `/a/bc` non-match, and a relative path (never matched).

#### Task 2 — git_root on the session model (`internal/session/session.go`)
- Add a `GitRoot string` field to `Session`, filled from `meta["git_root"]` in `Load`. It is
  not filled from the DB, which has no such column.

#### Task 3 — rewrite (`internal/session/move.go`, new)
- `Repoint(ctx, home, ss []Session, old, new string) (failed []string, err error)`. This is
  the point of no return, so it uses `context.WithoutCancel`, as `Delete` does.
  - For each session with a `Dir`: read `workspace.yaml`, replace the values of the
    top-level `cwd:` and `git_root:` lines (parsed the way `readWorkspace` reads them) via
    `Rebase`, and write the file back atomically (decision 10). The write reuses the
    temp-and-rename pattern of `writeJSONFile` in `internal/session/delete.go`; factor it out
    if that keeps the code short. A per-session error is added to `failed` and the loop
    continues.
  - DB: `OpenDB` (missing DB is a no-op); in one transaction, for each selected ID,
    `SELECT cwd FROM sessions WHERE id = ?` and, if it can be rebased,
    `UPDATE sessions SET cwd = ? WHERE id = ?`. A DB error is returned as `err`.

#### Task 4 — the command (`internal/cli/move.go`, new; register it in `cli.go` `AddCommand`)
- `Use: "move OLD NEW"`, `Aliases: ["mv"]`, `Args: usageArgs(cobra.ExactArgs(2))`. Flags
  `-n/--dry-run` and `-y/--yes`. The help text and example mention running `backup` first.
- Flow:
  1. Expand both arguments (`expandUser`, `filepath.Abs`).
  2. Usage error (exit 2) if they are equal, or if NEW is under OLD.
  3. `os.Lstat` both: both present → error `NEW already exists`; neither present → error
     `OLD does not exist`; only NEW present → `manual := true`.
  4. `load` all sessions, then select by decision 3. Print the table with `printTable`, then
     `N session(s) to repoint (cwd under OLD)`.
  5. If any selected session is ongoing or current, error `N ongoing session(s) under OLD;
     close them first` and change nothing.
  6. With `-n`: print `Dry run: nothing moved.` and stop.
  7. Ask `confirm("Move OLD to NEW and repoint these sessions?")`; on no, print `Aborted.`.
  8. Unless `manual`: `os.Rename(old, new)`, wrapping errors per decision 8.
  9. `Repoint`.
  10. Print `Moved OLD to NEW.` (or `OLD already moved; repointed sessions only.`) and
      `Repointed N session(s).`. Failures go to stderr with `failed: <id>: <err>`, and the
      command exits 1.
- With zero selected sessions the folder is still moved, and the output says `No sessions to
  repoint.`.
- Completion (`internal/cli/complete.go`): both arguments complete as directories, with
  `ShellCompDirectiveFilterDirs`, as `--cwd` does.

#### Task 5 — tests (`internal/cli/move_test.go`, new; uses `testfix` and the helpers in `clean_test.go`)
- A real move: write a session whose `cwd` and `git_root` are a fresh `t.TempDir()/proj`,
  plus one session in `proj/sub` with `git_root` set to `proj`. After
  `move proj moved -y`, the folder is at `moved`, both YAMLs point under `moved` with every
  other line byte-identical, and the DB `sessions.cwd` is rewritten. An unrelated session
  (`/home/u/projX`) is unchanged, which also covers the `/a/b` vs `/a/bc` case.
- A manual move: OLD missing, NEW present, so sessions are repointed and nothing is moved.
- Refusals: NEW exists; neither exists; NEW under OLD (exit 2); an ongoing session under
  OLD. Each leaves both disk and the snapshot (`snapshot()`) unchanged.
- `-n` changes nothing. A missing DB still repoints the YAMLs.

### Acceptance test

```
just lint && just test && just test-linux
```

All green. Then, against real Copilot data, in the `cprobe` container (Copilot CLI 1.0.108,
sessions in `/work/...`):

```
GOOS=linux GOARCH=$(docker exec cprobe dpkg --print-architecture) CGO_ENABLED=0 go build -o /tmp/cs-linux ./cmd/copilot-sessions
docker cp /tmp/cs-linux cprobe:/usr/local/bin/copilot-sessions
docker exec cprobe bash -c 'mkdir -p /work/p && cd /work/p && git init -q && copilot --no-auto-update --no-ask-user -p "say ok"'
docker exec cprobe copilot-sessions move -n /work/p /work/q     # lists the session, nothing changes
docker exec cprobe copilot-sessions move -y /work/p /work/q
docker exec cprobe copilot-sessions list --cwd /work/q          # shows the session
```

Then run the `--resume` picker from `/work/q` under `script` (as in the 2026-11-09 probe). The
session shows Location `/work/q` and ranks first. `copilot --resume=<id>` from `/work/q`
still works.

### Docs update (mandatory when user-facing)

- `README.md`: add a `move` line to the Usage block and the command list, plus a short
  example section "Move a project folder". The section says that history, rewind snapshots and
  trusted folders keep the old path.
- `development/spec.md`: add a `move` section with the flow, the decisions above and the
  `workspace.yaml` keys (`git_root`), and add `move` to the completion table in §8.
- `CHANGELOG.md`: under Unreleased, "Added: `move` command: moves a project folder and
  repoints its sessions (CS-017)".

### Finish (mandatory)

1. Acceptance test green; `just lint` and `just test` clean.
2. Docs updated.
3. Write a summary (files touched, decisions, anything deferred).
4. Suggested commit message: `feat(cli): add move command to relocate a project and its sessions (CS-017)`.
5. Commit locally on `feat/CS-017-move-command`, with no push.
   `pickle ticket move CS-017 in-review --reason "acceptance green"`.

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: chat: user asked for a way to move a project folder Copilot was used in; findings from a Copilot CLI 1.0.108 probe in Docker
- 2026-11-09 — TO DO → READY: plan complete; defaults confirmed by user
