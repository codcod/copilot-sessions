# copilot-sessions

`copilot-sessions` manages saved GitHub Copilot CLI sessions: it lists, cleans,
backs up and restores them.

It reads the session directories, the `session-store.db` index and the
UI state under `COPILOT_HOME` (default `~/.copilot`; on Windows `%USERPROFILE%\.copilot`), and shows each session's
status:

- `ongoing`: a live `copilot` process holds the session (busy or idle).
- `stale`: the session has an in-use lock left by a process that has exited.
- `closed`: the session is not in use.

It runs on Linux (tested on WSL2), macOS and Windows. Status detection reads `/proc` on
Linux, uses `sysctl` on macOS and the process snapshot API on Windows.

A Windows binary can't see processes inside WSL, and a WSL binary can't see Windows
processes. Sessions run on the other side therefore show as `stale`: run
`copilot-sessions` on the same side as the Copilot CLI it manages.

## Usage

```bash
copilot-sessions list                 # all sessions with their status
copilot-sessions clean -n <filters>   # dry run: show what would be deleted
copilot-sessions backup               # archive sessions to ~/.copilot-backups
copilot-sessions restore latest       # replace current sessions with the newest backup
copilot-sessions move OLD NEW         # move a project folder and repoint its sessions
```

- `list` shows sessions and their status. It takes filters, `--json` and `-q`.
- `clean` deletes sessions by ID or filter. `-n` does a dry run.
- `backup` creates backups, lists them (`--list`) and rotates them (`--keep N`).
- `restore` replaces the current sessions with a backup file or `latest`.
- `move` moves a project folder and repoints the sessions started in it. `-n` does a dry run.

Backups go to `COPILOT_BACKUP_DIR` (default `~/.copilot-backups`). Run
`copilot-sessions <command> --help` for the filters and flags.

For scripts, `-q` prints only IDs or paths on stdout; prompts and notes go to stderr.

Shell completion: `copilot-sessions completion bash|zsh|fish|powershell`. In PowerShell,
add `copilot-sessions completion powershell | Out-String | Invoke-Expression` to `$PROFILE`.

## Examples

Dates are `YYYY-MM-DD`, `today`, `yesterday`, or `Nd`/`Nw`/`Nm` (days, weeks or
months before today). `--since` and `--until` are both inclusive.

### Clear out last month's sessions, with a way back

```bash
# See what last month holds
copilot-sessions list --since 2026-10-01 --until 2026-10-31

# Back up first (a backup always holds all sessions, not just the filtered ones)
copilot-sessions backup

# Preview, then delete
copilot-sessions clean -n --since 2026-10-01 --until 2026-10-31
copilot-sessions clean --since 2026-10-01 --until 2026-10-31

# Changed your mind? Bring them back
copilot-sessions restore latest
```

`restore` replaces all current sessions with the backup: anything created or
changed after the backup is lost. Restore soon after cleaning, or back up again
before restoring an older file (`copilot-sessions backup --list` shows them).

### Remove empty sessions

Sessions that were opened and closed without a single prompt:

```bash
copilot-sessions clean -n --empty   # preview
copilot-sessions clean --empty
```

### Move a project folder

Copilot records each session's folder and never updates it. After a plain `mv`, the
`--resume` picker shows the old location and stops ranking the session first in the new folder.
`move` moves the folder and repoints its sessions, including those started in subfolders:

```bash
copilot-sessions move -n ~/src/app ~/work/app   # preview
copilot-sessions backup && copilot-sessions move ~/src/app ~/work/app
```

Already moved the folder by hand? Run the same command: when only the new folder exists,
`move` just repoints the sessions. It refuses while a session in the folder is `ongoing`,
when the new folder already exists, when the old one is itself a symlink, and across
filesystems. Session history and Copilot's
trusted folders keep the old path, so Copilot asks to trust the new folder again.

### More

```bash
# Sessions whose copilot process died and left a lock behind
copilot-sessions clean --status stale

# Everything not touched in the last 3 months
copilot-sessions clean --until 3m

# Sessions started in a project, filtered by name
copilot-sessions list --cwd ~/src/my-app --grep refactor

# Nightly backup, keeping the newest 7 (cron-friendly: no prompts, prints the path)
copilot-sessions backup --keep 7 -y -q
```

`clean` never deletes `ongoing` sessions unless you pass `--include-ongoing`. It
asks for confirmation; `-y` skips the prompt (needed when stdin is not a
terminal). On Windows, `--include-ongoing` may leave part of a session directory behind:
Windows refuses to delete files a running Copilot holds open, and `clean` carries on.
`restore` while Copilot runs either succeeds or fails and puts `COPILOT_HOME` back; if even
putting it back fails, it says where it kept the old data.

## Install

### apt (Debian, Ubuntu, WSL2)

Packages for `amd64` and `arm64` are hosted on
[Cloudsmith](https://cloudsmith.io/~codcod/repos/copilot-sessions/). They include bash,
zsh and fish completions.

With the setup script, which adds the signing key and the apt source for you:

```bash
curl -1sLf 'https://dl.cloudsmith.io/public/codcod/copilot-sessions/setup.deb.sh' | sudo -E bash
sudo apt install copilot-sessions
```

Or by hand, if you'd rather not pipe a script into `sudo bash`:

```bash
# 1. Add the signing key
curl -1sLf 'https://dl.cloudsmith.io/public/codcod/copilot-sessions/gpg.key' \
  | sudo gpg --dearmor -o /usr/share/keyrings/codcod-copilot-sessions-archive-keyring.gpg

# 2. Add the apt source
. /etc/os-release   # sets $ID (ubuntu, debian) and $VERSION_CODENAME (noble, bookworm)
echo "deb [signed-by=/usr/share/keyrings/codcod-copilot-sessions-archive-keyring.gpg] https://dl.cloudsmith.io/public/codcod/copilot-sessions/deb/$ID $VERSION_CODENAME main" \
  | sudo tee /etc/apt/sources.list.d/codcod-copilot-sessions.list

# 3. Install
sudo apt update
sudo apt install copilot-sessions
```

### From a release

Download `copilot-sessions_<version>_<os>_<arch>.tar.gz` (`<os>` is `linux` or
`darwin`, `<arch>` is `amd64` or `arm64`) and `checksums.txt` from
[GitHub Releases](https://github.com/codcod/copilot-sessions/releases). Then
verify the archive and unpack it into `~/.local/bin`:

```bash
sha256sum --ignore-missing -c checksums.txt   # macOS: shasum -a 256 --ignore-missing -c
mkdir -p ~/.local/bin
tar xzf copilot-sessions_<version>_<os>_<arch>.tar.gz -C ~/.local/bin copilot-sessions
```

The macOS binaries are not signed. If you downloaded the archive with a browser, remove the
quarantine flag first: `xattr -d com.apple.quarantine ~/.local/bin/copilot-sessions`.

On Windows, download `copilot-sessions_<version>_windows_<arch>.zip`, check it against
`checksums.txt` (`Get-FileHash <zip>`), and put `copilot-sessions.exe` in a directory on
your `PATH`.

### With Go

Needs Go 1.27 or later.

```bash
go install github.com/codcod/copilot-sessions/cmd/copilot-sessions@latest
```

### From a checkout

```bash
just install   # builds and installs ~/.local/bin/copilot-sessions
```

If you install with apt (`/usr/bin`) and with one of the other methods
(`~/.local/bin`), whichever directory comes first on `PATH` wins.
