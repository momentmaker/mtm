// Package app runs mtm's commands: it applies the preflight gate, manages
// ~/.mtm, calls each module, and decides the verdict.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/momentmaker/mtm/internal/macos"
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
	ErrFinishOnOldMac  = errors.New("this is the Mac that ran prepare; run mtm finish on the new Mac after Migration Assistant")
	ErrUndoOnNewMac    = errors.New("~/.mtm holds the record from another Mac; prepare --undo only works on the Mac that ran prepare")
	ErrNotVerified     = errors.New("mtm verify has not reported SAFE TO WIPE on this Mac; run mtm verify first, or pass --force to delete ~/.mtm anyway")
)

// App runs mtm's commands. Free reports a volume's free space; nil means
// stage.Free.
type App struct {
	Runner  runner.Runner
	Home    string
	Version string
	Now     func() time.Time
	Modules []module.Module
	Free    func(path string) (uint64, error)
}

type ScanReport struct {
	Findings []module.Finding `json:"findings"`
}

// PrepareOptions are prepare's flags. StageDir is the --stage folder on an
// external volume; empty keeps exports in ~/.mtm/exports.
type PrepareOptions struct {
	Only     []string
	Deep     bool
	StageDir string
}

type PrepareReport struct {
	Ready    bool             `json:"ready"`
	Done     []string         `json:"done"`
	Problems []module.Finding `json:"problems"`
	// ExportsDir is the --stage folder holding this run's exports, or empty
	// when they are under ~/.mtm.
	ExportsDir string `json:"exports_dir,omitempty"`
}

type VerifyOptions struct {
	Only []string
	Deep bool
}

type VerifyReport struct {
	Checks  []module.Check  `json:"checks"`
	Verdict verdict.Verdict `json:"verdict"`
}

type FinishOptions struct {
	Only        []string
	Force       bool
	RestoreDump bool
}

// CleanupReport says what cleanup deleted. External is the prepare --stage
// folder, if prepare used one; ExternalRemoved is false when its drive
// isn't attached.
type CleanupReport struct {
	Removed         bool
	External        string
	ExternalRemoved bool
}

// StepReport is what finish or prepare --undo did, and what is left to do
// by hand.
type StepReport struct {
	Done     []string         `json:"done"`
	Problems []module.Finding `json:"problems"`
}

func (r *StepReport) add(name string, res module.Result) {
	for _, d := range res.Done {
		r.Done = append(r.Done, name+": "+d)
	}
	r.Problems = append(r.Problems, res.Problems...)
}

// Scan reports what mtm, Migration Assistant, and the user will each
// handle. It changes nothing on disk.
func (a *App) Scan(ctx context.Context) ScanReport {
	findings := preflight.Check(ctx, a.Runner)
	env := a.env(ctx, module.OldMac, false, nil, a.Runner, nil)
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
	case m.MachineID != info.MachineID:
		return rep, fmt.Errorf("%w (recorded %s). If this is the new Mac, run mtm verify instead", ErrOtherMacsRecord, m.PreparedAt.Local().Format(time.RFC1123))
	}
	exports, problem := a.prepareExports(ctx, run, st, m, opts)
	if problem != nil {
		rep.Problems = append(rep.Problems, *problem)
		return rep, nil
	}
	m.MtmVersion = a.Version
	m.MacOSVersion, m.MacOSBuild, m.MachineID = info.MacOSVersion, info.MacOSBuild, info.MachineID
	m.Hostname, _ = os.Hostname()
	oldExports := m.ExportsDir
	m.ExportsDir = ""
	if opts.StageDir != "" {
		m.ExportsDir = exports.Root
	}
	if len(opts.Only) == 0 && oldExports != "" && oldExports != m.ExportsDir {
		rep.Problems = append(rep.Problems, abandonedExports(oldExports))
	}

	startedAt := a.Now()
	env := a.env(ctx, module.OldMac, opts.Deep, m, run, exports)
	for _, mod := range mods {
		res, err := mod.Prepare(env, st)
		if err != nil {
			// An earlier record describes an older state of this Mac and must
			// not vouch for the current one, so verify reports it not prepared.
			delete(m.Modules, mod.Name())
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
	rep.ExportsDir = m.ExportsDir
	return rep, nil
}

// abandonedExports warns that a full prepare stopped using an earlier
// --stage folder. The record can name only one folder, so cleanup will not
// delete the old one.
func abandonedExports(old string) module.Finding {
	return module.Finding{
		Module: "stage", ID: "old-exports", Severity: module.SeverityWarn, Handler: module.Manual,
		Title: "This prepare doesn't use the earlier exports in " + old,
		Fix:   "Delete " + macos.ShellQuote(old) + " yourself when that drive is attached; mtm cleanup won't.",
	}
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
	m, err := loadRecord(st)
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

	env := a.env(ctx, module.NewMac, opts.Deep, m, run, exportsFor(st, m))
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
	if len(opts.Only) == 0 {
		checks = append(checks, a.unknownModules(m)...)
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

// Finish restores, on the new Mac, what prepare set aside on the old one.
// It refuses on the old Mac, where it would restart what prepare stopped.
func (a *App) Finish(ctx context.Context, opts FinishOptions) (StepReport, error) {
	mods, err := a.selectModules(opts.Only)
	if err != nil {
		return StepReport{}, err
	}
	if problems := preflight.Check(ctx, a.Runner); module.HasBlocker(problems) {
		return StepReport{Problems: problems}, nil
	}
	st, err := stage.Existing(a.Home)
	if err != nil {
		return StepReport{}, err
	}
	logFile, run, err := a.openLog(st, "finish")
	if err != nil {
		return StepReport{}, err
	}
	defer logFile.Close()
	m, err := loadRecord(st)
	if err != nil {
		return StepReport{}, err
	}
	info, err := preflight.Gather(ctx, run)
	if err != nil {
		return StepReport{}, err
	}
	if info.MachineID == m.MachineID {
		return StepReport{}, ErrFinishOnOldMac
	}
	env := a.env(ctx, module.NewMac, false, m, run, exportsFor(st, m))
	env.Force, env.RestoreDump = opts.Force, opts.RestoreDump
	var rep StepReport
	for _, mod := range mods {
		if _, ok := m.Modules[mod.Name()]; !ok {
			continue
		}
		res, err := mod.Finish(env, st)
		if err != nil {
			rep.Problems = append(rep.Problems, moduleFailure(mod.Name(), "finish", err))
			continue
		}
		rep.add(mod.Name(), res)
	}
	return rep, nil
}

// Undo puts the old Mac back in use when the migration is called off.
// Undone modules lose their record: it describes a state that no longer
// holds, so verify must not trust it.
func (a *App) Undo(ctx context.Context) (StepReport, error) {
	st, err := stage.Existing(a.Home)
	if err != nil {
		return StepReport{}, err
	}
	logFile, run, err := a.openLog(st, "undo")
	if err != nil {
		return StepReport{}, err
	}
	defer logFile.Close()
	m, err := loadRecord(st)
	if err != nil {
		return StepReport{}, err
	}
	info, err := preflight.Gather(ctx, run)
	if err != nil {
		return StepReport{}, err
	}
	if info.MachineID != m.MachineID {
		return StepReport{}, ErrUndoOnNewMac
	}
	env := a.env(ctx, module.OldMac, false, m, run, exportsFor(st, m))
	var rep StepReport
	for _, mod := range a.Modules {
		u, ok := mod.(module.Undoer)
		if !ok {
			continue
		}
		if _, recorded := m.Modules[mod.Name()]; !recorded {
			continue
		}
		res, err := u.Undo(env, st)
		if err != nil {
			rep.Problems = append(rep.Problems, moduleFailure(mod.Name(), "undo", err))
			continue
		}
		delete(m.Modules, mod.Name())
		rep.add(mod.Name(), res)
	}
	if err := manifest.Save(st, m); err != nil {
		return rep, err
	}
	return rep, nil
}

// loadRecord reads the manifest prepare wrote, explaining a missing one.
func loadRecord(st *stage.Stage) (*manifest.Manifest, error) {
	m, err := manifest.Load(st)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("~/.mtm has no record from the old Mac; run mtm prepare there before Migration Assistant: %w", err)
	}
	return m, err
}

// Cleanup deletes ~/.mtm, and the prepare --stage folder when its drive is
// attached. Without force, it requires a SAFE TO WIPE result from a full
// verify on this Mac.
func (a *App) Cleanup(force bool) (CleanupReport, error) {
	st, err := stage.Existing(a.Home)
	if errors.Is(err, stage.ErrNotFound) {
		return CleanupReport{}, nil
	}
	if err != nil {
		return CleanupReport{}, err
	}
	if !force {
		s, err := state.Load(st)
		if err != nil {
			return CleanupReport{}, err
		}
		if s.LastVerify == nil || !s.LastVerify.Safe {
			return CleanupReport{}, ErrNotVerified
		}
	}
	var rep CleanupReport
	// A damaged record can't name a --stage folder; with --force, ~/.mtm
	// still goes.
	if m, err := manifest.Load(st); err == nil && m.ExportsDir != "" {
		rep.External = m.ExportsDir
		if rep.ExternalRemoved, err = removeExternal(m.ExportsDir); err != nil {
			return rep, err
		}
	}
	if err := st.Remove(); err != nil {
		return rep, err
	}
	rep.Removed = true
	return rep, nil
}

// removeExternal deletes the prepare --stage folder if its drive is
// attached. It deletes only a private folder named mtm-exports, so a
// damaged record can't point cleanup at other data.
func removeExternal(root string) (bool, error) {
	if filepath.Base(root) != stage.ExternalDirName {
		return false, fmt.Errorf("the record names %s as the exports folder, which mtm didn't make; delete it yourself if it holds only exports", root)
	}
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	exports, err := stage.ExistingDir(root)
	if err != nil {
		return false, err
	}
	if err := exports.Remove(); err != nil {
		return false, err
	}
	return true, nil
}

// prepareExports opens the folder this run's exports go in. A problem ends
// prepare before any module runs: an unsafe --stage volume, or a partial
// run that would scatter one record's exports across two folders.
func (a *App) prepareExports(ctx context.Context, run runner.Runner, st *stage.Stage, m *manifest.Manifest, opts PrepareOptions) (*stage.Stage, *module.Finding) {
	want := ""
	if opts.StageDir != "" {
		want = filepath.Join(opts.StageDir, stage.ExternalDirName)
	}
	if len(opts.Only) > 0 && len(m.Modules) > 0 && m.ExportsDir != want {
		return nil, &module.Finding{
			Module: "stage", ID: "exports", Severity: module.SeverityBlocker, Handler: module.Manual,
			Title:  "prepare --only must keep exports where the earlier prepare put them",
			Detail: fmt.Sprintf("the earlier prepare used %s; this one would use %s", exportsName(m.ExportsDir), exportsName(want)),
			Fix:    "Pass the same --stage as before, or run mtm prepare without --only.",
		}
	}
	exports, err := a.openExports(ctx, run, st, opts.StageDir)
	if err != nil {
		return nil, &module.Finding{
			Module: "stage", ID: "exports", Severity: module.SeverityBlocker, Handler: module.Manual,
			Title:  "Could not use the folder for exports",
			Detail: err.Error(),
			Fix:    "Fix the cause above and run mtm prepare again.",
		}
	}
	return exports, nil
}

func (a *App) openExports(ctx context.Context, run runner.Runner, st *stage.Stage, stageDir string) (*stage.Stage, error) {
	if stageDir == "" {
		return stage.OpenDir(st.Path(stage.ExportsDirName))
	}
	if !filepath.IsAbs(stageDir) {
		return nil, fmt.Errorf("--stage needs a full path, such as /Volumes/Backup, not %s", stageDir)
	}
	vol, err := stage.VolumeOf(stageDir)
	if err != nil {
		return nil, err
	}
	if err := stage.CheckExternal(ctx, run, vol); err != nil {
		return nil, err
	}
	exports, err := stage.OpenDir(filepath.Join(stageDir, stage.ExternalDirName))
	if err != nil {
		return nil, err
	}
	if err := exports.ExcludeFromBackups(ctx, run); err != nil {
		return nil, err
	}
	return exports, nil
}

func exportsName(dir string) string {
	if dir == "" {
		return "~/.mtm/exports"
	}
	return dir
}

// exportsFor is the folder prepare wrote exports to. Nothing is checked
// here: a module checks the folder when it reads an export, so a detached
// --stage drive fails only the restores that need it.
func exportsFor(st *stage.Stage, m *manifest.Manifest) *stage.Stage {
	if m.ExportsDir != "" {
		return &stage.Stage{Root: m.ExportsDir}
	}
	return &stage.Stage{Root: st.Path(stage.ExportsDirName)}
}

// unknownModules fails each module in the record that this mtm can't
// verify. A newer mtm recorded data this one would otherwise skip silently.
func (a *App) unknownModules(m *manifest.Manifest) []module.Check {
	var checks []module.Check
	for _, name := range slices.Sorted(maps.Keys(m.Modules)) {
		if slices.ContainsFunc(a.Modules, func(mod module.Module) bool { return mod.Name() == name }) {
			continue
		}
		checks = append(checks, module.Check{
			Module: name, ID: name + ":unknown", Status: module.StatusFail, Critical: true,
			Title:  name + " was recorded by a newer mtm",
			Detail: "upgrade mtm on this Mac and run mtm verify again",
		})
	}
	return checks
}

func (a *App) env(ctx context.Context, machine module.Machine, deep bool, m *manifest.Manifest, run runner.Runner, exports *stage.Stage) module.Env {
	free := a.Free
	if free == nil {
		free = stage.Free
	}
	return module.Env{Ctx: ctx, Run: run, Home: a.Home, Machine: machine, Deep: deep, Manifest: m, Exports: exports, Free: free}
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
