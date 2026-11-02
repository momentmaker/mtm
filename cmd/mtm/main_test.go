package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/runner"
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
