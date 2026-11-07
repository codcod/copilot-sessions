# copilot-sessions

`copilot-sessions` lists, cleans, backs up and restores GitHub Copilot CLI sessions. It
reads the session directories, the `session-store.db` index and the UI state under
`COPILOT_HOME` (default `~/.copilot`), and shows each session's status: `ongoing`, `stale`
or `closed`. It is a Go rewrite of a Python script, specified in
[`development/spec.md`](development/spec.md).

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

`TestParity` compares `list` against the Python reference script in
`development/copilot-sessions.sh`. It runs only when `python3` is on `PATH`.

See the Platform note in [`AGENTS.md`](AGENTS.md) for what must be validated on Linux.
