---
id: CS-008
title: Release process: tag-driven goreleaser builds, CI and changelog
project: copilot-sessions
depends-on: []
spawned-by: []
impact: medium
complexity: low
cost: S
---

# CS-008 — Release process: tag-driven goreleaser builds, CI and changelog

## Outcome

After this ships, pushing a `vX.Y.Z` tag publishes a GitHub release with Linux
`amd64`/`arm64` tarballs and `checksums.txt`, so a WSL machine installs a versioned
`copilot-sessions` by download (or `go install …@vX.Y.Z`) instead of building from a
checkout. Every push and PR also runs the full test suite on Linux, `/proc` tests included.

## Description

Modelled on pickle's release process (`~/Projects/codcod/pickle`: `RELEASING.md`,
`.goreleaser.yaml`, `.github/workflows/{ci,release}.yml`, `CHANGELOG.md`), cut down to what a
single-user, Linux-only tool needs. Today this project has no CI, no tags, no changelog; the
only install path is `just install` from a checkout.

**Adopt from pickle**

- **goreleaser v2, tag-driven.** `.goreleaser.yaml` with `CGO_ENABLED=0`, `goos: [linux]`,
  `goarch: [amd64, arm64]`, `ldflags: -s -w -X main.Version={{.Version}}` (note: capital
  `Version`, `cmd/copilot-sessions/main.go:13`), `main: ./cmd/copilot-sessions`, tar.gz
  archives named `copilot-sessions_{{.Version}}_linux_{{.Arch}}`, `checksums.txt`,
  `prerelease: auto`, `mode: replace`, `release.github` pinned to `codcod/copilot-sessions`,
  changelog excluding `docs:`/`test:`/`chore:`, `before.hooks: go test ./...`.
- **`release.yml`** on `push: tags: v*` plus `workflow_dispatch` with a `tag` input (re-run
  an existing tag); `permissions: contents: write`; `concurrency` with
  `cancel-in-progress: false`; checkout `fetch-depth: 0`; setup-go from `go.mod`;
  `goreleaser/goreleaser-action@v7.3.0` (latest, 2026-10-12) with `version: '~> v2'`; only
  `GITHUB_TOKEN`, no extra secrets.
- **`ci.yml`** on push to `main` and PRs: vet, gofmt, `go test ./...`, build, plus a
  `goreleaser check` job on the same `goreleaser-action@v7.3.0` with `install-only: true` (exit 0 only — no deprecated `brews` pipe here, so no `|| [ $? -eq 2 ]`). The ubuntu runner runs the `/proc` tests natively, which is the
  CI-side counterpart of `just test-linux`.
- **`CHANGELOG.md`** in Keep a Changelog form with `[Unreleased]`, retitled at release time
  and committed before the tag, so the tag contains it.
- **`RELEASING.md`** — short: cut a release (changelog → tag → push), re-run via dispatch,
  validate locally.
- **justfile `release` group:** `dist-check` (`goreleaser check`) and `dist-snapshot`
  (`goreleaser release --snapshot --clean`), with the `env -u GITLAB_TOKEN …` guard pickle uses.
- **`go install` fallback.** The repo is public, so `go install
  github.com/codcod/copilot-sessions/cmd/copilot-sessions@vX.Y.Z` works; without ldflags
  `Version` stays `dev`, so fall back to `runtime/debug.ReadBuildInfo()`'s module version
  as pickle does.

**Drop from pickle (and why)**

- darwin/windows builds — the binary reads `/proc` for status; on macOS status is wrong.
- Homebrew tap, Cloudsmith `.deb`, and their secrets — one user on WSL; a tarball + `go
  install` cover it. Revisit if anyone else installs it.
- User manual (snowball PDF/EPUB), `manual-smoke`, `build-manual.sh` — the docs are a README.
- `windows-smoke`, `ci-surface` (actionlint/shellcheck) — no shell scripts to lint; two
  small workflows.
- `pickle changelog check` — optional at release time; it is a pickle command and can be run
  by hand from this board, no wiring needed.

**Decided (2026-11-08, chat)**

1. **Linux only.** No darwin/windows targets. Status detection reads `/proc`, so on those
   platforms every locked session would show as `stale`. Revisit when the platform work
   below exists.
2. **No `pickle board audit` in CI.** It would need pickle installed on the runner, and the
   bookkeeping is local anyway.
3. **First tag is `v0.1.0`**, SemVer from there.
4. **Pin `goreleaser/goreleaser-action@v7.3.0`** in both workflows, rather than pickle's
   floating `@v7`. The pin is exact, so an upgrade is a deliberate edit.

**Out of scope: macOS / Windows support.** All the `/proc` use is in two functions in
`internal/session/session.go`: `pidIsCopilot` (reads `cmdline`) and `ancestorsFrom` (reads
the parent PID from `stat`). Porting means replacing those per OS (`session_linux.go` /
`_darwin.go` / `_windows.go`). On macOS, `golang.org/x/sys/unix` (already an indirect
dependency) covers it: `SysctlKinfoProc` for the parent PID, `SysctlRaw("kern.procargs2")`
for the command line. Windows is harder: Toolhelp32 gives the parent PID and exe name but
not the command line, and the lock layout and rename-while-open behaviour of `restore` are
unverified there. If this is picked up it becomes its own ticket, and darwin/windows join
`goos` in `.goreleaser.yaml`.

Soft coupling: CS-005 (install/cutover) — README's *Install* section gains a "from a
release" path beside `just install`.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-08 — created (TO DO). source: chat: design a release process for this project modelled on pickle's
