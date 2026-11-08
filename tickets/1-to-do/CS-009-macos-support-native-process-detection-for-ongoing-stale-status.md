---
id: CS-009
title: macOS support: native process detection for ongoing/stale status
project: copilot-sessions
depends-on: []
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

**Approach.** Keep both signatures and move the bodies into build-tagged files:
`session_linux.go` (current code, unchanged) and `session_darwin.go`. Callers don't change.
On darwin, use `golang.org/x/sys/unix`, which is already in the module graph (v0.49.0,
indirect through modernc sqlite) and so adds no new download:

- parent PID: `unix.SysctlKinfoProc("kern.proc.pid", pid)` → `.Eproc.Ppid`;
- command line: `unix.SysctlRaw("kern.procargs2", pid)`. This is the full argument list.
  `p_comm` alone isn't enough, because Copilot runs as `node` and the `copilot` text is in
  the script path.

Fallback if sysctl proves awkward: `ps -o ppid=,command= -p <pid>`, with no dependency but
one fork per lock.

**Unverified assumption (check first at refinement).** Copilot CLI on macOS writes the same
`session-state/<uuid>/inuse.<pid>.lock` files as on Linux. This Mac has `~/.copilot` but no
`session-state/` yet, so run a Copilot CLI session and inspect it.

**Also in scope.** Un-skip the `/proc`-gated tests on darwin (`internal/session/session_test.go`,
`internal/cli/clean_test.go`, `internal/testfix/testfix.go`). Update the spec's platform line
(`development/spec.md:12`) and AGENTS.md's Platform section. Add `darwin` to `goos` in
`.goreleaser.yaml` and a `macos-latest` job to `ci.yml`.

**Out of scope.** Windows support. Process command lines need `NtQueryInformationProcess`
or WMI; open-file rename and delete semantics put `restore`/`clean` at risk; and a Windows
process can't see Copilot running inside WSL. Not worth it without demand.

Soft coupling: CS-008 creates `.goreleaser.yaml` and `ci.yml`. If CS-008 hasn't shipped,
the release/CI edits wait for it, so make it a hard `depends-on` at refinement if that holds.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-08 — created (TO DO). source: review: CS-008 platform exploration found /proc use confined to two functions; macOS portable via x/sys/unix
