package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codcod/copilot-sessions/internal/backup"
	"github.com/codcod/copilot-sessions/internal/session"
)

func newRestore(env *Env) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore [FILE|latest]",
		Short: "Replace current sessions with a backup",
		Long:  "Replace current sessions with a backup.\n\nFILE: backup file or 'latest' (default: pick from a list).",
		Example: `  copilot-sessions restore latest
  copilot-sessions restore ~/.copilot-backups/copilot-sessions-20261031-120000.tar.gz
  copilot-sessions restore`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRestore(cmd.Context(), env, args, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return cmd
}

// runRestore follows the script's cmd_restore step by step (spec §6.4).
func runRestore(ctx context.Context, env *Env, args []string, yes bool) error {
	return restore(ctx, env, args, yes, os.Rename)
}

// restore is runRestore with the rename swapItems uses; tests pass a failing one.
func restore(ctx context.Context, env *Env, args []string, yes bool, rename func(string, string) error) error {
	source, err := restoreSource(ctx, env, args)
	if err != nil {
		return err
	}
	fi, err := os.Stat(source)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("backup not found: %s", source)
	}
	if err != nil {
		return fmt.Errorf("backup %s: %w", source, err)
	}
	isBackup, n, err := backup.Inspect(ctx, source)
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
	ok, err := confirm(ctx, env, "Continue?", yes)
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
			_ = os.RemoveAll(tmp) // best effort: a leftover tmp* is not a session or an item
		}
	}()
	if err := backup.Extract(ctx, source, tmp); err != nil {
		return err
	}
	// Last chance to cancel: the swap is a few fast renames, undone only on failure.
	if err := ctx.Err(); err != nil {
		return err
	}
	if keepTmp, err = swapItems(env.Home, tmp, rename); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Restored %s from %s\n", plural(n, "session"), source)
	return nil
}

// swapItems moves each current item into <tmp>/old and the extracted one into home.
// Deliberate departure from the script, which deletes before moving: on any failure
// every move is undone, so home is left as it was; the old items go with tmp on success.
// keepTmp reports a rollback that did not finish: tmp then holds data that must survive.
// rename is os.Rename outside tests.
func swapItems(home, tmp string, rename func(string, string) error) (keepTmp bool, err error) {
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
		for _, d := range slices.Backward(done) {
			if rename(d.to, d.from) != nil {
				keepTmp = true
			}
		}
		if keepTmp {
			err = fmt.Errorf("restore failed and rollback incomplete; old data kept in %s: %w", old, err)
		} else {
			err = fmt.Errorf("restore failed, nothing changed: %w", err)
		}
	}()
	for _, item := range backup.Items() {
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
func restoreSource(ctx context.Context, env *Env, args []string) (string, error) {
	if len(args) > 0 && args[0] != "latest" {
		return expandUser(args[0]), nil
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
	line, err := readLine(ctx, env)
	if err != nil {
		return "", err
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		choice = "1"
	}
	i, ok := session.ParseUint(choice)
	if !ok || i < 1 || i > len(backups) {
		return "", fmt.Errorf("invalid choice %q", choice)
	}
	return backups[i-1], nil
}
