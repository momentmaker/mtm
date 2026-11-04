package testutil

import "testing"

func TestIsolateGitDisablesAutoMaintenance(t *testing.T) {
	IsolateGit(t)
	repo := t.TempDir()
	Git(t, repo, "init", "-q")
	if got := Git(t, repo, "config", "--get", "maintenance.auto"); got != "false" {
		t.Errorf("maintenance.auto = %q, want false", got)
	}
}
