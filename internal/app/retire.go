package app

import (
	"context"
	"errors"
	"time"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/preflight"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
)

var ErrRetireOnNewMac = errors.New("~/.mtm holds the record from another Mac; run mtm retire on the old Mac, the one that ran prepare")

// RetireReport is what retire found on the old Mac: what changed since
// prepare, modules that failed to look, and the retirement steps with what
// the user did with each so far.
type RetireReport struct {
	PreparedAt time.Time        `json:"prepared_at"`
	Changes    []module.Check   `json:"changes"`
	Problems   []module.Finding `json:"problems,omitempty"`
	Steps      checklist.List   `json:"steps"`
}

// retireSteps follow the modules' steps, which need the Mac still signed
// in. Signing out of iCloud turns off Find My, and the erase comes last.
var retireSteps = []checklist.Item{
	{ID: "retire:icloud", Module: "retire", Where: "System Settings > your name > Sign Out",
		Title:  "Sign out of iCloud on this Mac",
		Detail: "Signing out turns off Find My, which removes Activation Lock, so the next owner can set the Mac up."},
	{ID: "retire:devices", Module: "retire", Where: "On the new Mac: System Settings > your name, then this Mac in the device list",
		Title: "Remove this Mac from your Apple Account's devices"},
	{ID: "retire:erase", Module: "retire", Where: "System Settings > General > Transfer or Reset > Erase All Content and Settings",
		Title:  "Erase this Mac with Erase All Content and Settings",
		Detail: "It destroys the disk's encryption key, so nothing on it can be recovered. mtm never erases anything itself."},
}

// notPrepared keeps retire from saying nothing changed about a module it
// couldn't look at.
func notPrepared(name string) module.Finding {
	return module.Finding{
		Module: name, ID: name + ":not-prepared", Severity: module.SeverityWarn, Handler: module.Manual,
		Title: name + " was not recorded by prepare, so retire can't check it",
		Fix:   "Check it yourself before you erase this Mac.",
	}
}

// Retire lists what changed on the old Mac since prepare and adds the
// retirement steps to retire.json, keeping what the user did with steps
// already there. It runs only on the Mac that ran prepare.
func (a *App) Retire(ctx context.Context) (RetireReport, error) {
	st, err := stage.Existing(a.Home)
	if err != nil {
		return RetireReport{}, err
	}
	logFile, run, err := a.openLog(st, "retire")
	if err != nil {
		return RetireReport{}, err
	}
	defer logFile.Close()
	m, err := loadRecord(st)
	if err != nil {
		return RetireReport{}, err
	}
	info, err := preflight.Gather(ctx, run)
	if err != nil {
		return RetireReport{}, err
	}
	if info.MachineID != m.MachineID {
		return RetireReport{}, ErrRetireOnNewMac
	}
	env := a.env(ctx, module.OldMac, false, m, run, exportsFor(st, m))
	rep := RetireReport{PreparedAt: m.PreparedAt}
	var items []checklist.Item
	for _, mod := range a.Modules {
		r, ok := mod.(module.Retirer)
		if !ok {
			continue
		}
		if _, recorded := m.Modules[mod.Name()]; !recorded {
			rep.Problems = append(rep.Problems, notPrepared(mod.Name()))
			continue
		}
		got, err := r.Retire(env, st)
		if err != nil {
			rep.Problems = append(rep.Problems, moduleFailure(mod.Name(), "retire", err))
			continue
		}
		rep.Changes = append(rep.Changes, got.Changes...)
		items = append(items, got.Steps...)
	}
	s, err := state.LoadFile(st, state.RetireFileName)
	if err != nil {
		return rep, err
	}
	s.Checklist.Add(append(items, retireSteps...)...)
	if err := state.SaveFile(st, state.RetireFileName, s); err != nil {
		return rep, err
	}
	rep.Steps = s.Checklist
	return rep, nil
}
