package main

import "testing"

func TestResolveVersion(t *testing.T) {
	orig := Version
	t.Cleanup(func() { Version = orig })
	for _, tc := range []struct{ in, want string }{{"1.2.3", "1.2.3"}, {"dev", ""}} {
		Version = tc.in
		got := resolveVersion()
		if tc.want != "" && got != tc.want {
			t.Errorf("Version=%q: got %q, want %q", tc.in, got, tc.want)
		}
		if got == "" || got == "(devel)" {
			t.Errorf("Version=%q: got %q", tc.in, got)
		}
	}
}
