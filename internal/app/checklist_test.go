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

// skip closes a step in home's checklist as mtm checklist skip does, at
// the time given.
func skip(t *testing.T, home, id, reason string, at time.Time) {
	t.Helper()
	a := stepApp(home, newUUID)
	a.Now = func() time.Time { return at }
	if err := a.Checklist().Skip(id, reason); err != nil {
		t.Fatal(err)
	}
}

func TestFinishKeepsWhatTheUserDidWhenItRunsAgain(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	skip(t, newHome, licenseStep.ID, "sold the license", time.Now())
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
	skip(t, newHome, licenseStep.ID, "sold the license", time.Now())
	rep, err := stepApp(newHome, newUUID, stepsModule{grants: true}).Verify(ctx, VerifyOptions{})
	if err != nil || !rep.Verdict.Safe {
		t.Errorf("verify = %+v, %v; want SAFE", rep.Verdict, err)
	}
}

func TestVerifyKeepsWhatTheUserRecordedForAStepItProves(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	skippedAt := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	skip(t, newHome, grantStep.ID, "granted by my company's device management", skippedAt)
	if _, err := stepApp(newHome, newUUID, stepsModule{grants: true}).Verify(ctx, VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	e, _ := loadState(t, newHome).Checklist.Find(grantStep.ID)
	if e.Status != checklist.Skipped || e.Reason != "granted by my company's device management" || !e.At.Equal(skippedAt) {
		t.Errorf("step = %+v, want it skipped as the user recorded", e)
	}
}

func TestResolveLeavesStepsOpenWhenTheCheckDoesNotPass(t *testing.T) {
	var l checklist.List
	l.Add(grantStep)
	resolve(&l, []module.Check{
		{ID: "steps:warn", Status: module.StatusWarn, Resolves: []string{grantStep.ID}},
		{ID: "steps:fail", Status: module.StatusFail, Resolves: []string{grantStep.ID}},
		{ID: "steps:info", Status: module.StatusInfo, Resolves: []string{grantStep.ID}},
	}, time.Now())
	if e, _ := l.Find(grantStep.ID); e.Status != checklist.Pending {
		t.Errorf("step = %+v, want it still pending", e)
	}
}

func TestResolveIgnoresAStepTheChecklistDoesNotHave(t *testing.T) {
	var l checklist.List
	l.Add(grantStep)
	resolve(&l, []module.Check{{ID: "steps:grants", Status: module.StatusPass, Resolves: []string{"steps:gone", grantStep.ID}}}, time.Now())
	if len(l.Entries) != 1 || l.Entries[0].Status != checklist.Done {
		t.Errorf("checklist = %+v, want only the grant step, done", l.Entries)
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
	if err := a.Checklist().Skip(licenseStep.ID, "sold the license"); err != nil {
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

func TestVerifyOnTheOldMacLeavesNoStepsToCarryOver(t *testing.T) {
	old := t.TempDir()
	mustPrepare(t, stepApp(old, oldUUID, stepsModule{}))
	if _, err := stepApp(old, oldUUID, stepsModule{}).Verify(ctx, VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	if entries := loadState(t, old).Checklist.Entries; len(entries) != 0 {
		t.Fatalf("old Mac's checklist = %+v, want it empty", entries)
	}
	rep, err := stepApp(migrate(t, old), newUUID, stepsModule{}).Verify(ctx, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{grantStep.Title, licenseStep.Title}; !slices.Equal(rep.Verdict.Pending, want) {
		t.Errorf("new Mac's pending = %q, want %q", rep.Verdict.Pending, want)
	}
}

// answersMidFinish is stepsModule with a finish slow enough for the user to
// close a step with mtm checklist in another terminal meanwhile.
type answersMidFinish struct {
	stepsModule
	answer func()
}

func (m answersMidFinish) Finish(module.Env, *stage.Stage) (module.Result, error) {
	m.answer()
	return module.Result{}, nil
}

func TestFinishKeepsAnAnswerGivenWhileItRuns(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	mod := answersMidFinish{answer: func() { skip(t, newHome, licenseStep.ID, "sold the license", time.Now()) }}
	if _, err := stepApp(newHome, newUUID, mod).Finish(ctx, FinishOptions{}); err != nil {
		t.Fatal(err)
	}
	if e, _ := loadState(t, newHome).Checklist.Find(licenseStep.ID); e.Status != checklist.Skipped {
		t.Errorf("license step = %+v, want the skip given during finish", e)
	}
}

func TestResolveClosesEveryKindOfOpenStep(t *testing.T) {
	l := checklist.List{Entries: []checklist.Entry{
		{Item: grantStep, Status: ""},
		{Item: licenseStep, Status: checklist.Skipped},
	}}
	resolve(&l, []module.Check{{ID: "steps:all", Status: module.StatusPass, Resolves: []string{grantStep.ID, licenseStep.ID}}}, time.Now())
	if open := l.Pending(); len(open) != 0 {
		t.Errorf("open steps = %+v, want none", open)
	}
}
