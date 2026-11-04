// Package testutil holds helpers shared by tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/runner"
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
// hooks, templates), gives commits a fixed identity, and stops commits from
// starting background maintenance that could still be writing to the repo
// when a test records it.
func IsolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "maintenance.auto")
	t.Setenv("GIT_CONFIG_VALUE_0", "false")
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

// MacResponses are fake answers from a supported Mac (Apple Silicon,
// macOS 26.7.1, FileVault on) whose hardware UUID is uuid.
func MacResponses(uuid string) map[string]runner.Response {
	return map[string]runner.Response{
		"/usr/sbin/sysctl -n hw.optional.arm64":          {Stdout: "1\n"},
		"/usr/bin/sw_vers -productVersion":               {Stdout: "26.7.1\n"},
		"/usr/bin/sw_vers -buildVersion":                 {Stdout: "25G241\n"},
		"/usr/bin/fdesetup status":                       {Stdout: "FileVault is On.\n"},
		"/usr/sbin/ioreg -rd1 -c IOPlatformExpertDevice": {Stdout: `    "IOPlatformUUID" = "` + uuid + `"` + "\n"},
	}
}
