// Package app runs mtm's commands: it applies the preflight gate, manages
// ~/.mtm, calls each module, and decides the verdict.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/preflight"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
	"github.com/momentmaker/mtm/internal/verdict"
)

var (
	ErrOtherMacsRecord = errors.New("~/.mtm holds the record from another Mac, and prepare would overwrite it")
	ErrNotVerified     = errors.New("mtm verify has not reported SAFE TO WIPE on this Mac; run mtm verify first, or pass --force to delete ~/.mtm anyway")
)

type App struct {
	Runner  runner.Runner
	Home    string
	Version string
	Now     func() time.Time
	Modules []module.Module
}

type ScanReport struct {
	Findings []module.Finding `json:"findings"`
}

type PrepareOptions struct {
	Only []string
	Deep bool
}

type PrepareReport struct {
	Ready    bool             `json:"ready"`
	Done     []string         `json:"done"`
	Problems []module.Finding `json:"problems"`
}

type VerifyOptions struct {
	Only []string
	Deep bool
}

type VerifyReport struct {
	Checks  []module.Check  `json:"checks"`
	Verdict verdict.Verdict `json:"verdict"`
}

// Scan reports what mtm, Migration Assistant, and the user will each
// handle. It changes nothing on disk.
func (a *App) Scan(ctx context.Context) ScanReport {
	findings := preflight.Check(ctx, a.Runner)
	env := a.env(ctx, module.OldMac, false, nil, a.Runner)
	for _, m := range a.Modules {
		found, err := m.Scan(env)
		if err != nil {
			findings = append(findings, moduleFailure(m.Name(), "scan", err))
			continue
		}
		findings = append(findings, found...)
	}
	return ScanReport{Findings: findings}
}

func (a *App) Prepare(ctx context.Context, opts PrepareOptions) (PrepareReport, error) {
	mods, err := a.selectModules(opts.Only)
	if err != nil {
		return PrepareReport{}, err
	}
	rep := PrepareReport{Problems: preflight.Check(ctx, a.Runner)}
	if module.HasBlocker(rep.Problems) {
		return rep, nil
	}
	st, err := stage.Open(a.Home)
	if err != nil {
		return rep, err
	}
	logFile, run, err := a.openLog(st, "prepare")
	if err != nil {
		return rep, err
	}
	defer logFile.Close()
	if err := st.ExcludeFromBackups(ctx, run); err != nil {
		rep.Problems = append(rep.Problems, module.Finding{
			Module: "stage", ID: "time-machine", Severity: module.SeverityBlocker, Handler: module.ByMtm,
			Title:  "Could not exclude ~/.mtm from Time Machine",
			Detail: err.Error(),
			Fix:    "Run: tmutil addexclusion ~/.mtm, then run mtm prepare again.",
		})
		return rep, nil
	}
	info, err := preflight.Gather(ctx, run)
	if err != nil {
		return rep, err
	}
	m, err := manifest.Load(st)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		m = manifest.New(a.Version)
	case err != nil:
		return rep, err
	case m.MachineID != "" && m.MachineID != info.MachineID:
		return rep, fmt.Errorf("%w (recorded %s). If this is the new Mac, run mtm verify instead", ErrOtherMacsRecord, m.PreparedAt.Local().Format(time.RFC1123))
	}
	m.MtmVersion = a.Version
	m.MacOSVersion, m.MacOSBuild, m.MachineID = info.MacOSVersion, info.MacOSBuild, info.MachineID
	m.Hostname, _ = os.Hostname()

	startedAt := a.Now()
	env := a.env(ctx, module.OldMac, opts.Deep, nil, run)
	for _, mod := range mods {
		res, err := mod.Prepare(env, st)
		if err != nil {
			rep.Problems = append(rep.Problems, moduleFailure(mod.Name(), "prepare", err))
			continue
		}
		if err := m.SetModule(mod.Name(), startedAt, res.Inventory); err != nil {
			return rep, err
		}
		for _, d := range res.Done {
			rep.Done = append(rep.Done, mod.Name()+": "+d)
		}
		rep.Problems = append(rep.Problems, res.Problems...)
	}
	if err := manifest.Save(st, m); err != nil {
		return rep, err
	}
	rep.Ready = !module.HasBlocker(rep.Problems)
	return rep, nil
}

func (a *App) Verify(ctx context.Context, opts VerifyOptions) (VerifyReport, error) {
	mods, err := a.selectModules(opts.Only)
	if err != nil {
		return VerifyReport{}, err
	}
	st, err := stage.Existing(a.Home)
	if err != nil {
		return VerifyReport{}, err
	}
	logFile, run, err := a.openLog(st, "verify")
	if err != nil {
		return VerifyReport{}, err
	}
	defer logFile.Close()
	m, err := manifest.Load(st)
	if errors.Is(err, fs.ErrNotExist) {
		return VerifyReport{}, fmt.Errorf("~/.mtm has no record from the old Mac; run mtm prepare there before Migration Assistant: %w", err)
	}
	if err != nil {
		return VerifyReport{}, err
	}

	var checks []module.Check
	for _, f := range preflight.Check(ctx, run) {
		checks = append(checks, module.Check{Module: f.Module, ID: f.ID, Status: module.StatusFail, Critical: true, Title: f.Title, Detail: f.Fix})
	}
	info, err := preflight.Gather(ctx, run)
	if err != nil {
		return VerifyReport{}, err
	}
	if info.MachineID == m.MachineID {
		checks = append(checks, module.Check{
			Module: preflight.Module, ID: "same-mac", Status: module.StatusFail, Critical: true,
			Title:  "This is the Mac that ran prepare",
			Detail: "run mtm verify on the new Mac after Migration Assistant finishes",
		})
	}
	if m.MtmVersion != a.Version {
		checks = append(checks, module.Check{
			Module: preflight.Module, ID: "version", Status: module.StatusWarn,
			Title:  "Different mtm version",
			Detail: fmt.Sprintf("prepare ran mtm %s; this is mtm %s", m.MtmVersion, a.Version),
		})
	}

	env := a.env(ctx, module.NewMac, opts.Deep, m, run)
	for _, mod := range mods {
		if _, ok := m.Modules[mod.Name()]; !ok {
			checks = append(checks, module.Check{
				Module: mod.Name(), ID: mod.Name() + ":not-prepared", Status: module.StatusFail, Critical: true,
				Title:  mod.Name() + " was not recorded by prepare",
				Detail: "run mtm prepare on the old Mac to record it, then run mtm verify again",
			})
			continue
		}
		cs, err := mod.Verify(env, st)
		if err != nil {
			checks = append(checks, module.Check{
				Module: mod.Name(), ID: mod.Name() + ":error", Status: module.StatusFail, Critical: true,
				Title: mod.Name() + ": verify failed", Detail: err.Error(),
			})
			continue
		}
		checks = append(checks, cs...)
	}

	v := verdict.Decide(checks, nil)
	s, err := state.Load(st)
	if err != nil {
		return VerifyReport{}, err
	}
	s.LastVerify = &state.VerifyResult{At: a.Now(), Safe: v.Safe && len(opts.Only) == 0}
	if err := state.Save(st, s); err != nil {
		return VerifyReport{}, err
	}
	return VerifyReport{Checks: checks, Verdict: v}, nil
}

// Cleanup deletes ~/.mtm. Without force, it requires a SAFE TO WIPE result
// from a full verify on this Mac.
func (a *App) Cleanup(force bool) (bool, error) {
	st, err := stage.Existing(a.Home)
	if errors.Is(err, stage.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !force {
		s, err := state.Load(st)
		if err != nil {
			return false, err
		}
		if s.LastVerify == nil || !s.LastVerify.Safe {
			return false, ErrNotVerified
		}
	}
	if err := st.Remove(); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) env(ctx context.Context, machine module.Machine, deep bool, m *manifest.Manifest, run runner.Runner) module.Env {
	return module.Env{Ctx: ctx, Run: run, Home: a.Home, Machine: machine, Deep: deep, Manifest: m}
}

func (a *App) selectModules(only []string) ([]module.Module, error) {
	if len(only) == 0 {
		return a.Modules, nil
	}
	byName := map[string]module.Module{}
	var names []string
	for _, m := range a.Modules {
		byName[m.Name()] = m
		names = append(names, m.Name())
	}
	var out []module.Module
	for _, name := range only {
		m, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown module %q (known: %s)", name, strings.Join(names, ", "))
		}
		out = append(out, m)
	}
	return out, nil
}

func (a *App) openLog(st *stage.Stage, command string) (*os.File, runner.Runner, error) {
	if err := st.MkdirAll("logs"); err != nil {
		return nil, nil, err
	}
	path := st.Path("logs", a.Now().UTC().Format("20060102T150405Z")+"-"+command+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log %s: %w", path, err)
	}
	return f, &runner.Logged{Inner: a.Runner, W: f}, nil
}

func moduleFailure(name, step string, err error) module.Finding {
	return module.Finding{
		Module: name, ID: name + ":error", Severity: module.SeverityBlocker, Handler: module.ByMtm,
		Title:  fmt.Sprintf("%s: %s failed", name, step),
		Detail: err.Error(),
		Fix:    "Fix the cause above and run the command again. Details are in ~/.mtm/logs.",
	}
}
