package buildinfo

import "testing"

func TestReadPrefersLinkTimeValues(t *testing.T) {
	oldV, oldC := Version, Commit
	t.Cleanup(func() { Version, Commit = oldV, oldC })

	Version, Commit = "v9.9.9", "abc123"
	got := Read()
	if got.Version != "v9.9.9" || got.Commit != "abc123" {
		t.Fatalf("link-time values ignored: %+v", got)
	}
	if got.GoVersion == "" {
		t.Fatal("go version missing from build info")
	}
}
