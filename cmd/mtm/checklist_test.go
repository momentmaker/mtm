package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
	"github.com/momentmaker/mtm/internal/testutil"
)

const raycastLink = "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?Privacy_Accessibility"

var (
	raycastStep = checklist.Item{ID: "permissions:com.raycast.macos:Accessibility", Module: "permissions",
		Title: "Give Raycast Accessibility access", Where: "System Settings > Privacy & Security > Accessibility", Link: raycastLink}
	dashStep = checklist.Item{ID: "system:license:dash", Module: "system", Title: "Reactivate the Dash license"}
)

// checklistHome is a new Mac whose finish left two steps, with HOME set
// to it. The app it returns answers open from a fake and records it.
func checklistHome(t *testing.T) (*runner.Fake, func(string) *app.App) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	var s state.State
	s.Checklist.Add(raycastStep, dashStep)
	if err := state.Save(st, s); err != nil {
		t.Fatal(err)
	}
	fake := &runner.Fake{Responses: testutil.MacResponses("NEW-MAC")}
	fake.Responses["/usr/bin/open "+raycastLink] = runner.Response{}
	return fake, func(home string) *app.App {
		return &app.App{Runner: fake, Home: home, Version: "test", Now: time.Now}
	}
}

func asTerminal(t *testing.T, input string) {
	t.Helper()
	oldIn, oldInteractive := stdin, interactive
	stdin, interactive = strings.NewReader(input), func() bool { return true }
	t.Cleanup(func() { stdin, interactive = oldIn, oldInteractive })
}

func notTerminal(t *testing.T) {
	t.Helper()
	old := interactive
	interactive = func() bool { return false }
	t.Cleanup(func() { interactive = old })
}

func TestChecklistWithoutATerminalListsTheOpenSteps(t *testing.T) {
	_, newApp := checklistHome(t)
	notTerminal(t)
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"checklist"}, &out, &errOut, newApp)
	if code != exitOK || !strings.Contains(out.String(), "2 steps open") || !strings.Contains(out.String(), "mtm checklist done system:license:dash") {
		t.Errorf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
}

func TestChecklistDoneAndSkip(t *testing.T) {
	_, newApp := checklistHome(t)
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"checklist", "done", raycastStep.ID}, &out, &errOut, newApp); code != exitOK {
		t.Fatalf("done exit %d: %s", code, errOut.String())
	}
	if code := run(ctx, []string{"checklist", "skip", dashStep.ID}, &out, &errOut, newApp); code != exitFailed || !strings.Contains(errOut.String(), "needs a reason") {
		t.Errorf("skip without a reason: exit %d, stderr %q", code, errOut.String())
	}
	if code := run(ctx, []string{"checklist", "skip", dashStep.ID, "--reason", "sold it"}, &out, &errOut, newApp); code != exitOK {
		t.Fatalf("skip exit %d: %s", code, errOut.String())
	}
	out.Reset()
	run(ctx, []string{"checklist", "list", "--all"}, &out, &errOut, newApp)
	if !strings.Contains(out.String(), "0 steps open") || !strings.Contains(out.String(), "skipped: sold it") {
		t.Errorf("list --all:\n%s", out.String())
	}
}

func TestChecklistDoneRejectsAnUnknownStep(t *testing.T) {
	_, newApp := checklistHome(t)
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"checklist", "done", "nope"}, &out, &errOut, newApp); code != exitFailed || !strings.Contains(errOut.String(), "no checklist item has that id") {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}

func TestChecklistWalkOpensEachPaneAndRecordsTheAnswers(t *testing.T) {
	fake, newApp := checklistHome(t)
	asTerminal(t, "d\ns\nI sold my Dash license\n")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"checklist"}, &out, &errOut, newApp); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	opened := false
	for _, c := range fake.Calls() {
		opened = opened || c.String() == "/usr/bin/open "+raycastLink
	}
	if !opened {
		t.Error("the walk didn't open Raycast's pane")
	}
	if !strings.Contains(out.String(), "[1/2] Give Raycast Accessibility access") || !strings.Contains(out.String(), "Every checklist step is done or skipped.") {
		t.Errorf("walk output:\n%s", out.String())
	}
}

func TestChecklistWalkQuitsAndLaterLeavesStepsOpen(t *testing.T) {
	_, newApp := checklistHome(t)
	asTerminal(t, "l\nq\n")
	var out, errOut bytes.Buffer
	run(ctx, []string{"checklist"}, &out, &errOut, newApp)
	if !strings.Contains(out.String(), "2 steps open") {
		t.Errorf("walk output:\n%s", out.String())
	}
}

func TestChecklistWalkEndsAtTheEndOfInput(t *testing.T) {
	_, newApp := checklistHome(t)
	asTerminal(t, "")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"checklist"}, &out, &errOut, newApp); code != exitOK {
		t.Errorf("exit %d: %s", code, errOut.String())
	}
}

func TestChecklistUnknownSubcommand(t *testing.T) {
	_, newApp := checklistHome(t)
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"checklist", "frob"}, &out, &errOut, newApp); code != exitFailed || !strings.Contains(errOut.String(), `unknown subcommand "frob"`) {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}

func TestChecklistWithoutAnMtmFolder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	notTerminal(t)
	var out, errOut bytes.Buffer
	newApp := func(home string) *app.App {
		return &app.App{Runner: &runner.Fake{}, Home: home, Version: "test", Now: time.Now}
	}
	if code := run(ctx, []string{"checklist"}, &out, &errOut, newApp); code != exitFailed || !strings.Contains(errOut.String(), "run mtm prepare") {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}

func TestChecklistOpenRefusalsExitAsProblemsAndEscapeText(t *testing.T) {
	_, newApp := checklistHome(t)
	st, err := stage.Open(os.Getenv("HOME"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := state.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	s.Checklist.Add(
		checklist.Item{ID: "x:nolink", Title: "No link", Where: "Evil \x1b[2J place"},
		checklist.Item{ID: "x:odd", Title: "Odd link", Link: "file:///Applications/Calculator.app"},
	)
	if err := state.Save(st, s); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"x:nolink", "x:odd"} {
		var out, errOut bytes.Buffer
		if code := run(ctx, []string{"checklist", "open", id}, &out, &errOut, newApp); code != exitProblems {
			t.Errorf("%s: exit %d, want %d", id, code, exitProblems)
		}
		if strings.ContainsRune(errOut.String(), 0x1b) {
			t.Errorf("%s: raw ESC in stderr %q", id, errOut.String())
		}
	}
}

func TestChecklistWalkStopsWhenCancelled(t *testing.T) {
	_, newApp := checklistHome(t)
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	oldIn, oldInteractive := stdin, interactive
	stdin, interactive = pr, func() bool { return true }
	t.Cleanup(func() { stdin, interactive = oldIn, oldInteractive })
	cctx, cancel := context.WithCancel(ctx)
	var out, errOut bytes.Buffer
	finished := make(chan int)
	go func() { finished <- run(cctx, []string{"checklist"}, &out, &errOut, newApp) }()
	cancel()
	select {
	case code := <-finished:
		if code != exitOK {
			t.Errorf("exit %d: %s", code, errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("walk did not return after cancel")
	}
}
