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

### 0. Feature branch (mandatory)

```
cd /Users/nka/Projects/codcod/copilot-sessions   # root-path, in-tree child
git checkout main && git pull --ff-only
git checkout -b feat/CS-008-release-process
```

Commit WIP locally as you go. No push and no PR without explicit user approval. Before
pushing, `git fetch origin main && git diff --name-only origin/main...HEAD | grep '^tickets/'`
must print nothing. Merging is the human's.

### Prerequisite gate (hard)

None. `goreleaser` (v2) and `docker` are installed on the dev Mac and are needed for the
acceptance test.

### Confirmed design decisions (do not deviate without asking)

1. **Linux only: `goos: [linux]`, `goarch: [amd64, arm64]`.** On other platforms status
   reads `/proc`. darwin and windows join through CS-009 and CS-010.
2. **No `pickle board audit` in CI.** Bookkeeping is local.
3. **First tag is `v0.1.0`, SemVer from there.** The tag is cut by the human after merge,
   following `RELEASING.md`. This ticket does not push a tag.
4. **Pin `goreleaser/goreleaser-action@v7.3.0`** in both workflows. Pickle's floating `@v7`
   is not used.
5. **`before.hooks` runs only `go test ./...`.** No `go mod tidy`, because it can dirty the
   tree mid-release.
6. **Only `GITHUB_TOKEN`.** No Homebrew tap, no `.deb`/Cloudsmith, no manual, no extra secrets.
7. **`go install` version fallback** goes in `cmd/copilot-sessions/main.go` as
   `resolveVersion()`, copied from pickle's `main.go`. It keeps the ldflags value unless that
   is `""`/`"dev"`, else uses `debug.ReadBuildInfo().Main.Version` unless that is `"(devel)"`.
   The variable stays `main.Version` (capital V), as the justfile and ldflags expect.
8. **The leading `v` stays as each path produces it.** goreleaser stamps `0.1.0`;
   `just build`/`go install` report `v0.1.0`. This is cosmetic and is not normalised.
9. **`ci.yml`'s `goreleaser check` must exit 0.** There is no deprecated `brews` pipe, so the
   `|| [ $? -eq 2 ]` escape is not copied. The same applies to `just dist-check`.

### Tasks

#### Task 1: `.goreleaser.yaml`
At the repo root, `version: 2`, `project_name: copilot-sessions`. Contents:
- `before.hooks: [go test ./...]`
- one build: `main: ./cmd/copilot-sessions`, `binary: copilot-sessions`, `env: [CGO_ENABLED=0]`,
  `goos: [linux]`, `goarch: [amd64, arm64]`, `ldflags: [-s -w -X main.Version={{.Version}}]`
- archives: `formats: [tar.gz]`, `name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"`
- `checksum.name_template: checksums.txt`
- `snapshot.version_template: "{{ incpatch .Version }}-snapshot"`
- `release`: `github: {owner: codcod, name: copilot-sessions}`, `draft: false`,
  `prerelease: auto`, `mode: replace`. Add a one-line comment on why the forge is pinned
  (`GITLAB_TOKEN` inference).
- `changelog`: `sort: asc`, exclude `^docs:`, `^test:`, `^chore:`.

`/dist/` is already in `.gitignore`.

#### Task 2: `.github/workflows/release.yml`
Copy pickle's trigger, permissions, concurrency and checkout/setup-go steps:
`push: tags: ['v*']` plus `workflow_dispatch` with a required `tag` input;
`permissions: contents: write`; `concurrency: {group: release-${{ github.ref }}, cancel-in-progress: false}`;
`actions/checkout@v7` with `fetch-depth: 0` and `ref: ${{ inputs.tag || github.ref }}`;
`actions/setup-go@v7` with `go-version-file: go.mod` and `cache: true`. Then one step,
`goreleaser/goreleaser-action@v7.3.0` with `distribution: goreleaser`, `version: '~> v2'`,
`args: release --clean` and env `GITHUB_TOKEN`. Leave out the manual and Cloudsmith steps.

#### Task 3: `.github/workflows/ci.yml`
On `push: branches: [main]` and `pull_request`, with `permissions: contents: read` and
pickle's concurrency block. Jobs:
- `build-test` (ubuntu-latest): checkout, setup-go, `go vet ./...`,
  `test -z "$(gofmt -l .)"`, `go test ./...` (the `/proc` tests run natively here), and
  `CGO_ENABLED=0 go build -o copilot-sessions ./cmd/copilot-sessions`.
- `goreleaser-check` (ubuntu-latest): checkout with `fetch-depth: 0`,
  `goreleaser-action@v7.3.0` with `install-only: true` and `version: '~> v2'`, then
  `run: goreleaser check`.

#### Task 4: version fallback
`cmd/copilot-sessions/main.go`: add `resolveVersion()` (decision 7, `runtime/debug`) and
pass it to `cli.Execute` in place of `Version`. Add `cmd/copilot-sessions/main_test.go` with
one table test: `Version="1.2.3"` returns `1.2.3`, and `Version="dev"` returns either `dev`
or a non-`(devel)` build-info version, never `""` or `(devel)`. Restore `Version` with
`t.Cleanup`.

#### Task 5: justfile `release` group
Next to `show-version`, add these, each with a comment naming the GitLab-token reason:
```
# Validate .goreleaser.yaml
[group('release')]
dist-check:
    env -u GITLAB_TOKEN -u GITLAB_PERSONAL_ACCESS_TOKEN goreleaser check

# Build an unpublished snapshot into dist/
[group('release')]
dist-snapshot:
    env -u GITLAB_TOKEN -u GITLAB_PERSONAL_ACCESS_TOKEN goreleaser release --snapshot --clean
```

#### Task 6: `CHANGELOG.md` and `RELEASING.md`
- `CHANGELOG.md`: a Keep a Changelog 1.1.0 header plus a SemVer line, and `## [Unreleased]`
  with `### Added` listing the shipped surface: `list`, `clean`, `backup`, `restore` and shell
  completion (CS-001…CS-005), backup robustness (CS-007), and release builds (CS-008). Add a
  link reference for `[Unreleased]`.
- `RELEASING.md`, kept to one screen. **Cut a release**: retitle `[Unreleased]` to
  `[X.Y.Z] - YYYY-MM-DD`, add a fresh `[Unreleased]`, fix the link references, commit, then
  `git tag vX.Y.Z && git push origin vX.Y.Z`. Optionally run `pickle changelog check` first.
  **Re-run**: Actions → release → Run workflow with an existing tag (`mode: replace`).
  **Validate locally**: `just dist-check`, `just dist-snapshot`. Also say what a release
  contains (two Linux tarballs and `checksums.txt`) and give the `go install …@vX.Y.Z` line.

### Acceptance test

Run from the repo root on the branch:

```
just lint                     # gofmt + vet clean
just test                     # passes (includes main_test.go)
just test-linux               # passes in Docker, /proc tests included
just dist-check               # exit 0, no deprecation warnings
just dist-snapshot            # exit 0
ls dist/*.tar.gz dist/checksums.txt
#   -> copilot-sessions_<v>-snapshot_linux_amd64.tar.gz, …_linux_arm64.tar.gz, checksums.txt
#      and no darwin/windows archives
tar tzf dist/copilot-sessions_*_linux_amd64.tar.gz | grep -x copilot-sessions
docker run --rm --platform linux/amd64 -v "$PWD/dist":/d alpine sh -c \
  '/d/copilot-sessions_linux_amd64_v1/copilot-sessions --version'
#   -> prints the snapshot version (not "dev"); static binary runs on musl
(cd dist && shasum -a 256 -c checksums.txt)   # all OK
just build && ./copilot-sessions --version    # git-describe version, as before
```

The build dir under `dist/` may be named `copilot-sessions_linux_amd64_v1`. Use whatever
`ls dist` shows. Workflow YAML is checked by `goreleaser check` and by the first CI run on
the PR, which must be green before the review approves the merge.

### Docs update (mandatory when user-facing)

- `README.md` *Install*: add "From a release" ahead of `just install`. It covers downloading
  `copilot-sessions_<ver>_linux_<arch>.tar.gz` from GitHub Releases, verifying it with
  `checksums.txt`, untarring into `~/.local/bin`, or
  `go install github.com/codcod/copilot-sessions/cmd/copilot-sessions@latest`. The
  *Development* section links `RELEASING.md`.
- `CHANGELOG.md` and `RELEASING.md` are new (Task 6). This ticket's own entry is the
  "release builds" line under `[Unreleased]`.

### Finish (mandatory)

1. Acceptance test green; `just lint` and `just test` clean.
2. Docs updated (README, CHANGELOG, RELEASING).
3. Summary: files touched, decisions, anything deferred.
4. Root-path child: tidy the WIP commits into atomic ones and keep that history on merge,
   e.g.:
   ```
   ci: add CI and tag-driven goreleaser release workflows (CS-008)
   feat(cli): fall back to module version for go install builds (CS-008)
   docs: add CHANGELOG, RELEASING and release install path (CS-008)
   ```
5. Commit locally, no push. `pickle ticket move CS-008 in-review --reason "acceptance green"`.
   After merge, the human cuts `v0.1.0` per `RELEASING.md`.

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-08 — created (TO DO). source: chat: design a release process for this project modelled on pickle's
- 2026-11-08 — TO DO → READY: plan complete
- 2026-11-08 — READY → IN DEVELOPMENT: picked up
- 2026-11-08 — plan amended inline: acceptance `docker run` gains `--platform linux/amd64` (applicability gate: dev Mac is arm64, the amd64 binary needs explicit emulation). Gate note-and-close: pickle's `resolveVersion` reads lowercase `version`; decision 7 keeps `Version`.
