// Package cli holds the cobra commands, their output and exit codes.
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codcod/copilot-sessions/internal/filter"
	"github.com/codcod/copilot-sessions/internal/session"
)

const description = "List, clean, back up and restore GitHub Copilot CLI sessions."

// Env holds everything the commands read from the outside world, so tests can inject it.
type Env struct {
	Home, BackupDir string
	Stdin           io.Reader
	Stdout, Stderr  io.Writer
	IsTTY           func() bool
	Now             func() time.Time
	AncestorPIDs    func() map[int]bool

	in *bufio.Reader // the one buffered Stdin reader of a run; set by Execute
}

// DefaultEnv fills Env from the process environment.
func DefaultEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{
		Home:      envPath("COPILOT_HOME", filepath.Join(home, ".copilot")),
		BackupDir: envPath("COPILOT_BACKUP_DIR", filepath.Join(home, ".copilot-backups")),
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		IsTTY: func() bool {
			fi, err := os.Stdin.Stat()
			return err == nil && fi.Mode()&os.ModeCharDevice != 0
		},
		Now:          time.Now,
		AncestorPIDs: session.DefaultAncestors,
	}
}

func envPath(name, def string) string {
	if v, ok := os.LookupEnv(name); ok {
		return filepath.Clean(v)
	}
	return def
}

// usageError marks errors that exit with code 2.
type usageError struct{ error }

// Execute runs the command line args and returns the exit code.
func Execute(env Env, args []string, version string) int {
	env.in = bufio.NewReader(env.Stdin)
	root := &cobra.Command{
		Use:   "copilot-sessions",
		Short: description,
		Long: fmt.Sprintf(`%s

DATE: YYYY-MM-DD, today, yesterday, Nd, Nw, Nm (N days / weeks / months before
today). Dates use the local timezone.

Status:
  ongoing  a live copilot process holds the session (busy or idle)
  stale    the session has an inuse lock left by a dead process
  closed   the session is not in use

Copilot home: %s (COPILOT_HOME). Backups: %s (COPILOT_BACKUP_DIR).`, description, env.Home, env.BackupDir),
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	root.SetIn(env.Stdin)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	root.SetArgs(args)
	root.AddCommand(newList(env), newClean(env), newBackup(env), newRestore(env))
	registerCompletions(root, env)

	err := root.Execute()
	if err == nil {
		return 0
	}
	// Cobra's unknown-command message carries its own "Did you mean this?" and newline.
	fmt.Fprintf(env.Stderr, "error: %s\n", strings.TrimRight(err.Error(), "\n"))
	if errors.As(err, new(usageError)) || strings.HasPrefix(err.Error(), "unknown command ") {
		return 2
	}
	return 1
}

// addFilters registers the filter flags shared by list and clean.
func addFilters(cmd *cobra.Command, f *filter.Filters, env Env) {
	fl := cmd.Flags()
	fl.VarP(filter.DateFlag(&f.Until, env.Now), "until", "u", "last updated on or before DATE (inclusive)")
	fl.Var(filter.DateFlag(&f.Since, env.Now), "since", "last updated on or after DATE (inclusive)")
	fl.VarP(filter.StatusFlag(&f.Status), "status", "s", "comma-separated: ongoing, stale, closed")
	fl.StringVarP(&f.Cwd, "cwd", "c", "", "working directory is `PATH` or below it")
	fl.StringVarP(&f.Grep, "grep", "g", "", "name contains `TEXT` (case-insensitive)")
	fl.BoolVarP(&f.Empty, "empty", "e", false, "no user message was ever sent (opened and closed without a prompt)")
}

func newList(env Env) *cobra.Command {
	var f filter.Filters
	var asJSON, quiet bool
	cmd := &cobra.Command{
		Use:     "list [ID...]",
		Aliases: []string{"ls"},
		Short:   "List sessions with their status",
		Long:    "List sessions with their status.\n\nID: session ID or unique ID prefix.",
		Example: `  copilot-sessions list --since 2026-10-01 --until 2026-10-31
  copilot-sessions list --cwd ~/src/my-app --grep refactor
  copilot-sessions list --status stale --json`,
		RunE: func(_ *cobra.Command, args []string) error {
			if asJSON && quiet {
				return usageError{errors.New("--json cannot be combined with --quiet")}
			}
			f.IDs = args
			return runList(env, f, asJSON, quiet)
		},
	}
	addFilters(cmd, &f, env)
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "print session IDs only")
	return cmd
}

// load reads all sessions and applies the filters, sorted for output.
func load(env Env, f filter.Filters, withSize bool) (all, selected []session.Session, err error) {
	all, err = session.Load(env.Home, env.AncestorPIDs(), withSize)
	if err != nil {
		return nil, nil, err
	}
	selected, err = filter.Apply(all, f, func() (map[string]bool, error) { return session.HasTurns(env.Home) })
	session.Sort(selected)
	return all, selected, err
}

func runList(env Env, f filter.Filters, asJSON, quiet bool) error {
	all, selected, err := load(env, f, !quiet)
	if err != nil {
		return err
	}
	w := env.Stdout
	switch {
	case asJSON:
		return writeJSON(w, selected)
	case quiet:
		for _, s := range selected {
			fmt.Fprintln(w, s.ID)
		}
		return nil
	case len(selected) == 0:
		fmt.Fprintln(w, "No sessions match.")
		return nil
	}
	printTable(w, selected, true)
	counts := map[string]int{}
	var total int64
	for _, s := range selected {
		counts[s.Status]++
		total += *s.Size
	}
	var parts []string
	for _, st := range filter.Statuses {
		if counts[st] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[st], st))
		}
	}
	fmt.Fprintf(w, "%d of %d sessions (%s), %s.\n", len(selected), len(all), strings.Join(parts, ", "), session.HumanSize(total))
	return nil
}

// plural returns "<n> <word>", with an s unless n == 1 (spec §6.5).
func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// printTable prints the session table; the SIZE column needs computed sizes.
func printTable(w io.Writer, ss []session.Session, showSize bool) {
	sizeHead := ""
	if showSize {
		sizeHead = fmt.Sprintf("%6s  ", "SIZE")
	}
	fmt.Fprintf(w, "  %-16s  %-14s  %s%-36s  NAME\n", "UPDATED", "STATUS", sizeHead, "ID")
	for _, s := range ss {
		when := "unknown"
		if s.Updated != nil {
			when = s.Updated.Local().Format("2006-01-02 15:04")
		}
		status := s.Status
		if status == "ongoing" {
			status = "ongoing (idle)"
			if s.Busy {
				status = "ongoing (busy)"
			}
		}
		size := ""
		if showSize {
			v := "-"
			if s.Dir != "" {
				v = session.HumanSize(*s.Size)
			}
			size = fmt.Sprintf("%6s  ", v)
		}
		name := []rune(s.Name)
		if len(name) > 60 {
			name = name[:60]
		}
		notes := ""
		if s.Current {
			notes += " (this session)"
		}
		if s.Dir == "" {
			notes += " (db only)"
		}
		fmt.Fprintf(w, "  %-16s  %-14s  %s%s  %s%s\n", when, status, size, s.ID, string(name), notes)
	}
}

type sessionJSON struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Cwd       string  `json:"cwd"`
	UpdatedAt *string `json:"updated_at"`
	Status    string  `json:"status"`
	Busy      *bool   `json:"busy"`
	Current   bool    `json:"current"`
	Size      *int64  `json:"size"`
	DBOnly    bool    `json:"db_only"`
}

func writeJSON(w io.Writer, ss []session.Session) error {
	out := make([]sessionJSON, 0, len(ss))
	for _, s := range ss {
		j := sessionJSON{ID: s.ID, Name: s.Name, Cwd: s.Cwd, Status: s.Status, Current: s.Current, Size: s.Size, DBOnly: s.Dir == ""}
		if s.Updated != nil {
			v := s.Updated.UTC().Format(time.RFC3339Nano)
			j.UpdatedAt = &v
		}
		if s.Status == "ongoing" {
			busy := s.Busy
			j.Busy = &busy
		}
		out = append(out, j)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}
