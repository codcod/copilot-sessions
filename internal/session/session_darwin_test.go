package session

import (
	"encoding/binary"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/codcod/copilot-sessions/internal/testfix"
)

// procargs builds a kern.procargs2-shaped buffer: argc, exec path, NUL padding, argv, env.
func procargs(argc int32, exec string, argv, env []string) []byte {
	b := binary.NativeEndian.AppendUint32(nil, uint32(argc))
	b = append(b, exec...)
	b = append(b, 0, 0, 0, 0)
	for _, s := range append(argv, env...) {
		b = append(append(b, s...), 0)
	}
	return b
}

func TestProcargsArgv(t *testing.T) {
	copilot := procargs(2, "/usr/local/bin/copilot", []string{"/usr/local/bin/copilot", "--resume"}, []string{"PATH=/bin"})
	node := procargs(2, "/usr/local/bin/node", []string{"node", "/x/y.js"}, []string{"COPILOT_HOME=/h", "PWD=/src/copilot"})
	tests := map[string]struct {
		b    []byte
		want []string
	}{
		"copilot":   {copilot, []string{"/usr/local/bin/copilot", "--resume"}},
		"env only":  {node, []string{"node", "/x/y.js"}},
		"empty":     {nil, nil},
		"short":     {[]byte{1, 0}, nil},
		"zero argc": {procargs(0, "/bin/x", nil, nil), nil},
		"truncated": {copilot[:len(copilot)-len("--resume")-len("PATH=/bin")-2], nil},
		"no exec":   {binary.NativeEndian.AppendUint32(nil, 1), nil},
	}
	for name, tt := range tests {
		var got []string
		for _, a := range procargsArgv(tt.b) {
			got = append(got, string(a))
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: argv = %q, want %q", name, got, tt.want)
		}
	}
}

func TestProcargsMatch(t *testing.T) {
	if !argvHasCopilot(procargs(1, "/usr/local/bin/copilot", []string{"/usr/local/bin/copilot"}, nil)) {
		t.Error("copilot argv: want match")
	}
	if argvHasCopilot(procargs(2, "/usr/local/bin/node", []string{"node", "/x/y.js"}, []string{"COPILOT_HOME=/h"})) {
		t.Error("copilot only in env: want no match")
	}
	// The live process: this test binary is Copilot only if some argv string says so (a
	// -test.run pattern or a profile path under the checkout can).
	want := slices.ContainsFunc(os.Args, func(a string) bool { return strings.Contains(a, "copilot") })
	if pidIsCopilot(os.Getpid()) != want {
		t.Errorf("pidIsCopilot(self) = %v for %q", !want, os.Args)
	}
	if pidIsCopilot(testfix.StalePID) {
		t.Error("missing pid: want false")
	}
}
