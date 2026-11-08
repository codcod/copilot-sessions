# copilot-sessions

`copilot-sessions` lists, cleans, backs up and restores GitHub Copilot CLI sessions. It
reads the session directories, the `session-store.db` index and the UI state under
`COPILOT_HOME` (default `~/.copilot`), and shows each session's status: `ongoing`, `stale`
or `closed`. It is a Go rewrite of a Python script, specified in
[`development/spec.md`](development/spec.md).

## Install

From a release: download `copilot-sessions_<ver>_linux_<arch>.tar.gz` (`amd64` or `arm64`)
and `checksums.txt` from [GitHub Releases](https://github.com/codcod/copilot-sessions/releases),
then verify and unpack into `~/.local/bin`:

```
sha256sum --ignore-missing -c checksums.txt
tar xzf copilot-sessions_<ver>_linux_<arch>.tar.gz -C ~/.local/bin copilot-sessions
```

Or with Go: `go install github.com/codcod/copilot-sessions/cmd/copilot-sessions@latest`.

From a checkout:

```
just install      # builds and installs ~/.local/bin/copilot-sessions (replaces the Python script)
just completion   # writes the zsh completion to ~/.zsh/completions/_copilot-sessions
```

For zsh to find the completion, `~/.zshrc` needs `fpath=(~/.zsh/completions $fpath)` before
`compinit`; then run `exec zsh`. Completion offers session IDs (with status and name),
`--status` values, dates, directories and backup files. Bash and fish scripts come from
`copilot-sessions completion bash|fish`.

Commands:

- `list` shows sessions and their status, with filters, `--json` and `-q`.
- `clean` deletes sessions by ID or filter (`-n` for a dry run).
- `backup` creates, lists (`--list`) and rotates (`--keep N`) backups.
- `restore` replaces the current sessions with a backup file or `latest`.

See `copilot-sessions --help` and [`development/spec.md`](development/spec.md) for the details.

## Development

Recipes live in the `justfile`; a bare `just` lists them. `just build`, `just lint` and
`just install` build the binary (no CGO), run `gofmt` + `go vet`, and install it to
`~/.local/bin`.

Run the tests on the development machine:

```
just test
```

The binary targets Linux (WSL) and reads `/proc` for process detection, so tests that need
`/proc` skip on macOS. Run the full suite on Linux in Docker (the `golang` image tag follows
`go.mod`):

```
just test-linux
```

`TestParity` compares `list`, `list --json -q`, `clean -n` with each filter and
`backup --list` against the Python reference script in `development/copilot-sessions.sh`.
It runs only when `python3` 3.12 or newer is on `PATH`, so it skips in `just test-linux`.

See the Platform note in [`AGENTS.md`](AGENTS.md) for what must be validated on Linux.

Releases are tag-driven; see [`RELEASING.md`](RELEASING.md).
