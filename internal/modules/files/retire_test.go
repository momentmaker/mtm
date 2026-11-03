package files

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

func TestWalkBeforeFollowsTheWalk(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"a", "a/b", true},
		{"a/b", "a.txt", true},
		{"a.txt", "a/b", false},
		{"B", "a", true},
		{"a/z", "b", true},
		{"a", "a", false},
	}
	for _, tt := range tests {
		if got := walkBefore(tt.a, tt.b); got != tt.want {
			t.Errorf("walkBefore(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func retireHome(t *testing.T, home string, m *manifest.Manifest) []module.Check {
	t.Helper()
	st, err := stage.Existing(home)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New().Retire(module.Env{Ctx: context.Background(), Home: home, Manifest: m}, st)
	if err != nil {
		t.Fatal(err)
	}
	return r.Changes
}

func TestRetireListsNothingWhenNothingChanged(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	if changes := retireHome(t, home, m); len(changes) != 0 {
		t.Errorf("changes = %+v, want none", changes)
	}
}

func TestRetireListsChangedAndNewFiles(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	testutil.WriteFile(t, filepath.Join(home, "Documents/notes.md"), "notes, longer now\n")
	testutil.WriteFile(t, filepath.Join(home, "Documents/a.md"), "new\n")
	testutil.WriteFile(t, filepath.Join(home, "Documents/drafts/b.md"), "new\n")
	testutil.WriteFile(t, filepath.Join(home, "Library/Preferences/q.plist"), "app state\n")
	testutil.WriteFile(t, filepath.Join(home, "GitHub/app/.DS_Store"), "volatile, rewritten\n")
	if err := os.Remove(filepath.Join(home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	changes := retireHome(t, home, m)
	want := []module.Check{
		{Module: Name, ID: "files:Documents", Status: module.StatusWarn, Title: "Documents", Detail: "1 changed, 2 new",
			Paths: []string{"Documents/a.md", "Documents/drafts/b.md", "Documents/notes.md"}},
		{Module: Name, ID: "files:Library/Preferences", Status: module.StatusInfo, Title: "Library/Preferences", Detail: "1 new",
			Paths: []string{"Library/Preferences/q.plist"}},
	}
	if !slices.EqualFunc(changes, want, checksEqual) {
		t.Errorf("changes = %+v\nwant      %+v", changes, want)
	}
}

func checksEqual(a, b module.Check) bool {
	return a.ID == b.ID && a.Status == b.Status && a.Title == b.Title && a.Detail == b.Detail && slices.Equal(a.Paths, b.Paths)
}

// A file copied in with its old time, as Finder and cp -p do, still
// isn't in the record.
func TestRetireListsANewFileWithAnOldTime(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	path := filepath.Join(home, "Documents/old-photo.jpg")
	testutil.WriteFile(t, path, "jpeg\n")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	changes := retireHome(t, home, m)
	if len(changes) != 1 || !slices.Equal(changes[0].Paths, []string{"Documents/old-photo.jpg"}) {
		t.Errorf("changes = %+v, want the copied-in file", changes)
	}
}

func TestRetireListsAFileRewrittenWithTheSameSize(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	path := filepath.Join(home, "Documents/notes.md")
	later := time.Now().Add(time.Minute)
	testutil.WriteFile(t, path, "NOTES\n")
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	changes := retireHome(t, home, m)
	if len(changes) != 1 || changes[0].Detail != "1 changed" {
		t.Errorf("changes = %+v, want notes.md changed", changes)
	}
}

func TestRetireListsAFolderItCanNoLongerRead(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	dir := filepath.Join(home, "Documents")
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	changes := retireHome(t, home, m)
	if len(changes) != 1 || changes[0].Title != "Documents" || changes[0].Detail != "1 couldn't be read" {
		t.Errorf("changes = %+v, want Documents unreadable", changes)
	}
}

func TestRetireSkipsAFileUnreadableAtPrepare(t *testing.T) {
	home := seedHome(t)
	path := filepath.Join(home, ".zshrc")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })
	m := prepareHome(t, home)
	if changes := retireHome(t, home, m); len(changes) != 0 {
		t.Errorf("changes = %+v, want none", changes)
	}
}

func TestRetireSkipsWhatAFolderUnreadableAtPrepareHolds(t *testing.T) {
	home := seedHome(t)
	dir := filepath.Join(home, "Documents")
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	m := prepareHome(t, home)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if changes := retireHome(t, home, m); len(changes) != 0 {
		t.Errorf("changes = %+v, want none", changes)
	}
}

func TestRetireSkipsWhatAFolderThatWasCloudOnlyOrAnotherVolumeAtPrepareHolds(t *testing.T) {
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, "Documents/cloudy/a.txt"), "a\n")
	testutil.WriteFile(t, filepath.Join(home, "Media/x.txt"), "x\n")
	var buf bytes.Buffer
	w := manifest.NewRecordWriter(&buf)
	for _, e := range []manifest.Entry{
		{Path: "Documents", Type: manifest.TypeDir},
		{Path: "Documents/cloudy", Type: manifest.TypeCloud},
		{Path: "Media", Type: manifest.TypeMount},
	} {
		if err := w.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	rr, err := manifest.NewRecordReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tally, err := New().changes(module.Env{Ctx: context.Background(), Home: home}, rr)
	if err != nil {
		t.Fatal(err)
	}
	if changes := tally.checks(); len(changes) != 0 {
		t.Errorf("changes = %+v, want none", changes)
	}
}

// The record holds Documents/a/x.md before Documents/a.txt, in walk order.
// Deleting the first must not make mtm lose its place and call the
// second new.
func TestRetireKeepsItsPlacePastADeletedFile(t *testing.T) {
	home := seedHome(t)
	testutil.WriteFile(t, filepath.Join(home, "Documents/a/x.md"), "x\n")
	testutil.WriteFile(t, filepath.Join(home, "Documents/a.txt"), "a\n")
	m := prepareHome(t, home)
	if err := os.Remove(filepath.Join(home, "Documents/a/x.md")); err != nil {
		t.Fatal(err)
	}
	if changes := retireHome(t, home, m); len(changes) != 0 {
		t.Errorf("changes = %+v, want none", changes)
	}
}
