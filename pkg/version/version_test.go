package version

import "testing"

func TestGetReturnsTheBuildValues(t *testing.T) {
	got := Get()

	if got.Version != Version {
		t.Errorf("Version = %q, want %q", got.Version, Version)
	}

	if got.Commit != Commit {
		t.Errorf("Commit = %q, want %q", got.Commit, Commit)
	}

	if got.GoVersion == "" {
		t.Error("GoVersion is empty")
	}
}
