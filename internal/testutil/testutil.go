// Package testutil holds helpers shared by tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// WriteFile creates path with content, making parent folders as needed.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Copy copies src to dst with ditto, which keeps modification times,
// permissions, and extended attributes, like Migration Assistant.
func Copy(t *testing.T, src, dst string) {
	t.Helper()
	if out, err := exec.Command("/usr/bin/ditto", src, dst).CombinedOutput(); err != nil {
		t.Fatalf("ditto %s %s: %v\n%s", src, dst, err, out)
	}
}

// IsolateGit makes git ignore the developer's own configuration (signing,
// hooks, templates) and gives commits a fixed identity.
func IsolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "mtm test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "mtm test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

// Git runs git in dir and returns its trimmed output, failing the test on error.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
