#!/usr/bin/env python3
"""Manage GitHub Copilot CLI sessions: list, clean, backup, restore.

Usage:
  copilot-sessions list|ls [ID...] [FILTERS] [--json] [-q]
  copilot-sessions clean|rm [ID...] [FILTERS] [--include-ongoing] [-n] [-y] [-q]
  copilot-sessions clean|rm --all [--include-ongoing] [-n] [-y] [-q]
  copilot-sessions backup [FILE] [-k N] [-y] [-q]
  copilot-sessions backup --list [--json]
  copilot-sessions backup --keep N --no-create [-y]
  copilot-sessions restore [FILE|latest] [-y]

Filters (combined with AND, also with ID prefixes):
  -u, --until DATE      last updated on or before DATE (inclusive)
  -S, --since DATE      last updated on or after DATE (inclusive)
  -s, --status LIST     comma-separated: ongoing, stale, closed
  -c, --cwd PATH        working directory is PATH or below it
  -g, --grep TEXT       name contains TEXT (case-insensitive)
  -e, --empty           no user message was ever sent (Copilot was opened and
                        closed without a prompt; these sessions have no title)

DATE is YYYY-MM-DD, today, yesterday, or Nd / Nw / Nm (N days / weeks /
months before today). Dates use the local timezone and updated_at.

Status:
  ongoing  a live copilot process holds the session (busy or idle)
  stale    the session has an inuse lock left by a dead process
  closed   the session is not in use

Clean deletes session directories, their rows in session-store.db and their UI
state entries. It requires a filter, an ID or --all. It skips ongoing sessions
unless you pass --include-ongoing or --status with ongoing. It never deletes
the session that runs the command.

Backup covers session-state/, session-store.db (consistent snapshot),
open-sessions-state.json and sidebar-sessions-state/. Logs are not included.
Restore replaces all current sessions with the backup content. Without a FILE,
restore asks you to pick a backup.
"""

import argparse
import datetime as dt
import json
import os
import shutil
import sqlite3
import sys
import tarfile
import tempfile
from pathlib import Path

COPILOT_HOME = Path(os.environ.get("COPILOT_HOME", Path.home() / ".copilot"))
BACKUP_DIR = Path(os.environ.get("COPILOT_BACKUP_DIR", Path.home() / ".copilot-backups"))

STATE_DIR = "session-state"
DB_FILE = "session-store.db"
OPEN_STATE = "open-sessions-state.json"
SIDEBAR_DIR = "sidebar-sessions-state"
BACKUP_ITEMS = [STATE_DIR, DB_FILE, OPEN_STATE, SIDEBAR_DIR]
BACKUP_GLOB = "copilot-sessions-*.tar.gz"
LOCKS_DIR = ".session-operation-locks"
STATUSES = ("ongoing", "stale", "closed")
DB_SESSION_TABLES = [
    "search_index",
    "turns",
    "checkpoints",
    "session_files",
    "session_refs",
    "forge_trajectory_events",
    "assistant_usage_events",
]
EPOCH = dt.datetime.min.replace(tzinfo=dt.timezone.utc)


def die(msg):
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(1)


def confirm(prompt, assume_yes):
    if assume_yes:
        return True
    if not sys.stdin.isatty():
        die("confirmation required; rerun with --yes")
    return input(f"{prompt} [y/N] ").strip().lower() in ("y", "yes")


def plural(n, word):
    return f"{n} {word}{'' if n == 1 else 's'}"


def human_size(n):
    for unit in ("B", "K", "M", "G"):
        if n < 1024 or unit == "G":
            return f"{n:.0f}{unit}" if unit == "B" else f"{n:.1f}{unit}"
        n /= 1024


def dir_size(path):
    total = 0
    for root, _dirs, files in os.walk(path):
        for f in files:
            try:
                total += os.lstat(os.path.join(root, f)).st_size
            except OSError:
                pass
    return total


# ---------------------------------------------------------------- dates

def parse_iso(value):
    if not value:
        return None
    try:
        return dt.datetime.fromisoformat(value.strip().strip("'\"").replace("Z", "+00:00"))
    except ValueError:
        return None


def local_date(ts):
    if ts.tzinfo is None:
        ts = ts.replace(tzinfo=dt.timezone.utc)
    return ts.astimezone().date()


def months_before(day, n):
    month_index = day.year * 12 + day.month - 1 - n
    year, month = divmod(month_index, 12)
    month += 1
    for d in (day.day, 30, 29, 28):
        try:
            return dt.date(year, month, min(day.day, d))
        except ValueError:
            continue


def parse_date(value):
    """Argparse type: YYYY-MM-DD, today, yesterday, Nd, Nw, Nm."""
    v = value.strip().lower()
    today = dt.date.today()
    if v == "today":
        return today
    if v == "yesterday":
        return today - dt.timedelta(days=1)
    if len(v) > 1 and v[:-1].isdigit() and v[-1] in "dwm":
        n = int(v[:-1])
        if v[-1] == "d":
            return today - dt.timedelta(days=n)
        if v[-1] == "w":
            return today - dt.timedelta(weeks=n)
        return months_before(today, n)
    try:
        return dt.date.fromisoformat(v)
    except ValueError:
        raise argparse.ArgumentTypeError(
            f"invalid date '{value}'; use YYYY-MM-DD, today, yesterday, Nd, Nw or Nm"
        )


def parse_statuses(value):
    """Argparse type: comma-separated statuses."""
    statuses = {v.strip().lower() for v in value.split(",") if v.strip()}
    invalid = statuses - set(STATUSES)
    if invalid or not statuses:
        raise argparse.ArgumentTypeError(
            f"invalid status '{value}'; use a comma-separated list of {', '.join(STATUSES)}"
        )
    return statuses


# ---------------------------------------------------------------- sessions

def read_workspace(session_dir):
    meta = {}
    path = session_dir / "workspace.yaml"
    if path.is_file():
        for line in path.read_text(errors="replace").splitlines():
            key, sep, value = line.partition(":")
            if sep and not line.startswith(" "):
                meta[key.strip()] = value.strip()
    return meta


def db_connect():
    db = COPILOT_HOME / DB_FILE
    if not db.is_file():
        return None
    return sqlite3.connect(db, timeout=30)


def session_dirs():
    state = COPILOT_HOME / STATE_DIR
    if not state.is_dir():
        return []
    return [d for d in state.iterdir() if d.is_dir() and not d.name.startswith(".")]


def pid_is_copilot(pid):
    try:
        return b"copilot" in Path(f"/proc/{pid}/cmdline").read_bytes()
    except OSError:
        return False


def ancestor_pids():
    pids, pid = set(), os.getpid()
    while pid > 1:
        pids.add(pid)
        try:
            stat = Path(f"/proc/{pid}/stat").read_text()
            pid = int(stat.rsplit(")", 1)[1].split()[1])
        except (OSError, ValueError, IndexError):
            break
    return pids


def lock_pids(session_dir):
    """PIDs from inuse.<pid>.lock files; None if there are no lock files."""
    locks = list(session_dir.glob("inuse.*.lock")) if session_dir else []
    if not locks:
        return None
    parts = (lock.name.split(".")[1] for lock in locks)
    return {int(p) for p in parts if p.isdigit()}


def session_status(pids):
    if pids is None:
        return "closed"
    return "ongoing" if any(pid_is_copilot(p) for p in pids) else "stale"


def read_open_state():
    path = COPILOT_HOME / OPEN_STATE
    try:
        return json.loads(path.read_text() or "{}")
    except (OSError, ValueError):
        return {}


def collect_sessions(with_size=False):
    """Return {session_id: {updated, name, cwd, dir, status, busy, current, size}}."""
    sessions = {}
    for d in session_dirs():
        meta = read_workspace(d)
        updated = parse_iso(meta.get("updated_at")) or parse_iso(meta.get("created_at"))
        if updated is None:
            updated = dt.datetime.fromtimestamp(d.stat().st_mtime, dt.timezone.utc)
        sessions[d.name] = {
            "updated": updated, "name": meta.get("name", ""), "cwd": meta.get("cwd", ""), "dir": d,
        }
    conn = db_connect()
    if conn:
        with conn:
            for sid, summary, cwd, created, updated in conn.execute(
                "SELECT id, summary, cwd, created_at, updated_at FROM sessions"
            ):
                ts = parse_iso(updated) or parse_iso(created)
                entry = sessions.setdefault(sid, {"updated": ts, "name": "", "cwd": "", "dir": None})
                entry["updated"] = entry["updated"] or ts
                entry["name"] = entry["name"] or summary or ""
                entry["cwd"] = entry["cwd"] or cwd or ""
        conn.close()
    open_state = read_open_state()
    me = ancestor_pids()
    for sid, s in sessions.items():
        pids = lock_pids(s["dir"])
        s["status"] = session_status(pids)
        s["busy"] = bool(open_state.get(sid, {}).get("working"))
        s["current"] = bool(pids and me & pids)
        if with_size:
            s["size"] = dir_size(s["dir"]) if s["dir"] else 0
    return sessions


def resolve_ids(sessions, prefixes):
    ids = set()
    for p in prefixes:
        matches = [sid for sid in sessions if sid.startswith(p)]
        if not matches:
            die(f"no session matches ID '{p}'")
        if len(matches) > 1:
            die(f"ID '{p}' is ambiguous: {', '.join(sorted(matches)[:5])}")
        ids.add(matches[0])
    return ids


def under(path, root):
    path = os.path.normpath(path)
    return path == root or path.startswith(root.rstrip("/") + "/")


def has_filters(args):
    return bool(args.ids or args.until or args.since or args.status or args.cwd or args.grep or args.empty)


def sessions_with_turns():
    conn = db_connect()
    if not conn:
        return set()
    with conn:
        ids = {r[0] for r in conn.execute("SELECT DISTINCT session_id FROM turns")}
    conn.close()
    return ids


def has_user_message(session_dir):
    events = session_dir / "events.jsonl" if session_dir else None
    if not events or not events.is_file():
        return False
    with events.open(errors="replace") as f:
        return any('"type":"user.message"' in line for line in f)


def is_empty(sid, s, turns):
    return sid not in turns and not has_user_message(s["dir"])


def apply_filters(sessions, args):
    selected = dict(sessions)
    if args.ids:
        wanted = resolve_ids(sessions, args.ids)
        selected = {sid: s for sid, s in selected.items() if sid in wanted}
    if args.until:
        selected = {k: s for k, s in selected.items() if s["updated"] and local_date(s["updated"]) <= args.until}
    if args.since:
        selected = {k: s for k, s in selected.items() if s["updated"] and local_date(s["updated"]) >= args.since}
    if args.status:
        selected = {k: s for k, s in selected.items() if s["status"] in args.status}
    if args.cwd:
        root = os.path.normpath(os.path.abspath(os.path.expanduser(args.cwd)))
        selected = {k: s for k, s in selected.items() if s["cwd"] and under(s["cwd"], root)}
    if args.grep:
        needle = args.grep.lower()
        selected = {k: s for k, s in selected.items() if needle in s["name"].lower()}
    if args.empty:
        turns = sessions_with_turns()
        selected = {k: s for k, s in selected.items() if is_empty(k, s, turns)}
    return selected


def describe_filters(args):
    parts = []
    if args.ids:
        parts.append(f"IDs {', '.join(args.ids)}")
    if args.since:
        parts.append(f"updated on or after {args.since}")
    if args.until:
        parts.append(f"updated on or before {args.until}")
    if args.status:
        parts.append(f"status {'/'.join(sorted(args.status))}")
    if args.cwd:
        parts.append(f"cwd under {args.cwd}")
    if args.grep:
        parts.append(f"name contains '{args.grep}'")
    if args.empty:
        parts.append("empty")
    return "; ".join(parts) or "all sessions"


def format_status(s):
    if s["status"] == "ongoing":
        return f"ongoing ({'busy' if s['busy'] else 'idle'})"
    return s["status"]


def sorted_sessions(sessions):
    return sorted(sessions.items(), key=lambda kv: kv[1]["updated"] or EPOCH)


def print_sessions(sessions):
    show_size = any("size" in s for s in sessions.values())
    header = f"  {'UPDATED':<16}  {'STATUS':<14}  " + (f"{'SIZE':>6}  " if show_size else "") + f"{'ID':<36}  NAME"
    print(header)
    for sid, s in sorted_sessions(sessions):
        when = s["updated"].astimezone().strftime("%Y-%m-%d %H:%M") if s["updated"] else "unknown"
        size = (f"{human_size(s['size']) if s['dir'] else '-':>6}  ") if show_size else ""
        notes = (" (this session)" if s["current"] else "") + ("" if s["dir"] else " (db only)")
        print(f"  {when:<16}  {format_status(s):<14}  {size}{sid}  {s['name'][:60]}{notes}")


def session_json(sid, s):
    return {
        "id": sid,
        "name": s["name"],
        "cwd": s["cwd"],
        "updated_at": s["updated"].isoformat() if s["updated"] else None,
        "status": s["status"],
        "busy": s["busy"] if s["status"] == "ongoing" else None,
        "current": s["current"],
        "size": s.get("size"),
        "db_only": s["dir"] is None,
    }


# ---------------------------------------------------------------- list

def cmd_list(args):
    sessions = collect_sessions(with_size=not args.quiet)
    selected = apply_filters(sessions, args)
    if args.json:
        print(json.dumps([session_json(k, s) for k, s in sorted_sessions(selected)], indent=2))
        return
    if args.quiet:
        for sid, _s in sorted_sessions(selected):
            print(sid)
        return
    if not selected:
        print("No sessions match.")
        return
    print_sessions(selected)
    counts = {st: sum(s["status"] == st for s in selected.values()) for st in STATUSES}
    summary = ", ".join(f"{n} {st}" for st, n in counts.items() if n)
    total = human_size(sum(s["size"] for s in selected.values()))
    print(f"{len(selected)} of {len(sessions)} sessions ({summary}), {total}.")


# ---------------------------------------------------------------- clean

def prune_db(ids):
    conn = db_connect()
    if not conn or not ids:
        return
    with conn:
        conn.execute("CREATE TEMP TABLE prune_ids (id TEXT PRIMARY KEY)")
        conn.executemany("INSERT OR IGNORE INTO prune_ids VALUES (?)", [(i,) for i in ids])
        existing = {r[0] for r in conn.execute("SELECT name FROM sqlite_master")}
        for table in DB_SESSION_TABLES:
            if table in existing:
                conn.execute(f"DELETE FROM {table} WHERE session_id IN (SELECT id FROM prune_ids)")
        conn.execute("DELETE FROM sessions WHERE id IN (SELECT id FROM prune_ids)")
    conn.close()


def write_json(path, data):
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(data, indent=2) + "\n")
    os.chmod(tmp, path.stat().st_mode & 0o777)
    tmp.replace(path)


def prune_ui_state(ids, everything=False):
    open_state = COPILOT_HOME / OPEN_STATE
    if open_state.is_file():
        data = json.loads(open_state.read_text() or "{}")
        kept = {k: v for k, v in data.items() if not everything and k not in ids}
        if len(kept) != len(data):
            write_json(open_state, kept)
    sidebar = COPILOT_HOME / SIDEBAR_DIR
    if sidebar.is_dir():
        for f in sidebar.glob("*.json"):
            data = json.loads(f.read_text() or "{}")
            sids = data.get("sessionIds")
            if isinstance(sids, list):
                kept = [s for s in sids if not everything and s not in ids]
                if len(kept) != len(sids):
                    data["sessionIds"] = kept
                    write_json(f, data)


def cmd_clean(args):
    if args.all and has_filters(args):
        die("--all cannot be combined with IDs or filters")
    if not args.all and not has_filters(args):
        die("specify session IDs, a filter (--until, --since, --status, --cwd, --grep, --empty) or --all")

    sessions = collect_sessions(with_size=not args.quiet)
    selected = apply_filters(sessions, args)

    notes = []
    current = [k for k, s in selected.items() if s["current"]]
    if current:
        notes.append("Skipped this session (it runs this command).")
    include_ongoing = args.include_ongoing or (args.status and "ongoing" in args.status)
    ongoing = [k for k, s in selected.items() if s["status"] == "ongoing" and not s["current"]]
    if ongoing and not include_ongoing:
        notes.append(f"Skipped {plural(len(ongoing), 'ongoing session')} (use --include-ongoing).")
    else:
        ongoing = []
    selected = {k: s for k, s in selected.items() if k not in current and k not in ongoing}

    for n in notes:
        print(n)
    if not selected:
        print("No sessions match.")
        return

    if not args.quiet:
        print_sessions(selected)
    scope = "all sessions" if args.all else describe_filters(args)
    size = "" if args.quiet else f", {human_size(sum(s['size'] for s in selected.values()))}"
    print(f"{len(selected)} of {len(sessions)} sessions selected ({scope}{size}).")

    if args.dry_run:
        print("Dry run: nothing deleted.")
        return
    if not confirm("Permanently delete these sessions?", args.yes):
        print("Aborted.")
        return

    ids = set(selected)
    for s in selected.values():
        if s["dir"]:
            shutil.rmtree(s["dir"], ignore_errors=True)
    prune_db(ids)
    prune_ui_state(ids, everything=ids == set(sessions))
    print(f"Deleted {plural(len(ids), 'session')}.")


# ---------------------------------------------------------------- backup / restore

def skip_locks(info):
    return None if info.name.startswith(f"{STATE_DIR}/{LOCKS_DIR}") else info


def list_backups():
    """Backups in BACKUP_DIR, newest first."""
    return sorted(BACKUP_DIR.glob(BACKUP_GLOB), reverse=True) if BACKUP_DIR.is_dir() else []


def backup_time(path):
    stamp = path.name[len("copilot-sessions-"):-len(".tar.gz")]
    try:
        return dt.datetime.strptime(stamp, "%Y%m%d-%H%M%S")
    except ValueError:
        return dt.datetime.fromtimestamp(path.stat().st_mtime)


def print_backups(backups):
    print(f"  {'#':>2}  {'CREATED':<16}  {'SIZE':>6}  FILE")
    for i, b in enumerate(backups, 1):
        print(f"  {i:>2}  {backup_time(b):%Y-%m-%d %H:%M}  {human_size(b.stat().st_size):>6}  {b}")


def create_backup(target, quiet):
    target.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as tmp:
        snapshot = Path(tmp) / DB_FILE
        src = db_connect()
        if src:
            dst = sqlite3.connect(snapshot)
            src.backup(dst)
            dst.close()
            src.close()
        with tarfile.open(target, "w:gz") as tar:
            for item in BACKUP_ITEMS:
                path = snapshot if item == DB_FILE else COPILOT_HOME / item
                if path.exists():
                    tar.add(path, arcname=item, filter=skip_locks)
    os.chmod(target, 0o600)
    if quiet:
        print(target)
    else:
        print(f"Backup created: {target} ({plural(len(session_dirs()), 'session')}, {human_size(target.stat().st_size)})")


def apply_retention(keep, assume_yes, quiet):
    old = list_backups()[keep:]
    if not old:
        if not quiet:
            print(f"Nothing to remove: at most {keep} backups in {BACKUP_DIR}.")
        return
    if not quiet:
        print(f"Backups to remove (keeping the newest {keep}):")
        print_backups(old)
    if not confirm(f"Delete {plural(len(old), 'old backup')}?", assume_yes):
        print("Aborted.")
        return
    for b in old:
        b.unlink()
    print(f"Deleted {plural(len(old), 'old backup')}.")


def cmd_backup(args):
    if args.list:
        if args.file or args.keep is not None or args.no_create:
            die("--list cannot be combined with FILE, --keep or --no-create")
        backups = list_backups()
        if args.json:
            print(json.dumps([
                {"file": str(b), "created": backup_time(b).isoformat(), "size": b.stat().st_size}
                for b in backups
            ], indent=2))
        elif not backups:
            print(f"No backups in {BACKUP_DIR}")
        else:
            print_backups(backups)
        return
    if args.json:
        die("--json only works with --list")
    if args.no_create and args.keep is None:
        die("--no-create requires --keep N")
    if args.keep is not None and args.keep < 1:
        die("--keep must be at least 1")

    if not args.no_create:
        if args.file:
            target = Path(args.file).expanduser()
        else:
            target = BACKUP_DIR / f"copilot-sessions-{dt.datetime.now():%Y%m%d-%H%M%S}.tar.gz"
        create_backup(target, args.quiet)
    if args.keep is not None:
        apply_retention(args.keep, args.yes, args.quiet)


def pick_backup():
    backups = list_backups()
    if not backups:
        die(f"no backups in {BACKUP_DIR}")
    if not sys.stdin.isatty():
        die("no backup given; pass FILE or 'latest'")
    print_backups(backups)
    choice = input("Restore which backup? [1] ").strip() or "1"
    if not choice.isdigit() or not 1 <= int(choice) <= len(backups):
        die(f"invalid choice '{choice}'")
    return backups[int(choice) - 1]


def cmd_restore(args):
    if args.file is None:
        source = pick_backup()
    elif args.file == "latest":
        backups = list_backups()
        if not backups:
            die(f"no backups in {BACKUP_DIR}")
        source = backups[0]
    else:
        source = Path(args.file).expanduser()
    if not source.is_file():
        die(f"backup not found: {source}")

    with tarfile.open(source, "r:gz") as tar:
        names = tar.getnames()
        if STATE_DIR not in names and DB_FILE not in names:
            die(f"{source} does not look like a copilot-sessions backup")
        backup_sessions = {
            n.split("/")[1] for n in names
            if n.startswith(f"{STATE_DIR}/") and not n.split("/")[1].startswith(".")
        }
        current_count = len(session_dirs())

        print("WARNING: restore REPLACES all current Copilot sessions.")
        print(f"  Backup:  {source} ({plural(len(backup_sessions), 'session')})")
        print(f"  Current: {COPILOT_HOME} ({plural(current_count, 'session')} will be deleted)")
        print("  Close all running Copilot CLI sessions first; they may overwrite restored data.")
        if not confirm("Continue?", args.yes):
            print("Aborted.")
            return

        with tempfile.TemporaryDirectory(dir=COPILOT_HOME) as tmp:
            tar.extractall(tmp, filter="data")
            for item in BACKUP_ITEMS:
                dest = COPILOT_HOME / item
                if dest.is_dir():
                    shutil.rmtree(dest)
                elif dest.exists():
                    dest.unlink()
                if item == DB_FILE:
                    for suffix in ("-wal", "-shm"):
                        Path(f"{dest}{suffix}").unlink(missing_ok=True)
                extracted = Path(tmp) / item
                if extracted.exists():
                    extracted.rename(dest)
    print(f"Restored {plural(len(backup_sessions), 'session')} from {source}")


# ---------------------------------------------------------------- CLI

def add_filters(p):
    p.add_argument("ids", nargs="*", metavar="ID", help="session ID or unique ID prefix")
    p.add_argument("-u", "--until", type=parse_date, metavar="DATE",
                   help="last updated on or before DATE (inclusive)")
    p.add_argument("-S", "--since", type=parse_date, metavar="DATE",
                   help="last updated on or after DATE (inclusive)")
    p.add_argument("-s", "--status", type=parse_statuses, metavar="LIST",
                   help="comma-separated: ongoing, stale, closed")
    p.add_argument("-c", "--cwd", metavar="PATH", help="working directory is PATH or below it")
    p.add_argument("-g", "--grep", metavar="TEXT", help="name contains TEXT (case-insensitive)")
    p.add_argument("-e", "--empty", action="store_true",
                   help="no user message was ever sent (opened and closed without a prompt)")


def main():
    parser = argparse.ArgumentParser(
        prog="copilot-sessions",
        description="List, clean, back up and restore GitHub Copilot CLI sessions.",
        epilog="DATE: YYYY-MM-DD, today, yesterday, Nd, Nw, Nm. "
               f"Copilot home: {COPILOT_HOME} (COPILOT_HOME). Backups: {BACKUP_DIR} (COPILOT_BACKUP_DIR).",
    )
    sub = parser.add_subparsers(dest="command", required=True, metavar="COMMAND")

    p = sub.add_parser("list", aliases=["ls"], help="list sessions with their status")
    add_filters(p)
    p.add_argument("--json", action="store_true", help="print JSON")
    p.add_argument("-q", "--quiet", action="store_true", help="print session IDs only")
    p.set_defaults(func=cmd_list)

    p = sub.add_parser("clean", aliases=["rm"], help="delete sessions")
    add_filters(p)
    p.add_argument("-a", "--all", action="store_true", help="select all sessions")
    p.add_argument("--include-ongoing", action="store_true", help="also delete ongoing sessions")
    p.add_argument("-n", "--dry-run", action="store_true", help="only show what would be deleted")
    p.add_argument("-y", "--yes", action="store_true", help="skip confirmation")
    p.add_argument("-q", "--quiet", action="store_true", help="do not list the selected sessions")
    p.set_defaults(func=cmd_clean)

    p = sub.add_parser("backup", help="create, list or rotate backups")
    p.add_argument("file", nargs="?", help="output file (default: timestamped file in the backup dir)")
    p.add_argument("-l", "--list", action="store_true", help="list backups, newest first")
    p.add_argument("-k", "--keep", type=int, metavar="N", help="keep only the newest N backups in the backup dir")
    p.add_argument("--no-create", action="store_true", help="with --keep: only rotate, do not create a backup")
    p.add_argument("-y", "--yes", action="store_true", help="skip confirmation when deleting old backups")
    p.add_argument("--json", action="store_true", help="with --list: print JSON")
    p.add_argument("-q", "--quiet", action="store_true", help="print only the backup path")
    p.set_defaults(func=cmd_backup)

    p = sub.add_parser("restore", help="replace current sessions with a backup")
    p.add_argument("file", nargs="?", help="backup file or 'latest' (default: pick from a list)")
    p.add_argument("-y", "--yes", action="store_true", help="skip confirmation")
    p.set_defaults(func=cmd_restore)

    args = parser.parse_args()
    try:
        args.func(args)
    except KeyboardInterrupt:
        print("\nAborted.")
        sys.exit(130)


if __name__ == "__main__":
    main()

