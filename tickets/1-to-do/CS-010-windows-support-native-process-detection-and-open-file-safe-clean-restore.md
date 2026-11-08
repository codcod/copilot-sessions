---
id: CS-010
title: Windows support: native process detection and open-file-safe clean/restore
project: copilot-sessions
depends-on: []
spawned-by: [CS-008]
impact: low
complexity: high
cost: L
---

# CS-010 — Windows support: native process detection and open-file-safe clean/restore

## Outcome

After this ships, a native Windows `copilot-sessions.exe` lists, cleans, backs up and
restores Copilot CLI sessions run from Windows (not WSL), with correct `ongoing`/`stale`
status. `clean`/`restore` fail safely instead of half-applying when Copilot holds files
open. Releases include `windows/amd64` and `windows/arm64` zip archives.

## Description

Spawned by CS-008's platform exploration; the sibling of CS-009 (macOS). Harder than
macOS on three fronts, so it is graded separately and lower: worth doing only once someone
runs Copilot CLI natively on Windows.

**1. Process detection.** The same two functions as CS-009 (`pidIsCopilot`, `ancestorsFrom`
in `internal/session/session.go`) get a `session_windows.go`, using `golang.org/x/sys/windows`:

- parent PID: `CreateToolhelp32Snapshot` + `Process32First/Next` → `ParentProcessID`.
  Walk one snapshot rather than one per PID. Windows reuses PIDs aggressively and a parent
  may have exited, so stop the walk on a missing PID, as the Linux code does.
- command line: `ExeFile` from the same snapshot is only `node.exe`, so the `copilot`
  match needs the full command line, from `NtQueryInformationProcess` with
  `ProcessCommandLineInformation` (Windows 8.1+, `PROCESS_QUERY_LIMITED_INFORMATION`).
  Confirm at refinement whether `x/sys/windows` exposes that class or it needs a small
  `syscall` shim. Rejected: shelling out to WMI/PowerShell (`Get-CimInstance
  Win32_Process`), because it is seconds per call.

**2. File semantics.** Windows can't delete or rename a file another process holds open
without share-delete, and SQLite locks are mandatory there. At risk:

- `clean` removing a session directory while Copilot has its files open;
- `restore` renaming top-level items into `COPILOT_HOME` (`internal/backup/restore.go`)
  while Copilot runs, which could leave a half-restored home. It needs either a refusal
  when any session is `ongoing` or rollback on the first failed rename;
- `backup` reading live files and `session-store.db` (CS-007's live-file races take a
  different form).

Archive entry names already use `filepath.ToSlash` (`internal/backup/backup.go:147`).
Restore's safe-extraction checks (CS-004) still need Windows cases: drive letters,
backslashes, reserved names (`CON`, `NUL`…).

**3. Unverified environment (check first at refinement).** The default `COPILOT_HOME`
on Windows (assumed `%USERPROFILE%\.copilot`) and whether Copilot CLI writes the same
`session-state/<uuid>/inuse.<pid>.lock` files.

**Boundary.** A Windows binary can't see processes inside WSL, nor the reverse. Sessions
from the other side read as `stale`. Document it; don't try to bridge it.

**Also in scope.** Un-skip the `/proc`-gated tests on Windows. Add a `windows-latest` CI job
(pickle's `windows-smoke` is the model). Add `windows` to `goos` with a zip
`format_overrides`, as pickle does. Document `completion powershell` in the README. Update
the spec's platform line and AGENTS.md.

Soft couplings: CS-008 (creates `.goreleaser.yaml`/`ci.yml`) and CS-009 (introduces the
per-OS file split; whichever lands first sets the pattern). Make CS-008 a hard
`depends-on` at refinement if it hasn't shipped.

## Implementation Plan

<!-- empty until refined; must meet the READY gate before moving to 2-ready/ -->

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-08 — created (TO DO). source: review: CS-008 platform exploration; Windows split from CS-009 at user request
