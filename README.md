# copilot-sessions

`copilot-sessions` manages saved GitHub Copilot CLI sessions: it lists, cleans,
backs up and restores them.

It reads the session directories, the `session-store.db` index and the
UI state under `COPILOT_HOME` (default `~/.copilot`), and shows each session's
status:

- `ongoing`: a live `copilot` process holds the session (busy or idle).
- `stale`: the session has an in-use lock left by a process that has exited.
- `closed`: the session is not in use.

It runs on Linux (tested on WSL2) and macOS. Status detection reads `/proc` on Linux
and uses `sysctl` on macOS. Windows is not supported yet.

## Usage

```bash
copilot-sessions list                 # all sessions with their status
copilot-sessions clean -n <filters>   # dry run: show what would be deleted
copilot-sessions backup               # archive sessions to ~/.copilot-backups
copilot-sessions restore latest       # replace current sessions with the newest backup
```

- `list` shows sessions and their status. It takes filters, `--json` and `-q`.
- `clean` deletes sessions by ID or filter. `-n` does a dry run.
- `backup` creates backups, lists them (`--list`) and rotates them (`--keep N`).
- `restore` replaces the current sessions with a backup file or `latest`.

Backups go to `COPILOT_BACKUP_DIR` (default `~/.copilot-backups`). Run
`copilot-sessions <command> --help` for the filters and flags.

For scripts, `-q` prints only IDs or paths on stdout; prompts and notes go to stderr.

Shell completion: `copilot-sessions completion bash|zsh|fish`.

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
terminal).

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
