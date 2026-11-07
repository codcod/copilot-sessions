---
id: CS-006
title: justfile for build, test, lint and install
project: copilot-sessions
depends-on: [CS-001]
spawned-by: []
impact: medium
complexity: low
cost: S
---

# CS-006 — justfile for build, test, lint and install

## Outcome

After this ships, a developer can run `just build`, `just test`, `just lint` and
`just install` in this repo to build, check and install `copilot-sessions` without
remembering any flags. A bare `just` lists the recipes.

## Description

Add a `justfile` modelled on the template at
<https://github.com/codcod/jerry/blob/main/justfile>, read on 2026-11-06. It covers the
`build`, `test` and `install` targets from `development/spec.md` §2, which lets
`justfile` win the spec's "`Makefile` or `justfile`" choice.

Keep these from the template, adapted:

- The `version` variable (`git describe --tags --always --dirty`, falling back to `dev`)
  and `bin := "copilot-sessions"`.
- The default `@_` recipe, which runs `just --list`.
- The `[group(...)]` attributes and a one-line doc comment on every recipe.
- **build**: `go build -ldflags "-X main.Version={{version}}" -o {{bin}} <main package>`
  with `CGO_ENABLED=0`, because spec §13 requires a CGO-free build. The main-package path
  follows the package layout CS-001 settles on, for example `./cmd/copilot-sessions`.
  `main.Version` should feed cobra's `Version` field so that `--version` works.
- **test**: `go test ./...`
- **lint**: the `gofmt -l` drift check plus `go vet ./...`. Spec §13 requires `go vet`.
- **fmt-check** and **fmt**: copied from the template unchanged.
- **clean**: removes `{{bin}}`.

Add these, which this project needs:

- **install** (group `lifecycle`): depends on `build` and installs the binary to
  `~/.local/bin/copilot-sessions` (spec §2), replacing the Python script there.
- **completion** (group `lifecycle`): runs
  `{{bin}} completion zsh > ~/.zsh/completions/_copilot-sessions` (spec §8).
- **show-version** (group `release`): kept from the template; prints `./{{bin}} --version`.
- **test-linux** (group `qa`): runs `go test ./...` in a container so the tests that need
  `/proc` run from macOS (see the `AGENTS.md` Platform note).
  - The image is `golang:<go directive>`, which is `golang:1.26` as of CS-001, because
    `modernc.org/sqlite` v1.61.1 needs Go 1.26. Keep the image in step with `go.mod`.
  - Mount the repo at `/src`. Use the named volumes `go-mod` (for `/go/pkg/mod`) and
    `go-build` (for `/root/.cache/go-build`) so later runs are fast.
  - The reference script is in the repo at `development/copilot-sessions.sh`, so the
    repo mount covers it. The Python-backed tests (CS-001's parity test and spec §9
    case 12) run only if the image has `python3`; otherwise they skip, and they still run
    natively on macOS.
  - Docker covers the Linux code paths but not real Copilot data. The real-data check in
    CS-005 still runs on WSL.

Leave these template recipes out, since this project has no matching need: `test-update`
(no golden fixtures planned), `dist-check` and `dist-snapshot` (no goreleaser), and
`docs-check` and `docs-build` (no AsciiDoc manual).

Soft couplings:

- The `build` path and `main.Version` assume the Go module and main package from CS-001.
  The recipes can be written first, but `just build` only goes green once CS-001 lands.
- CS-001's Description lists "a `Makefile` or `justfile`" in its scaffold scope. That item
  now belongs to this ticket, and CS-001 only needs to provide the `main.Version` hook.
- CS-005 uses `just install` and `just completion` for the cutover, and its README section
  should mention them.

## Implementation Plan

### 0. Feature branch (mandatory)

Before any change, create the feature branch. The child is this repository (`path = "."`):

```
git checkout main && git pull --ff-only
git checkout -b feat/CS-006-justfile
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

- CS-001 is in `6-done/` and merged to `main` (PR #1, `533c9c0`). Holds as of 2026-11-07.
- `just` (1.59 or later, for `[group]`) and Docker are installed on the dev machine.
- The working tree is clean.

### Confirmed design decisions (do not deviate without asking)

1. **The justfile is the spec §2 build file; there is no Makefile.** CS-005 installs with
   `just install` and `just completion`.
2. **The build has no CGO.** `CGO_ENABLED=0` is set on the `build` recipe line (spec §13).
3. **The main package is `./cmd/copilot-sessions`, and `-X main.Version` feeds cobra's
   `--version`.** Both exist on `main` (`cmd/copilot-sessions/main.go`). `--version` prints
   `copilot-sessions version <v>`.
4. **The `test-linux` image tag comes from `go.mod`.** It is the `go` directive's
   major.minor, read with `awk`, so the image keeps step with `go.mod` without a hand edit
   (`go 1.26.0` → `golang:1.26`).
5. **`install` and `completion` write under `~`, through the shell's `$HOME`.** Acceptance
   runs them with a throwaway `HOME`, so a test never touches the real `~/.local/bin`.
6. **The template recipes `test-update`, `dist-check`, `dist-snapshot`, `docs-check` and
   `docs-build` are left out**, as the Description says. `clean` removes only `{{bin}}`,
   since there is no `dist/`.

### Tasks

#### Task 1 — `justfile`

Create `justfile` at the repo root with exactly these recipes; the doc comment and group of
each are as shown:

```just
version    := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
bin        := "copilot-sessions"
go_version := `awk '/^go /{split($2, v, "."); print v[1] "." v[2]}' go.mod`

# List recipes
@_:
    just --list

# Build the binary (no CGO)
[group('build')]
build:
    CGO_ENABLED=0 go build -ldflags "-X main.Version={{version}}" -o {{bin}} ./cmd/copilot-sessions

# Run tests
[group('qa')]
test:
    go test ./...

# Run tests on Linux in Docker, so the /proc-dependent tests run from macOS
[group('qa')]
test-linux:
    docker run --rm -v "$PWD":/src -w /src \
      -v go-mod:/go/pkg/mod -v go-build:/root/.cache/go-build \
      golang:{{go_version}} go test -count=1 ./...

# Static checks: formatting + go vet
[group('qa')]
lint:
    @unformatted="$(gofmt -l .)"; \
      if [ -n "$unformatted" ]; then echo "gofmt drift:"; echo "$unformatted"; exit 1; fi
    go vet ./...

# Check formatting only
[group('qa')]
fmt-check:
    @unformatted="$(gofmt -l .)"; \
      if [ -n "$unformatted" ]; then echo "gofmt drift:"; echo "$unformatted"; exit 1; fi

# Format in place
[group('qa')]
fmt:
    gofmt -w .

# Print the version the binary reports
[group('release')]
show-version: build
    ./{{bin}} --version

# Install the binary to ~/.local/bin (replaces the Python script)
[group('lifecycle')]
install: build
    mkdir -p ~/.local/bin
    install -m 0755 {{bin}} ~/.local/bin/{{bin}}

# Regenerate the zsh completion in ~/.zsh/completions
[group('lifecycle')]
completion: build
    mkdir -p ~/.zsh/completions
    ./{{bin}} completion zsh > ~/.zsh/completions/_{{bin}}

# Remove build output
[group('lifecycle')]
clean:
    rm -f {{bin}}

# vim: set ft=make:et:ai
```

`/copilot-sessions` is already in `.gitignore`, so it needs no change.

#### Task 2 — README

In `README.md` § Development, replace the raw `docker run …` block with `just test` and
`just test-linux`. Add one line saying that a bare `just` lists the recipes, and one naming
`just build`, `just lint` and `just install`. Leave the install and completion walkthrough to
CS-005.

### Acceptance test

Run from the repo root on macOS, with Docker running:

```
just | grep -E 'build|test-linux|install|completion|show-version'   # all five listed
just lint                                    # exit 0
just test                                    # exit 0
just test-linux                              # exit 0; the ongoing/ancestor tests run (not skipped)
just show-version                            # prints "copilot-sessions version <git describe output>"
go version -m copilot-sessions | grep 'CGO_ENABLED=0'
H=$(mktemp -d); HOME=$H just install completion
test -x $H/.local/bin/copilot-sessions && grep -q '#compdef copilot-sessions' $H/.zsh/completions/_copilot-sessions
just clean && test ! -e copilot-sessions
```

### Docs update (mandatory when user-facing)

`README.md` § Development, as in Task 2. The project keeps no changelog.

### Finish (mandatory)

1. Acceptance test green; `just lint` and `just test` clean.
2. README updated.
3. Write a summary: files touched, decisions made, anything deferred.
4. Suggested commit message:

   ```
   build: add justfile for build, test, lint and install (CS-006)

   Recipes for a CGO-free build with the version stamped in, tests on macOS and in
   Docker (image tag read from go.mod), lint, install to ~/.local/bin and zsh
   completion generation.
   ```

5. Tidy the WIP commits into atomic ones (root-path child).
6. Commit locally; do not push. `pickle ticket move CS-006 in-review --reason "acceptance green"`.

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-06 — created (TO DO). source: chat: user asked for a justfile modelled on codcod/jerry's justfile template
- 2026-11-07 — TO DO → READY: plan complete
