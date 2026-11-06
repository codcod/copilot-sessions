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

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-06 — created (TO DO). source: chat: user asked for a justfile modelled on codcod/jerry's justfile template
