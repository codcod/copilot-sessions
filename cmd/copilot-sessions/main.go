// Command copilot-sessions lists, cleans, backs up and restores GitHub Copilot CLI sessions.
package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/codcod/copilot-sessions/internal/cli"
)

// Version is set at build time with -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	// ponytail: os.Exit skips deferred cleanup, so Ctrl-C mid-backup leaves a temp dir in
	// $TMPDIR and mid-restore a tmp* dir in COPILOT_HOME (not a session, not a backup item).
	// Between restore deleting an item and renaming its replacement in, that tmp* dir holds
	// the only copy of the restored item, the same window as the Python script. Switch to
	// context cancellation (and roll back the replace, CS-007) if that ever matters.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		fmt.Println("\nAborted.")
		os.Exit(130)
	}()
	os.Exit(cli.Execute(cli.DefaultEnv(), os.Args[1:], Version))
}
