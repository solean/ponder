package version

import "testing"

func TestUserAgentUsesVersion(t *testing.T) {
	saved := Version
	t.Cleanup(func() { Version = saved })

	Version = "1.2.3"
	if got, want := UserAgent("local tracker"), "ponder/1.2.3 (local tracker)"; got != want {
		t.Fatalf("UserAgent() = %q, want %q", got, want)
	}
}
