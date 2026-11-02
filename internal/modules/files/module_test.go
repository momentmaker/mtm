package files

import (
	"context"
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

func TestVerifyReportsUnreadableAsNotChecked(t *testing.T) {
	home := seedHome(t)
	testutil.WriteFile(t, filepath.Join(home, "Private/secret.txt"), "x")
	private := filepath.Join(home, "Private")
	os.Chmod(private, 0)
	t.Cleanup(func() { os.Chmod(private, 0o755) })
	m := prepareHome(t, home)
	c := verifyHome(t, home, m)["Private"]
	if c.Status != module.StatusPass || !strings.Contains(c.Detail, "not checked") {
		t.Errorf("Private check = %+v, want pass noting not checked", c)
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

func TestScanWithReadableHomeHasNoAccessWarning(t *testing.T) {
	findings, err := New().Scan(module.Env{Ctx: context.Background(), Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].ID != "home" {
		t.Errorf("findings = %+v, want only the home info finding", findings)
	}
}
