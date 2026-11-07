package cli

import (
	"errors"
	"fmt"
	"strings"
)

// confirm asks question with [y/N] unless yes is set; only y/yes (any case) is true.
// Without a TTY it fails instead of asking (spec §6).
func confirm(env Env, question string, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !env.IsTTY() {
		return false, errors.New("confirmation required; rerun with --yes")
	}
	fmt.Fprintf(env.Stdout, "%s [y/N] ", question)
	switch strings.ToLower(strings.TrimSpace(readLine(env))) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// readLine reads one line through the run's shared reader; EOF reads as "".
func readLine(env Env) string {
	line, _ := env.in.ReadString('\n')
	return line
}
