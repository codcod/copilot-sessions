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

Soft coupling: builds on CS-001 (the session model, filters, fixture and seams). CS-001 review findings F5 (unsanitised names/IDs) and F11 (an unstattable `session-state/` reads as empty, like the script) bear on `clean`'s "all known sessions" full clear. Decided at refinement (2026-11-07, user): script parity for both, with no stricter view and no sanitising (plan decisions 2–3). CS-004's
round-trip test uses `clean --all`.

## Implementation Plan

### 0. Feature branch (mandatory)

Before any change, create the feature branch. The child is this repository (`path = "."`):

```
git checkout main && git pull --ff-only
git checkout -b feat/CS-002-clean-command
```

Do all work on this branch and commit locally as you go; WIP commits are encouraged. Do not push
or open a merge request without explicit user approval. This is a root-path child, so before
presenting, tidy the WIP commits into atomic Conventional Commits; the default is to keep that
history rather than squash. Ticket and board bookkeeping is never committed on this branch
(in-tree layout, rules §0). Before pushing, check that the remote base is not behind:
`git fetch origin main && git diff --name-only origin/main...HEAD | grep '^tickets/'` must print
nothing. Merging is always the human's.

> If a dependency's branch is not merged to `main`, stop and tell the human rather than
> building on top of it.

### Prerequisite gate (hard)

- CS-001 is in `6-done/` and merged to `main` (PR #1, `533c9c0`). Holds as of 2026-11-07.
- The working tree is clean, and `go test ./...` is green on `main`.

### Confirmed design decisions (do not deviate without asking)

1. **The Python script is the reference where the spec is silent.** `cmd_clean`,
   `prune_db`, `prune_ui_state` and `write_json` are in `development/copilot-sessions.sh`
   (lines 420–509).
2. **The full clear keeps script parity (CS-001 F11, user decision 2026-11-07).** "All known
   sessions" means every session `session.Load` returns, current and ongoing ones included,
   so `--all` with the current session skipped never triggers a full clear. When entries in
   `session-state/` cannot be stat'ed and show up as DB-only, Go behaves exactly as the
   script does. No stricter view is added.
3. **Names and IDs are printed as-is (CS-001 F5 stays noted).** `clean` pipes nothing, and
   stripping control runes would depart from the script.
4. **The confirmation helper lives in `internal/cli/confirm.go`, with this exact signature:**
   `func confirm(env Env, question string, yes bool) (bool, error)`. It returns `true` if
   `yes` is set. With no TTY (`!env.IsTTY()`) it returns
   `errors.New("confirmation required; rerun with --yes")`. Otherwise it writes
   `<question> [y/N] ` to stdout and reads one line; only `y`/`yes` (trimmed,
   case-insensitive) is true. EOF counts as an empty answer. CS-003 and CS-004 reuse it
   unchanged.
5. **There is one buffered stdin reader per run.** `Execute` wraps `env.Stdin` once in a
   `*bufio.Reader`, stored in a new unexported `Env` field `in`. A helper
   `readLine(env Env) string` reads through it, so restore's pick-then-confirm (CS-004) never
   loses buffered input.
6. **Deletion lives in `internal/session/delete.go`:**
   `func Delete(home string, ss []Session, everything bool) error`. It runs the spec's three
   steps in order: directories, DB, UI state. `os.RemoveAll` errors are ignored, as in the
   script's `shutil.rmtree(ignore_errors=True)`; DB and UI-state errors are returned.
7. **DB pruning runs in one transaction on one connection.** It uses `BeginTx`, then
   `CREATE TEMP TABLE prune_ids (id TEXT PRIMARY KEY)` and `INSERT OR IGNORE` with a bound
   parameter per ID. It reads `SELECT name FROM sqlite_master`, then for each table of the
   fixed list `search_index, turns, checkpoints, session_files, session_refs,
   forge_trajectory_events, assistant_usage_events` that exists, runs
   `DELETE FROM <t> WHERE session_id IN (SELECT id FROM prune_ids)`. Then it deletes from
   `sessions` and commits. Table names come only from that constant list. The existing
   `openDB` already sets the 30 s busy timeout, and a missing DB is a no-op.
8. **UI-state files decode as `map[string]json.RawMessage`,** so nested values survive
   untouched. A file is written only when the key count or the `sessionIds` length changed
   (script parity). Writes go to `os.CreateTemp` in the same directory, then the file is
   chmodded to the original `Mode().Perm()` and renamed into place. Encoding uses a
   `json.Encoder` with `SetIndent("", "  ")` and `SetEscapeHTML(false)`, and the encoder
   adds the trailing newline. Accepted text differences from the script: Go sorts object
   keys and writes non-ASCII as UTF-8 where Python writes `\uXXXX`. Both are the same JSON.
   An empty file reads as `{}`, like the script.
9. **Validation runs before any session is loaded, and its errors are plain errors
   (exit 1).** The checks are `--all` with `filter.Filters.Any()`, and neither `--all` nor
   `Any()`.

### Tasks

#### Task 1 — confirmation helper and stdin reader

Create `internal/cli/confirm.go` with `confirm` and `readLine` (decisions 4 and 5). Add the
`in *bufio.Reader` field to `Env` and set it at the top of `Execute`.

#### Task 2 — `session.Delete`

Create `internal/session/delete.go` per decisions 6–8: `Delete`, the unexported `pruneDB` and
`pruneUIState`, and a `writeJSONFile(path string, v any) error` helper. Add a
`SidebarDir = "sidebar-sessions-state"` constant next to `StateDir`/`DBFile`/`OpenState`.
With `everything`, `open-sessions-state.json` becomes `{}` and every sidebar file's
`sessionIds` becomes `[]`. A sidebar file whose `sessionIds` is missing or not an array is
left alone.

#### Task 3 — `clean` command

Create `internal/cli/clean.go` with `newClean(env Env) *cobra.Command`: `Use: "clean [ID...]"`,
`Aliases: rm`, `Short: "delete sessions"`. Port the flag help strings from the script (lines
698–703). Register it in `Execute` after `newList`. Its run function:

1. Validation (decision 9).
2. `load(env, f, !quiet)` (the existing helper in `cli.go`) gives `all` and `selected`.
3. Drop current sessions and note `Skipped this session (it runs this command).`. Unless
   `--include-ongoing` or `f.Status["ongoing"]`, drop non-current ongoing sessions and note
   `Skipped <plural(n, "ongoing session")> (use --include-ongoing).`. Add a `plural(n, word)`
   helper to `cli.go` (spec §6.5); CS-003 and CS-004 use it.
4. Print the notes. If nothing remains, print `No sessions match.` and return nil.
5. Unless `-q`, `printTable(w, selected, true)`. Then print
   `<N> of <len(all)> sessions selected (<scope>[, <HumanSize(total)>]).`, where scope is
   `all sessions` for `--all`, else `f.Describe()`.
6. With `-n`, print `Dry run: nothing deleted.` and return.
7. `confirm(env, "Permanently delete these sessions?", yes)`. On false, print `Aborted.` and
   return nil.
8. `session.Delete(env.Home, selected, <selected ID set equals all ID set>)`, then print
   `Deleted <plural(n, "session")>.`

#### Task 4 — tests

Create `internal/cli/clean_test.go`. Generalise the `run` helper in `list_test.go` into
`runIn(fx, ancestors, stdin string, tty bool, args...)` and keep `run` as a wrapper around it.
Cover spec §9 cases 5–9:

- **Case 5.** Both validation messages, with exit 1; `--all --grep x` is rejected.
- **Case 6.** With `nil` ancestors and only on Linux (`fx.OngoingLive`; skip otherwise), the
  ongoing session is left out with `--all` (the skip note
  `Skipped 1 ongoing session (use --include-ongoing).` is printed) and with `--status closed`
  (not selected, so no note). It is included with `--all --include-ongoing` and with
  `--status ongoing`. Assert the notes and the summary counts with `-n`.
- **Case 7.** With ancestors `{StalePID: true}`, `clean --all --include-ongoing -y` leaves
  the `Stale` dir and its DB rows in place and prints the "Skipped this session" note.
- **Case 8.** `clean aaaa1 -y` (Titled) checks:
  - the dir is gone;
  - zero rows for the ID in `search_index`, `turns` and the five other tables, and in
    `sessions`;
  - the `Stale` rows are untouched;
  - `forge_skill_proposals`/`schema_version` are untouched;
  - Titled is gone from `open-sessions-state.json` and `a.json` while `extra-key-id` and
    `collapsed` are kept;
  - `a.json`, chmodded to `0600` before the run, is still `0600`.

  Then, with a fresh fixture, `nil` ancestors and `clean --all --include-ongoing -y`: open
  state is `{}` (extra key gone too) and `sessionIds` is `[]`. Open the DB in the test with
  `database/sql` + `modernc.org/sqlite`.
- **Case 9.** `clean --all -n` changes nothing: the dirs, DB row counts and both JSON files
  are byte-identical. `clean --all` with no TTY fails with
  `error: confirmation required; rerun with --yes`, exit 1. With a TTY and stdin `"n\n"` it
  prints `Aborted.` and deletes nothing; with stdin `"Yes\n"` it deletes.

Extend `TestParity` in `internal/cli/parity_test.go`: for each existing filter set, compare
`clean -n <filters>` through `withoutSize`, using `clean -n --all` for the empty set. This is
the spec §13 `clean -n` parity criterion.

### Acceptance test

```
gofmt -l . | wc -l                     # 0
go vet ./...
go test -count=1 ./...                 # macOS: clean tests and TestParity pass
just test-linux                        # if CS-006 is merged; else the docker run in README § Development
CGO_ENABLED=0 go build ./cmd/copilot-sessions   # -> ./copilot-sessions (gitignored)
./copilot-sessions clean; echo $?                 # "error: specify session IDs, a filter (...) or --all", 1
./copilot-sessions clean --all --grep x; echo $?  # "error: --all cannot be combined with IDs or filters", 1
./copilot-sessions rm --help                      # lists all clean flags plus the shared filters
```

The Linux run must show case 6 executing, not skipped.

### Docs update (mandatory when user-facing)

The user-facing surface is `clean --help`, with flag help ported from the script (Task 3). The
README is unchanged here; CS-005 writes the usage and install section. No changelog.

### Finish (mandatory)

1. Acceptance test green on macOS and Linux (Docker).
2. Help strings in place.
3. Write a summary: files touched, decisions made, anything deferred.
4. Suggested commit message:

   ```
   feat(clean): delete sessions, DB rows and UI state with filters (CS-002)

   Adds the clean (rm) command with the spec §6.2 validation, safety rules and
   output, a shared confirmation helper, and session.Delete, which prunes the
   DB in one transaction and rewrites UI state atomically.
   ```

5. Tidy the WIP commits into atomic ones (root-path child).
6. Commit locally; do not push. `pickle ticket move CS-002 in-review --reason "acceptance green"`.

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md §6.2 (Go rewrite of copilot-sessions)
- 2026-11-06 — impact sweep from CS-001 review: added pointer to CS-001 findings F5 and F11.
- 2026-11-07 — TO DO → READY: plan complete
