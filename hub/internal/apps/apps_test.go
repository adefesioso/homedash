package apps

import "testing"

// TestSafeRelPath is the catalog's file-path guard: a setup file must
// stay under the stack directory it's written into.
func TestSafeRelPath(t *testing.T) {
	ok := []string{"nginx/nginx.conf", "a.txt", "a/b/c.yml"}
	bad := []string{"", "/etc/passwd", "../escape", "a/../../b", "a//b", "a/../b"}
	for _, p := range ok {
		if !safeRelPath(p) {
			t.Errorf("safeRelPath(%q) = false, want true", p)
		}
	}
	for _, p := range bad {
		if safeRelPath(p) {
			t.Errorf("safeRelPath(%q) = true, want false", p)
		}
	}
}
