# Releasing copilot-sessions

Releases are built by [goreleaser](https://goreleaser.com) from a `vX.Y.Z` tag. Each release
has two Linux tarballs, `copilot-sessions_X.Y.Z_linux_amd64.tar.gz` and
`copilot-sessions_X.Y.Z_linux_arm64.tar.gz`, plus `checksums.txt`. Only `GITHUB_TOKEN` is
needed, so there is no secret to set up.

## Cut a release

1. Optional: run `pickle changelog check` to list shipped tickets missing from `[Unreleased]`.
2. In [`CHANGELOG.md`](CHANGELOG.md), retitle `[Unreleased]` to `[X.Y.Z] - YYYY-MM-DD`, add a
   fresh empty `[Unreleased]` above it, and fix the link references at the bottom:
   ```
   [Unreleased]: https://github.com/codcod/copilot-sessions/compare/vX.Y.Z...HEAD
   [X.Y.Z]: https://github.com/codcod/copilot-sessions/releases/tag/vX.Y.Z
   ```
   Commit it, so the tag contains the changelog.
3. Tag and push. The [`release`](.github/workflows/release.yml) workflow does the rest:
   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

## Re-run a release

Actions → release → Run workflow, with an existing tag. `mode: replace` overwrites the assets
that are already there.

## Validate locally

```sh
just dist-check      # goreleaser check
just dist-snapshot   # unpublished snapshot build into dist/
```

## Versions

goreleaser stamps the version without the `v` (`0.1.0`). A `go install` build has no ldflags
and reports the module version (`v0.1.0`):

```sh
go install github.com/codcod/copilot-sessions/cmd/copilot-sessions@vX.Y.Z
```
