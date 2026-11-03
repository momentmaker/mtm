package app

import (
	"errors"
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
	noteStep    = checklist.Item{ID: "steps:note", Module: "steps", Title: "Sign in to Dash again"}
)

// stepsModule leaves the grant and license steps, and any extra ones, as a
// newer catalog might. Its verify check passes and proves the grant step
// done when grants is set.
type stepsModule struct {
	grants bool
	extra  []checklist.Item
}

func (stepsModule) Name() string                              { return "steps" }
func (stepsModule) Scan(module.Env) ([]module.Finding, error) { return nil, nil }
func (stepsModule) Prepare(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{Inventory: true}, nil
}
func (stepsModule) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}
func (m stepsModule) Steps(module.Env) ([]checklist.Item, error) {
	return append([]checklist.Item{grantStep, licenseStep}, m.extra...), nil
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

// brokenSteps can't list its steps, as when its part of the record is
// damaged.
type brokenSteps struct{ finishOnly }

func (brokenSteps) Steps(module.Env) ([]checklist.Item, error) {
	return nil, errors.New("record is damaged")
}

func TestVerifyListsStepsEvenWhenFinishNeverRan(t *testing.T) {
	old := t.TempDir()
	mustPrepare(t, stepApp(old, oldUUID, stepsModule{}))
	newHome := migrate(t, old)
	rep, err := stepApp(newHome, newUUID, stepsModule{}).Verify(ctx, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{grantStep.Title, licenseStep.Title}; rep.Verdict.Safe || !slices.Equal(rep.Verdict.Pending, want) {
		t.Errorf("verdict = %+v, want NOT SAFE with %q open", rep.Verdict, want)
	}
}

func TestVerifyOnlyListsTheStepsOfModulesItSkips(t *testing.T) {
	old := t.TempDir()
	plain := finishOnly{name: "plain"}
	mustPrepare(t, stepApp(old, oldUUID, stepsModule{}, plain))
	newHome := migrate(t, old)
	rep, err := stepApp(newHome, newUUID, stepsModule{}, plain).Verify(ctx, VerifyOptions{Only: []string{"plain"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{grantStep.Title, licenseStep.Title}; !slices.Equal(rep.Verdict.Pending, want) {
		t.Errorf("pending = %q, want %q", rep.Verdict.Pending, want)
	}
}

func TestVerifyFailsWhenAModuleCannotListItsSteps(t *testing.T) {
	old := t.TempDir()
	broken := brokenSteps{finishOnly{name: "broken"}}
	mustPrepare(t, stepApp(old, oldUUID, broken))
	rep, err := stepApp(migrate(t, old), newUUID, broken).Verify(ctx, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict.Safe || !slices.Contains(blockingIDs(rep), "broken:steps") {
		t.Errorf("safe = %v, blocking = %v; want broken:steps blocking", rep.Verdict.Safe, blockingIDs(rep))
	}
}

func TestFinishSavesTheStepsOfModulesThatCanListThem(t *testing.T) {
	old := t.TempDir()
	broken := brokenSteps{finishOnly{name: "broken"}}
	mustPrepare(t, stepApp(old, oldUUID, broken, stepsModule{}))
	newHome := migrate(t, old)
	rep, err := stepApp(newHome, newUUID, broken, stepsModule{}).Finish(ctx, FinishOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(rep.Problems, func(f module.Finding) bool {
		return f.ID == "broken:error" && f.Severity == module.SeverityBlocker && f.Title == "broken: list checklist steps failed"
	}) {
		t.Errorf("problems = %+v, want a blocker for broken's steps", rep.Problems)
	}
	if n := len(loadState(t, newHome).Checklist.Pending()); n != 2 {
		t.Errorf("%d steps saved, want stepsModule's 2", n)
	}
}

func TestFinishThatAddsAStepNeedsAnotherVerifyBeforeCleanup(t *testing.T) {
	old := t.TempDir()
	mustPrepare(t, stepApp(old, oldUUID, stepsModule{}))
	newHome := migrate(t, old)
	a := stepApp(newHome, newUUID, stepsModule{grants: true})
	if _, err := a.Finish(ctx, FinishOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := a.Skip(licenseStep.ID, "sold the license"); err != nil {
		t.Fatal(err)
	}
	if rep, err := a.Verify(ctx, VerifyOptions{}); err != nil || !rep.Verdict.Safe {
		t.Fatalf("verify = %+v, %v; want SAFE", rep.Verdict, err)
	}
	newer := stepApp(newHome, newUUID, stepsModule{grants: true, extra: []checklist.Item{noteStep}})
	if _, err := newer.Finish(ctx, FinishOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := newer.Cleanup(false); !errors.Is(err, ErrNotVerified) {
		t.Errorf("cleanup after finish added a step: %v, want ErrNotVerified", err)
	}
}
