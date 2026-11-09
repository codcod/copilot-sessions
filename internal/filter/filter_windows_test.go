package filter

import "testing"

// Windows paths compare case-insensitively, drive letter included (CS-010 review F3).
func TestUnderWindowsCase(t *testing.T) {
	for _, tt := range []struct {
		path, root string
		want       bool
	}{
		{`c:\Users\Me\proj`, `C:\Users\me`, true},
		{`C:\USERS\ME`, `c:\users\me`, true},
		{`c:\Users\Me2`, `C:\Users\me`, false},
	} {
		if got := under(tt.path, tt.root); got != tt.want {
			t.Errorf("under(%q, %q) = %v", tt.path, tt.root, got)
		}
	}
}
