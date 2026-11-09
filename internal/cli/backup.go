package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/codcod/copilot-sessions/internal/backup"
	"github.com/codcod/copilot-sessions/internal/session"
)

type backupOpts struct {
	List, NoCreate, Yes, JSON, Quiet bool
	Keep                             int
}

func newBackup(env *Env) *cobra.Command {
	var o backupOpts
	cmd := &cobra.Command{
		Use:   "backup [FILE]",
		Short: "Create, list or rotate backups",
		Long:  "Create, list or rotate backups.\n\nFILE: output file (default: timestamped file in the backup dir).",
		Example: `  copilot-sessions backup
  copilot-sessions backup --list
  copilot-sessions backup --keep 7 -y -q`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			hasKeep := cmd.Flags().Changed("keep")
			if o.List {
				if len(args) > 0 || hasKeep || o.NoCreate {
					return usageError{errors.New("--list cannot be combined with FILE, --keep or --no-create")}
				}
				return runBackupList(env, o)
			}
			switch {
			case o.JSON:
				return usageError{errors.New("--json only works with --list")}
			case o.NoCreate && !hasKeep:
				return usageError{errors.New("--no-create requires --keep N")}
			case hasKeep && o.Keep < 1:
				return usageError{errors.New("--keep must be at least 1")}
			}
			if !o.NoCreate {
				target := filepath.Join(env.BackupDir, "copilot-sessions-"+env.Now().Local().Format("20060102-150405")+".tar.gz")
				if len(args) > 0 {
					target = expandUser(args[0])
				}
				if err := createBackup(ctx, env, target, len(args) > 0, o); err != nil {
					return err
				}
			}
			if hasKeep {
				return applyRetention(ctx, env, o)
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVarP(&o.List, "list", "l", false, "list backups, newest first")
	fl.IntVarP(&o.Keep, "keep", "k", 0, "keep only the newest `N` backups in the backup dir")
	fl.BoolVar(&o.NoCreate, "no-create", false, "with --keep: only rotate, do not create a backup")
	fl.BoolVarP(&o.Yes, "yes", "y", false, "skip confirmation when deleting old backups")
	fl.BoolVar(&o.JSON, "json", false, "with --list: print JSON")
	fl.BoolVarP(&o.Quiet, "quiet", "q", false, "print only the backup path")
	return cmd
}

// createdLayout is RFC 3339 with a numeric offset, "+00:00" rather than "Z" in UTC.
const createdLayout = "2006-01-02T15:04:05-07:00"

func runBackupList(env *Env, o backupOpts) error {
	backups, err := backup.List(env.BackupDir)
	if err != nil {
		return err
	}
	w := env.Stdout
	switch {
	case o.JSON:
		type row struct {
			File    string `json:"file"`
			Created string `json:"created"`
			Size    int64  `json:"size"`
		}
		rows := make([]row, 0, len(backups))
		for _, b := range backups {
			rows = append(rows, row{b, backup.Created(b).Format(createdLayout), fileSize(b)})
		}
		return newJSONEncoder(w).Encode(rows)
	case len(backups) == 0:
		fmt.Fprintf(w, "No backups in %s\n", env.BackupDir)
	default:
		printBackups(w, backups)
	}
	return nil
}

func fileSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// printBackups prints the numbered backup table; restore's picker reuses it.
func printBackups(w io.Writer, backups []string) {
	fmt.Fprintf(w, "  %2s  %-16s  %6s  FILE\n", "#", "CREATED", "SIZE")
	for i, b := range backups {
		fmt.Fprintf(w, "  %2d  %s  %6s  %s\n", i+1, backup.Created(b).Format("2006-01-02 15:04"), session.HumanSize(fileSize(b)), b)
	}
}

func createBackup(ctx context.Context, env *Env, target string, overwrite bool, o backupOpts) error {
	if err := backup.Create(ctx, env.Home, target, overwrite); err != nil {
		return err
	}
	if o.Quiet {
		fmt.Fprintln(env.Stdout, target)
		return nil
	}
	dirs, err := session.Dirs(env.Home)
	if err != nil {
		// The backup exists; report it anyway and let --keep run.
		fmt.Fprintf(env.Stderr, "warning: %v\n", err)
		fmt.Fprintf(env.Stdout, "Backup created: %s (%s)\n", target, session.HumanSize(fileSize(target)))
		return nil
	}
	fmt.Fprintf(env.Stdout, "Backup created: %s (%s, %s)\n", target, plural(len(dirs), "session"), session.HumanSize(fileSize(target)))
	return nil
}

func applyRetention(ctx context.Context, env *Env, o backupOpts) error {
	keep := o.Keep
	backups, err := backup.List(env.BackupDir)
	if err != nil {
		return err
	}
	w := env.Stdout
	if len(backups) <= keep {
		if !o.Quiet {
			fmt.Fprintf(w, "Nothing to remove: at most %d backups in %s.\n", keep, env.BackupDir)
		}
		return nil
	}
	old := backups[keep:]
	if willPrompt(env, o.Yes) {
		// Context for the prompt, so stderr, and only when the prompt is shown.
		fmt.Fprintf(env.Stderr, "Backups to remove (keeping the newest %d):\n", keep)
		printBackups(env.Stderr, old)
	}
	ok, err := confirm(ctx, env, fmt.Sprintf("Delete %s?", plural(len(old), "old backup")), o.Yes)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(env.Stderr, "Aborted.")
		return nil
	}
	// Last chance to cancel: once removal starts it runs to completion.
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, b := range old {
		if err := os.Remove(b); err != nil {
			return err
		}
	}
	if !o.Quiet {
		fmt.Fprintf(w, "Deleted %s.\n", plural(len(old), "old backup"))
	}
	return nil
}
