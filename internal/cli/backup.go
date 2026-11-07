package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/codcod/copilot-sessions/internal/backup"
	"github.com/codcod/copilot-sessions/internal/filter"
	"github.com/codcod/copilot-sessions/internal/session"
)

func newBackup(env Env) *cobra.Command {
	var list, noCreate, yes, asJSON, quiet bool
	var keep int
	cmd := &cobra.Command{
		Use:   "backup [FILE]",
		Short: "create, list or rotate backups",
		Long:  "Create, list or rotate backups.\n\nFILE: output file (default: timestamped file in the backup dir).",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			hasKeep := cmd.Flags().Changed("keep")
			if list {
				if len(args) > 0 || hasKeep || noCreate {
					return errors.New("--list cannot be combined with FILE, --keep or --no-create")
				}
				return runBackupList(env, asJSON)
			}
			switch {
			case asJSON:
				return errors.New("--json only works with --list")
			case noCreate && !hasKeep:
				return errors.New("--no-create requires --keep N")
			case hasKeep && keep < 1:
				return errors.New("--keep must be at least 1")
			}
			if !noCreate {
				target := filepath.Join(env.BackupDir, "copilot-sessions-"+env.Now().Local().Format("20060102-150405")+".tar.gz")
				if len(args) > 0 {
					target = filter.ExpandUser(args[0])
				}
				if err := createBackup(env, target, len(args) > 0, quiet); err != nil {
					return err
				}
			}
			if hasKeep {
				return applyRetention(env, keep, yes, quiet)
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVarP(&list, "list", "l", false, "list backups, newest first")
	fl.IntVarP(&keep, "keep", "k", 0, "keep only the newest `N` backups in the backup dir")
	fl.BoolVar(&noCreate, "no-create", false, "with --keep: only rotate, do not create a backup")
	fl.BoolVarP(&yes, "yes", "y", false, "skip confirmation when deleting old backups")
	fl.BoolVar(&asJSON, "json", false, "with --list: print JSON")
	fl.BoolVarP(&quiet, "quiet", "q", false, "print only the backup path")
	return cmd
}

func runBackupList(env Env, asJSON bool) error {
	backups, err := backup.List(env.BackupDir)
	if err != nil {
		return err
	}
	w := env.Stdout
	switch {
	case asJSON:
		type row struct {
			File    string `json:"file"`
			Created string `json:"created"`
			Size    int64  `json:"size"`
		}
		rows := make([]row, 0, len(backups))
		for _, b := range backups {
			rows = append(rows, row{b, isoformat(backup.Created(b)), fileSize(b)})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(rows)
	case len(backups) == 0:
		fmt.Fprintf(w, "No backups in %s\n", env.BackupDir)
	default:
		printBackups(w, backups)
	}
	return nil
}

// isoformat matches Python's datetime.isoformat for a naive local time.
func isoformat(t time.Time) string {
	t = t.Round(time.Microsecond)
	s := t.Format("2006-01-02T15:04:05")
	if us := t.Nanosecond() / 1000; us != 0 {
		s += fmt.Sprintf(".%06d", us)
	}
	return s
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

func createBackup(env Env, target string, overwrite, quiet bool) error {
	if err := backup.Create(env.Home, target, overwrite); err != nil {
		return err
	}
	if quiet {
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

func applyRetention(env Env, keep int, yes, quiet bool) error {
	backups, err := backup.List(env.BackupDir)
	if err != nil {
		return err
	}
	w := env.Stdout
	if len(backups) <= keep {
		if !quiet {
			fmt.Fprintf(w, "Nothing to remove: at most %d backups in %s.\n", keep, env.BackupDir)
		}
		return nil
	}
	old := backups[keep:]
	if !quiet {
		fmt.Fprintf(w, "Backups to remove (keeping the newest %d):\n", keep)
		printBackups(w, old)
	}
	ok, err := confirm(env, fmt.Sprintf("Delete %s?", plural(len(old), "old backup")), yes)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(w, "Aborted.")
		return nil
	}
	for _, b := range old {
		if err := os.Remove(b); err != nil {
			return err
		}
	}
	fmt.Fprintf(w, "Deleted %s.\n", plural(len(old), "old backup"))
	return nil
}
