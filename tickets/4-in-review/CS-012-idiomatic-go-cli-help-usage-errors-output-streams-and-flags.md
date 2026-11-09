---
id: CS-012
title: Idiomatic Go CLI: help, usage errors, output streams and flags
project: copilot-sessions
depends-on: []
spawned-by: []
impact: medium
complexity: medium
cost: M
---

# CS-012 — Idiomatic Go CLI: help, usage errors, output streams and flags

## Outcome

After this ships, `copilot-sessions` behaves like other Cobra-based Go tools: a bare invocation
shows help, every usage mistake exits 2, typos get "Did you mean …?", `--help` has consistent
summaries and examples, and stdout carries only results so it can be piped safely. The spec
records each of these as an intended difference from the Python script.

## Description

A review of the command line against Go/Cobra conventions on 2026-11-09 found eight
discrepancies. Items 1–3 are mandated today by `development/spec.md` (Python-script parity,
§11 l. 550 "flags, aliases, output, messages, exit codes … match"); this ticket drops parity for
them and updates the spec. All eight are in scope.

1. **Bare invocation errors instead of showing help.** `copilot-sessions` exits 2 with the
   argparse-style `error: the following arguments are required: COMMAND`; the root's `RunE`
   (`internal/cli/cli.go`) also makes the usage line show `copilot-sessions [flags]`. Idiom: no
   root `RunE`, print help, exit 0.
2. **Flag-combination mistakes exit 1.** `clean` with no selection, `clean --all` with
   IDs/filters, `backup --list` with FILE/`--keep`/`--no-create`, `backup --json` without
   `--list`, `backup --no-create` without `--keep` (spec §6 "Validation (exit 1)", l. 244, 319).
   Idiom: usage errors exit 2, declared with `MarkFlagsMutuallyExclusive` /
   `MarkFlagsRequiredTogether` / `MarkFlagsOneRequired` where they fit, a `usageError` otherwise.
   Today value errors (`--until bogus`) already exit 2, so the two are inconsistent.
3. **Cobra's suggestions are lost.** The root's custom `Args` replaces Cobra's unknown-command
   handling: `copilot-sessions lst` gets no "Did you mean this? list"; `help lst` prints root help
   and exits 0.
4. **`Short` strings are lower-case** ("list sessions with their status") while Cobra's own
   (`completion`, `help`) are capitalised; `--help` mixes both. Idiom: capitalised, no full stop.
5. **No `Example:` on any command.** Reuse the README's *Examples* section.
6. **Prompts and diagnostics go to stdout.** The `[y/N]` prompt (`internal/cli/confirm.go`),
   `Skipped …`, `Aborted.`, and the Ctrl-C `
Aborted.` (`cmd/copilot-sessions/main.go`). Idiom:
   stdout for results, stderr for prompts and diagnostics (split decided 2026-11-09, decision 4).
7. **`-q/--quiet` means something different per command** (`list`: IDs only; `clean`: no table
   but still counts; `backup`: path only), and `list --json -q` is accepted with `--json` silently
   winning. Decided 2026-11-09: `-q` prints only machine-usable results (decision 5), and
   `list --json -q` is a usage error.
8. **`-s` (`--status`) and `-S` (`--since`)** differ only by case. Decided 2026-11-09: drop `-S`;
   `--since` is long-only.

Also in scope: `development/spec.md` (§6 per-command validation and exit codes, §7 help text,
§11 intended differences — one entry per changed behaviour), the README if a flag or example
changes, CHANGELOG (`### Changed`, breaking for scripts relying on exit 1 or `-S`/`-s`), and the
tests. `TestParity` compares `list`, `list --json -q`, `clean -n` and `backup --list` with the
Python script in `development/copilot-sessions.sh`; moving messages to stderr or changing `-q`
can break it. Decided 2026-11-09: parity narrows to stdout results with listed exclusions
(decision 8).

Soft couplings: CS-009/CS-010 add platform code under the same commands but no flags; whichever
lands second rebases. No release has been tagged yet, so changing exit codes and short flags now
breaks no published version.

## Implementation Plan

### 0. Feature branch (mandatory)

```
cd /Users/nka/Projects/codcod/copilot-sessions
git checkout main && git pull --ff-only
git checkout -b feat/CS-012-idiomatic-cli
```

Commit locally as you go. Root-path child (`path = "."`): tidy WIP commits into atomic ones
before presenting; keep that history by default. Before pushing (in-tree layout):
`git fetch origin main && git diff --name-only origin/main...HEAD | grep '^tickets/'` must print
nothing. No push / MR without the user's approval; merging is the human's.

### Prerequisite gate (hard)

- Clean tree on `main`; no `depends-on`.
- `python3` ≥ 3.12 on `PATH`, so `TestParity` runs instead of skipping (3.15 on the dev Mac,
  checked 2026-11-09). `docker` for `just test-linux`.
- No release tag exists (checked 2026-11-09). If one has been cut since, the CHANGELOG entry
  must say which released behaviour breaks; ask the user before proceeding.

### Confirmed design decisions (do not deviate without asking)

1. **The root command is not runnable.** No `RunE`, no custom `Args` on the root. A bare
   `copilot-sessions` prints the root help to stdout and exits 0; an unknown command gets
   Cobra's own message with "Did you mean this?" suggestions and exits 2.
2. **Every usage mistake exits 2, with today's message texts.** The flag-combination checks in
   `clean` and `backup` (spec §6.2/§6.3 "Validation") keep their exact messages but return
   `usageError`. Use our own checks returning `usageError`, not `MarkFlags*`, so every usage error
   goes through one mechanism and keeps a readable message. Unknown-command errors from Cobra are
   classified as usage errors in `Execute`. Runtime failures (ID not found, I/O, confirmation
   required without a TTY) stay exit 1.
3. **Help follows Cobra style.** Every `Short` is capitalised with no full stop; every command
   (`list`, `clean`, `backup`, `restore`) has an `Example:` taken from the README *Examples*.
4. **stdout carries results; stderr carries prompts and notes.** To stderr: the `[y/N]`
   confirmation prompt, `Restore which backup? [1] ` and the backup table printed before it,
   restore's `WARNING:` block, `Backups to remove (keeping the newest N):` and its table (prompt
   context), `Skipped …` notes, `Dry run: nothing deleted.`, `Aborted.` (including Ctrl-C's
   `
Aborted.`). Everything else stays on stdout: session/backup tables shown as results, the
   `N of M sessions …` lines, `No sessions match.`, `No backups in …`, `Nothing to remove: …`,
   `Deleted …`, `Backup created: …`, `Restored …`.
5. **`-q/--quiet` prints only machine-usable results, one per line, and nothing else on
   stdout.** `list -q`: session IDs (unchanged). `clean -q`: the IDs of the sessions deleted, or
   with `-n` that would be deleted; no table, no summary line, no `No sessions match.`, no
   `Deleted …`; sizes are still not computed. `backup -q`: the created path only; with `--keep`,
   rotation prints nothing on stdout (`Nothing to remove`/`Deleted N old backups` suppressed).
   stderr notes and prompts still appear under `-q`. Amended 2026-11-09 (review 1 F1, user sign-off): `clean -q`,
   when its prompt will be shown (no `-y`, no `-n`, TTY), prints the summary line without
   its size part to stderr before the prompt. `list --json` with `-q` is a usage error:
   `--json cannot be combined with --quiet`.
6. **`--since` loses its `-S` shorthand.** `-s` stays `--status`; `-u` stays `--until`. `-S` then
   fails as an unknown shorthand (exit 2).
7. **The spec records every change.** `development/spec.md` §11 drops "exit codes … must match"
   and lists each item above as an intended difference; §5.2, §6 (global, §6.1–§6.4), §7 and §13
   are updated to the new behaviour.
8. **`TestParity` compares stdout results only.** It strips from the Python output the lines that
   now go to stderr (`Skipped …`, `Dry run: nothing deleted.`), uses `list --json` instead of
   `list -q --json`, and uses `--since` instead of `-S`. Exclusions are listed in spec §13.

### Tasks

#### Task 1 — Root command (`internal/cli/cli.go`)

Remove the root's `Args` and `RunE`. In `Execute`, treat an error whose text starts with
`unknown command ` as a usage error (exit 2). Check `copilot-sessions help lst` and record what
Cobra does (it prints `Unknown help topic` and the root usage); accept Cobra's behaviour and note
it in the spec rather than overriding the help command.

#### Task 2 — Usage errors exit 2 (`internal/cli/clean.go`, `internal/cli/backup.go`, `cli.go`)

Wrap the `clean` checks (`--all cannot be combined…`, `specify session IDs…`) and the `backup`
checks (`--list cannot be combined…`, `--json only works with --list`, `--no-create requires
--keep N`, `--keep must be at least 1`) in `usageError`. Add the `list` check for `--json` with
`--quiet` (decision 5).

#### Task 3 — Help text

Capitalise `Short` for `list`, `clean`, `backup`, `restore`. Add `Example:` to each, indented two
spaces per Cobra convention, drawn from the README *Examples* (e.g. `clean -n --empty`,
`backup --keep 7 -y -q`, `restore latest`).

#### Task 4 — Output streams (decision 4)

`confirm.go`: prompt to `env.Stderr`. `restore.go`: the WARNING block, the backup table and
`Restore which backup?` prompt in `restoreSource`, and `Aborted.` to stderr. `clean.go`: `Skipped
…`, `Dry run: nothing deleted.`, `Aborted.` to stderr. `backup.go`: `Backups to remove …` heading
and table, `Aborted.` to stderr. `cmd/copilot-sessions/main.go`: Ctrl-C `
Aborted.` to
`os.Stderr`.

#### Task 5 — Unified `-q` (decision 5)

`clean.go`: under `-q` print only the IDs (after a successful delete, or at once with `-n`);
suppress the summary line, `No sessions match.` and `Deleted …`. `backup.go`: under `-q`
suppress `Nothing to remove …` and `Deleted N old backups.`. Update the `-q` help strings:
`list`: "print session IDs only"; `clean`: "print only the IDs of deleted sessions"; `backup`:
"print only the backup path".

#### Task 6 — Drop `-S` (`cli.go` `addFilters`)

`fl.Var(filter.DateFlag(&f.Since, env.Now), "since", …)` with no shorthand. Grep the repo
(README, spec, tests, completion) for `-S` and fix every hit.

#### Task 7 — Tests (`internal/cli/*_test.go`)

Update existing assertions that read moved messages from stdout to read stderr. Add tests:
bare invocation → exit 0, root help on stdout; `lst` → exit 2, stderr has `Did you mean this?`
and `list`; each of the six flag-combination errors and `list --json -q` → exit 2 with the exact
message; `list -S today` → exit 2; confirmation prompt and restore WARNING on stderr, not stdout;
`clean -q -n --all` → stdout is exactly the selected IDs; `clean -q --all -y` → stdout exactly the
deleted IDs; `backup -q --keep 1 -y` with older backups present → stdout is exactly the path.

#### Task 8 — `TestParity` (`internal/cli/parity_test.go`, decision 8)

Filter `Skipped ` and `Dry run: ` lines from the Python output before comparing `clean -n`;
replace the `-q --json` case with `--json`; replace `-S` with `--since` in the filter list.

#### Task 9 — Spec (`development/spec.md`, decision 7)

§5.2 table: `--since DATE` without `-S`. §6 global: usage errors (exit 2) include flag
combinations; prompts and notes go to stderr (list them); bare invocation prints help, exit 0;
unknown command suggestions. §6.1–§6.4: move the "Validation (exit 1)" lists to exit 2, rewrite
`-q` rows per decision 5, mark which messages go to stderr. §7: capitalised `Short`, examples per
command. §11: replace "Everything else … must match" with the list of intended differences
(decisions 1–6), keeping the archive format and message texts as must-match. §13: parity is
stdout results with the exclusions from decision 8.

#### Pickup amendments (2026-11-09, applicability gate)

- **`list --json -q` loses size-less JSON** (user confirmed reject as planned): delete the
  `--json -q` test (`list_test.go` ~l.135), rewrite spec §6.1 (`size` is `null` under `-q`) and
  §9 case 4, and note in CHANGELOG that `--json` always computes sizes.
- **Existing tests that flip** (Task 7): bare run expecting exit 2 (`list_test.go` ~l.156),
  `-S 1x` (~l.153), exit-1 assertions (`clean_test.go` ~l.84, `backup_test.go` ~l.125),
  prompt/`Aborted.` on stdout (`clean_test.go` ~l.223, `restore_test.go` ~l.117).
- **`backup --keep` rotation listing** (user confirmed): `Backups to remove …` and its table go
  to stderr only when the `[y/N]` prompt is actually shown (no `-y`); never with `-y`.
- Note-and-close: Task 8's `--json` case is rebuilt from `list + f` (args already carry `-q`);
  `Execute` trims the trailing newline Cobra's unknown-command message already carries.

### Acceptance test

From the repo root on the feature branch:

1. `just lint && just test && just test-linux` — all green; `go test ./internal/cli -run
   TestParity -v` shows `--- PASS: TestParity` (not SKIP).
2. CLI probes against an empty home (`just build` first):
   ```sh
   export COPILOT_HOME=$(mktemp -d) COPILOT_BACKUP_DIR=$(mktemp -d)
   c=./copilot-sessions
   $c >/tmp/o 2>/tmp/e; echo $?; grep -q '^Available Commands:' /tmp/o && test ! -s /tmp/e && echo bare-ok
   $c lst 2>&1 >/dev/null | grep -q 'Did you mean this?'; $c lst 2>/dev/null; echo "lst=$?"
   for a in "clean" "clean -a -s closed" "backup --json" "backup --no-create" "backup -l -k 1" "backup -k 0" "list --json -q" "list -S today"; do
     $c $a >/dev/null 2>&1; echo "$a => $?"; done
   $c --help | grep -E '^  (backup|clean|list|restore) ' ; $c clean --help | grep -q '^Examples:' && echo examples-ok
   ```
   Expected: `0` and `bare-ok`; `lst=2`; every listed command `=> 2`; the four `Short` lines
   start with a capital letter; `examples-ok`.
3. Streams and `-q` with data are checked by Task 7's tests (step 1).

### Docs update (mandatory when user-facing)

- `development/spec.md` per Task 9.
- `README.md`: any `-S` usage (none at refinement) and the `-q` description if quoted; add a line
  under *Usage* that `-q` prints only IDs/paths for scripting.
- `CHANGELOG.md` `[Unreleased]` → `### Changed`: "The CLI follows Go/Cobra conventions: a bare
  `copilot-sessions` shows help; flag-combination mistakes exit 2; unknown commands suggest
  alternatives; prompts and notes go to stderr; `-q` prints only IDs or paths (`clean -q` now
  prints the deleted IDs); `list --json -q` is rejected; `--since` has no `-S` shorthand
  (CS-012)."

### Finish (mandatory)

1. Acceptance tests green; `just lint`, `just test`, `just test-linux` clean.
2. Docs updated (spec, README, CHANGELOG).
3. Summary: files touched, decisions honoured, anything deferred.
4. Suggested commits (root-path child, tidied history kept by default), e.g.:
   ```
   feat(cli)!: follow Go/Cobra CLI conventions (CS-012)

   Bare invocation shows help; usage mistakes exit 2; unknown commands get
   suggestions; prompts and notes go to stderr; -q prints only results;
   --since drops -S.

   BREAKING CHANGE: exit codes, stdout content and the -S shorthand change.
   ```
   plus `docs(spec): record CS-012's intended differences (CS-012)`.
5. Tidy WIP commits; commit locally; do not push. Move to `4-in-review/`.

## Review

### Review 1 — 2026-11-09 (branch @ a862e5b)

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| F1 | blocking | correctness | — | Interactive `clean -q` without `-y` asks `Permanently delete these sessions? [y/N]` with no count or scope (before this branch, the summary line printed first). Mandated by CS-012 decision 5; the user signed off on the fix 2026-11-09. | `runIn(fx,nil,"y\n",true,"clean","-q","--all")`: stderr is only the prompt, and 6 sessions are deleted. On main: `6 of 6 sessions selected (all sessions).` before the prompt. `internal/cli/clean.go` `if !quiet {…}` then `confirm` | Under `-q`, when the prompt will show (no `-y`, TTY), print `N of M sessions selected (<scope>).` to stderr, as `backup --keep` does; add a test |
| F2 | non-blocking | stale-xref | noted | The IN REVIEW History line names `f77d2f9`, the tip from before the rebase onto main; the real tip is `a862e5b` | `git log main..feat/CS-012-idiomatic-cli` | Next move records the current tip (History is append-only) |
| F3 | non-blocking | design | noted | Unknown commands are classified as exit 2 by matching Cobra's message prefix `unknown command `, not a typed error | `internal/cli/cli.go` `Execute`. No runtime message starts that way, and `TestExitCodes` (`lst` → 2) fails if Cobra rewords it | Per decision 2; revisit only if Cobra changes |
| F4 | non-blocking | design | fixed inline | The `backup --keep` listing guard `!yes && env.IsTTY()` duplicates `confirm`'s prompt condition; F1 would add a third copy | `internal/cli/backup.go` `applyRetention`, `confirm.go` | One `willPrompt(env, yes)` helper used by `confirm`, `backup` and `clean` (in the rework, no behaviour change) |
| F5 | non-blocking | design | noted | `backup --list` silently accepts `-q`/`-y`, while `list --json -q` is now rejected | `backup --list --json -q` → exit 0 | Behaviour from before this branch; out of scope |
| F6 | non-blocking | other | noted | Picker prompt and `invalid choice` error now share stderr, so the error can land on the prompt's line when the input has no newline | `restore.go` `restoreSource`; TTY only, and typed input echoes a newline | Accept |
| F7 | non-blocking | design | fixed inline | `scope` (incl. `f.Describe()`) is computed under `-q` and never used | `internal/cli/clean.go` | Compute it only where it is printed (in the F1 rework) |
| F8 | non-blocking | design | noted | Quiet output in `clean` is spread over three exit paths (`printIDs` closure) | `internal/cli/clean.go` | Style; tests pin the `-q` contract |
| F9 | non-blocking | test-gap | noted | `TestParity`'s `withoutNotes` drops Python's `Skipped`/`Dry run` lines without comparing them with Go's stderr | `internal/cli/parity_test.go` | Covered by `clean_test.go`'s stderr assertions |

Dispositions: 2 fixed inline (F4, F7), 6 noted (F2, F3, F5, F6, F8, F9), 0 folded, 0 new tickets; 1 blocking (F1).
cost: estimated M, actual M

Checklist:
- [x] Reviewer independence (step 0): the audits (steps 1–4a) were delegated to a fresh reviewer, hunt `angle 7 only`; `pickle doctor` warned of a stale ticket copy, fixed by rebasing onto main, then 0 warnings.
- [x] Correctness hunt (step 3): host tool path (`/code-review medium` on `main...feat/CS-012-idiomatic-cli`), 8 findings → F1, F3–F9 (0 discarded); angle 7 by the delegated reviewer, 1 finding (= F1), 6 candidates discarded, all held up when checked.
- [x] Acceptance re-run by the delegated reviewer: `just lint`, `go test ./...`, `just test-linux`, `TestParity` PASS, CLI probes as expected.
- [x] Consistency (step 4) and docs (step 4a): spec §5.2/§6/§7/§9/§11/§13, README, CHANGELOG match the code.

### Rework fix record — round 1 (commit c17feb7)

- F1 (blocking): `clean.go` prints `N of M sessions selected (<scope>).` to stderr under `-q` when the prompt will show; new `clean_test.go` case `clean -q aaaa1` on a TTY asserts stdout empty, stderr summary + prompt. Spec §6 *Streams* and §6.2 updated.
- F4 (fixed inline): `willPrompt(env, yes)` in `confirm.go`, used by `confirm`, `applyRetention` and `runClean`.
- F7 (fixed inline): `scope` is built inside the `summary` closure, only when a summary prints.
- Acceptance re-run: `just lint`, `just test`, `just test-linux` green; `TestParity` PASS.

## History

- 2026-11-09 — created (TO DO). source: chat: review of CLI options against Go/Cobra conventions found 8 discrepancies; user asked to fix all and update the spec
- 2026-11-09 — TO DO → READY: plan complete
- 2026-11-09 — READY → IN DEVELOPMENT: picked up; applicability gate: 5 non-blocking findings (3 inline amendments, 2 note-and-close)
- 2026-11-09 — plan amended inline: applicability gate — `--json -q` removal consequences (spec/test/CHANGELOG), list of flipping tests, rotation listing on stderr only when prompting; 2 findings noted and closed
- 2026-11-09 — IN DEVELOPMENT → IN REVIEW: acceptance green (lint, test, test-linux, TestParity PASS, CLI probes); branch feat/CS-012-idiomatic-cli @ f77d2f9
- 2026-11-09 — IN REVIEW → REWORK: review 1: 1 blocking (F1, clean -q prompt without context; decision 5 amended with user sign-off)
- 2026-11-09 — plan amended inline: decision 5 — `clean -q` prints its summary line to stderr before an interactive prompt (review 1 F1, user sign-off)
- 2026-11-09 — REWORK → IN REVIEW: findings fixed (F1, F4, F7); branch feat/CS-012-idiomatic-cli @ 1667b53
