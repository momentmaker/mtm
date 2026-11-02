// Package testutil holds helpers shared by tests.
package testutil

import (
	"os"
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
