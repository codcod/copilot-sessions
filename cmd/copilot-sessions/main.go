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
	// ponytail: os.Exit skips deferred cleanup; switch to context cancellation once a
	// command leaves temp files behind (CS-003, CS-004).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		fmt.Println("\nAborted.")
		os.Exit(130)
	}()
	os.Exit(cli.Execute(cli.DefaultEnv(), os.Args[1:], Version))
}
