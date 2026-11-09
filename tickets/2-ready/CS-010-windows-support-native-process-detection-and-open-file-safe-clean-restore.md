---
id: CS-010
title: Windows support: native process detection and open-file-safe clean/restore
project: copilot-sessions
depends-on: [CS-008, CS-009, CS-013]
spawned-by: [CS-008]
impact: low
complexity: medium
cost: M
---

# CS-010 — Windows support: native process detection and open-file-safe clean/restore

## Outcome

After this ships, a native Windows `copilot-sessions.exe` lists, cleans, backs up and
restores Copilot CLI sessions run from Windows (not WSL), with correct `ongoing`/`stale`
status. `clean` skips ongoing sessions by default, and `restore` leaves `COPILOT_HOME`
unchanged when Copilot holds a file open. Releases include `windows/amd64` and
`windows/arm64` zip archives.

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
  Confirmed at refinement (2026-11-08): `x/sys/windows` v0.49.0 exposes
  `NtQueryInformationProcess` and `ProcessCommandLineInformation`, so no shim is needed. Rejected: shelling out to WMI/PowerShell (`Get-CimInstance
  Win32_Process`), because it is seconds per call.

**2. File semantics.** Windows can't delete or rename a file another process holds open
without share-delete, and SQLite locks are mandatory there. At risk:

- `clean` removing a session directory while Copilot has its files open;
- `restore` renaming top-level items into `COPILOT_HOME` (`internal/backup/restore.go`)
  while Copilot runs. Refinement found this already handled: since CS-007, `swapItems`
  undoes every move on the first failed rename, so home is left as it was. No new guard
  (user decision, 2026-11-08); acceptance proves it on Windows;
- `backup` reading live files and `session-store.db` (CS-007's live-file races take a
  different form).

Archive entry names already use `filepath.ToSlash` (`internal/backup/backup.go:159`).
Restore's safe-extraction checks (CS-004) use `filepath.IsLocal`, which already rejects
drive letters, backslash traversal and reserved names (`CON`, `NUL`…) on Windows. Only
tests are missing.

**State after CS-013 (re-verified 2026-11-09).** Three things CS-013 changed help here and
need no work: TTY detection is `term.IsTerminal` (C6), which is correct on Windows consoles;
Ctrl-C is `signal.NotifyContext(…, os.Interrupt)` (X1), which Windows delivers; and the
`rename` that `swapItems` uses is now a parameter of `restore(ctx, env, args, yes, rename)`
(`internal/cli/restore.go:43`, D6) rather than a package global, so the Windows restore test
can pass `os.Rename` directly. One new gap: CS-013's permission-error tests (C1/C3) and the older
mode assertions rely on Unix permission bits. On Windows `chmod 0` doesn't block reads, modes
read back as `0o666`/`0o444`, and `os.Getuid`/`Geteuid` return -1, so the existing root guards
never skip. Those tests fail on Windows as written (task 2b). Cobra's default `completion`
command already includes `powershell`; only the README (line 36, "bash|zsh|fish") omits it.

**3. Unverified environment (prerequisite gate of the plan).** The default `COPILOT_HOME`
on Windows (assumed `%USERPROFILE%\.copilot`) and whether Copilot CLI writes the same
`session-state/<uuid>/inuse.<pid>.lock` files.

**Boundary.** A Windows binary can't see processes inside WSL, nor the reverse. Sessions
from the other side read as `stale`. Document it; don't try to bridge it.

**Also in scope.** Un-skip the `/proc`-gated tests on Windows, and skip the Unix-permission
tests there. Add a `windows-latest` CI job
(pickle's `windows-smoke` is the model). Add `windows` to `goos` with a zip
`format_overrides`, as pickle does. Document `completion powershell` in the README. Update
the spec's platform line and AGENTS.md.

Hard dependencies (refinement, 2026-11-08, user sign-off): CS-008 (creates
`.goreleaser.yaml`/`ci.yml`) and CS-009 (introduces the per-OS file split this ticket
extends).

## Implementation Plan

### 0. Feature branch (mandatory)

```
cd /Users/nka/Projects/codcod/copilot-sessions   # root-path, in-tree child
git checkout main && git pull --ff-only
git checkout -b feat/CS-010-windows-support
```

Commit WIP locally. No push and no PR without explicit user approval. Before pushing,
`git fetch origin main && git diff --name-only origin/main...HEAD | grep '^tickets/'` must
print nothing. Merging is the human's.

### Prerequisite gate (hard)

1. **CS-008, CS-009 and CS-013 are in `6-done/` and merged to `main`** (CS-008 and CS-013 are,
   as of 2026-11-09). `.goreleaser.yaml`,
   `ci.yml` and the `session_{linux,darwin,other}.go` split must exist.
2. **The environment is verified on native Windows before any code** (not WSL). Install
   Copilot CLI, start a session, and in PowerShell run:
   ```
   Get-ChildItem $env:USERPROFILE\.copilot\session-state\*\   # expect inuse.<pid>.lock
   Get-CimInstance Win32_Process -Filter "ProcessId=<pid>" | Select ParentProcessId,CommandLine
   #   <pid> from the lock; CommandLine contains "copilot"
   ```
   Record the result as a dated `plan amended inline:` History line. **If `COPILOT_HOME` is
   not `%USERPROFILE%\.copilot`, the lock layout differs, or the PID isn't the Copilot
   process, stop** and move the ticket back to `1-to-do/`. `cli.DefaultEnv` already resolves
   `os.UserHomeDir()` + `.copilot`, which is `%USERPROFILE%` on Windows, so a matching layout
   needs no change there.

### Confirmed design decisions (do not deviate without asking)

1. **Add `internal/session/session_windows.go`, same signatures.** `session_other.go`'s
   build tag becomes `!linux && !darwin && !windows`.
2. **Parent PIDs come from one Toolhelp32 snapshot per walk.** `ancestorsFrom` takes
   `windows.CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)` once and builds a
   `pid → ParentProcessID` map. It walks the map and stops on a missing PID, `ppid == pid`,
   PID 0/4 (Idle/System), or a PID already visited, because Windows reuses PIDs and can form
   cycles. `ponytail:` comment: a reused parent PID can add an unrelated ancestor. That only
   affects the "(this session)" marker, so creation times are not compared.
3. **The command line comes from `NtQueryInformationProcess`, with no shim and no WMI.** Use
   `windows.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, …)` and then
   `windows.NtQueryInformationProcess(h, ProcessCommandLineInformation, …)`. Both, plus the
   constant, are in `x/sys/windows` v0.49.0 (checked 2026-11-08). Query the size first, then
   read the `NTUnicodeString` at the start of the buffer. `ExeFile` alone is not enough
   (`node.exe`). Any error, such as access denied or the process having exited, reads as
   `false`.
4. **`restore` gets no new guard.** `swapItems` (`internal/cli/restore.go:113`) already undoes
   every move when a rename fails. A file Windows holds open therefore aborts the restore and
   leaves `COPILOT_HOME` as it was. Acceptance proves this on Windows; no code changes.
5. **`clean` is unchanged, and its Windows caveat is documented.** Ongoing sessions are
   skipped by default. With `--include-ongoing`, Windows may refuse to delete open files.
   `os.RemoveAll` errors are ignored (`deleteSessions`, `internal/session/delete.go:38`), so a partial directory
   can remain. The README says so; there is no code change.
6. **Safe extraction is unchanged, and Windows test cases are added.** `filepath.IsLocal`
   (`internal/backup/restore.go:61`) already rejects drive letters, `..\`, and reserved names
   (`CON`, `NUL`, …) when built for Windows. The cases go in as a `_windows_test.go` table.
7. **The test helper on Windows is `PING.EXE`.** `testfix.startCopilotHelper` copies
   `%SystemRoot%\System32\PING.EXE` to `copilot-test.exe` and runs `-n 300 127.0.0.1`.
   Linux and darwin keep `sleep`.
8. **WSL ↔ Windows is a documented boundary.** Each side reads the other's sessions as
   `stale`, and there is no bridging.
9. **Release and CI:** `goos` gains `windows` with a zip `format_overrides` (as pickle does).
   `ci.yml` gains a `test-windows` job (`windows-latest`, `shell: bash`, vet + test). `nfpms`
   stays linux-only; update the `goos` comment in `.goreleaser.yaml` that names this ticket.
10. **Unix-permission tests skip on Windows. They are not ported.** Each test or assertion
    that depends on mode bits gets `if runtime.GOOS == "windows" { t.Skip("Unix permission
    bits") }`. Where only one assertion inside a larger test depends on mode bits, only that
    assertion is guarded, so the rest of the test still runs. Simulating `EACCES` on Windows with
    ACLs is not worth the code (`ponytail:` comment at one site naming the gap).
    Symlink tests (`TestLoadEdgePaths`'s `os.Symlink`, restore's symlink entries) stay as they
    are: GitHub's Windows runners can create symlinks. If one fails locally without Developer
    Mode, that is an environment issue, not a skip.

### Tasks

#### Task 1: Windows process detection
`internal/session/session_windows.go` per decisions 2–3, and narrow the `session_other.go`
tag. `go mod tidy` (x/sys is already direct after CS-009).

#### Task 2: test helper and un-skips
`internal/testfix/testfix.go`: in `startCopilotHelper`, pick the source binary and args per
`runtime.GOOS` (decision 7), and add `windows` to the condition at line ~50. In
`internal/session/session_test.go`, add `windows` to the live-process conditions CS-009
widened.

#### Task 2b: Unix-permission tests (decision 10)
Guard the mode-bit dependencies found at refinement (2026-11-09):
`internal/cli/cancel_test.go` `TestPermissionErrors` (`chmod 0`, whole test);
`internal/session/session_test.go` `TestLoadEdgePaths` (the `chmod 0` block at ~294 only);
`internal/backup/backup_test.go` `TestCreate` (mode assertions at ~87 and ~119),
`TestCreateFailureLeavesNothing` (whole test), `TestListErrors` (the locked-dir part at ~251);
`internal/cli/clean_test.go` `TestCleanDelete` (the sidebar mode assertion at ~187);
`internal/backup/restore_test.go` `TestExtractModesAndLinks` (the mode check at ~44).
Then let the `test-windows` CI run judge. Any further failure that is purely a mode-bit
assumption gets the same guard. **Any other failure is a real Windows bug: stop and report it,
and don't skip it.**

#### Task 3: Windows tests
- `internal/session/session_windows_test.go`: `ancestorsFrom(os.Getpid())` contains its own
  PID and reaches at least one parent. `pidIsCopilot(fx.OngoingPID)` is true and
  `pidIsCopilot(os.Getpid())` is false.
- `internal/backup/restore_windows_test.go`: `Extract` rejects archive entries named
  `C:/x`, `C:x`, `..\\x`, `CON`, `nul.txt` and `s/AUX` with "unsafe path".
- `internal/cli/restore_windows_test.go`: open a file inside a current session with
  `os.Open` (Windows refuses the rename without share-delete). Then
  `restore(t.Context(), env, []string{"latest"}, true, os.Rename)` must fail, and afterwards
  `COPILOT_HOME`'s tree must be byte-identical to before (decision 4). Reuse `snapshot(t, fx)`
  from `restore_test.go` rather than writing a new tree comparison.

#### Task 4: release and CI
`.goreleaser.yaml`: `goos: [darwin, linux, windows]`, plus
`format_overrides: [{goos: windows, formats: [zip]}]`. `ci.yml`: add the `test-windows` job.

### Acceptance test

On the dev Mac:
```
just lint && just test && just test-linux
GOOS=windows GOARCH=amd64 go vet ./... && GOOS=windows GOARCH=arm64 go build -o /dev/null ./cmd/copilot-sessions
just dist-check && just dist-snapshot
ls dist/*.tar.gz dist/*.zip   # -> darwin/linux tar.gz ×4, windows_{amd64,arm64}.zip
```
On Windows (the CI `test-windows` job, or locally):
```
go test -count=1 -v ./... 2>&1 | findstr /R "^--- ^ok FAIL"
#   live-process tests (TestLoadOngoing, TestDefaultAncestors, TestCleanOngoing) and the
#   *_windows_test.go tests PASS, none SKIP
```
Manual check, native Windows with a real Copilot session. `copilot-sessions.exe list` shows
it `ongoing`, with "(this session)" when run from inside Copilot. `backup` succeeds while it
runs. `restore -y latest` while it runs either succeeds or fails with `COPILOT_HOME`
unchanged. `copilot-sessions completion powershell | Out-String | Invoke-Expression` gives
tab completion.

### Docs update (mandatory when user-facing)

- `README.md`: Windows install (zip from Releases, put `copilot-sessions.exe` on `PATH`),
  `completion powershell` (line 36 becomes `bash|zsh|fish|powershell`; add it to `$PROFILE`), the WSL boundary (decision 8), and the
  `--include-ongoing` caveat (decision 5).
- `development/spec.md:14` and `AGENTS.md` *Platform*: add Windows (Toolhelp32 +
  `NtQueryInformationProcess`) and note that live-process tests also run on Windows CI.
- `CHANGELOG.md` `[Unreleased]` → `### Added` (created by CS-009 if unreleased): "Windows support: native process detection,
  PowerShell completion and windows zip archives (CS-010)."

### Finish (mandatory)

1. Acceptance test green; `just lint`, `just test` and `just test-linux` clean; CI
   `test-windows` green.
2. Docs updated.
3. Summary: files touched, decisions, and the prerequisite-2 observation.
4. Tidy the WIP commits into atomic ones and keep that history, e.g.:
   ```
   feat(session): detect Copilot processes on Windows (CS-010)
   test: run live-process and restore-safety tests on Windows (CS-010)
   ci: build and test on Windows, ship zip archives (CS-010)
   ```
5. Commit locally, no push. `pickle ticket move CS-010 in-review --reason "acceptance green"`.

## Review

<!-- empty until IN REVIEW -->

## History

- 2026-11-08 — created (TO DO). source: review: CS-008 platform exploration; Windows split from CS-009 at user request
- 2026-11-08 — TO DO → READY: plan complete
- 2026-11-09 — re-refined after CS-013 (stays READY): line refs and restore test seam updated to the post-CS-013 tree; added decision 10 + task 2b (Unix-permission tests skip on Windows); approach otherwise unchanged
- 2026-11-09 — note (CS-009 applicability gate F7): Copilot CLI 1.0.x runs as a native per-platform binary (`@github/copilot-win32-<arch>/copilot.exe`), not `node.exe`. Re-check decision 3's rationale at pickup; `ExeFile` may suffice, but argv fails safe (see CS-009 F1).
- 2026-11-09 — impact sweep (CS-009 review): `testfix.startCopilotHelper` now symlinks `sleep` (macOS killed a copy on code-signature mismatch). Task 2's Windows branch should keep copying `PING.EXE`, because Windows symlinks need privilege. `session_other.go` is now `//go:build !linux && !darwin`, so decision 1 narrows it to `&& !windows`. `argvHasCopilot`/`procargsArgv` are darwin-only and not reused.
