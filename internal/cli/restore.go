package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codcod/copilot-sessions/internal/backup"
	"github.com/codcod/copilot-sessions/internal/filter"
	"github.com/codcod/copilot-sessions/internal/session"
)

func newRestore(env Env) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore [FILE|latest]",
		Short: "Replace current sessions with a backup",
		Long:  "Replace current sessions with a backup.\n\nFILE: backup file or 'latest' (default: pick from a list).",
		Example: `  copilot-sessions restore latest
  copilot-sessions restore ~/.copilot-backups/copilot-sessions-20261031-120000.tar.gz
  copilot-sessions restore`,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			return runRestore(env, args, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return cmd
}

// rename is os.Rename; tests swap it to force a failure mid-restore.
var rename = os.Rename

// runRestore follows the script's cmd_restore step by step (spec §6.4).
func runRestore(env Env, args []string, yes bool) error {
	source, err := restoreSource(env, args)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(source); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("backup not found: %s", source)
	}
	isBackup, n, err := backup.Inspect(source)
	if err != nil {
		return err
	}
	if !isBackup {
		return fmt.Errorf("%s does not look like a copilot-sessions backup", source)
	}
	current, err := session.Dirs(env.Home)
	if err != nil {
		return err
	}
	w := env.Stderr
	fmt.Fprintln(w, "WARNING: restore REPLACES all current Copilot sessions.")
	fmt.Fprintf(w, "  Backup:  %s (%s)\n", source, plural(n, "session"))
	fmt.Fprintf(w, "  Current: %s (%s will be deleted)\n", env.Home, plural(len(current), "session"))
	fmt.Fprintln(w, "  Close all running Copilot CLI sessions first; they may overwrite restored data.")
	ok, err := confirm(env, "Continue?", yes)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(w, "Aborted.")
		return nil
	}

	// Deliberate departure from the script: a missing home is created (fresh machine).
	if err := os.MkdirAll(env.Home, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(env.Home, "tmp")
	if err != nil {
		return err
	}
	keepTmp := false
	defer func() {
		if !keepTmp {
			os.RemoveAll(tmp)
		}
	}()
	if err := backup.Extract(source, tmp); err != nil {
		return err
	}
	if keepTmp, err = swapItems(env.Home, tmp); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Restored %s from %s\n", plural(n, "session"), source)
	return nil
}

// swapItems moves each current item into <tmp>/old and the extracted one into home.
// Deliberate departure from the script, which deletes before moving: on any failure
// every move is undone, so home is left as it was; the old items go with tmp on success.
// keepTmp reports a rollback that did not finish: tmp then holds data that must survive.
func swapItems(home, tmp string) (keepTmp bool, err error) {
	old := filepath.Join(tmp, "old")
	if err := os.MkdirAll(old, 0o700); err != nil {
		return false, err
	}
	type move struct{ from, to string }
	var done []move
	mv := func(from, to string) error {
		if _, err := os.Lstat(from); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		if err := rename(from, to); err != nil {
			return err
		}
		done = append(done, move{from, to})
		return nil
	}
	defer func() {
		if err == nil {
			return
		}
		for i := len(done) - 1; i >= 0; i-- {
			if rename(done[i].to, done[i].from) != nil {
				keepTmp = true
			}
		}
		if keepTmp {
			err = fmt.Errorf("restore failed and rollback incomplete; old data kept in %s: %w", old, err)
		} else {
			err = fmt.Errorf("restore failed, nothing changed: %w", err)
		}
	}()
	for _, item := range backup.Items {
		dest := filepath.Join(home, item)
		names := []string{item}
		if item == session.DBFile {
			names = append(names, item+"-wal", item+"-shm")
		}
		for _, n := range names {
			if err := mv(filepath.Join(home, n), filepath.Join(old, n)); err != nil {
				return false, err
			}
		}
		if err := mv(filepath.Join(tmp, item), dest); err != nil {
			return false, err
		}
	}
	return false, nil
}

// restoreSource resolves FILE, latest, or the interactive pick, as the script's pick_backup does.
func restoreSource(env Env, args []string) (string, error) {
	if len(args) > 0 && args[0] != "latest" {
		return filter.ExpandUser(args[0]), nil
	}
	backups, err := backup.List(env.BackupDir)
	if err != nil {
		return "", err
	}
	if len(backups) == 0 {
		return "", fmt.Errorf("no backups in %s", env.BackupDir)
	}
	if len(args) > 0 {
		return backups[0], nil
	}
	if !env.IsTTY() {
		return "", errors.New("no backup given; pass FILE or 'latest'")
	}
	printBackups(env.Stderr, backups)
	fmt.Fprint(env.Stderr, "Restore which backup? [1] ")
	choice := strings.TrimSpace(readLine(env))
	if choice == "" {
		choice = "1"
	}
	i, err := strconv.Atoi(choice)
	if err != nil || strings.ContainsAny(choice, "+-") || i < 1 || i > len(backups) {
		return "", fmt.Errorf("invalid choice '%s'", choice)
	}
	return backups[i-1], nil
}
