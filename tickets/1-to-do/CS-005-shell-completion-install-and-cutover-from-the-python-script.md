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
- **Install and cutover.** `make install` (or the `just` equivalent) puts the binary at
  `~/.local/bin/copilot-sessions`. The completion file
  `~/.zsh/completions/_copilot-sessions` is regenerated from the binary.
- **README.** An install section that also covers generating the zsh completion (§13).
- **Parity check (§13).** `list`, `list --json -q`, `clean -n` with each filter, and
  `backup --list` give the same output as the Python script on the same data. Do this check
  before the Python script is replaced.

Soft couplings: needs the commands from CS-001 to CS-004 in place. The restore completion
uses CS-003's backup listing, and the parity check covers every command.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-06 — created (TO DO). source: chat: split from development/spec.md §8, §13 (Go rewrite of copilot-sessions)
