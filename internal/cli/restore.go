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
		Short: "replace current sessions with a backup",
		Long:  "Replace current sessions with a backup.\n\nFILE: backup file or 'latest' (default: pick from a list).",
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
	w := env.Stdout
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
	defer os.RemoveAll(tmp)
	if err := backup.Extract(source, tmp); err != nil {
		return err
	}
	for _, item := range backup.Items {
		dest := filepath.Join(env.Home, item)
		if fi, err := os.Lstat(dest); err == nil {
			if fi.IsDir() {
				err = os.RemoveAll(dest)
			} else {
				err = os.Remove(dest)
			}
			if err != nil {
				return err
			}
		}
		if item == session.DBFile {
			for _, suffix := range []string{"-wal", "-shm"} {
				if err := os.Remove(dest + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
		extracted := filepath.Join(tmp, item)
		if _, err := os.Lstat(extracted); err == nil {
			if err := os.Rename(extracted, dest); err != nil {
				return err
			}
		}
	}
	fmt.Fprintf(w, "Restored %s from %s\n", plural(n, "session"), source)
	return nil
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
	printBackups(env.Stdout, backups)
	fmt.Fprint(env.Stdout, "Restore which backup? [1] ")
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
