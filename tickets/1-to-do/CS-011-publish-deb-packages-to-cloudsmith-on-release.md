---
id: CS-011
title: Publish .deb packages to Cloudsmith on release
project: copilot-sessions
depends-on: []
spawned-by: []
impact: medium
complexity: low
cost: S
---

# CS-011 — Publish .deb packages to Cloudsmith on release

## Outcome

After this ships, `sudo apt install copilot-sessions` (or `apt upgrade`) from the Cloudsmith
repository gets the latest tagged release on both amd64 and arm64, instead of the one-off
CS-005 test build (`0.0.0~cs005.d72b817`, amd64 only) that is there today.

## Description

The README documents an apt install from the Cloudsmith repository `codcod/copilot-sessions`
(setup script, or key + `sources.list.d` entry by hand). Both were verified on 2026-11-09 in an
`ubuntu:26.04` container: on amd64 they install the CS-005 test package; on arm64 `apt install`
fails with `Unable to locate package`. Nothing in the release process publishes there —
CS-008 scoped `.deb`/Cloudsmith out on purpose (its decision 6, "only `GITHUB_TOKEN`"), and the
CS-005 review (F5) noted that a release-automation ticket would own the apt path.

Model it on pickle's release (`codcod/pickle`: `.github/workflows/release.yml` step *Publish
.deb to Cloudsmith*, `.goreleaser.yaml` `nfpms`, RELEASING.md *Debian/Ubuntu packages*):

- **`.goreleaser.yaml`** gains an `nfpms` entry: `formats: [deb]`, package name
  `copilot-sessions`, `bindir: /usr/bin`, `file_name_template:
  "{{ .ProjectName }}_{{ .Version }}_{{ .Arch }}"`, vendor/maintainer/homepage/license/description.
  The `.deb` files are also attached to the GitHub release (goreleaser does this by default).
  Shell completions in the package (bash/zsh/fish under `/usr/share/...`) are optional: pickle
  generates them in a `before` hook; decide at refinement whether that is worth it here.
- **`.github/workflows/release.yml`** gains a last step after goreleaser that pushes
  `dist/*.deb` with `cloudsmith push deb codcod/copilot-sessions/any-distro/any-version <f>
  --republish` (pinned `cloudsmith-cli==1.30.0` via `pipx`). It soft-fails like pickle's:
  missing `CLOUDSMITH_API_KEY`, a prerelease tag (contains `-`), no `.deb`, or a failed CLI
  install each exit 0 with a `::warning::` and a job-summary line; every package is tried so one
  failed push never strands the other architecture.
- **Secret** `CLOUDSMITH_API_KEY` on `codcod/copilot-sessions` (one-time, human; prefer a service
  account scoped to this Cloudsmith repo).
- **RELEASING.md** gets a *Debian/Ubuntu packages* section: secret, soft-fail rules, re-pushing an
  old tag by hand (`gh release download … -p '*.deb'` + push loop), retention (deferred, as in
  pickle).
- **Cleanup**: delete the CS-005 test package from Cloudsmith once a real release is there. Its
  version sorts below any real one, so it never shadows an upgrade; this is hygiene only.

Facts checked on 2026-11-09 that the plan must honour: Cloudsmith serves `any-distro` packages
under real distro paths (`deb/ubuntu resolute`, `deb/debian bookworm`), and
`deb/any-distro any-version` has no `Release` file — the README already uses
`$ID $VERSION_CODENAME`. The binary is static (`CGO_ENABLED=0`), so one package per
architecture serves every Debian/Ubuntu release.

Soft couplings: CS-008 (release process this extends); CS-005 (the test package and F5 note).
The README's apt section is accurate only once this ships.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-09 — created (TO DO). source: chat: README apt install tested in ubuntu:26.04; repo holds only the CS-005 amd64 test build and no release publishes .deb
