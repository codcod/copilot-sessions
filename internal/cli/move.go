package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/codcod/copilot-sessions/internal/session"
)

type moveOpts struct{ DryRun, Yes bool }

func newMove(env *Env) *cobra.Command {
	var o moveOpts
	cmd := &cobra.Command{
		Use:     "move OLD NEW",
		Aliases: []string{"mv"},
		Short:   "Move a project folder and repoint its sessions",
		Long: `Move the folder OLD to NEW, then repoint every session started in or below OLD:
the cwd and git_root of its workspace.yaml and its cwd in the DB.

If OLD is already gone and NEW exists (moved by hand), only the sessions are
repointed. NEW must not exist otherwise; a move across filesystems is refused.
Session history (events, turns, rewind snapshots) and Copilot's trusted folders
keep the old path. Ongoing sessions under OLD block the move: close them first.

There is no automatic backup: run "copilot-sessions backup" first.`,
		Example: `  copilot-sessions move -n ~/src/app ~/work/app
  copilot-sessions backup && copilot-sessions move ~/src/app ~/work/app`,
		Args: usageArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMove(cmd.Context(), env, args[0], args[1], o)
		},
	}
	cmd.Flags().BoolVarP(&o.DryRun, "dry-run", "n", false, "only show what would be moved")
	cmd.Flags().BoolVarP(&o.Yes, "yes", "y", false, "skip confirmation")
	return cmd
}

func runMove(ctx context.Context, env *Env, oldArg, newArg string, o moveOpts) error {
	oldPath, err := filepath.Abs(expandUser(oldArg))
	if err != nil {
		return err
	}
	newPath, err := filepath.Abs(expandUser(newArg))
	if err != nil {
		return err
	}
	if session.Under(newPath, oldPath) {
		return usageError{fmt.Errorf("NEW %s is OLD or inside it", newPath)}
	}
	// Resolved before the rename: NEW does not exist yet, so its parent is resolved.
	newReal, err := moveTarget(newPath)
	if err != nil {
		return err
	}
	// Copilot records the cwd with symlinks resolved (review F1), so match both forms.
	oldRoots := []string{oldPath}
	if real := resolveSymlinks(oldPath); real != oldPath {
		oldRoots = append(oldRoots, real)
	}
	manual, err := moveSides(oldPath, newPath)
	if err != nil {
		return err
	}

	selected, ongoing, err := moveSelection(ctx, env, oldRoots)
	if err != nil {
		return err
	}
	w, ew := env.Stdout, env.Stderr
	if len(selected) == 0 {
		fmt.Fprintln(w, "No sessions to repoint.")
	} else {
		if err := session.FillSizes(ctx, selected); err != nil {
			return err
		}
		printTable(w, selected)
		fmt.Fprintf(w, "%s to repoint (cwd under %s).\n", plural(len(selected), "session"), oldPath)
	}
	if ongoing > 0 {
		return ongoingError(ongoing, oldPath)
	}
	if o.DryRun {
		fmt.Fprintln(ew, "Dry run: nothing moved.")
		return nil
	}
	question := fmt.Sprintf("Move %s to %s and repoint these sessions?", oldPath, newPath)
	if manual {
		question = fmt.Sprintf("%s is already gone; repoint these sessions to %s?", oldPath, newPath)
	}
	ok, err := confirm(ctx, env, question, o.Yes)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(ew, "Aborted.")
		return nil
	}
	// Last chance to cancel: the move and the rewrite run to completion once started.
	if err := ctx.Err(); err != nil {
		return err
	}
	// The prompt can wait for minutes: check again for a Copilot started in OLD or a NEW
	// created meanwhile (review F4). Sessions that appeared meanwhile are repointed too.
	if again, err := moveSides(oldPath, newPath); err != nil || again != manual {
		return errors.Join(errors.New("the folders changed while confirming; nothing moved"), err)
	}
	// A symlink on NEW's path retargeted meanwhile would send the sessions elsewhere (S1).
	if again, err := moveTarget(newPath); err != nil || again != newReal {
		return errors.Join(errors.New("the folders changed while confirming; nothing moved"), err)
	}
	if selected, ongoing, err = moveSelection(ctx, env, oldRoots); err != nil {
		return err
	}
	if ongoing > 0 {
		return ongoingError(ongoing, oldPath)
	}
	if manual {
		fmt.Fprintf(w, "%s already moved; repointing sessions only.\n", oldPath)
	} else {
		// No copy fallback: a rename across filesystems fails and changes nothing (decision 8).
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("move %s to %s: %w", oldPath, newPath, err)
		}
		fmt.Fprintf(w, "Moved %s to %s.\n", oldPath, newPath)
	}
	failed, err := session.Repoint(ctx, env.Home, selected, oldRoots, newReal)
	for _, f := range failed {
		fmt.Fprintf(ew, "failed: %v\n", f)
	}
	if err != nil {
		return err
	}
	if len(failed) > 0 {
		// Reported, not rolled back: rerunning the same move repoints the rest (decision 9).
		return fmt.Errorf("repointed %d of %s; rerun the same move to finish",
			len(selected)-len(failed), plural(len(selected), "session"))
	}
	fmt.Fprintf(w, "Repointed %s.\n", plural(len(selected), "session"))
	return nil
}

// moveSides checks OLD and NEW: manual is true when only NEW exists (moved by hand,
// CS-017 decision 7).
func moveSides(oldPath, newPath string) (manual bool, err error) {
	// Renaming a symlink moves only the link; a relative one can dangle (reviews R2, P1).
	if fi, err := os.Lstat(oldPath); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return false, usageError{fmt.Errorf("OLD %s is a symlink; move the folder it points to", oldPath)}
	}
	oldExists, err := pathExists(oldPath)
	if err != nil {
		return false, err
	}
	newExists, err := pathExists(newPath)
	if err != nil {
		return false, err
	}
	switch {
	case oldExists && newExists:
		return false, fmt.Errorf("%s already exists", newPath)
	case !oldExists && !newExists:
		return false, fmt.Errorf("%s does not exist", oldPath)
	}
	return !oldExists, nil
}

// moveSelection returns the sessions whose cwd, git_root or DB cwd is under one of
// oldRoots, sorted, and how many of them are ongoing (or run this command).
func moveSelection(ctx context.Context, env *Env, oldRoots []string) ([]session.Session, int, error) {
	all, err := session.Load(ctx, env.Home, env.AncestorPIDs())
	if err != nil {
		return nil, 0, err
	}
	session.Sort(all)
	var selected []session.Session
	ongoing := 0
	for _, s := range all {
		// DBCwd too: a rerun after a half-done repoint finds the DB rows left behind (decision 9).
		for _, r := range oldRoots {
			if session.Under(s.Cwd, r) || session.Under(s.GitRoot, r) || session.Under(s.DBCwd, r) {
				selected = append(selected, s)
				if s.Current || s.Status == session.StatusOngoing {
					ongoing++
				}
				break
			}
		}
	}
	return selected, ongoing, nil
}

func ongoingError(n int, oldPath string) error {
	return fmt.Errorf("%s under %s; close them first", plural(n, "ongoing session"), oldPath)
}

// moveTarget returns NEW as Copilot will record it, with symlinks resolved (review R1).
// workspace.yaml has one line per value, so a control character in NEW as typed or as
// resolved would forge keys (reviews F3, G1): that is a usage error.
func moveTarget(newPath string) (string, error) {
	newReal := resolveSymlinks(newPath)
	for _, p := range []string{newPath, newReal} {
		if strings.ContainsFunc(p, unicode.IsControl) {
			return "", usageError{fmt.Errorf("NEW %q contains a control character", p)}
		}
	}
	return newReal, nil
}

// resolveSymlinks returns p with symlinks resolved; for a p that no longer exists (moved by
// hand), its parent is resolved instead. On error p is returned as is.
func resolveSymlinks(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if r, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(r, filepath.Base(p))
	}
	return p
}

// pathExists reports whether p exists (symlinks not followed); errors other than "not found"
// are returned, so an unreadable parent is not mistaken for a missing folder.
func pathExists(p string) (bool, error) {
	_, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
