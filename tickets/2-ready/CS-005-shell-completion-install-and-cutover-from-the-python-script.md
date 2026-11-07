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

<!-- empty until IN REVIEW -->

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md §8, §13 (Go rewrite of copilot-sessions)
- 2026-11-07 — TO DO → READY: plan complete
