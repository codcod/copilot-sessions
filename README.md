# copilot-sessions

`copilot-sessions` lists, cleans, backs up and restores GitHub Copilot CLI sessions. It
reads the session directories, the `session-store.db` index and the UI state under
`COPILOT_HOME` (default `~/.copilot`), and shows each session's status: `ongoing`, `stale`
or `closed`. It is a Go rewrite of a Python script, specified in
[`development/spec.md`](development/spec.md).

## Development

Run the tests on the development machine:

```
go test ./...
```

The binary targets Linux (WSL) and reads `/proc` for process detection, so tests that need
`/proc` skip on macOS. Run the full suite on Linux in Docker:

```
docker run --rm -v "$PWD":/src -w /src \
  -v go-mod:/go/pkg/mod -v go-build:/root/.cache/go-build \
  golang:1.26 go test -count=1 ./...
```

`TestParity` compares `list` against the Python reference script in
`development/copilot-sessions.sh`. It runs only when `python3` is on `PATH`.

See the Platform note in [`AGENTS.md`](AGENTS.md) for what must be validated on Linux.
