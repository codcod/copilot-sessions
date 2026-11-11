package cli

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codcod/copilot-sessions/internal/backup"
	"github.com/codcod/copilot-sessions/internal/session"
)

// dateSuggestions are offered for --until and --since; free text stays valid.
var dateSuggestions = []string{
	"today\ttoday",
	"yesterday\tyesterday",
	"7d\t7 days ago",
	"2w\t2 weeks ago",
	"1m\t1 month ago",
	"3m\t3 months ago",
}

// registerCompletions wires the dynamic shell completions (spec §8) onto the subcommands.
// RegisterFlagCompletionFunc fails only for an unknown or already-registered flag, a
// programming error the completion tests catch, so its errors are dropped.
func registerCompletions(root *cobra.Command, env *Env) {
	noFile := func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	for _, cmd := range root.Commands() {
		switch cmd.Name() {
		case "list", "clean":
			cmd.ValidArgsFunction = completeIDs(env)
			_ = cmd.RegisterFlagCompletionFunc("status", completeStatus)
			dates := func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
				return dateSuggestions, cobra.ShellCompDirectiveNoFileComp
			}
			_ = cmd.RegisterFlagCompletionFunc("until", dates)
			_ = cmd.RegisterFlagCompletionFunc("since", dates)
			_ = cmd.RegisterFlagCompletionFunc("cwd", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
				return nil, cobra.ShellCompDirectiveFilterDirs
			})
			_ = cmd.RegisterFlagCompletionFunc("grep", noFile)
		case "backup":
			cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
				if len(args) > 0 {
					return nil, cobra.ShellCompDirectiveNoFileComp
				}
				return []string{"tar.gz"}, cobra.ShellCompDirectiveFilterFileExt
			}
			_ = cmd.RegisterFlagCompletionFunc("keep", noFile)
		case "restore":
			cmd.ValidArgsFunction = completeRestore(env)
		case "move":
			cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
				if len(args) >= 2 {
					return nil, cobra.ShellCompDirectiveNoFileComp
				}
				return nil, cobra.ShellCompDirectiveFilterDirs
			}
		}
	}
}

// completeIDs offers every session ID with "<status> <name or ->", without computing sizes.
// KeepOrder stops the shell re-sorting them out of session.Sort order.
func completeIDs(env *Env) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		ss, err := session.Load(cmd.Context(), env.Home, env.AncestorPIDs())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		session.Sort(ss)
		out := make([]string, 0, len(ss))
		for _, s := range ss {
			name := s.Name
			if name == "" {
				name = "-"
			}
			out = append(out, s.ID+"\t"+string(s.Status)+" "+name)
		}
		return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	}
}

// completeStatus completes comma lists: "closed," offers "closed,ongoing" and "closed,stale".
func completeStatus(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	prefix := toComplete[:strings.LastIndex(toComplete, ",")+1]
	chosen := strings.Split(prefix, ",")
	var out []string
	for _, s := range session.Statuses() {
		if !slices.Contains(chosen, string(s)) {
			out = append(out, prefix+string(s))
		}
	}
	return out, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// completeRestore offers latest, the backups newest first, then *.tar.gz files in the working
// dir — in-process, because one directive cannot mix fixed values with file completion. Once
// the argument has a "/", the shell completes the path itself: cobra's scripts expand "~"
// before __complete runs, so only the shell can answer "~/", "./" and subdirectories.
func completeRestore(env *Env) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		if strings.Contains(toComplete, "/") {
			return []string{"tar.gz"}, cobra.ShellCompDirectiveFilterFileExt
		}
		out := []string{"latest"}
		backups, _ := backup.List(env.BackupDir) // completion degrades to fewer candidates
		out = append(out, backups...)
		files, _ := filepath.Glob(toComplete + "*.tar.gz") // a bad pattern just offers no files
		for _, f := range files {
			if !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	}
}
