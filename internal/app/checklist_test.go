package app

import (
	"slices"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
)

var (
	grantStep   = checklist.Item{ID: "steps:grant", Module: "steps", Title: "Give Raycast Accessibility access"}
	licenseStep = checklist.Item{ID: "steps:license", Module: "steps", Title: "Reactivate Dash"}
)

// stepsModule leaves two checklist steps in finish. Its verify check
// passes and proves the grant step done when grants is set.
type stepsModule struct{ grants bool }

func (stepsModule) Name() string                              { return "steps" }
func (stepsModule) Scan(module.Env) ([]module.Finding, error) { return nil, nil }
func (stepsModule) Prepare(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{Inventory: true}, nil
}
func (stepsModule) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{Checklist: []checklist.Item{grantStep, licenseStep}}, nil
}
func (m stepsModule) Verify(module.Env, *stage.Stage) ([]module.Check, error) {
	c := module.Check{Module: "steps", ID: "steps:grants", Status: module.StatusWarn, Title: "grants"}
	if m.grants {
		c.Status, c.Resolves = module.StatusPass, []string{grantStep.ID}
	}
	return []module.Check{c}, nil
}

func finishedNewMac(t *testing.T, mod module.Module) string {
	t.Helper()
	old := t.TempDir()
	mustPrepare(t, stepApp(old, oldUUID, mod))
	newHome := migrate(t, old)
	rep, err := stepApp(newHome, newUUID, mod).Finish(ctx, FinishOptions{})
	if err != nil || rep.Pending != 2 {
		t.Fatalf("finish = %+v, %v; want 2 pending steps", rep, err)
	}
	return newHome
}

func loadState(t *testing.T, home string) state.State {
	t.Helper()
	st, err := stage.Existing(home)
	if err != nil {
		t.Fatal(err)
	}
	s, err := state.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFinishKeepsWhatTheUserDidWhenItRunsAgain(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	st, _ := stage.Existing(newHome)
	s := loadState(t, newHome)
	if err := s.Checklist.Skip(licenseStep.ID, "sold the license", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(st, s); err != nil {
		t.Fatal(err)
	}
	rep, err := stepApp(newHome, newUUID, stepsModule{}).Finish(ctx, FinishOptions{})
	if err != nil || rep.Pending != 1 {
		t.Errorf("second finish = %+v, %v; want 1 pending step", rep, err)
	}
}

func TestVerifyIsNotSafeWhileStepsAreOpen(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	rep, err := stepApp(newHome, newUUID, stepsModule{}).Verify(ctx, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict.Safe || !slices.Contains(rep.Verdict.Pending, licenseStep.Title) {
		t.Errorf("verdict = %+v, want NOT SAFE with the open steps", rep.Verdict)
	}
}

func TestVerifyMarksDoneTheStepsAPassingCheckProves(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	rep, err := stepApp(newHome, newUUID, stepsModule{grants: true}).Verify(ctx, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Verdict.Pending, []string{licenseStep.Title}) {
		t.Errorf("pending = %q, want only the license step", rep.Verdict.Pending)
	}
	if e, _ := loadState(t, newHome).Checklist.Find(grantStep.ID); e.Status != checklist.Done {
		t.Errorf("grant step = %+v, want done", e)
	}
}

func TestVerifyIsSafeOnceEveryStepIsClosed(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	st, _ := stage.Existing(newHome)
	s := loadState(t, newHome)
	s.Checklist.Skip(licenseStep.ID, "sold the license", time.Now())
	state.Save(st, s)
	rep, err := stepApp(newHome, newUUID, stepsModule{grants: true}).Verify(ctx, VerifyOptions{})
	if err != nil || !rep.Verdict.Safe {
		t.Errorf("verify = %+v, %v; want SAFE", rep.Verdict, err)
	}
}
