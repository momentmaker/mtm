package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/testutil"
)

func recordHome(t *testing.T, home string, deep bool) (map[string]manifest.Entry, Stats) {
	t.Helper()
	var buf bytes.Buffer
	w := manifest.NewRecordWriter(&buf)
	stats, err := writeRecord(context.Background(), home, w, deep)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := manifest.NewRecordReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]manifest.Entry{}
	for {
		e, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		entries[e.Path] = e
	}
	return entries, stats
}

func sha(content string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
}

func TestRecordFingerprintsImportantFilesOnly(t *testing.T) {
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	testutil.WriteFile(t, filepath.Join(home, ".ssh/id_ed25519"), "key\n")
	testutil.WriteFile(t, filepath.Join(home, "GitHub/app/main.go"), "package main\n")

	entries, stats := recordHome(t, home, false)

	if got := entries[".zshrc"].Hash; got != sha("export A=1\n") {
		t.Errorf(".zshrc hash = %q", got)
	}
	if got := entries[".ssh/id_ed25519"].Hash; got != sha("key\n") {
		t.Errorf("ssh key hash = %q", got)
	}
	main := entries["GitHub/app/main.go"]
	if main.Hash != "" || main.Type != manifest.TypeFile || main.Size != 13 || main.Area != manifest.AreaCritical {
		t.Errorf("main.go entry = %+v", main)
	}
	if entries["GitHub"].Type != manifest.TypeDir {
		t.Errorf("GitHub entry = %+v, want a folder", entries["GitHub"])
	}
	if stats.Fingerprinted != 2 {
		t.Errorf("Fingerprinted = %d, want 2", stats.Fingerprinted)
	}
}

func TestRecordDeepFingerprintsEverything(t *testing.T) {
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, "GitHub/app/main.go"), "package main\n")
	entries, _ := recordHome(t, home, true)
	if got := entries["GitHub/app/main.go"].Hash; got != sha("package main\n") {
		t.Errorf("deep hash = %q", got)
	}
}

func TestRecordSkipsExcludedPaths(t *testing.T) {
	home := t.TempDir()
	for _, rel := range []string{
		"Library/Caches/big",
		".mtm/manifest.json",
		".Trash/old.txt",
		"Library/Containers/com.docker.docker/Data/vms/0/data/Docker.raw",
		"Library/Preferences/p.plist",
	} {
		testutil.WriteFile(t, filepath.Join(home, rel), "x")
	}
	entries, _ := recordHome(t, home, false)
	for rel := range entries {
		if excluded.Match(rel) {
			t.Errorf("excluded path recorded: %q", rel)
		}
	}
	if got := entries["Library/Preferences/p.plist"]; got.Area != manifest.AreaAppState {
		t.Errorf("preferences entry = %+v, want app state", got)
	}
}

func TestRecordKeepsUnusualNames(t *testing.T) {
	home := t.TempDir()
	names := []string{"Docs/résumé.pdf", "Docs/with space.txt", "Docs/odd\nname.txt", "Docs/-rf"}
	for _, rel := range names {
		testutil.WriteFile(t, filepath.Join(home, rel), "x")
	}
	entries, _ := recordHome(t, home, false)
	for _, rel := range names {
		if entries[rel].Type != manifest.TypeFile {
			t.Errorf("%q not recorded as a file: %+v", rel, entries[rel])
		}
	}
}

func TestRecordNeverFollowsSymlinks(t *testing.T) {
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, "GitHub/app/main.go"), "package main\n")
	links := map[string]string{"applink": "GitHub/app", "loop": "loop", "dangling": "nowhere", "outside": "/etc"}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(home, name)); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := recordHome(t, home, false)
	for name, target := range links {
		if e := entries[name]; e.Type != manifest.TypeLink || e.Target != target {
			t.Errorf("%s = %+v, want link to %q", name, e, target)
		}
	}
	for rel := range entries {
		if strings.HasPrefix(rel, "applink/") {
			t.Errorf("walk followed a symlink: %q", rel)
		}
	}
}

func TestRecordMarksUnreadableFolders(t *testing.T) {
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, "Private/secret.txt"), "x")
	testutil.WriteFile(t, filepath.Join(home, "Public/readme.txt"), "y")
	private := filepath.Join(home, "Private")
	if err := os.Chmod(private, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(private, 0o755) })

	entries, stats := recordHome(t, home, false)

	if got := entries["Private"].Type; got != manifest.TypeUnreadable {
		t.Errorf("Private type = %q, want unreadable", got)
	}
	if _, ok := entries["Private/secret.txt"]; ok {
		t.Error("recorded a file inside an unreadable folder")
	}
	if _, ok := entries["Public/readme.txt"]; !ok {
		t.Error("walk stopped at the unreadable folder")
	}
	if !slices.Equal(stats.Unreadable, []string{"Private"}) {
		t.Errorf("Unreadable = %q, want [Private]", stats.Unreadable)
	}
}

func TestRecordSkipsPipes(t *testing.T) {
	home := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(home, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, _ := recordHome(t, home, true)
	if _, ok := entries["pipe"]; ok {
		t.Error("recorded a named pipe")
	}
}
