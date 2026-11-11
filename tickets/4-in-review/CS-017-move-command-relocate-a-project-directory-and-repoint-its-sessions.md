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
   `NEW + rest`. A session is selected when its `cwd` (YAML, else DB), its `git_root` **or**
   its DB `sessions.cwd` is under OLD. Each field is then rewritten only if it is under OLD itself.
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

#### Task 1 — move prefix matching into `session` (`internal/session/path.go`, new; the Windows test goes in `path_windows_test.go`)
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
  4. `load` all sessions (with sizes, `sized=true`, for `printTable`), then select by decision 3. Print the table with `printTable`, then
     `N session(s) to repoint (cwd under OLD)`.
  5. If any selected session is ongoing or current, error `N ongoing session(s) under OLD;
     close them first` and change nothing.
  6. With `-n`: print `Dry run: nothing moved.` and stop.
  7. Ask `confirm("Move OLD to NEW and repoint these sessions?")`; on no, print `Aborted.`.
  8. Last-chance `ctx.Err()` check, as `clean` does. Unless `manual`: `os.Rename(oldPath, newPath)`, wrapping errors per decision 8. Name the parameters `oldPath`/`newPath`, not `new`.
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
  other line byte-identical, and the DB `sessions.cwd` is rewritten. A sibling session in
  `t.TempDir()/projX` is unchanged, which covers the `/a/b` vs `/a/bc` case.
- A manual move: OLD missing, NEW present, so sessions are repointed and nothing is moved.
- Refusals: NEW exists; neither exists; NEW under OLD (exit 2); an ongoing session under
  OLD. For that last one, rewrite the fixture Ongoing session's YAML cwd into the temp tree,
  and `t.Skip` when `!fx.OngoingLive`, as `TestCleanOngoing` does. Each refusal leaves both
  disk and the snapshot (`snapshot()`) unchanged.
- Add `move` to `TestHelp` (`internal/cli/list_test.go`) for its Short and example.
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
- `development/spec.md`: add `move` to the §7 command list (with its example), and add a `move` section with the flow, the decisions above and the
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

Review round 1 (2026-11-09). Orchestrator: the authoring session. Audits were delegated to an independent reviewer, and the correctness hunt ran through the host code-review tool.

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| F1 | blocking | correctness | — | OLD is matched without resolving symlinks, but Copilot records the resolved cwd. Moving via a symlinked path finds no sessions, yet the folder still moves. | cprobe: `cd /work/link/app` (link → `/data/real`); Copilot recorded `cwd: /data/real/app`; `move -n /work/link/app /work/app2` printed `No sessions to repoint.` | Match sessions against both the given and the symlink-resolved OLD. |
| F2 | blocking | correctness | — | A quoted YAML value (`cwd: '/a: b'`) is never rebased, yet the command reports `Repointed 1 session` (only the DB row changes). | cprobe: Copilot wrote `cwd: '/work/a: b'`; after `move -y '/work/a: b' /work/ab` the YAML was unchanged, DB `/work/ab`. | Unquote single- and double-quoted scalars before matching; keep the style when writing back. |
| F3 | blocking | correctness | — | NEW is written unquoted. A NEW containing `: `, ` #` or a newline produces YAML that Copilot parses differently, or extra keys. | Delegated reviewer: `move -y T/proj $'T/new\nname: pwned'` wrote a `name: pwned` line, exit 0; `'T/a: b'` wrote `cwd: T/a: b`. | Quote NEW when YAML needs it; refuse control characters (usage error). |
| F4 | blocking | correctness | — | The ongoing check runs before the prompt and is not repeated. A Copilot started in OLD while the prompt waits is moved out from under it, and its workspace.yaml is rewritten while it is live. Same for a NEW created in the meantime. | unreproduced: needs a concurrent Copilot start. Traced: `runMove` ongoing check → `confirm` → `os.Rename` → `Repoint` (`internal/cli/move.go`); CS-017 decision 4. | Reload and re-check sessions and NEW after confirming. |
| F5 | non-blocking | correctness | noted | A case-only rename (`App` → `app`) is refused on case-insensitive filesystems (Windows, default APFS). | Code review: `Under` is case-insensitive on Windows; `Lstat(NEW)` finds OLD on APFS. | Rename by hand in two steps; revisit if asked. |
| F6 | non-blocking | correctness | noted | On Windows, `Rebase` cuts at the byte length of OLD after a case-insensitive match. Case folds that change byte length (non-ASCII) would garble the result. | `internal/session/path.go` `Rebase`; already marked with a `ponytail:` comment. | Compare runes if such paths show up. |
| F7 | non-blocking | correctness | noted | The moved-by-hand path accepts a NEW that is a file or a dangling symlink. | Code review: `pathExists` uses `Lstat` only. | Require a directory at NEW. |
| F8 | non-blocking | design | fixed inline | `repointDB` runs a SELECT per session although `Load` already filled `DBCwd`. | `internal/session/move.go` `repointDB`. | Rebase from `s.DBCwd` and only UPDATE. |
| F9 | non-blocking | design | fixed inline | `_, all, err := load(...)` names the filtered result `all` and runs filters for nothing. | `internal/cli/move.go` `runMove`. | Call `session.Load` and `session.Sort` directly. |
| F10 | non-blocking | design | noted | `pathExists` re-implements the Lstat/ErrNotExist check found in `backup.go` and `restore.go`. | `internal/backup/backup.go:74`, `internal/cli/restore.go:121`. | Share a helper if a fourth copy appears. |
| F11 | non-blocking | test-gap | fixed inline | The per-session failure path of decision 9 (`failed:` line, siblings still repointed, exit 1) is untested. | `move_test.go` has no failure case. | Add a test with an unwritable session dir (skip on Windows and as root). |
| F12 | non-blocking | test-gap | fixed inline | The interactive confirm path (answer `n` → `Aborted.`, nothing moved) is untested. | Every move test passes `-y` or `-n`. | Add one TTY test with stdin `n`. |
| F13 | non-blocking | correctness | noted | `rename(dir, emptydir)` on Unix replaces an empty NEW created between the check and the rename. F4's re-check narrows the window but does not close it. | unreproduced: needs a concurrent creator. Traced: `pathExists(newPath)` → `os.Rename`. | `renameat2(RENAME_NOREPLACE)` is Linux-only; not worth it. |

Disposition summary: 4 blocking (F1–F4) → rework; 9 non-blocking: 4 fixed inline (F8, F9, F11, F12), 5 noted (F5, F6, F7, F10, F13), 0 new tickets.
cost: estimated M, actual M

Verdict: rework (F1–F4).

- [x] Reviewer independence settled (step 0): delegated to an independent reviewer (steps 1–4a, hunt: angle 7 only); none of its findings discarded.
- [x] In-tree stale-branch check (step 0a): `pickle doctor` warned that the branch was behind main; rebased; clean.
- [x] Implementation audit (steps 1, 2): the delegated reviewer re-ran `just lint && just test && just test-linux` (green) and the cprobe check (`/work/rvp` → `/work/rvq`); Tasks 1–5 and decisions 1–10 were met before these findings.
- [x] Correctness hunt (step 3): host tool (`/code-review medium main...feat/CS-017-move-command`) run by the orchestrator, 9 findings, 0 discarded; angle 7 run separately by the delegated reviewer; F1 and F2 reproduced in cprobe by the orchestrator, F3 by the delegated reviewer; F4, F13 unreproduced with the traced path.
- [x] Consistency audit (step 4): delegated, clean.
- [x] Documentation audit (step 4a): delegated; README, spec §6.6/§7/§8 and CHANGELOG are covered; no docs build is configured.
- [ ] Docs-readability pass (step 4b): conscious skip; optional.
- [x] Findings recorded (step 5).
- [x] Ticket moved to `5-rework/` (step 6).

### Rework fix record — round 1 (commits `fix(cli): move handles symlinked paths, quoted YAML and changes during the prompt (CS-017)` and `docs(spec): move resolves symlinks, reads quoted YAML, re-checks after the prompt (CS-017)`; SHAs a71cbfb, 311c453 as of round 2, rewritten by rebases onto main, see R6)

- F1: `runMove` matches sessions against OLD both as given and with symlinks resolved (`resolveSymlinks`, which falls back to the parent for a manual move). `Repoint`/`RebaseAny` take every root. Test `TestMoveSymlink`. cprobe: `move /work/link/app /work/app2` repointed the session recorded at `/data/real/app`.
- F2: `unquoteYAML` reads single- and double-quoted values; `Load` unquotes `cwd`/`git_root` (`yamlPath`), and the rewrite keeps the quotes. A double-quoted value it cannot unquote is reported as `failed:`, never skipped. Test `TestMoveQuoted`. cprobe: Copilot wrote `cwd: '/work/p: q'`; `list --cwd` found it and `move` rewrote it to `'/work/r: s'`.
- F3: a NEW with a control character is a usage error (exit 2). NEW is single-quoted when YAML needs it (`needsQuote`). Covered by `TestMoveRefused` and `TestMoveQuoted`. cprobe: a NEW with a newline exits 2.
- F4: after the confirm, `moveSides` and `moveSelection` run again; a change stops the move with nothing done. Test `TestMoveRecheckAfterConfirm` (stdin creates NEW while answering).
- Inline fixes: F8 (`repointDB` updates from `s.DBCwd`, no SELECT), F9 (`moveSelection` calls `session.Load` + `Sort`), F11 (`TestMoveFailedSession`, which also covers the rerun), F12 (`TestMoveConfirmNo`).
- Spec §6.6 updated for F1–F4. Mutation checks: disabling the F1 symlink root, the F4 re-check or the F2 unquote each makes its test fail.
- `just lint`, `just test`, `just test-linux` green; `GOOS=windows go vet` clean.


### Scoped re-review — round 2 (2026-11-09)

The fix diff was read as `3ed4791..1d8b1a3`; see R6.

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| R1 | blocking | correctness | — | F1 is only half fixed. NEW is not resolved, so sessions are rewritten to NEW as typed (through the symlink), while Copilot records the resolved cwd. | Orchestrator repro: a session with `cwd: /private/tmp/r1…/proj`; after `move -y /tmp/r1…/proj /tmp/r1…/moved` the YAML read `cwd: /tmp/r1…/moved`, but `pwd -P` in NEW gave `/private/tmp/r1…/moved`. | Repoint to NEW with symlinks resolved, after the rename. |
| R2 | non-blocking | correctness | noted | When OLD is itself a symlink, `os.Rename` moves only the link, yet sessions in the real folder are rewritten through the new link name. The paths still resolve (`link2 -> real`). | Reviewer repro: `move -y …/link …/link2` left `real/` in place, and the YAML read `cwd: …/link2/app`. | Refuse an OLD that is a symlink, if it matters. |
| R3 | non-blocking | correctness | noted | A YAML `"\xNN"` escape decodes as a byte in Go but as U+00NN in YAML, so a value like this is silently not matched. js-yaml writes printable non-ASCII literally, so this is unlikely. | Reviewer repro: a hand-written `cwd: "<R>/caf\xe9"` was not repointed, exit 0. | Reject `\x`, or decode it as a rune, if such files show up. |
| R4 | non-blocking | test-gap | fixed inline | The post-confirm ongoing re-check of F4 has no test. | Reviewer read: no test reaches that branch. | Make the fixture Ongoing session go under OLD during the prompt. |
| R5 | non-blocking | spec-unclear | fixed inline | Spec §6.6 step 5 does not say that sessions appearing during the prompt are repointed too. | `internal/cli/move.go` comment vs spec §6.6. | Add the clause. |
| R6 | non-blocking | stale-xref | fixed inline | The round 1 fix record cites `f82efee..99ac632`, which the rebase replaced with `42d18da`, `1d8b1a3`. | `git range-diff`. | Cite the commits on the branch. |
| R7 | non-blocking | stale-xref | fixed inline | The `ponytail:` comment in `unquoteYAML` claims a YAML-only escape is always reported. R3 shows it is not. | `internal/session/move.go`. | State the `\x` limit. |

Disposition summary: 1 blocking (R1) → rework; 6 non-blocking: 4 fixed inline (R4–R7), 2 noted (R2, R3), 0 new tickets.

Verdict: rework (R1).

- [x] Reviewer independence (step 0): delegated scoped re-review, hunt one pass; none of its findings discarded. R1 re-verified by the orchestrator.
- [x] Stale-branch check (step 0a): `pickle doctor` clean.
- [x] Implementation audit: F2–F4 and the inline fixes verified; F1 half closed (R1). `just lint`, `just test`, `just test-linux` green; cprobe check `/work/rr1` → `/work/rr2` passed.
- [x] Correctness hunt: one pass over 8 angles on the fix diff. R1, R2 and R3 reproduced.
- [x] Consistency and docs audits: clean apart from R5.


### Rework fix record — round 2 (commit `fix(cli): move writes NEW with symlinks resolved (CS-017)`; recorded as a3f958e, 6469590 after the rebase onto main, see G3)

- R1: `Repoint` is given `resolveSymlinks(newPath)`, so sessions get NEW as Copilot records it. `TestMoveSymlink` now moves through the link on both sides; it fails when the fix is reverted. cprobe: `move -y /work/rr2 /lnk/rr3` (`/lnk -> /work`) wrote `cwd: /work/rr3`, and the session resumed from `/lnk/rr3`.
- Inline fixes: R4 (`TestMoveRecheckOngoing`; fails when the post-confirm ongoing check is removed), R5 (spec §6.6 step 5 clause and step 7 resolved NEW), R6 (round 1 record cites commit subjects), R7 (the `ponytail:` comment states the `\x` limit).
- `just lint`, `just test`, `just test-linux` green; `GOOS=windows go vet` clean.


### Scoped re-review — round 3 (2026-11-09)

This round read the round 2 fix commit, `fix(cli): move writes NEW with symlinks resolved (CS-017)` (6469590).

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| G1 | blocking | correctness | — | The R1 fix reopens F3. The control-character guard checks NEW as typed, but the resolved NEW is what gets written. A symlink whose target name contains a newline puts a raw newline into `workspace.yaml`, and the command exits 0. | Orchestrator repro (macOS, scratch `COPILOT_HOME`, HEAD binary): `lnk -> "a<LF>b"`; `move -y proj lnk/moved` printed `Repointed 1 session.`, and the YAML then read `cwd: …/g1b/a` with `b/moved` on the next line. | Refuse a control character in the resolved NEW too, before the rename (usage error), and cover it in a test. |
| G2 | non-blocking | correctness | noted | On Windows, Node's `process.cwd()` keeps directory symlinks, so writing the resolved NEW may differ from what Copilot records. `list --cwd` still matches; picker ranking may not. Only real symlinks are affected (junctions are not resolved). | unreproduced: no Windows host. Traced: `resolveSymlinks` → `filepath.EvalSymlinks` → `Repoint`. | Skip resolving NEW on Windows if this shows up. |
| G3 | non-blocking | stale-xref | fixed inline | The round 2 fix record cited `a3f958e`, which the rebase rewrote to `6469590`. | `git range-diff`: `1: a3f958e = 1: 6469590`. | The record now names the commit subject and both SHAs. |

Disposition summary: 1 blocking (G1) → rework; 2 non-blocking: 1 fixed inline (G3), 1 noted (G2), 0 new tickets.

Verdict: rework (G1). This was the second rework round of this invocation, so the session stops here.

- [x] Reviewer independence (step 0): delegated scoped re-review, hunt one pass; G1 re-verified by the orchestrator, nothing discarded.
- [x] Stale-branch check (step 0a): `pickle doctor` clean.
- [x] Implementation audit: R1, R4, R5 and R7 met; `just lint`, `just test`, `just test-linux` green; `GOOS=windows go vet` clean.
- [x] Correctness hunt: one pass over 8 angles on the fix diff; G1 reproduced, G2 unreproduced (traced).
- [x] Consistency and docs audits: clean apart from G2 and G3.

### Rework fix record — round 3 (commit `fix(cli): move refuses a control character in the resolved NEW (CS-017)`; recorded as ab7284b, 00fe69e after the rebase onto main, see V3)

- G1: `moveTarget` resolves NEW and refuses a control character in NEW as typed *or* as resolved (usage error, exit 2), before anything moves. It runs again after the confirm, because a symlink on NEW's path may change during the prompt, and its result is the value passed to `Repoint`. Test `TestMoveSymlinkControl` (`lnk -> "a<LF>b"`, `move -y proj lnk/moved` → exit 2, disk and snapshot unchanged); it fails when the resolved-path check is removed. Spec §6.6 step 1 says "as typed or with symlinks resolved".
- `just lint`, `just test`, `just test-linux` green; `GOOS=windows go vet` clean. cprobe re-check skipped: the container was not running; the macOS repro path is covered by the test.

### Scoped re-review — round 4 (2026-11-11)

This round read the round 3 fix commit 00fe69e.

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| V1 | blocking | correctness | — | The G1 fix resolves NEW before `os.Rename`, while NEW does not exist yet. When OLD is a symlink, only the link moves, and sessions are rewritten through the new link name instead of the real folder Copilot records. Round 2 got this right because it resolved NEW after the rename. Spec §6.6 step 7 no longer holds. | Reviewer repro (macOS, scratch `COPILOT_HOME`): `link -> real`, session `cwd: <T>/real/app`; `move -y <T>/link <T>/link2` left the YAML at `<T>/real/app` with dfc62bf and changed it to `<T>/link2/app` with 00fe69e. Orchestrator: the new `TestMoveSymlinkOld` fails on 00fe69e. | Keep the pre-rename check; resolve NEW again after the rename and pass that value to `Repoint`. |
| V2 | non-blocking | test-gap | fixed inline | The post-confirm `moveTarget` re-check had no test. | Reviewer mutation: deleting it left the suite green. | It was removed in the V1 fix: the post-rename check runs later and covers a symlink swapped during the prompt. |
| V3 | non-blocking | stale-xref | fixed inline | The round 3 record cited `ab7284b`, which the rebase rewrote to `00fe69e`. | `git range-diff`: `ab7284b = 00fe69e`. | The record now names the commit subject and both SHAs. |

The reviewer reported V1 as non-blocking and left the call to the orchestrator. The orchestrator classed it blocking: it is a regression of the path the ticket's Outcome depends on (picker ranking from the new folder).

Disposition summary: 1 blocking (V1) → rework; 2 non-blocking, both fixed inline (V2, V3); 0 new tickets.

Verdict: rework (V1).

- [x] Reviewer independence (step 0): delegated scoped re-review, hunt one pass; V1 re-verified by the orchestrator, nothing discarded; severity of V1 raised by the orchestrator.
- [x] Stale-branch check (step 0a): `pickle doctor` clean for CS-017 (warnings only for the AGENTS.md marker drift and the payload 1.4.3 vs 1.5.0 mismatch).
- [x] Implementation audit: G1 met; `just lint`, `just test`, `just test-linux`, `GOOS=windows go vet` green; the reviewer also ran the cprobe real-data check (`/work/v4p` → `/work/v4q`).
- [x] Correctness hunt: one pass over 8 angles on the fix diff; V1 reproduced, V2 shown by mutation.
- [x] Consistency and docs audits: clean apart from spec §6.6 step 7 (V1).

### Rework fix record — round 4 (commit `fix(cli): move resolves NEW after the rename (CS-017)`; recorded as 0e7205e, d16a6c3 after the rebase onto main)

- V1: `runMove` checks `moveTarget(NEW)` before the rename (G1 guard, nothing moved on refusal) and resolves it again after the rename. That value goes to `Repoint`, so when OLD is a symlink, NEW resolves to the real folder. A control character found at that point is reported as `repointed no sessions: …`. Test `TestMoveSymlinkOld` (`link -> proj`, `move -y link link2` leaves the YAML byte-identical); it fails on 00fe69e. Spec §6.6 step 7 now says NEW is resolved after the rename.
- V2: the post-confirm re-check is removed, because the post-rename check supersedes it.
- `just lint`, `just test`, `just test-linux` green; `GOOS=windows go vet` clean.

### Scoped re-review — round 5 (2026-11-11)

This round read the round 4 fix commit d16a6c3.

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| P1 | blocking | correctness | — | When OLD is a **relative** symlink and NEW is in another directory, the moved link dangles. Resolving NEW after the rename falls back to NEW as typed, so sessions in the real folder, which never moved, are rewritten to a path that does not exist, and the command exits 0. This is R2 in a case where R2's reason for noting it ("the paths still resolve") no longer holds. It predates round 4 (00fe69e and dfc62bf behave the same). | Reviewer repro (HEAD binary, scratch `COPILOT_HOME`): `t/link -> proj` (relative); `move -y t/link t/other/link2` → `Repointed 1 session.`, exit 0, YAML `cwd: …/other/link2/app`, which does not exist. Orchestrator re-verified with a throwaway test on the fixture: exit 0, `Repointed 3 sessions.`, `cwd`/`git_root` → `…/other/link2`. | Refuse an OLD that is a symlink (`Lstat`) as a usage error before anything moves (R2's suggestion). This also closes P2 and P3, and makes the post-rename resolve unnecessary. |
| P2 | non-blocking | correctness | noted | A post-rename `moveTarget` failure exits 2 (usage error wrapped with `%w`, matched by `errors.As`) after the folder has moved, and a rerun is refused by the pre-rename check, so decision 9 does not hold on this path. It also fails when no session needed repointing. | Reviewer repro: `t/link -> "a<LF>b"`, no sessions; `move -y t/link t/link2` printed `Moved …`, then error, exit 2; the rerun was refused, exit 2. `internal/cli/move.go:132-135`. | Closed by P1's fix. Otherwise, use a plain error (exit 1) and skip the check when nothing is selected. |
| P3 | non-blocking | test-gap | noted | The post-rename refusal branch has no test. | Reviewer read: deleting `move.go:132-135` leaves the suite green. | Becomes moot under P1's fix. |
| P4 | non-blocking | spec-unclear | noted | Spec §6.6 steps 1, 7 and 8 do not hold for P1/P2 (step 7's "NEW resolves to the real folder"; step 8's "a rerun finishes the job"). | `development/spec.md` §6.6. | Rewrite these steps with P1's fix. |

Disposition summary: 1 blocking (P1) → rework; 3 non-blocking, noted (P2–P4) because P1's fix is expected to close them; 0 new tickets. The round 4 record's SHA was also corrected inline.

Verdict: rework (P1). This was the second rework round of this invocation, so the session stops here.

- [x] Reviewer independence (step 0): delegated scoped re-review, hunt one pass; P1 re-verified by the orchestrator, nothing discarded.
- [x] Stale-branch check (step 0a): `pickle doctor` clean for CS-017.
- [x] Implementation audit: V1 met for absolute symlinks only (P1); V2 as recorded; `just lint`, `just test`, `just test-linux`, `GOOS=windows go vet` green; cprobe check skipped (container not running).
- [x] Correctness hunt: one pass over 8 angles on the fix diff; P1 and P2 reproduced.
- [x] Consistency and docs audits: spec §6.6 mismatches recorded as P4.

### Rework fix record — round 5 (commit `fix(cli): move refuses an OLD that is a symlink (CS-017)`, 78b0ff5 as recorded; the in-tree rebase onto main may rewrite the SHA, the subject stays)

- P1: an OLD that is itself a symlink (`os.Lstat`) is a usage error (exit 2) before anything moves, as R2 suggested. NEW is resolved once, before the rename (its parent, since NEW does not exist yet), and that value goes to `Repoint`. `TestMoveSymlinkOld` now uses a relative link moved into another directory (the P1 repro) and expects exit 2 with disk and snapshot unchanged; it fails when the guard is disabled. An OLD reached through a symlinked parent (F1) is unaffected (`TestMoveSymlink` green).
- P2, P3: closed. The post-rename resolve and its `repointed no sessions:` branch are removed, so no error remains after the folder has moved except per-session `failed:` (decision 9).
- P4: spec §6.6 step 1 adds the symlinked-OLD refusal, and step 7 drops the "resolved after the rename" clause. Step 8 holds again.
- R2 (noted in round 2) is closed by the same refusal.
- `just lint`, `just test`, `just test-linux` green; `GOOS=windows go vet` clean. cprobe re-check skipped: the container was not running.

## History

- 2026-11-09 — created (TO DO). source: chat: user asked for a way to move a project folder Copilot was used in; findings from a Copilot CLI 1.0.108 probe in Docker
- 2026-11-09 — TO DO → READY: plan complete; defaults confirmed by user
- 2026-11-09 — plan amended inline: applicability gate (independent sub-agent) found no blocking findings; folded in the test sibling `projX`, the ongoing-test skip, sized load, a last-chance ctx check, the TestHelp/spec §7 entries and the Windows test file; noted quoted-YAML and YAML-over-DB selection as known limits
- 2026-11-09 — READY → IN DEVELOPMENT: picked up, branch feat/CS-017-move-command
- 2026-11-09 — plan amended inline: decision 3 also selects by DB `sessions.cwd` (new `Session.DBCwd`), found in the cprobe check, where a session whose YAML was already repointed kept a stale DB row and a rerun skipped it, which broke decision 9; covered by `TestMoveRerunFinishesDB`
- 2026-11-09 — IN DEVELOPMENT → IN REVIEW: acceptance green: lint, test, test-linux, cprobe real-data check
- 2026-11-09 — IN REVIEW → REWORK: 4 blocking findings (F1-F4)
- 2026-11-09 — REWORK → IN REVIEW: findings fixed (F1-F4)
- 2026-11-09 — IN REVIEW → REWORK: re-review: 1 blocking (R1)
- 2026-11-09 — REWORK → IN REVIEW: R1 fixed
- 2026-11-09 — IN REVIEW → REWORK: re-review round 3: 1 blocking (G1); rework cap reached
- 2026-11-11 — REWORK → IN REVIEW: findings fixed (G1)
- 2026-11-11 — IN REVIEW → REWORK: re-review round 4: 1 blocking (V1)
- 2026-11-11 — REWORK → IN REVIEW: V1 fixed
- 2026-11-11 — IN REVIEW → REWORK: re-review round 5: 1 blocking (P1); rework cap reached
- 2026-11-11 — REWORK → IN REVIEW: findings fixed (P1)
