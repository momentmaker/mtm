package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
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
	if err := a.MarkDone(grantStep.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.Skip(licenseStep.ID, "sold it"); err != nil {
		t.Fatal(err)
	}
	l, err := a.Checklist()
	if err != nil || len(l.Pending()) != 0 {
		t.Errorf("checklist = %+v, %v; want nothing open", l, err)
	}
	if err := a.MarkDone("steps:nope"); !errors.Is(err, checklist.ErrUnknownItem) {
		t.Errorf("err = %v, want ErrUnknownItem", err)
	}
}

func TestOpenStepOpensItsSettingsPane(t *testing.T) {
	linked := grantStep
	linked.Link = "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?Privacy_Accessibility"
	home := savedSteps(t, linked)
	a := stepApp(home, newUUID)
	fake := a.Runner.(*runner.Fake)
	fake.Responses["/usr/bin/open "+linked.Link] = runner.Response{}
	if err := a.OpenStep(ctx, linked.ID); err != nil {
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
	a := stepApp(home, newUUID)
	if err := a.OpenStep(ctx, odd.ID); err == nil || !strings.Contains(err.Error(), "only System Settings links") {
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
	if err := stepApp(home, newUUID).OpenStep(ctx, plain.ID); !errors.Is(err, ErrNoLink) || !strings.Contains(err.Error(), "Dash > Settings") {
		t.Errorf("err = %v", err)
	}
}
