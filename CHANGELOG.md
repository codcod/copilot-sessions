# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `list` shows sessions with their `ongoing`/`stale`/`closed` status, with filters, `--json`
  and `-q` (CS-001).
- `clean` deletes sessions by ID or filter, with their `session-store.db` rows and UI state;
  `-n` for a dry run (CS-002).
- `backup` creates, lists (`--list`) and rotates (`--keep N`) backups (CS-003).
- `restore` replaces the current sessions with a backup file or `latest`, extracting safely
  and reading backups made by the Python script (CS-004).
- Shell completion for zsh, bash and fish (CS-005).
- Backups are written atomically, tolerate files that change while being archived, and report
  errors (CS-007).
- Release builds: a tag-driven GitHub release with Linux `amd64`/`arm64` tarballs and
  `checksums.txt`, `go install …@vX.Y.Z` reporting its module version, and CI on every push and
  PR (CS-008).
- `.deb` packages for `amd64` and `arm64`, with shell completions, attached to each release
  and published to the Cloudsmith apt repository `codcod/copilot-sessions` (CS-011).

### Changed

- The CLI follows Go/Cobra conventions. A bare `copilot-sessions` shows help, flag-combination
  mistakes exit 2 (was 1), and unknown commands suggest alternatives. Prompts and notes go to
  stderr. `-q` prints only IDs or paths: `clean -q` now prints the deleted IDs. `list --json -q`
  is rejected, so `--json` always computes sizes. `--since` has no `-S` shorthand. Breaking for
  scripts that rely on exit code 1, on `-S`, or on prompts and notes appearing on stdout (CS-012).
- Built with Go 1.27 and current dependencies. Error messages quote values Go-style (`"…"`).
  Permission errors on the Copilot home, its DB or a backup file are reported instead of read as
  "no sessions" / "backup not found". `backup --list --json` `created` is RFC 3339 with a numeric
  local offset. Ctrl-C aborts prompts immediately and is honoured before any irreversible step; a
  second Ctrl-C kills at once. `/dev/null` on stdin no longer counts as a terminal, so an
  unattended `clean`/`restore` without `-y` fails instead of aborting silently. The backup
  directory is created with mode 0700. Breaking for scripts that parse error text or `created`,
  or that relied on `</dev/null` exiting 0 (CS-013).

[Unreleased]: https://github.com/codcod/copilot-sessions/commits/main
