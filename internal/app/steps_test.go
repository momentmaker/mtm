package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
	"github.com/momentmaker/mtm/internal/testutil"
)

func savedSteps(t *testing.T, items ...checklist.Item) string {
	t.Helper()
	home := t.TempDir()
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	var s state.State
	s.Checklist.Add(items...)
	if err := state.Save(st, s); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestMarkDoneAndSkipAreSaved(t *testing.T) {
	home := savedSteps(t, grantStep, licenseStep)
	a := stepApp(home, newUUID)
	if err := a.Checklist().Done(grantStep.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.Checklist().Skip(licenseStep.ID, "sold it"); err != nil {
		t.Fatal(err)
	}
	l, err := a.Checklist().List()
	if err != nil || len(l.Pending()) != 0 {
		t.Errorf("checklist = %+v, %v; want nothing open", l, err)
	}
	if err := a.Checklist().Done("steps:nope"); !errors.Is(err, checklist.ErrUnknownItem) {
		t.Errorf("err = %v, want ErrUnknownItem", err)
	}
}

// noFallbackApp answers commands only from its canned responses, unlike
// stepApp: if Open let through a link it should refuse, or changed how
// it calls open, the test fails instead of opening it on this Mac.
func noFallbackApp(home string) *App {
	return &App{Runner: &runner.Fake{Responses: testutil.MacResponses(newUUID)}, Home: home, Version: "test", Now: time.Now}
}

func TestOpenStepOpensItsSettingsPane(t *testing.T) {
	linked := grantStep
	linked.Link = "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?Privacy_Accessibility"
	home := savedSteps(t, linked)
	a := noFallbackApp(home)
	fake := a.Runner.(*runner.Fake)
	fake.Responses["/usr/bin/open "+linked.Link] = runner.Response{}
	if err := a.Checklist().Open(ctx, linked.ID); err != nil {
		t.Fatal(err)
	}
	if calls := fake.Calls(); calls[len(calls)-1].String() != "/usr/bin/open "+linked.Link {
		t.Errorf("last command = %s", calls[len(calls)-1].String())
	}
}

func TestOpenStepRefusesALinkMtmDidntMake(t *testing.T) {
	odd := grantStep
	odd.Link = "file:///Applications/Calculator.app"
	home := savedSteps(t, odd)
	a := noFallbackApp(home)
	if err := a.Checklist().Open(ctx, odd.ID); !errors.Is(err, ErrRefusedLink) {
		t.Errorf("err = %v", err)
	}
	if len(a.Runner.(*runner.Fake).Calls()) != 0 {
		t.Error("open ran for a link mtm didn't make")
	}
}

func TestOpenStepWithoutALinkNamesThePlace(t *testing.T) {
	plain := licenseStep
	plain.Where = "Dash > Settings > License"
	home := savedSteps(t, plain)
	if err := noFallbackApp(home).Checklist().Open(ctx, plain.ID); !errors.Is(err, ErrNoLink) || !strings.Contains(err.Error(), "Dash > Settings") {
		t.Errorf("err = %v", err)
	}
}

func TestRetirementStepsLiveApartFromTheChecklist(t *testing.T) {
	home := savedSteps(t, grantStep)
	st, err := stage.Existing(home)
	if err != nil {
		t.Fatal(err)
	}
	erase := checklist.Item{ID: "retire:erase", Module: "retire", Title: "Erase this Mac"}
	var r state.State
	r.Checklist.Add(erase)
	if err := state.SaveFile(st, state.RetireFileName, r); err != nil {
		t.Fatal(err)
	}
	a := stepApp(home, oldUUID)
	if err := a.Checklist().Done(erase.ID); !errors.Is(err, checklist.ErrUnknownItem) {
		t.Errorf("the new Mac's checklist found a retirement step: %v", err)
	}
	if err := a.Retirement().Done(erase.ID); err != nil {
		t.Fatal(err)
	}
	if l, err := a.Retirement().List(); err != nil || len(l.Pending()) != 0 {
		t.Errorf("retirement steps = %+v, %v; want the erase step done", l, err)
	}
	if l, err := a.Checklist().List(); err != nil || len(l.Pending()) != 1 {
		t.Errorf("checklist = %+v, %v; want the grant step still open", l, err)
	}
}
