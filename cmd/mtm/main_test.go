package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/modules/files"
	"github.com/momentmaker/mtm/internal/modules/git"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

var ctx = context.Background()

func noApp(t *testing.T) func(string) *app.App {
	return func(string) *app.App {
		t.Fatal("app should not be built for this command")
		return nil
	}
}

func fileVaultOffApp(home string) *app.App {
	responses := testutil.MacResponses("AAAA")
	responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
	return &app.App{Runner: &runner.Fake{Responses: responses}, Home: home, Version: "test", Now: time.Now}
}

func TestVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"version"}, &out, &errOut, noApp(t)); code != exitOK || out.String() != "mtm dev\n" {
		t.Errorf("code %d out %q", code, out.String())
	}
}

func TestNoArgsShowsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(ctx, nil, &out, &errOut, noApp(t)); code != exitFailed || !strings.Contains(errOut.String(), "Usage:") {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"bogus"}, &out, &errOut, noApp(t)); code != exitFailed || !strings.Contains(errOut.String(), `unknown command "bogus"`) {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestBadFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"scan", "--bogus"}, &out, &errOut, fileVaultOffApp); code != exitFailed {
		t.Errorf("code %d, want %d", code, exitFailed)
	}
}

func TestScanExitsOneOnBlockers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"scan"}, &out, &errOut, fileVaultOffApp)
	if code != exitProblems || !strings.Contains(out.String(), "FileVault is off") {
		t.Errorf("code %d out:\n%s", code, out.String())
	}
}

func TestScanJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	run(ctx, []string{"scan", "--json"}, &out, &errOut, fileVaultOffApp)
	if !json.Valid(out.Bytes()) {
		t.Errorf("not valid JSON:\n%s", out.String())
	}
}

func TestPrepareNotReadyExitsOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"prepare"}, &out, &errOut, fileVaultOffApp)
	if code != exitProblems || !strings.Contains(out.String(), "NOT READY") {
		t.Errorf("code %d out:\n%s", code, out.String())
	}
}

func TestVerifyWithoutRecordFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"verify"}, &out, &errOut, fileVaultOffApp)
	if code != exitFailed || !strings.Contains(errOut.String(), "run mtm prepare") {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestCleanupWithNothingToClean(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"cleanup"}, &out, &errOut, fileVaultOffApp)
	if code != exitOK || !strings.Contains(out.String(), "Nothing to clean up") {
		t.Errorf("code %d out %q", code, out.String())
	}
}

// macApp builds the real app for a Mac whose hardware UUID is uuid. System
// tools are faked; git runs for real.
func macApp(uuid string) func(string) *app.App {
	return func(home string) *app.App {
		responses := testutil.MacResponses(uuid)
		responses["/usr/bin/tmutil addexclusion "+filepath.Join(home, stage.DirName)] = runner.Response{}
		return &app.App{
			Runner:  &runner.Fake{Responses: responses, Fallback: runner.Exec{}},
			Home:    home,
			Version: "test",
			Now:     time.Now,
			Modules: []module.Module{files.New(), git.New()},
		}
	}
}

// preparedHome seeds a temporary HOME with a dotfile, a document, and a
// repo, and runs mtm prepare there as the Mac with uuid.
func preparedHome(t *testing.T, uuid string) {
	t.Helper()
	testutil.IsolateGit(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	testutil.WriteFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	testutil.WriteFile(t, filepath.Join(home, "Documents/notes.md"), "notes\n")
	repo := filepath.Join(home, "GitHub", "app")
	testutil.WriteFile(t, filepath.Join(repo, "main.go"), "package main\n")
	testutil.Git(t, repo, "init", "-q", "-b", "main")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-q", "-m", "first")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"prepare"}, &out, &errOut, macApp(uuid)); code != exitOK {
		t.Fatalf("prepare exit %d\n%s%s", code, out.String(), errOut.String())
	}
}

func TestVerifyOnNewMacExitsZeroWhenSafe(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"verify"}, &out, &errOut, macApp("NEW-MAC"))
	if code != exitOK || !strings.Contains(out.String(), "SAFE TO WIPE") || strings.Contains(out.String(), "NOT SAFE") {
		t.Errorf("exit %d, want %d and SAFE TO WIPE:\n%s%s", code, exitOK, out.String(), errOut.String())
	}
}

func TestVerifyOnSameMacExitsOneWhenNotSafe(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"verify"}, &out, &errOut, macApp("OLD-MAC"))
	if code != exitProblems || !strings.Contains(out.String(), "NOT SAFE TO WIPE") {
		t.Errorf("exit %d, want %d and NOT SAFE TO WIPE:\n%s%s", code, exitProblems, out.String(), errOut.String())
	}
}

func fileVaultOnApp(home string) *app.App {
	return &app.App{Runner: &runner.Fake{Responses: testutil.MacResponses("AAAA")}, Home: home, Version: "test", Now: time.Now}
}

func TestFinishWithoutRecordFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"finish"}, &out, &errOut, fileVaultOnApp)
	if code != exitFailed || !strings.Contains(errOut.String(), "run mtm prepare") {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestPrepareUndoRejectsOtherFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"prepare", "--undo", "--only", "git"}, &out, &errOut, fileVaultOnApp)
	if code != exitFailed || !strings.Contains(errOut.String(), "can't be combined") {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestPrepareUndoWithoutRecordFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"prepare", "--undo"}, &out, &errOut, fileVaultOnApp)
	if code != exitFailed || !strings.Contains(errOut.String(), "run mtm prepare") {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestDefaultAppModules(t *testing.T) {
	var names []string
	for _, m := range defaultApp(t.TempDir()).Modules {
		names = append(names, m.Name())
	}
	if want := []string{"files", "git", "system", "homebrew", "toolchains", "loginitems"}; !slices.Equal(names, want) {
		t.Errorf("modules = %q, want %q", names, want)
	}
}
