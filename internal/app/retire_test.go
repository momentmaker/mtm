package app

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

func retireOn(t *testing.T, a *App) RetireReport {
	t.Helper()
	rep, err := a.Retire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func stepIDs(l checklist.List) []string {
	var ids []string
	for _, e := range l.Entries {
		ids = append(ids, e.ID)
	}
	return ids
}

func TestRetireListsAFileChangedAfterPrepare(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	testutil.WriteFile(t, filepath.Join(old, "Documents/notes.md"), "notes, edited after prepare\n")
	rep := retireOn(t, newTestApp(old, fakeMac(old, oldUUID)))
	if len(rep.Changes) != 1 || rep.Changes[0].ID != "files:Documents" || !slices.Equal(rep.Changes[0].Paths, []string{"Documents/notes.md"}) {
		t.Errorf("changes = %+v, want Documents/notes.md", rep.Changes)
	}
	if want := []string{"retire:icloud", "retire:devices", "retire:erase"}; !slices.Equal(stepIDs(rep.Steps), want) {
		t.Errorf("steps = %q, want %q", stepIDs(rep.Steps), want)
	}
}

func TestRetireRefusesOnTheNewMac(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	if _, err := newTestApp(newHome, fakeMac(newHome, newUUID)).Retire(ctx); !errors.Is(err, ErrRetireOnNewMac) {
		t.Errorf("err = %v, want ErrRetireOnNewMac", err)
	}
}

func TestRetireKeepsWhatTheUserDidAndStaysOutOfTheChecklist(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	a := newTestApp(old, fakeMac(old, oldUUID))
	retireOn(t, a)
	if err := a.Retirement().Skip("retire:devices", "it was never in my account"); err != nil {
		t.Fatal(err)
	}
	rep := retireOn(t, a)
	if e, _ := rep.Steps.Find("retire:devices"); e.Status != checklist.Skipped {
		t.Errorf("devices step = %+v, want it still skipped", e)
	}
	if l, err := a.Checklist().List(); err != nil || len(l.Entries) != 0 {
		t.Errorf("checklist = %+v, %v; want no retirement steps in it", l, err)
	}
}

// licenseRetirer leaves a step for mtm retire, as system does for a license.
type licenseRetirer struct{ finishOnly }

func (licenseRetirer) Retire(module.Env, *stage.Stage) (module.Retirement, error) {
	return module.Retirement{Steps: []checklist.Item{{ID: "lic:deactivate", Module: "lic", Title: "Deactivate the license"}}}, nil
}

// brokenRetire can't look, as when its part of the record is damaged.
type brokenRetire struct{ finishOnly }

func (brokenRetire) Retire(module.Env, *stage.Stage) (module.Retirement, error) {
	return module.Retirement{}, errors.New("record is damaged")
}

func TestRetirePutsModuleStepsFirstAndReportsAModuleThatFails(t *testing.T) {
	old := t.TempDir()
	mods := []module.Module{brokenRetire{finishOnly{name: "broken"}}, licenseRetirer{finishOnly{name: "lic"}}}
	mustPrepare(t, stepApp(old, oldUUID, mods...))
	rep := retireOn(t, stepApp(old, oldUUID, mods...))
	if len(rep.Problems) != 1 || rep.Problems[0].ID != "broken:error" || rep.Problems[0].Title != "broken: retire failed" {
		t.Errorf("problems = %+v, want broken's failure", rep.Problems)
	}
	if want := []string{"lic:deactivate", "retire:icloud", "retire:devices", "retire:erase"}; !slices.Equal(stepIDs(rep.Steps), want) {
		t.Errorf("steps = %q, want %q", stepIDs(rep.Steps), want)
	}
}

func TestRetireReportsARetirerThatPrepareDidNotRecord(t *testing.T) {
	old := t.TempDir()
	mustPrepare(t, stepApp(old, oldUUID, finishOnly{name: "other"}))
	rep := retireOn(t, stepApp(old, oldUUID, finishOnly{name: "other"}, licenseRetirer{finishOnly{name: "lic"}}))
	if len(rep.Problems) != 1 || rep.Problems[0].ID != "lic:not-prepared" || rep.Problems[0].Severity != module.SeverityWarn {
		t.Errorf("problems = %+v, want lic not prepared", rep.Problems)
	}
	if slices.Contains(stepIDs(rep.Steps), "lic:deactivate") {
		t.Errorf("steps = %q, want none from a module prepare didn't record", stepIDs(rep.Steps))
	}
}
