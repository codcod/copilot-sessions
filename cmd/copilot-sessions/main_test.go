package main

import "testing"

func TestResolveVersion(t *testing.T) {
	orig := version
	t.Cleanup(func() { version = orig })
	for _, tc := range []struct{ in, want string }{{"1.2.3", "1.2.3"}, {"dev", ""}} {
		version = tc.in
		got := resolveVersion()
		if tc.want != "" && got != tc.want {
			t.Errorf("version=%q: got %q, want %q", tc.in, got, tc.want)
		}
		if got == "" || got == "(devel)" {
			t.Errorf("version=%q: got %q", tc.in, got)
		}
	}
}
