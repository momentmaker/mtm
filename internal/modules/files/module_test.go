package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

func seedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for rel, content := range map[string]string{
		".zshrc":                      "export A=1\n",
		".ssh/id_ed25519":             "key\n",
		"GitHub/app/main.go":          "package main\n",
		"GitHub/app/.DS_Store":        "ds\n",
		"Documents/notes.md":          "notes\n",
		"Library/Preferences/p.plist": "plist\n",
	} {
		testutil.WriteFile(t, filepath.Join(home, rel), content)
	}
	return home
}

func prepareHome(t *testing.T, home string) *manifest.Manifest {
	t.Helper()
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	preparedAt := time.Now()
	res, err := New().Prepare(module.Env{Ctx: context.Background(), Home: home}, st)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.New("test")
	if err := m.SetModule(Name, preparedAt, res.Inventory); err != nil {
		t.Fatal(err)
	}
	return m
}

func verifyHome(t *testing.T, home string, m *manifest.Manifest) map[string]module.Check {
	t.Helper()
	st, err := stage.Existing(home)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := New().Verify(module.Env{Ctx: context.Background(), Home: home, Manifest: m}, st)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]module.Check{}
	for _, c := range checks {
		byTitle[c.Title] = c
	}
	return byTitle
}

func assertAllPass(t *testing.T, checks map[string]module.Check) {
	t.Helper()
	for title, c := range checks {
		if c.Status != module.StatusPass {
			t.Errorf("%s: %s (%s) %v", title, c.Status, c.Detail, c.Paths)
		}
	}
}

func TestPrepareThenVerifyPasses(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	checks := verifyHome(t, home, m)
	assertAllPass(t, checks)
	for _, group := range []string{HomeGroup, ".ssh", "GitHub", "Documents", "Library/Preferences"} {
		if _, ok := checks[group]; !ok {
			t.Errorf("no check for group %q", group)
		}
	}
}

func TestVerifyFailsOnMissingCriticalFile(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	os.Remove(filepath.Join(home, ".ssh/id_ed25519"))
	c := verifyHome(t, home, m)[".ssh"]
	if c.Status != module.StatusFail || !c.Critical || !slices.Contains(c.Paths, ".ssh/id_ed25519") {
		t.Errorf(".ssh check = %+v, want critical fail listing the key", c)
	}
}

func TestVerifyFailsOnCorruptionWithOriginalTime(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	path := filepath.Join(home, ".zshrc")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("export B=2\n"), 0o644)
	os.Chtimes(path, info.ModTime(), info.ModTime())
	if c := verifyHome(t, home, m)[HomeGroup]; c.Status != module.StatusFail {
		t.Errorf("home group = %+v, want fail", c)
	}
}

func TestVerifyFailsOnCorruptionOfFutureDatedFile(t *testing.T) {
	home := seedHome(t)
	path := filepath.Join(home, ".zshrc")
	future := time.Now().Add(time.Hour)
	os.Chtimes(path, future, future)
	m := prepareHome(t, home)
	os.WriteFile(path, []byte("export B=2\n"), 0o644)
	os.Chtimes(path, future, future)
	if c := verifyHome(t, home, m)[HomeGroup]; c.Status != module.StatusFail {
		t.Errorf("home group = %+v, want fail: same time as recorded, different content", c)
	}
}

func TestVerifyNotesChangesAfterPrepare(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	path := filepath.Join(home, "GitHub/app/main.go")
	os.WriteFile(path, []byte("package main // edited\n"), 0o644)
	later := time.Now().Add(time.Minute)
	os.Chtimes(path, later, later)
	c := verifyHome(t, home, m)["GitHub"]
	if c.Status != module.StatusPass || !strings.Contains(c.Detail, "1 changed after prepare") {
		t.Errorf("GitHub check = %+v, want pass noting the change", c)
	}
}

func TestVerifyWarnsOnMissingAppState(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	os.Remove(filepath.Join(home, "Library/Preferences/p.plist"))
	c := verifyHome(t, home, m)["Library/Preferences"]
	if c.Status != module.StatusWarn || c.Critical {
		t.Errorf("preferences check = %+v, want non-critical warn", c)
	}
}

func TestVerifyIgnoresMissingVolatile(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	os.Remove(filepath.Join(home, "GitHub/app/.DS_Store"))
	c := verifyHome(t, home, m)["GitHub"]
	if c.Status != module.StatusPass || !strings.Contains(c.Detail, "1 missing (expected)") {
		t.Errorf("GitHub check = %+v, want pass noting expected missing file", c)
	}
}

func lockDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o755) })
}

func TestVerifyBlocksOnUnreadableCriticalFolder(t *testing.T) {
	home := seedHome(t)
	testutil.WriteFile(t, filepath.Join(home, "Private/secret.txt"), "x")
	lockDir(t, filepath.Join(home, "Private"))
	m := prepareHome(t, home)
	c := verifyHome(t, home, m)["Private"]
	if c.Status != module.StatusWarn || !c.Critical || !strings.Contains(c.Detail, "not checked") || !strings.Contains(c.Detail, "Full Disk Access") {
		t.Errorf("Private check = %+v, want critical warn naming the Full Disk Access remedy", c)
	}
	if !slices.Contains(c.Paths, "Private") {
		t.Errorf("Private check paths = %q, want the unreadable folder listed", c.Paths)
	}
}

func TestVerifyBlocksOnCriticalFolderUnreadableOnNewMac(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	lockDir(t, filepath.Join(home, "Documents"))
	c := verifyHome(t, home, m)["Documents"]
	if c.Status != module.StatusWarn || !c.Critical || !slices.Contains(c.Paths, "Documents/notes.md") {
		t.Errorf("Documents check = %+v, want critical warn listing notes.md", c)
	}
}

func TestVerifyNotesUnreadableAppStateWithoutBlocking(t *testing.T) {
	home := seedHome(t)
	testutil.WriteFile(t, filepath.Join(home, "Library/Private/data.db"), "x")
	lockDir(t, filepath.Join(home, "Library/Private"))
	m := prepareHome(t, home)
	c := verifyHome(t, home, m)["Library/Private"]
	if c.Status != module.StatusPass || c.Critical || !strings.Contains(c.Detail, "not checked") {
		t.Errorf("Library/Private check = %+v, want non-critical pass noting not checked", c)
	}
}

func TestPrepareBlocksOnUnreadableCriticalFolder(t *testing.T) {
	home := seedHome(t)
	testutil.WriteFile(t, filepath.Join(home, "Private/secret.txt"), "x")
	testutil.WriteFile(t, filepath.Join(home, "Library/Private/data.db"), "x")
	lockDir(t, filepath.Join(home, "Private"))
	lockDir(t, filepath.Join(home, "Library/Private"))
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	res, err := New().Prepare(module.Env{Ctx: context.Background(), Home: home}, st)
	if err != nil {
		t.Fatal(err)
	}
	if !module.HasBlocker(res.Problems) {
		t.Fatalf("problems = %+v, want a blocker for the unreadable critical folder", res.Problems)
	}
	p := res.Problems[0]
	if p.Handler != module.Manual || !strings.Contains(p.Detail, "~/Private") || strings.Contains(p.Detail, "Library") {
		t.Errorf("blocker = %+v, want a manual blocker naming only ~/Private", p)
	}
	for _, want := range []string{"Full Disk Access", "sudo chown -R $(id -un)", "mtm prepare again"} {
		if !strings.Contains(p.Fix, want) {
			t.Errorf("fix %q missing %q", p.Fix, want)
		}
	}
	if !slices.ContainsFunc(res.Done, func(d string) bool { return strings.Contains(d, "could not read 1 folder") }) {
		t.Errorf("done = %q, want the app state unreadable count", res.Done)
	}
}

func TestPrepareListsAtMostTwentyUnreadablePaths(t *testing.T) {
	stats := Stats{}
	for i := range 25 {
		stats.Unreadable = append(stats.Unreadable, fmt.Sprintf("Dir%02d", i))
	}
	p := prepareProblems(stats)
	if len(p) != 1 || !strings.Contains(p[0].Detail, "~/Dir19") || strings.Contains(p[0].Detail, "~/Dir20") || !strings.Contains(p[0].Detail, "and 5 more") {
		t.Errorf("problems = %+v, want 20 paths and \"and 5 more\"", p)
	}
}

func TestVerifyFailsWhenLinkTargetOutsideHomeVanished(t *testing.T) {
	home := seedHome(t)
	outside := filepath.Join(t.TempDir(), "code")
	testutil.WriteFile(t, filepath.Join(outside, "precious.go"), "package p\n")
	if err := os.Symlink(outside, filepath.Join(home, "code")); err != nil {
		t.Fatal(err)
	}
	m := prepareHome(t, home)
	assertAllPass(t, verifyHome(t, home, m))
	os.RemoveAll(outside)
	c := verifyHome(t, home, m)[HomeGroup]
	if c.Status != module.StatusFail || !slices.Contains(c.Paths, "code") {
		t.Errorf("home group = %+v, want fail listing the dangling link", c)
	}
}

func TestVerifyReportsLinkTargetUnreadableOnNewMac(t *testing.T) {
	home := seedHome(t)
	if err := os.Symlink("Documents/notes.md", filepath.Join(home, "notes")); err != nil {
		t.Fatal(err)
	}
	m := prepareHome(t, home)
	lockDir(t, filepath.Join(home, "Documents"))
	c := verifyHome(t, home, m)[HomeGroup]
	if c.Status != module.StatusWarn || !strings.Contains(c.Detail, "not checked") {
		t.Errorf("home group = %+v, want warn: link target unreadable, not dangling", c)
	}
}

func TestPrepareWarnsAboutLinksOutsideHome(t *testing.T) {
	home := seedHome(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "code")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("Documents", filepath.Join(home, "docs")); err != nil {
		t.Fatal(err)
	}
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	res, err := New().Prepare(module.Env{Ctx: context.Background(), Home: home}, st)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(outside)
	i := slices.IndexFunc(res.Problems, func(f module.Finding) bool { return f.ID == "outside-links" })
	if i < 0 {
		t.Fatalf("problems = %+v, want a warning about ~/code", res.Problems)
	}
	p := res.Problems[i]
	if p.Severity != module.SeverityWarn || !strings.Contains(p.Detail, "~/code → "+real) || strings.Contains(p.Detail, "~/docs") || !strings.Contains(p.Fix, "mtm only checks your home folder") {
		t.Errorf("warning = %+v", p)
	}
}

func TestVerifyFailsWhenRecordIsShorterThanInventory(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	var stats Stats
	preparedAt, err := m.Module(Name, &stats)
	if err != nil {
		t.Fatal(err)
	}
	recorded := stats.Entries
	stats.Entries += 5
	if err := m.SetModule(Name, preparedAt, stats); err != nil {
		t.Fatal(err)
	}
	st, err := stage.Existing(home)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := New().Verify(module.Env{Ctx: context.Background(), Home: home, Manifest: m}, st)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(checks, func(c module.Check) bool { return c.ID == "files:record" })
	if i < 0 {
		t.Fatalf("checks = %+v, want files:record", checks)
	}
	want := fmt.Sprintf("read %d of %d entries", recorded, recorded+5)
	if c := checks[i]; c.Status != module.StatusFail || !c.Critical || c.Title != "the file record is incomplete" || !strings.Contains(c.Detail, want) {
		t.Errorf("check = %+v, want a critical fail: %s", c, want)
	}
}

func TestVerifyWorksAfterCopyToDifferentHome(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	newHome := filepath.Join(t.TempDir(), "newuser")
	testutil.Copy(t, home, newHome)
	assertAllPass(t, verifyHome(t, newHome, m))
}

func TestScanReportsMissingFullDiskAccess(t *testing.T) {
	home := t.TempDir()
	safari := filepath.Join(home, "Library/Safari")
	os.MkdirAll(safari, 0o755)
	os.Chmod(safari, 0)
	t.Cleanup(func() { os.Chmod(safari, 0o755) })
	findings, err := New().Scan(module.Env{Ctx: context.Background(), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(findings, func(f module.Finding) bool { return f.ID == "full-disk-access" }) {
		t.Errorf("findings = %+v, want full-disk-access warning", findings)
	}
}

func TestPrepareWarnsAboutMountPoints(t *testing.T) {
	var stats Stats
	stats.add(manifest.Entry{Path: "code", Type: manifest.TypeMount, Area: manifest.AreaCritical})
	p := prepareProblems(stats)
	if len(p) != 1 || p[0].Severity != module.SeverityWarn || p[0].Detail != "~/code" || !strings.Contains(p[0].Fix, "back it up before you erase this Mac") {
		t.Errorf("problems = %+v, want one warning naming ~/code", p)
	}
}

func TestScanBlocksOnUnreadableDocuments(t *testing.T) {
	home := t.TempDir()
	docs := filepath.Join(home, "Documents")
	os.MkdirAll(docs, 0o755)
	lockDir(t, docs)
	findings, err := New().Scan(module.Env{Ctx: context.Background(), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(findings, func(f module.Finding) bool { return f.Severity == module.SeverityBlocker })
	if i < 0 {
		t.Fatalf("findings = %+v, want a blocker for ~/Documents", findings)
	}
	if f := findings[i]; !strings.Contains(f.Detail, "~/Documents") || !strings.Contains(f.Fix, "Full Disk Access") {
		t.Errorf("blocker = %+v, want it to name ~/Documents and the remedy", f)
	}
}

func TestScanWithReadableHomeHasNoAccessWarning(t *testing.T) {
	findings, err := New().Scan(module.Env{Ctx: context.Background(), Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].ID != "home" {
		t.Errorf("findings = %+v, want only the home info finding", findings)
	}
}
