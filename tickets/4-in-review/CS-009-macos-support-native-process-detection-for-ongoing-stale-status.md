---
id: CS-009
title: macOS support: native process detection for ongoing/stale status
project: copilot-sessions
depends-on: [CS-008, CS-013]
spawned-by: [CS-008]
impact: low
complexity: medium
cost: M
---

# CS-009 — macOS support: native process detection for ongoing/stale status

## Outcome

After this ships, `copilot-sessions` on macOS shows `ongoing`, `stale` and the
"(this session)" marker correctly, so `clean` won't treat a live session as stale. Releases
include `darwin/amd64` and `darwin/arm64` archives.

## Description

Spawned by CS-008's platform exploration. Today all `/proc` use is in two functions in
`internal/session/session.go`: `pidIsCopilot(pid)` reads `/proc/<pid>/cmdline` (`ongoing`
vs `stale`), and `ancestorsFrom(pid)` reads the parent PID from `/proc/<pid>/stat`
(the current-session marker). Without `/proc`, every locked session reads as `stale` and
`DefaultAncestors` returns only the process's own PID.

**State after CS-013 (re-verified 2026-11-09).** CS-013 left both functions' signatures alone
(`pidIsCopilot(pid int) bool`, `ancestorsFrom(pid int) map[int]bool`, now at
`session.go:357–388`) and fixed C5 (`ancestorsFrom` checks `i < 0` before slicing), so the
Linux bodies move verbatim. `session.Load` now takes `ctx` first and `Status` is a typed string,
but neither touches process detection. `golang.org/x/sys` is still v0.49.0 and indirect after
CS-013's dependency upgrade. `just lint` and CI's `build-test` now also run `go fix -diff`.
`.goreleaser.yaml` builds linux only (a comment there names this ticket), and its `nfpms` .deb
packages (CS-011) are linux-only by nature, so the Cloudsmith push is unaffected.

**Approach.** Keep both signatures and move the bodies into build-tagged files:
`session_linux.go` (current code, unchanged) and `session_darwin.go`. Callers don't change.
On darwin, use `golang.org/x/sys/unix`, which is already in the module graph (v0.49.0,
indirect through modernc sqlite) and so adds no new download:

- parent PID: `unix.SysctlKinfoProc("kern.proc.pid", pid)` → `.Eproc.Ppid`;
- command line: `unix.SysctlRaw("kern.procargs2", pid)`. This is the full argument list.
  Since Copilot CLI 1.0.x the lock PID is a native per-platform binary
  (`…/@github/copilot-darwin-<arch>/copilot`), so `p_comm` would match today. argv is still
  used because it fails safe. If some install has node hosting the JS, `p_comm` reads
  `node`, the live session shows as `stale`, and `clean` deletes it (data loss). An argv miss
  can only make a session show as `ongoing`, which is harmless. argv also mirrors Linux's
  `cmdline`, and it keeps the `copilot-test/sleep` test helper working (applicability gate F1,
  2026-11-09). The buffer carries the **environment** after argv, so it must be parsed
  (argc, exec path, argv) and only argv matched. Otherwise any process with `COPILOT_HOME`
  set reads as Copilot (found at refinement, 2026-11-08).

Rejected at refinement: a `ps -o ppid=,command= -p <pid>` fallback (one fork per lock).

**Unverified assumption (prerequisite gate of the plan).** Copilot CLI on macOS writes the same
`session-state/<uuid>/inuse.<pid>.lock` files as on Linux. As of 2026-11-08 Copilot CLI isn't
installed on the dev Mac, so the plan checks this before any code and stops if it doesn't hold.

**Also in scope.** Un-skip the `/proc`-gated tests on darwin (`internal/session/session_test.go`,
`internal/cli/clean_test.go`, `internal/testfix/testfix.go`). Update the spec's platform line
(`development/spec.md:14`) and AGENTS.md's Platform section. Add `darwin` to `goos` in
`.goreleaser.yaml` and a `macos-latest` job to `ci.yml`.

**Out of scope.** Windows support, filed separately as CS-010.

Hard dependency: CS-008 creates `.goreleaser.yaml` and `ci.yml` (made hard at refinement,
2026-11-08, user sign-off). CS-010 depends on this ticket's per-OS file split.

## Implementation Plan

### 0. Feature branch (mandatory)

```
cd /Users/nka/Projects/codcod/copilot-sessions   # root-path, in-tree child
git checkout main && git pull --ff-only
git checkout -b feat/CS-009-macos-support
```

Commit WIP locally. No push and no PR without explicit user approval. Before pushing,
`git fetch origin main && git diff --name-only origin/main...HEAD | grep '^tickets/'` must
print nothing. Merging is the human's.

### Prerequisite gate (hard)

1. **CS-008 and CS-013 are in `6-done/` and merged to `main`** (both are, as of 2026-11-09).
   `.goreleaser.yaml` and `.github/workflows/ci.yml` must exist.
2. **The lock layout is verified on macOS before any code.** Install Copilot CLI on the Mac,
   start a session, and from another terminal run:
   ```
   ls ~/.copilot/session-state/*/            # expect inuse.<pid>.lock + workspace.yaml
   ps -o pid=,ppid=,command= -p <pid>        # <pid> from the lock; command contains "copilot"
   ```
   Record the result as a dated `plan amended inline:` History line. **If the lock files are
   absent or named differently, or the PID isn't the Copilot process, stop.** Move the ticket
   back to `1-to-do/` for re-description.

   **Met by proxy, 2026-11-09 (user decision).** Copilot CLI 1.0.107 in a Linux Docker
   container (no login needed) created `session-state/<uuid>/{inuse.<pid>.lock,
   inuse.<pid>.hold, workspace.yaml}`. The lock PID is the native
   `@github/copilot-linux-arm64/copilot` binary, and its parent is `node /usr/local/bin/copilot`.
   The darwin build is the same release built per platform. The real-macOS check moves to the
   manual acceptance step. **If it fails there, stop and move the ticket back to `1-to-do/`.**
   The `.hold` file needs nothing, because `lockPIDs` matches only `inuse.*.lock` (F2).

### Confirmed design decisions (do not deviate without asking)

1. **Signatures and callers are unchanged.** `pidIsCopilot(pid int) bool` and
   `ancestorsFrom(pid int) map[int]bool` move out of `internal/session/session.go` into
   build-tagged files. `DefaultAncestors` stays in `session.go`.
2. **Three files:** `session_linux.go` (today's bodies, moved verbatim), `session_darwin.go`,
   and `session_other.go` (`//go:build !linux && !darwin`). The last keeps today's non-Linux
   behaviour (`false`; own PID only), so windows and others still compile until CS-010.
3. **Darwin uses `golang.org/x/sys/unix` sysctls, with no `ps` fallback.** Parent PID comes
   from `unix.SysctlKinfoProc("kern.proc.pid", pid)` → `.Eproc.Ppid`. The command line comes
   from `unix.SysctlRaw("kern.procargs2", pid)`. Both exist in v0.49.0, which is already in
   the module graph. `go mod tidy` promotes it to a direct requirement and adds no new download.
4. **`kern.procargs2` is parsed. It is never substring-matched whole.** The buffer holds
   `argc` (native-endian int32), the exec path, NUL padding, the `argc` argv strings, and then
   **the environment**. Matching the whole buffer would flag any process with `COPILOT_HOME`
   or a `copilot` path in `PWD`. Only the argv strings are matched for `"copilot"`, the same as
   Linux's `cmdline`. A short or malformed buffer, or a sysctl error (EPERM on another user's
   process, ESRCH), reads as `false`.
5. **The ancestor walk keeps Linux's stopping rules.** It stops at PID ≤ 1, on any sysctl
   error, and when `Ppid == pid`.
6. **The test helper is unchanged on darwin.** `testfix.startCopilotHelper` copies `sleep` to
   `copilot-test` and is enabled on darwin too. If macOS kills the copied platform binary
   (code-signing), switch to `os.Symlink` for both OSes. `argv[0]` is the path as invoked, so
   it still contains `copilot-test`.
7. **Release and CI:** `goos` gains `darwin`, and `ci.yml` gains a `macos-latest` job that
   runs `go vet ./...` and `go test ./...`. Unsigned binaries; no notarisation or Homebrew
   tap (same as CS-008's decision 6).

### Tasks

#### Task 1: split by OS
Move `pidIsCopilot` and `ancestorsFrom` from `internal/session/session.go` (lines 357–360
and 366–388 after CS-013) into `internal/session/session_linux.go` unchanged. Add `session_other.go` with the
fallback from decision 2. Update the `DefaultAncestors` comment (today: "Without /proc
(macOS) it returns only the process's own PID"): on Linux and macOS it walks the real ancestry,
elsewhere it returns its own PID. Remove the imports that `session.go` no longer uses (`bytes`,
`fmt`, `strconv` are still used elsewhere in the file; let the compiler decide). Check: `GOOS=windows go vet ./...` still passes.

#### Task 2: darwin implementation
Add `internal/session/session_darwin.go`:
- `procargsArgv(b []byte) [][]byte`, a pure parser for decision 4 (read argc, skip the exec
  path and the NULs, split the next argc NUL-terminated strings), returning nil if malformed;
- `pidIsCopilot`, which calls `SysctlRaw` and then reports whether any argv contains
  `copilot`;
- `ancestorsFrom`, using `SysctlKinfoProc`.

#### Task 3: tests
- `internal/session/session_darwin_test.go`: table tests for `procargsArgv` and the match.
  Cover a real-shaped buffer whose argv has `/usr/local/bin/copilot`, which matches. Cover one
  whose argv is `node /x/y.js` with env `COPILOT_HOME=/h`, which must **not** match. Cover
  truncated and zero-argc buffers, which return nil. Name the tests so `-run Procargs`
  selects them (e.g. `TestProcargsArgv`, `TestProcargsMatch`).
- Un-skip the live-process tests on darwin. In `internal/session/session_test.go` (the
  `TestLoadOngoingLinux` skip and `TestDefaultAncestors`'s early return), widen the condition
  to `linux || darwin` and rename the test to `TestLoadOngoing`. In
  `internal/testfix/testfix.go:49` (`if runtime.GOOS == "linux"`), use the same condition and
  fix the `OngoingLive` field comment "(Linux only)". `internal/cli/clean_test.go:94` follows
  `fx.OngoingLive`, so only its skip text ("needs /proc …") changes to "needs a live ongoing
  session".

#### Task 4: release and CI
`.goreleaser.yaml`: `goos: [darwin, linux]`, and replace the "Linux only … (darwin: CS-009,
windows: CS-010)" comment with one naming only CS-010. Leave `nfpms` alone (linux-only).
`.github/workflows/ci.yml`: add the `test-macos` job (`runs-on: macos-latest`; checkout,
setup-go with `go-version-file: go.mod`, `go vet ./...`, `go test ./...`). `go fix` and `gofmt`
stay only in `build-test`, because they don't depend on the OS.

### Acceptance test

```
go mod tidy && git diff --exit-code go.sum   # x/sys/unix becomes direct, go.sum unchanged
just lint && just test                       # on the Mac
go test -count=1 -v -run 'Ongoing|Ancestors|Procargs' ./internal/... 2>&1 | grep -E '^(--- |ok|FAIL)'
#   -> TestLoadOngoing, TestDefaultAncestors, TestCleanOngoing, procargs tests: PASS, none SKIP
#   (TestDefaultAncestors returns early rather than skipping, so "no SKIP" proves nothing
#   for it. Its darwin path is covered only because task 3 widens its condition.)
just test-linux                              # Linux unchanged
GOOS=windows GOARCH=amd64 go vet ./...       # fallback still compiles
just dist-check && just dist-snapshot && ls dist/*.tar.gz dist/*.deb
#   -> four archives: darwin_{amd64,arm64}, linux_{amd64,arm64}; two .deb (amd64, arm64) only
```

Manual check with a real Copilot session on macOS (this is also prerequisite 2's deferred
check; on failure, back to `1-to-do/`). Install Copilot CLI on the Mac and start a session first. From a shell inside Copilot,
`./copilot-sessions list` shows that session as `ongoing` with the "(this session)" marker.
From an outside terminal it shows `ongoing` without the marker. After Copilot exits with a
leftover lock it shows `stale`. CI's `test-macos` job is green on the PR.

### Docs update (mandatory when user-facing)

- `development/spec.md:14`: the platform line becomes Linux (WSL) and macOS, with process
  detection from `/proc` on Linux and sysctl on macOS. `development/spec.md:93`: the
  `ongoing` definition ("`/proc/<pid>/cmdline` contains `copilot`") gains "or, on macOS, the
  argv from `kern.procargs2`" (F3).
- `AGENTS.md` *Platform*: macOS is now a target. `/proc` tests are now "live-process tests";
  they run on Linux and macOS. `just test-linux` is still needed for the Linux path.
- `README.md`: rewrite lines 14–15 ("Status detection reads `/proc`, so macOS and Windows are
  not supported yet") to say only Windows is unsupported, and extend the Linux-only download
  text (~127–135) to the darwin archives. Mention that unsigned binaries need
  `xattr -d com.apple.quarantine copilot-sessions` after a browser download (F4).
- `CHANGELOG.md` `[Unreleased]` → add an `### Added` section above the existing `### Changed`: "macOS support: correct ongoing/stale status
  and darwin release archives (CS-009)."

### Finish (mandatory)

1. Acceptance test green; `just lint` and `just test` clean on macOS, `just test-linux` clean.
2. Docs updated.
3. Summary: files touched, decisions, and the prerequisite-2 observation.
4. Tidy the WIP commits into atomic ones and keep that history, e.g.:
   ```
   refactor(session): split process detection by OS (CS-009)
   feat(session): detect Copilot processes on macOS via sysctl (CS-009)
   ci: build and test on macOS (CS-009)
   ```
5. Commit locally, no push. `pickle ticket move CS-009 in-review --reason "acceptance green"`.

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-08 — created (TO DO). source: review: CS-008 platform exploration found /proc use confined to two functions; macOS portable via x/sys/unix
- 2026-11-08 — TO DO → READY: plan complete
- 2026-11-09 — re-refined after CS-013 (stays READY): line refs, test-skip and goreleaser/CI details updated to the post-CS-013 tree; approach and decisions unchanged
- 2026-11-09 — plan amended inline: applicability gate (independent sub-agent) found no blocking issues. F1 updated the procargs2 rationale for the native binary; F3/F4 added spec:93 and README 14–15/127–135; F5 recorded prerequisite 2 as met by proxy (Docker, Copilot CLI 1.0.107) and moved the real-macOS check to manual acceptance; F6 tightened the acceptance notes. F2 and F8 closed. F7 noted on CS-010.
- 2026-11-09 — READY → IN DEVELOPMENT: picked up
- 2026-11-09 — plan amended inline: manual real-macOS Copilot check (prerequisite 2's deferred check) waived by user decision; macOS lock layout assumed from the Linux Docker proxy (Copilot CLI 1.0.107). Test helper switched from a copy of sleep to a symlink (decision 6 fallback): macOS killed the copied binary on code-signature mismatch. CI test-macos is still unverified until push.
- 2026-11-09 — IN DEVELOPMENT → IN REVIEW: acceptance green (manual macOS check waived)
