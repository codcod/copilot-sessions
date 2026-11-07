---
id: CS-005
title: Shell completion, install and cutover from the Python script
project: copilot-sessions
depends-on: [CS-001, CS-002, CS-003, CS-004, CS-006]
spawned-by: []
impact: medium
complexity: low
cost: S
---

# CS-005 — Shell completion, install and cutover from the Python script

## Outcome

After this ships, the Go binary is installed at `~/.local/bin/copilot-sessions` in place of
the Python script, and zsh completion comes from `copilot-sessions completion zsh`. The
README explains both.

## Description

Implements `development/spec.md` §8 and the §13 acceptance cutover.

- **Dynamic completions (§8).**
  - Positional IDs for `list` and `clean`, computed in-process without sizes, with the
    description `<status> <name or '-'>`.
  - `--status` with comma lists and `NoSpace`.
  - `--until` and `--since` with suggestions and descriptions; free text stays allowed.
  - `--cwd` completes directories.
  - `--grep` and `--keep` get no completion.
  - The `restore` argument completes `latest`, then the backup paths newest first, then
    `*.tar.gz` files.
  - The `backup` FILE completes `tar.gz` files.
  - Bash and fish come from cobra; do not test them.
- **Install and cutover.** `just install` (CS-006) puts the binary at
  `~/.local/bin/copilot-sessions`. `just completion` regenerates the completion file
  `~/.zsh/completions/_copilot-sessions` from the binary.
- **README.** An install section that also covers generating the zsh completion (§13).
- **Parity check (§13).** `list`, `list --json -q`, `clean -n` with each filter, and
  `backup --list` give the same output as the Python script on the same data. Do this check
  before the Python script is replaced.

Soft couplings: needs the commands from CS-001 to CS-004 in place. The restore completion
uses CS-003's backup listing, and the parity check covers every command.

## Implementation Plan

### 0. Feature branch (mandatory)

Before any change, create the feature branch. The child is this repository (`path = "."`):

```
git checkout main && git pull --ff-only
git checkout -b feat/CS-005-completion-and-cutover
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

- CS-001, CS-002, CS-003, CS-004 and CS-006 are all in `6-done/` and merged to `main`;
  check each one's History for the `merged to main` line. If any is not merged, stop and ask
  the human.
- `justfile` has the `install` and `completion` recipes (CS-006).
- The human has a WSL shell with the real `~/.copilot` and the Python script at
  `~/.local/bin/copilot-sessions`. Task 4 runs there and needs the human present.

### Confirmed design decisions (do not deviate without asking)

1. **Completions are cobra's dynamic completion functions only;** no hand-written script.
   They are registered with `ValidArgsFunction` and `RegisterFlagCompletionFunc` in a new
   `internal/cli/complete.go`. They are tested through cobra's hidden `__complete` command
   via `Execute`, which tests the Go side for all shells at once. Bash and fish scripts are
   not tested (spec §8).
2. **ID completion** reads `session.Load(env.Home, env.AncestorPIDs(), false)` (no sizes),
   sorted with `session.Sort`. Each entry is `<id>\t<status> <name or "-">`, with
   `ShellCompDirectiveNoFileComp`. IDs already on the command line are not filtered out;
   the script does not filter them either.
3. **`--status` completion** splits `toComplete` at its last comma. The part before the comma
   is the prefix of statuses already chosen. Offer `prefix + s` for each status in
   `filter.Statuses` not already in the prefix (`closed,` → `closed,ongoing`,
   `closed,stale`), with `NoSpace|NoFileComp`.
4. **`--until`/`--since`** offer `today`, `yesterday`, `7d`, `2w`, `1m` and `3m`, with the
   descriptions `today`, `yesterday`, `7 days ago`, `2 weeks ago`, `1 month ago` and
   `3 months ago`, and `NoFileComp`. Free text stays valid because the flag parser accepts
   it. `--cwd` returns `FilterDirs`. `--grep` and `--keep` return `NoFileComp`.
5. **The `restore` argument is completed in-process,** because cobra cannot mix fixed values
   with file completion in one directive. It offers `latest`, then `backup.List(BackupDir)`
   (newest first), then `filepath.Glob(toComplete + "*.tar.gz")` entries not already
   offered, with `NoFileComp`. A second argument gets nothing. The `backup` FILE uses
   `[]string{"tar.gz"}` with `ShellCompDirectiveFilterFileExt`.
6. **Install is `just install`; the completion is `just completion`.** Both come from
   CS-006, and neither runs in tests.
7. **§13 output parity is already automated.** `TestParity` covers `list`, `list --json -q`
   (CS-001), `clean -n` per filter (CS-002) and `backup --list` (CS-003) on the fixture. The
   cutover adds a one-time manual check on the real WSL data before the Python script is
   overwritten. The result is recorded as a dated History line on this ticket.

### Tasks

#### Task 1 — completion functions

Create `internal/cli/complete.go` with one `registerCompletions(root *cobra.Command, env Env)`
called from `Execute` after all commands are added. It wires the functions from decisions
2–5 onto `list`, `clean`, `backup` and `restore`; the shared filter flags are on `list` and
`clean`. If calling it from `Execute` turns out awkward, have `addFilters` register the
filter-flag functions itself.

#### Task 2 — completion tests

Create `internal/cli/complete_test.go`. Run `run(fx, nil, "__complete", …)` and parse the
output: candidate lines, then `:<directive>`. Cases:

- `list ""` gives all six fixture IDs, each with a `\t<status> <name or ->` description.
  The `EmptyNoEvents` entry ends in ` -`.
- `clean --status closed,` gives exactly `closed,ongoing` and `closed,stale`, and the
  directive includes `ShellCompDirectiveNoSpace`.
- `list --until ""` gives six entries with descriptions; `--cwd ""` gives `FilterDirs`;
  `--grep ""` and `backup --keep ""` give `NoFileComp`.
- `restore ""` lists `latest` first, then the `fx.BackupDir` backups newest first. A
  `x.tar.gz` in the test's working dir (`t.Chdir`) comes after them.
- `backup ""` gives `tar.gz` with `FilterFileExt`.

#### Task 3 — README

Add an `## Install` section to `README.md` before `## Development`:

- `just install` puts the binary at `~/.local/bin/copilot-sessions`, replacing the Python
  script.
- `just completion` writes `~/.zsh/completions/_copilot-sessions`. `~/.zshrc` needs
  `fpath=(~/.zsh/completions $fpath)` before `compinit`; after that, `exec zsh`.
- Add a short command overview, one line each for `list`, `clean`, `backup` and `restore`,
  and point at `copilot-sessions --help` and `development/spec.md`.
- Fix the `## Development` sentence that says TestParity compares only `list`: it covers
  `list`, `list --json -q`, `clean -n` per filter and `backup --list`.

#### Task 4 — cutover on WSL (human present)

The branch is not pushed before review, so build the binary on the Mac and copy it over:

1. On the Mac, on the feature branch, run `GOOS=linux GOARCH=amd64 just build`. Copy
   `./copilot-sessions` to WSL as `~/cs-new`.
2. Parity on WSL: for `list`, `list --json -q`, `backup --list` and `clean -n` with each of
   `--until 1m`, `--since 7d`, `--status stale`, `--cwd ~`, `--grep a` and `--empty`, diff
   `~/cs-new …` against `~/.local/bin/copilot-sessions …`. Sizes and times may differ only
   where data changed between runs (spec §13). Compare `--json` by parsed instant, as
   `normJSON` does.
3. Only if step 2 is clean, run what `just install completion` would:
   `install -m 0755 ~/cs-new ~/.local/bin/copilot-sessions && copilot-sessions completion zsh > ~/.zsh/completions/_copilot-sessions && exec zsh`.
   Then check that `copilot-sessions --version` prints the git version, and that
   `copilot-sessions clean <TAB>` and `copilot-sessions restore <TAB>` complete. Once the
   repo is on WSL after the merge, `just install completion` is the documented path.
4. Record a History line:
   `cutover on WSL: parity clean on <n> sessions; Python script replaced`, or name the
   diffs and stop.

The Python script remains in the repo as `development/copilot-sessions.sh`, so nothing is
lost by overwriting `~/.local/bin/copilot-sessions`.

### Acceptance test

```
gofmt -l . | wc -l       # 0
go vet ./...
go test -count=1 ./...   # completion tests and TestParity pass on macOS
just test-linux
go test -count=1 -v -run TestParity ./internal/cli   # must show RUN/PASS, not SKIP (needs python3 ≥ 3.12; the Docker image has none)
just build && ./copilot-sessions __complete clean --status closed, | tail -1   # ":6" (NoSpace|NoFileComp)
./copilot-sessions completion zsh | head -1                                   # "#compdef copilot-sessions"
```

Plus Task 4 completed on WSL, with its History line recorded.

### Docs update (mandatory when user-facing)

`README.md` `## Install` section (Task 3). This satisfies the spec §13 README criterion. No
changelog.

### Finish (mandatory)

1. Acceptance test green on macOS and Linux (Docker); WSL cutover recorded.
2. README updated.
3. Write a summary: files touched, decisions made, anything deferred.
4. Suggested commit message:

   ```
   feat(cli): dynamic shell completion and install docs (CS-005)

   Completes session IDs, --status comma lists, dates, --cwd and backup files
   for list, clean, backup and restore through cobra, and documents install
   and zsh completion in the README.
   ```

5. Tidy the WIP commits into atomic ones (root-path child).
6. Commit locally; do not push. `pickle ticket move CS-005 in-review --reason "acceptance green"`.

## Review

### Review — round 1 (2026-11-07)

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| F1 | blocking | correctness | — | `restore` argument completion drops the typed directory prefix: `filepath.Glob` cleans `./m*.tar.gz` to `my.tar.gz`, which bash/zsh then filter out against `./m`; a `~/` prefix is not expanded, so nothing under home completes | reproduced: `__complete restore ./m` in a dir holding `my.tar.gz` → `latest`, `my.tar.gz`, `:4`; `__complete restore '~/'` → only `latest`. `internal/cli/complete.go:100` | split `toComplete` into dir + base, glob in the expanded dir, return `dir + name` so the typed prefix is kept verbatim; add a test case |
| F2 | non-blocking | design | noted | `restore` completion does not offer directories, so a tarball in a subdirectory completes only once the directory is typed | `__complete restore s` → `latest`; `__complete restore sub/` → `sub/inner.tar.gz`. A direct consequence of CS-005 decision 5 (`NoFileComp` + in-process glob) | offer `toComplete*` directories with a trailing `/` — a change to decision 5, needs sign-off |
| F3 | non-blocking | test-gap | noted | no test asserts the ID order (`session.Sort`, decision 2), `--since`, `clean` IDs or `list --status`; `RegisterFlagCompletionFunc` errors are discarded, so a renamed flag would lose completion silently | `internal/cli/complete_test.go`; `internal/cli/complete.go:35-43,51` | assert order and `--since`, or panic on a registration error |
| F4 | non-blocking | design | noted | `--status` completion is case-sensitive while `ParseStatuses` is not: `Closed,<TAB>` offers `Closed,closed`, which the parser accepts as a duplicate | `internal/cli/complete.go:80-88`; `internal/filter/filter_test.go:55` | lowercase + trim the chosen parts |
| F5 | non-blocking | spec-unclear | noted | the cutover record diverges from Task 4's wording: no session count, and the binary came in as a `.deb` in `/usr/bin` rather than `~/.local/bin`; a later `just install` on that host would shadow the apt copy | History (cutover lines); `README.md:12` | the `.deb` was a one-off test channel, not a release path; a release-automation ticket would own README apt docs |

Disposition summary: 1 blocking (F1 → rework); 4 noted (F2, F3, F4, F5); 0 fixed inline, 0 folded, 0 new tickets.
cost: estimated S, actual S

- [x] Reviewer independence settled (step 0): delegated — the orchestrator wrote the branch, so an independent sub-agent ran steps 1–4a; no delegated finding discarded (F1 re-verified by hand)
- [x] In-tree stale-branch check (step 0a): `pickle doctor` 0 errors, 0 warnings (after rebasing the branch onto main)
- [x] Implementation audit — acceptance test re-run green (gofmt, vet, `go test` macOS, `just test-linux`, TestParity RUN/PASS, `:6`, `#compdef`); Tasks 1–3 met, Task 4 done by the user on WSL (F5)
- [x] Correctness hunt (step 3): angle hunt, one pass over angles 1–8 incl. 7, run by the delegated reviewer, no further sub-agents; F1/F2 reproduced via `__complete`; reviewer's own discard: newlines in names (cobra keeps first line only)
- [x] Consistency audit (step 4)
- [x] Documentation audit (step 4a): README covers install + completion + commands; no docs build, no changelog by design
- [x] Docs-readability pass (step 4b): skipped — conscious skip, README change is short and was read in the consistency audit
- [x] Findings recorded with severity, class and disposition; summary and cost lines present (step 5)
- [x] Ticket moved to `tickets/5-rework/` (step 6a)
- [ ] Other references / governing documents (step 7) — at conclusion
- [ ] Impact sweep (step 8) — at conclusion
- [ ] Summary + commit message & MR for approval (step 9) — at conclusion

### Rework fix record — round 1 (commit 1d874a1)

- **F1** — `completeRestore` splits `toComplete` into dir + base, globs `base*.tar.gz` in `filter.ExpandUser(dir)`, and returns `dir + name`, so `./`, `~/` and `sub/` prefixes come back as typed (`internal/cli/complete.go`). `TestCompleteRestore` gains `./x` → `./x.tar.gz`, `~/` → `~/h.tar.gz` (HOME pointed at a temp dir) and `sub/` → `sub/s.tar.gz`; the first two fail on the pre-fix code. Acceptance re-run green on macOS and Linux (Docker), TestParity RUN/PASS.

### Review — round 2, scoped re-review of 1d874a1 (2026-11-07)

| id | severity | class | disposition | description | evidence | suggestion |
|---|---|---|---|---|---|---|
| R2-F1 | blocking | correctness | — | the `~/` half of F1 is not closed: cobra's zsh (and bash) script `eval`s `__complete` with the words unquoted, so `~` is expanded before the binary sees it; `completeRestore` returns `$HOME/h.tar.gz`, which `_describe` drops against the literal `~/h`. The `ExpandUser(dir)` branch is unreachable from a shell, and the `"~/"` test case feeds input no shell sends | reproduced in a real `zsh -f` + compinit via zpty: debug log `eval copilot-sessions __complete restore ~/h` → `<abs>/home/h.tar.gz` → `_describe did not find completions`; generated script line `requestComp="${words[1]} __complete ${words[2,-1]}"`. `./m` and `sub/` do work. `internal/cli/complete.go:101-110`, `internal/cli/complete_test.go:89-99` | for a `toComplete` containing `/`, return `tar.gz` + `FilterFileExt` (shell-native file completion handles `~/`, `./`, dirs; also covers F2 for slash paths) — changes decision 5, needs sign-off; or narrow F1 to `./`, record `~/` as a cobra limitation, and drop the dead branch and test |
| R2-F2 | non-blocking | design | noted | the typed directory goes into `filepath.Glob` unescaped, so `[`, `*`, `?` in a directory name mismatch; `ErrBadPattern` is ignored | `internal/cli/complete.go:104` | disappears with the `FilterFileExt` route |
| R2-F3 | non-blocking | test-gap | noted | new test setup ignores `os.WriteFile`/`os.Mkdir` errors | `internal/cli/complete_test.go:91-94` | `t.Fatal` on setup errors |

Disposition summary: 1 blocking (R2-F1 → rework round 2); 2 noted (R2-F2, R2-F3).
Reviewer independence: delegated (fresh sub-agent, scoped to the fix diff); R2-F1 re-verified by hand against the generated zsh script; no delegated finding discarded.

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md §8, §13 (Go rewrite of copilot-sessions)
- 2026-11-07 — TO DO → READY: plan complete
- 2026-11-07 — READY → IN DEVELOPMENT: picked up; applicability gate: no blocking findings
- 2026-11-07 — plan amended inline: acceptance test now requires `go test -v -run TestParity` to run (not skip) on macOS, since the Docker image has no python3; Task 3 also fixes the stale README sentence on TestParity scope (applicability gate findings 1, 3; findings 2, 4, 5 noted and closed)
- 2026-11-07 — plan amended inline: Task 4 delivered the binary as a `.deb` (0.0.0~cs005.d72b817, amd64) pushed to Cloudsmith `codcod/copilot-sessions` (any-distro/any-version, built from a throwaway goreleaser config, nothing committed) and installed with `apt install copilot-sessions` into `/usr/bin`; parity compared `~/.local/bin` (Python) against `/usr/bin`, and the cutover removed the Python script from `~/.local/bin` instead of overwriting it
- 2026-11-07 — cutover on WSL: parity clean (user-reported; session count not given); Python script replaced
- 2026-11-07 — IN DEVELOPMENT → IN REVIEW: acceptance green
- 2026-11-07 — IN REVIEW → REWORK: review round 1: 1 blocking (F1); 4 noted (F2–F5)
- 2026-11-07 — REWORK → IN REVIEW: findings fixed
- 2026-11-07 — IN REVIEW → REWORK: re-review round 2: 1 blocking (R2-F1); 2 noted
