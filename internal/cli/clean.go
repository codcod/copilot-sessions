package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/codcod/copilot-sessions/internal/filter"
	"github.com/codcod/copilot-sessions/internal/session"
)

func newClean(env Env) *cobra.Command {
	var f filter.Filters
	var all, includeOngoing, dryRun, yes, quiet bool
	cmd := &cobra.Command{
		Use:     "clean [ID...]",
		Aliases: []string{"rm"},
		Short:   "Delete sessions",
		Long:    "Delete sessions: their directories, DB rows and UI state.\n\nID: session ID or unique ID prefix.",
		Example: `  copilot-sessions clean -n --since 2026-10-01 --until 2026-10-31
  copilot-sessions clean --empty
  copilot-sessions clean --status stale
  copilot-sessions clean --until 3m -y -q`,
		RunE: func(_ *cobra.Command, args []string) error {
			f.IDs = args
			if all && f.Any() {
				return usageError{errors.New("--all cannot be combined with IDs or filters")}
			}
			if !all && !f.Any() {
				return usageError{errors.New("specify session IDs, a filter (--until, --since, --status, --cwd, --grep, --empty) or --all")}
			}
			return runClean(env, f, all, includeOngoing || f.Status["ongoing"], dryRun, yes, quiet)
		},
	}
	addFilters(cmd, &f, env)
	fl := cmd.Flags()
	fl.BoolVarP(&all, "all", "a", false, "select all sessions")
	fl.BoolVar(&includeOngoing, "include-ongoing", false, "also delete ongoing sessions")
	fl.BoolVarP(&dryRun, "dry-run", "n", false, "only show what would be deleted")
	fl.BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	fl.BoolVarP(&quiet, "quiet", "q", false, "print only the IDs of deleted sessions")
	return cmd
}

func runClean(env Env, f filter.Filters, all, includeOngoing, dryRun, yes, quiet bool) error {
	sessions, selected, err := load(env, f, !quiet)
	if err != nil {
		return err
	}
	w, ew := env.Stdout, env.Stderr
	var kept []session.Session
	current, ongoing := 0, 0
	for _, s := range selected {
		switch {
		case s.Current:
			current++
		case s.Status == "ongoing" && !includeOngoing:
			ongoing++
		default:
			kept = append(kept, s)
		}
	}
	if current > 0 {
		fmt.Fprintln(ew, "Skipped this session (it runs this command).")
	}
	if ongoing > 0 {
		fmt.Fprintf(ew, "Skipped %s (use --include-ongoing).\n", plural(ongoing, "ongoing session"))
	}
	if len(kept) == 0 {
		if !quiet {
			fmt.Fprintln(w, "No sessions match.")
		}
		return nil
	}
	printIDs := func() {
		for _, s := range kept {
			fmt.Fprintln(w, s.ID)
		}
	}

	summary := func(out io.Writer, size string) {
		scope := "all sessions"
		if !all {
			scope = f.Describe()
		}
		fmt.Fprintf(out, "%d of %d sessions selected (%s%s).\n", len(kept), len(sessions), scope, size)
	}
	switch {
	case !quiet:
		printTable(w, kept, true)
		var total int64
		for _, s := range kept {
			total += *s.Size
		}
		summary(w, ", "+session.HumanSize(total))
	case !dryRun && willPrompt(env, yes):
		// -q keeps stdout to IDs, but the prompt still says what it deletes (CS-012 F1).
		summary(ew, "")
	}

	if dryRun {
		if quiet {
			printIDs()
		}
		fmt.Fprintln(ew, "Dry run: nothing deleted.")
		return nil
	}
	ok, err := confirm(env, "Permanently delete these sessions?", yes)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(ew, "Aborted.")
		return nil
	}
	// kept is a subset of sessions with unique IDs, so equal length means every session.
	if err := session.Delete(env.Home, kept, len(kept) == len(sessions)); err != nil {
		return err
	}
	if quiet {
		printIDs()
		return nil
	}
	fmt.Fprintf(w, "Deleted %s.\n", plural(len(kept), "session"))
	return nil
}
