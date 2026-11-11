// Package cli holds the cobra commands, their output and exit codes.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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

	in *bufio.Reader // the one buffered Stdin reader of a run; created by readLine
}

// DefaultEnv fills Env from the process environment.
func DefaultEnv() *Env {
	home, _ := os.UserHomeDir() // "" falls back to relative defaults; COPILOT_HOME overrides
	return &Env{
		Home:      envPath("COPILOT_HOME", filepath.Join(home, ".copilot")),
		BackupDir: envPath("COPILOT_BACKUP_DIR", filepath.Join(home, ".copilot-backups")),
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		// A terminal check, not ModeCharDevice: /dev/null is a char device but no TTY.
		IsTTY:        func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
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

// expandUser expands a leading ~ to the home directory.
func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

// usageError marks errors that exit with code 2.
type usageError struct{ error }

// usageArgs wraps the errors of v in usageError, so bad positional args exit 2.
func usageArgs(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := v(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}

// Execute runs the command line args and returns the exit code. A run that stopped
// because ctx was cancelled (Ctrl-C) prints only "Aborted." and returns 130; any other
// error is reported, even after a cancel, since it can come from a step past the point
// of no return (a failed rollback, a partial delete).
func Execute(ctx context.Context, env *Env, args []string, version string) int {
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
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	root.SetIn(env.Stdin)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	root.SetArgs(args)
	root.AddCommand(newList(env), newClean(env), newBackup(env), newRestore(env), newMove(env), &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "copilot-sessions version %s\n", version)
		},
	})
	registerCompletions(root, env)

	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	// Every ctx check and the sqlite driver return context.Canceled itself (possibly
	// wrapped), so this matches a cancel and nothing else.
	if errors.Is(err, context.Canceled) {
		fmt.Fprint(env.Stderr, "\nAborted.\n")
		return 130
	}
	// Cobra's unknown-command message carries its own "Did you mean this?" and newline.
	fmt.Fprintf(env.Stderr, "error: %s\n", strings.TrimRight(err.Error(), "\n"))
	// Cobra 1.10 has no typed unknown-command error, and a custom root Args is never
	// reached for a non-runnable root, so the message is the only signal.
	if errors.As(err, new(usageError)) || strings.HasPrefix(err.Error(), "unknown command ") {
		return 2
	}
	return 1
}

// addFilters registers the filter flags shared by list and clean.
func addFilters(cmd *cobra.Command, f *filter.Filters, env *Env) {
	fl := cmd.Flags()
	fl.VarP(&dateFlag{&f.Until, env.Now}, "until", "u", "last updated on or before DATE (inclusive)")
	fl.Var(&dateFlag{&f.Since, env.Now}, "since", "last updated on or after DATE (inclusive)")
	fl.VarP(&statusFlag{&f.Status}, "status", "s", "comma-separated: ongoing, stale, closed")
	fl.StringVarP(&f.Cwd, "cwd", "c", "", "working directory is `PATH` or below it")
	fl.StringVarP(&f.Grep, "grep", "g", "", "name contains `TEXT` (case-insensitive)")
	fl.BoolVarP(&f.Empty, "empty", "e", false, "no user message was ever sent (opened and closed without a prompt)")
}

type listOpts struct{ JSON, Quiet bool }

func newList(env *Env) *cobra.Command {
	var f filter.Filters
	var o listOpts
	cmd := &cobra.Command{
		Use:     "list [ID...]",
		Aliases: []string{"ls"},
		Short:   "List sessions with their status",
		Long:    "List sessions with their status.\n\nID: session ID or unique ID prefix.",
		Example: `  copilot-sessions list --since 2026-10-01 --until 2026-10-31
  copilot-sessions list --cwd ~/src/my-app --grep refactor
  copilot-sessions list --status stale --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if o.JSON && o.Quiet {
				return usageError{errors.New("--json cannot be combined with --quiet")}
			}
			f.IDs = args
			return runList(cmd.Context(), env, f, o)
		},
	}
	addFilters(cmd, &f, env)
	cmd.Flags().BoolVar(&o.JSON, "json", false, "print JSON")
	cmd.Flags().BoolVarP(&o.Quiet, "quiet", "q", false, "print session IDs only")
	return cmd
}

// load reads all sessions and applies the filters, sorted for output. Only the selected
// sessions are sized, and only when sized is set.
func load(ctx context.Context, env *Env, f filter.Filters, sized bool) (all, selected []session.Session, err error) {
	all, err = session.Load(ctx, env.Home, env.AncestorPIDs())
	if err != nil {
		return nil, nil, err
	}
	f.Cwd = expandUser(f.Cwd)
	selected, err = filter.Apply(all, f, func() (map[string]bool, error) { return session.HasTurns(ctx, env.Home) })
	if err != nil {
		return nil, nil, err
	}
	session.Sort(selected)
	if sized {
		if err := session.FillSizes(ctx, selected); err != nil {
			return nil, nil, err
		}
	}
	return all, selected, nil
}

func runList(ctx context.Context, env *Env, f filter.Filters, o listOpts) error {
	all, selected, err := load(ctx, env, f, !o.Quiet)
	if err != nil {
		return err
	}
	w := env.Stdout
	switch {
	case o.JSON:
		return writeJSON(w, selected)
	case o.Quiet:
		for _, s := range selected {
			fmt.Fprintln(w, s.ID)
		}
		return nil
	case len(selected) == 0:
		fmt.Fprintln(w, "No sessions match.")
		return nil
	}
	printTable(w, selected)
	counts := map[session.Status]int{}
	var total int64
	for _, s := range selected {
		counts[s.Status]++
		total += s.Size
	}
	var parts []string
	for _, st := range session.Statuses() {
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

// printTable prints the session table; the SIZE column needs FillSizes.
func printTable(w io.Writer, ss []session.Session) {
	fmt.Fprintf(w, "  %-16s  %-14s  %6s  %-36s  NAME\n", "UPDATED", "STATUS", "SIZE", "ID")
	for _, s := range ss {
		when := "unknown"
		if !s.Updated.IsZero() {
			when = s.Updated.Local().Format("2006-01-02 15:04")
		}
		status := string(s.Status)
		if s.Status == session.StatusOngoing {
			status = "ongoing (idle)"
			if s.Busy {
				status = "ongoing (busy)"
			}
		}
		size := "-"
		if s.Dir != "" {
			size = session.HumanSize(s.Size)
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
		fmt.Fprintf(w, "  %-16s  %-14s  %6s  %s  %s%s\n", when, status, size, s.ID, string(name), notes)
	}
}

// sessionJSON is the list --json row; the pointers encode "unknown" as null (spec §6.1).
type sessionJSON struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Cwd       string         `json:"cwd"`
	UpdatedAt *string        `json:"updated_at"`
	Status    session.Status `json:"status"`
	Busy      *bool          `json:"busy"`
	Current   bool           `json:"current"`
	Size      int64          `json:"size"`
	DBOnly    bool           `json:"db_only"`
}

func writeJSON(w io.Writer, ss []session.Session) error {
	out := make([]sessionJSON, 0, len(ss))
	for _, s := range ss {
		j := sessionJSON{ID: s.ID, Name: s.Name, Cwd: s.Cwd, Status: s.Status, Current: s.Current, Size: s.Size, DBOnly: s.Dir == ""}
		if !s.Updated.IsZero() {
			v := s.Updated.UTC().Format(time.RFC3339Nano)
			j.UpdatedAt = &v
		}
		if s.Status == session.StatusOngoing {
			busy := s.Busy
			j.Busy = &busy
		}
		out = append(out, j)
	}
	return newJSONEncoder(w).Encode(out)
}

// newJSONEncoder writes 2-space-indented JSON without HTML escaping.
func newJSONEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc
}
