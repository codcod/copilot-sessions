package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
)

// willPrompt reports whether confirm will show its prompt, so callers can print
// prompt context (on stderr) only then.
func willPrompt(env *Env, yes bool) bool { return !yes && env.IsTTY() }

// confirm asks question with [y/N] unless yes is set; only y/yes (any case) is true.
// Without a TTY it fails instead of asking (spec §6). A cancelled ctx returns its error.
func confirm(ctx context.Context, env *Env, question string, yes bool) (bool, error) {
	if !willPrompt(env, yes) {
		if yes {
			return true, nil
		}
		return false, errors.New("confirmation required; rerun with --yes")
	}
	fmt.Fprintf(env.Stderr, "%s [y/N] ", question)
	line, err := readLine(ctx, env)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// readLine reads one line through the run's shared reader. EOF is not an error: the
// partial line (or "") is returned. Only a done ctx is an error, returned at once even
// while the read blocks; the abandoned read goroutine ends with the process.
func readLine(ctx context.Context, env *Env) (string, error) {
	if env.in == nil {
		env.in = bufio.NewReader(env.Stdin)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	in := env.in
	ch := make(chan string, 1)
	go func() {
		line, _ := in.ReadString('\n') // EOF reads as the partial line
		ch <- line
	}()
	select {
	case line := <-ch:
		return line, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
