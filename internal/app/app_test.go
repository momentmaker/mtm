package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/modules/files"
	"github.com/momentmaker/mtm/internal/modules/git"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

const (
	oldUUID = "OLD-MAC-UUID"
	newUUID = "NEW-MAC-UUID"
)

var ctx = context.Background()

func fakeMac(home, uuid string) *runner.Fake {
	responses := testutil.MacResponses(uuid)
	responses["/usr/bin/tmutil addexclusion "+filepath.Join(home, stage.DirName)] = runner.Response{}
	return &runner.Fake{Responses: responses, Fallback: runner.Exec{}}
}

func newTestApp(home string, r runner.Runner) *App {
	return &App{Runner: r, Home: home, Version: "test", Now: time.Now, Modules: []module.Module{files.New(), git.New()}}
}

func seedHome(t *testing.T) string {
	t.Helper()
	testutil.IsolateGit(t)
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	testutil.WriteFile(t, filepath.Join(home, ".ssh/id_ed25519"), "key\n")
	testutil.WriteFile(t, filepath.Join(home, "Documents/notes.md"), "notes\n")
	testutil.WriteFile(t, filepath.Join(home, "Library/Preferences/p.plist"), "plist\n")
	repo := filepath.Join(home, "GitHub", "app")
	os.MkdirAll(repo, 0o755)
	testutil.Git(t, repo, "init", "-q", "-b", "main")
	testutil.WriteFile(t, filepath.Join(repo, ".gitignore"), ".env\n")
	testutil.WriteFile(t, filepath.Join(repo, "main.go"), "package main\n")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-q", "-m", "first")
	testutil.WriteFile(t, filepath.Join(repo, ".env"), "SECRET=1\n")
	return home
}

func prepareOld(t *testing.T, home string) {
	t.Helper()
	rep, err := newTestApp(home, fakeMac(home, oldUUID)).Prepare(ctx, PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Ready {
		t.Fatalf("not ready: %+v", rep.Problems)
	}
}

func migrate(t *testing.T, oldHome string) string {
	t.Helper()
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, oldHome, newHome)
	return newHome
}

func verifyOn(t *testing.T, home, uuid string, opts VerifyOptions) VerifyReport {
	t.Helper()
	rep, err := newTestApp(home, fakeMac(home, uuid)).Verify(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func blockingIDs(rep VerifyReport) []string {
	var ids []string
	for _, c := range rep.Verdict.Blocking {
		ids = append(ids, c.ID)
	}
	return ids
}

// keepTime rewrites path with same-length content and restores its old
// modification time: corruption that a timestamp check alone would miss.
func keepTime(t *testing.T, path, content string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(path, info.ModTime(), info.ModTime())
}

func TestSimulatedMigrationIsSafe(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	rep := verifyOn(t, migrate(t, old), newUUID, VerifyOptions{})
	if !rep.Verdict.Safe {
		t.Fatalf("not safe; blocking: %v", blockingIDs(rep))
	}
}

func TestVerifyCatchesFaults(t *testing.T) {
	tests := []struct {
		name   string
		damage func(t *testing.T, home string)
		want   string
	}{
		{"missing ssh key", func(t *testing.T, home string) {
			os.Remove(filepath.Join(home, ".ssh/id_ed25519"))
		}, "files:.ssh"},
		{"dotfile corrupted with original time", func(t *testing.T, home string) {
			keepTime(t, filepath.Join(home, ".zshrc"), "export B=2\n")
		}, "files:" + files.HomeGroup},
		{"repo missing", func(t *testing.T, home string) {
			os.RemoveAll(filepath.Join(home, "GitHub", "app"))
		}, "git:GitHub/app"},
		{"ignored secret corrupted with original time", func(t *testing.T, home string) {
			keepTime(t, filepath.Join(home, "GitHub", "app", ".env"), "SECRET=2\n")
		}, "git:GitHub/app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := seedHome(t)
			prepareOld(t, old)
			newHome := migrate(t, old)
			tt.damage(t, newHome)
			rep := verifyOn(t, newHome, newUUID, VerifyOptions{})
			if rep.Verdict.Safe {
				t.Fatal("verdict is SAFE TO WIPE, want NOT SAFE")
			}
			if !slices.Contains(blockingIDs(rep), tt.want) {
				t.Errorf("blocking = %v, want it to include %q", blockingIDs(rep), tt.want)
			}
		})
	}
}

func TestChangesAfterPrepareStaySafe(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	notes := filepath.Join(newHome, "Documents/notes.md")
	os.WriteFile(notes, []byte("edited on the new Mac\n"), 0o644)
	later := time.Now().Add(time.Minute)
	os.Chtimes(notes, later, later)
	if rep := verifyOn(t, newHome, newUUID, VerifyOptions{}); !rep.Verdict.Safe {
		t.Errorf("not safe after a normal edit; blocking: %v", blockingIDs(rep))
	}
}

func TestVerifyOnSameMacIsNotSafe(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	rep := verifyOn(t, old, oldUUID, VerifyOptions{})
	if rep.Verdict.Safe || !slices.Contains(blockingIDs(rep), "same-mac") {
		t.Errorf("blocking = %v, want same-mac", blockingIDs(rep))
	}
}

func TestVerifyWithoutRecord(t *testing.T) {
	home := t.TempDir()
	_, err := newTestApp(home, fakeMac(home, newUUID)).Verify(ctx, VerifyOptions{})
	if !errors.Is(err, stage.ErrNotFound) {
		t.Errorf("err = %v, want stage.ErrNotFound", err)
	}
}

func TestPrepareRefusesWithoutFileVault(t *testing.T) {
	home := seedHome(t)
	fake := fakeMac(home, oldUUID)
	fake.Responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
	rep, err := newTestApp(home, fake).Prepare(ctx, PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ready {
		t.Error("ready without FileVault")
	}
	if _, err := os.Stat(filepath.Join(home, stage.DirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("~/.mtm created without FileVault: %v", err)
	}
}

func TestPrepareOnlyKeepsOtherModules(t *testing.T) {
	home := seedHome(t)
	prepareOld(t, home)
	rep, err := newTestApp(home, fakeMac(home, oldUUID)).Prepare(ctx, PrepareOptions{Only: []string{"git"}})
	if err != nil || !rep.Ready {
		t.Fatalf("prepare --only git: %+v, %v", rep, err)
	}
	st, _ := stage.Existing(home)
	m, err := manifest.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Modules[files.Name]; !ok {
		t.Error("files module dropped from the manifest")
	}
}

func TestPrepareRejectsUnknownModule(t *testing.T) {
	home := seedHome(t)
	_, err := newTestApp(home, fakeMac(home, oldUUID)).Prepare(ctx, PrepareOptions{Only: []string{"bogus"}})
	if err == nil || !strings.Contains(err.Error(), `unknown module "bogus"`) {
		t.Errorf("err = %v, want unknown module", err)
	}
}

func TestPrepareRefusesToOverwriteAnotherMacsRecord(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	_, err := newTestApp(newHome, fakeMac(newHome, newUUID)).Prepare(ctx, PrepareOptions{})
	if !errors.Is(err, ErrOtherMacsRecord) || !strings.Contains(err.Error(), "mtm verify") {
		t.Errorf("err = %v, want ErrOtherMacsRecord pointing to mtm verify", err)
	}
}

func TestPrepareWritesRedactedLog(t *testing.T) {
	home := seedHome(t)
	prepareOld(t, home)
	logs, err := os.ReadDir(filepath.Join(home, stage.DirName, "logs"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs = %v, %v; want one log file", logs, err)
	}
	data, _ := os.ReadFile(filepath.Join(home, stage.DirName, "logs", logs[0].Name()))
	if !strings.Contains(string(data), "$ /usr/sbin/ioreg") || !strings.Contains(string(data), "$ git -C") {
		t.Errorf("log missing expected commands:\n%s", data)
	}
	if strings.Contains(string(data), "SECRET=1") {
		t.Error("log contains file contents")
	}
}

func TestScanIsReadOnlyAndCombinesFindings(t *testing.T) {
	home := seedHome(t)
	fake := fakeMac(home, oldUUID)
	fake.Responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
	rep := newTestApp(home, fake).Scan(ctx)
	var ids []string
	for _, f := range rep.Findings {
		ids = append(ids, f.ID)
	}
	for _, want := range []string{"filevault", "home", "repos"} {
		if !slices.Contains(ids, want) {
			t.Errorf("finding ids %v missing %q", ids, want)
		}
	}
	if _, err := os.Stat(filepath.Join(home, stage.DirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("scan created ~/.mtm")
	}
}

func TestCleanupRules(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	a := newTestApp(newHome, fakeMac(newHome, newUUID))

	if _, err := a.Cleanup(false); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("cleanup before verify: %v, want ErrNotVerified", err)
	}
	verifyOn(t, newHome, newUUID, VerifyOptions{Only: []string{"git"}})
	if _, err := a.Cleanup(false); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("cleanup after verify --only: %v, want ErrNotVerified", err)
	}
	if rep := verifyOn(t, newHome, newUUID, VerifyOptions{}); !rep.Verdict.Safe {
		t.Fatalf("full verify not safe: %v", blockingIDs(rep))
	}
	removed, err := a.Cleanup(false)
	if err != nil || !removed {
		t.Fatalf("cleanup after SAFE: %v, %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(newHome, stage.DirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("~/.mtm still exists")
	}
	if removed, err := a.Cleanup(false); err != nil || removed {
		t.Errorf("second cleanup = %v, %v; want nothing to remove", removed, err)
	}
}

func TestCleanupForce(t *testing.T) {
	home := seedHome(t)
	prepareOld(t, home)
	removed, err := newTestApp(home, fakeMac(home, oldUUID)).Cleanup(true)
	if err != nil || !removed {
		t.Errorf("forced cleanup = %v, %v", removed, err)
	}
}
