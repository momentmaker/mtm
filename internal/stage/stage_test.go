package stage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/runner"
)

func mustOpen(t *testing.T) *Stage {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestOpenCreatesPrivateFolder(t *testing.T) {
	home := t.TempDir()
	st, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if st.Root != filepath.Join(home, DirName) {
		t.Errorf("Root = %q, want %q", st.Root, filepath.Join(home, DirName))
	}
	info, err := os.Stat(st.Root)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("mode = %04o, want 0700", perm)
	}
}

func TestOpenRefusesLoosePermissions(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, DirName)
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Open(home)
	if err == nil || !strings.Contains(err.Error(), "chmod 700") {
		t.Errorf("err = %v, want a refusal that suggests chmod 700", err)
	}
}

func TestOpenRefusesSymlink(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(home, DirName)); err != nil {
		t.Fatal(err)
	}
	_, err := Open(home)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("err = %v, want a refusal mentioning symlink", err)
	}
}

func TestExistingReportsMissingStage(t *testing.T) {
	_, err := Existing(t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestWriteFileIsPrivateAndAtomic(t *testing.T) {
	st := mustOpen(t)
	if err := st.WriteFile("logs/a.txt", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(st.Path("logs", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %04o, want 0600", perm)
	}
	dirInfo, err := os.Stat(st.Path("logs"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %04o, want 0700", perm)
	}
	entries, _ := os.ReadDir(st.Path("logs"))
	if len(entries) != 1 {
		t.Errorf("logs/ has %d entries, want 1 (no temp files left)", len(entries))
	}
}

func TestAbortLeavesNothing(t *testing.T) {
	st := mustOpen(t)
	f, err := st.Create("x.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("partial"))
	f.Abort()
	entries, _ := os.ReadDir(st.Root)
	if len(entries) != 0 {
		t.Errorf("stage has %d entries after Abort, want 0", len(entries))
	}
}

func TestExcludeFromBackupsRunsTmutil(t *testing.T) {
	st := mustOpen(t)
	fake := &runner.Fake{Responses: map[string]runner.Response{"/usr/bin/tmutil addexclusion " + st.Root: {}}}
	if err := st.ExcludeFromBackups(context.Background(), fake); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls()) != 1 {
		t.Errorf("calls = %d, want 1", len(fake.Calls()))
	}
}

func TestRemoveDeletesStage(t *testing.T) {
	st := mustOpen(t)
	st.WriteFile("manifest.json", []byte("{}"))
	if err := st.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st.Root); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat after Remove: %v, want not exist", err)
	}
}
