// Command copilot-sessions lists, cleans, backs up and restores GitHub Copilot CLI sessions.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/codcod/copilot-sessions/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// Ctrl-C cancels ctx: commands stop before their irreversible step and deferred
	// cleanup runs. stop() after the first signal restores default handling, so a second
	// Ctrl-C kills at once.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() { <-ctx.Done(); stop() }()
	code := cli.Execute(ctx, cli.DefaultEnv(), os.Args[1:], resolveVersion())
	stop()
	os.Exit(code)
}

// resolveVersion keeps the ldflags-stamped version (goreleaser, `just build`). A
// `go install …@vX.Y.Z` build runs no ldflags, so it falls back to the module version
// in the build info; "(devel)" and "" are discarded in favour of the default "dev".
func resolveVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}
