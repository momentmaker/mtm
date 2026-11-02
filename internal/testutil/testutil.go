// Package testutil holds helpers shared by tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
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
