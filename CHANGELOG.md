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

[Unreleased]: https://github.com/codcod/copilot-sessions/commits/main
