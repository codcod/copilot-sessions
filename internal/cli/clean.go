package cli

import (
	"errors"
	"fmt"

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
		Short:   "delete sessions",
		Long:    "Delete sessions: their directories, DB rows and UI state.\n\nID: session ID or unique ID prefix.",
		RunE: func(_ *cobra.Command, args []string) error {
			f.IDs = args
			if all && f.Any() {
				return errors.New("--all cannot be combined with IDs or filters")
			}
			if !all && !f.Any() {
				return errors.New("specify session IDs, a filter (--until, --since, --status, --cwd, --grep, --empty) or --all")
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
	fl.BoolVarP(&quiet, "quiet", "q", false, "do not list the selected sessions")
	return cmd
}

func runClean(env Env, f filter.Filters, all, includeOngoing, dryRun, yes, quiet bool) error {
	sessions, selected, err := load(env, f, !quiet)
	if err != nil {
		return err
	}
	w := env.Stdout
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
		fmt.Fprintln(w, "Skipped this session (it runs this command).")
	}
	if ongoing > 0 {
		fmt.Fprintf(w, "Skipped %s (use --include-ongoing).\n", plural(ongoing, "ongoing session"))
	}
	if len(kept) == 0 {
		fmt.Fprintln(w, "No sessions match.")
		return nil
	}

	scope := "all sessions"
	if !all {
		scope = f.Describe()
	}
	if !quiet {
		printTable(w, kept, true)
		var total int64
		for _, s := range kept {
			total += *s.Size
		}
		scope += ", " + session.HumanSize(total)
	}
	fmt.Fprintf(w, "%d of %d sessions selected (%s).\n", len(kept), len(sessions), scope)

	if dryRun {
		fmt.Fprintln(w, "Dry run: nothing deleted.")
		return nil
	}
	ok, err := confirm(env, "Permanently delete these sessions?", yes)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(w, "Aborted.")
		return nil
	}
	// kept is a subset of sessions with unique IDs, so equal length means every session.
	if err := session.Delete(env.Home, kept, len(kept) == len(sessions)); err != nil {
		return err
	}
	fmt.Fprintf(w, "Deleted %s.\n", plural(len(kept), "session"))
	return nil
}
