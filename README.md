# copilot-sessions

`copilot-sessions` manages saved Github Copilot CLI sessions: lists, cleans,
backs up and restores 
them.

It reads the session directories, the `session-store.db` index and the
UI state under `COPILOT_HOME` (default `~/.copilot`), and shows each session's
status:

- `ongoing`: a live `copilot` process holds the session (busy or idle).
- `stale`: the session has an in-use lock left by a process that has exited.
- `closed`: the session is not in use.

It runs on Linux and has been tested on WSL2. Status detection reads `/proc`, so
macOS and Windows are not supported yet.

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

Shell completion: `copilot-sessions completion bash|zsh|fish`.

## Install

### apt (Debian, Ubuntu, WSL2)

Packages are hosted on
[Cloudsmith](https://cloudsmith.io/~codcod/repos/copilot-sessions/).

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

Download `copilot-sessions_<version>_linux_<arch>.tar.gz` (`<arch>` is `amd64`
or `arm64`) and `checksums.txt` from
[GitHub Releases](https://github.com/codcod/copilot-sessions/releases). Then
verify the archive and unpack it into `~/.local/bin`:

```bash
sha256sum --ignore-missing -c checksums.txt
mkdir -p ~/.local/bin
tar xzf copilot-sessions_<version>_linux_<arch>.tar.gz -C ~/.local/bin copilot-sessions
```

### With Go

```bash
go install github.com/codcod/copilot-sessions/cmd/copilot-sessions@latest
```

### From a checkout

```bash
just install   # builds and installs ~/.local/bin/copilot-sessions
```

If you install with apt (`/usr/bin`) and with one of the other methods
(`~/.local/bin`), whichever directory comes first on `PATH` wins. Use
`copilot-sessions --version` to check which copy runs.
