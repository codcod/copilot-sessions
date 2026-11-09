package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/codcod/copilot-sessions/internal/filter"
	"github.com/codcod/copilot-sessions/internal/session"
)

type cleanOpts struct{ All, IncludeOngoing, DryRun, Yes, Quiet bool }

func newClean(env *Env) *cobra.Command {
	var f filter.Filters
	var o cleanOpts
	cmd := &cobra.Command{
		Use:     "clean [ID...]",
		Aliases: []string{"rm"},
		Short:   "Delete sessions",
		Long:    "Delete sessions: their directories, DB rows and UI state.\n\nID: session ID or unique ID prefix.",
		Example: `  copilot-sessions clean -n --since 2026-10-01 --until 2026-10-31
  copilot-sessions clean --empty
  copilot-sessions clean --status stale
  copilot-sessions clean --until 3m -y -q`,
		RunE: func(cmd *cobra.Command, args []string) error {
			f.IDs = args
			if o.All && f.Any() {
				return usageError{errors.New("--all cannot be combined with IDs or filters")}
			}
			if !o.All && !f.Any() {
				return usageError{errors.New("specify session IDs, a filter (--until, --since, --status, --cwd, --grep, --empty) or --all")}
			}
			opts := o
			opts.IncludeOngoing = o.IncludeOngoing || f.Status[session.StatusOngoing]
			return runClean(cmd.Context(), env, f, opts)
		},
	}
	addFilters(cmd, &f, env)
	fl := cmd.Flags()
	fl.BoolVarP(&o.All, "all", "a", false, "select all sessions")
	fl.BoolVar(&o.IncludeOngoing, "include-ongoing", false, "also delete ongoing sessions")
	fl.BoolVarP(&o.DryRun, "dry-run", "n", false, "only show what would be deleted")
	fl.BoolVarP(&o.Yes, "yes", "y", false, "skip confirmation")
	fl.BoolVarP(&o.Quiet, "quiet", "q", false, "print only the IDs of deleted sessions")
	return cmd
}

func runClean(ctx context.Context, env *Env, f filter.Filters, o cleanOpts) error {
	sessions, selected, err := load(ctx, env, f, !o.Quiet)
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
		case s.Status == session.StatusOngoing && !o.IncludeOngoing:
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
		if !o.Quiet {
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
		if !o.All {
			scope = f.Describe()
		}
		fmt.Fprintf(out, "%d of %d sessions selected (%s%s).\n", len(kept), len(sessions), scope, size)
	}
	switch {
	case !o.Quiet:
		printTable(w, kept)
		var total int64
		for _, s := range kept {
			total += s.Size
		}
		summary(w, ", "+session.HumanSize(total))
	case !o.DryRun && willPrompt(env, o.Yes):
		// -q keeps stdout to IDs, but the prompt still says what it deletes (CS-012 F1).
		summary(ew, "")
	}

	if o.DryRun {
		if o.Quiet {
			printIDs()
		}
		fmt.Fprintln(ew, "Dry run: nothing deleted.")
		return nil
	}
	ok, err := confirm(ctx, env, "Permanently delete these sessions?", o.Yes)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(ew, "Aborted.")
		return nil
	}
	// Last chance to cancel: deletion runs to completion once started.
	if err := ctx.Err(); err != nil {
		return err
	}
	del := session.Delete
	// kept is a subset of sessions with unique IDs, so equal length means every session.
	if len(kept) == len(sessions) {
		del = session.DeleteAll
	}
	if err := del(ctx, env.Home, kept); err != nil {
		return err
	}
	if o.Quiet {
		printIDs()
		return nil
	}
	fmt.Fprintf(w, "Deleted %s.\n", plural(len(kept), "session"))
	return nil
}
