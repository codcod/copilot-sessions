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
   stdout for results, stderr for prompts and diagnostics. Decide per message at refinement which
   ones are results (e.g. `Deleted N sessions.`, `No sessions match.`).
7. **`-q/--quiet` means something different per command** (`list`: IDs only; `clean`: no table
   but still counts; `backup`: path only), and `list --json -q` is accepted with `--json` silently
   winning. Idiom: one meaning where possible, and `--json`/`-q` mutually exclusive on `list`.
8. **`-s` (`--status`) and `-S` (`--since`)** differ only by case. Idiom: drop one short form
   (refinement picks which, and checks shell completion and README/spec references).

Also in scope: `development/spec.md` (§6 per-command validation and exit codes, §7 help text,
§11 intended differences — one entry per changed behaviour), the README if a flag or example
changes, CHANGELOG (`### Changed`, breaking for scripts relying on exit 1 or `-S`/`-s`), and the
tests. `TestParity` compares `list`, `list --json -q`, `clean -n` and `backup --list` with the
Python script in `development/copilot-sessions.sh`; moving messages to stderr or changing `-q`
can break it. Refinement decides whether parity narrows to stdout results, gains documented
exclusions, or retires.

Soft couplings: CS-009/CS-010 add platform code under the same commands but no flags; whichever
lands second rebases. No release has been tagged yet, so changing exit codes and short flags now
breaks no published version.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: chat: review of CLI options against Go/Cobra conventions found 8 discrepancies; user asked to fix all and update the spec
