package session

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

func TestWindowsProcesses(t *testing.T) {
	pids := ancestorsFrom(os.Getpid())
	if !pids[os.Getpid()] || len(pids) < 2 {
		t.Errorf("ancestors of self = %v", pids)
	}
	fx := testfix.Build(t)
	if !pidIsCopilot(fx.OngoingPID) {
		t.Error("copilot-test helper: want true")
	}
	// This test binary is Copilot only if some argv string says so (a -test.run pattern can).
	want := slices.ContainsFunc(os.Args, func(a string) bool { return strings.Contains(a, "copilot") })
	if pidIsCopilot(os.Getpid()) != want {
		t.Errorf("pidIsCopilot(self) = %v for %q", !want, os.Args)
	}
	if pidIsCopilot(testfix.StalePID) {
		t.Error("missing pid: want false")
	}
}
