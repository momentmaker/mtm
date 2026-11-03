package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
)

// exportModule writes an export in prepare and checks it in finish, as the
// docker and databases modules do. It keeps the Env finish gave it.
type exportModule struct{ seen *module.Env }

func (exportModule) Name() string                              { return "exp" }
func (exportModule) Scan(module.Env) ([]module.Finding, error) { return nil, nil }

func (exportModule) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	f, err := env.Exports.CreateArtifact("exp/data")
	if err != nil {
		return module.Result{}, err
	}
	if _, err := f.Write([]byte("data")); err != nil {
		f.Abort()
		return module.Result{}, err
	}
	a, err := f.Commit()
	if err != nil {
		return module.Result{}, err
	}
	return module.Result{Inventory: a}, nil
}

func (m exportModule) Finish(env module.Env, _ *stage.Stage) (module.Result, error) {
	*m.seen = env
	var a stage.Artifact
	if _, err := module.LoadInventory(env, "exp", &a); err != nil {
		return module.Result{}, err
	}
	if err := env.Exports.CheckArtifact(a); err != nil {
		return module.Result{}, err
	}
	return module.Result{Done: []string{"checked the export"}}, nil
}

func (exportModule) Verify(module.Env, *stage.Stage) ([]module.Check, error) { return nil, nil }

// stagingMac is fakeMac with diskutil answering for the volume holding dir
// and Time Machine accepting the exclusion of its exports folder.
func stagingMac(t *testing.T, home, dir, encrypted string) *runner.Fake {
	t.Helper()
	vol, err := stage.VolumeOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := fakeMac(home, oldUUID)
	f.Responses["/usr/sbin/diskutil info -plist "+vol.MountPoint] = runner.Response{Stdout: "<plist/>"}
	f.Responses["/usr/bin/plutil -extract Encryption raw -o - -"] = runner.Response{Stdout: encrypted + "\n"}
	f.Responses["/usr/bin/tmutil addexclusion "+filepath.Join(dir, stage.ExternalDirName)] = runner.Response{}
	return f
}

func loadManifest(t *testing.T, home string) *manifest.Manifest {
	t.Helper()
	st, err := stage.Existing(home)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPrepareStageRefusesAnUnencryptedVolume(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	var seen module.Env
	a := &App{Runner: stagingMac(t, home, dir, "false"), Home: home, Version: "test", Now: time.Now, Modules: []module.Module{exportModule{&seen}}}
	rep, err := a.Prepare(ctx, PrepareOptions{StageDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ready || !slices.ContainsFunc(rep.Problems, func(f module.Finding) bool {
		return f.ID == "exports" && strings.Contains(f.Detail, "not encrypted")
	}) {
		t.Errorf("report = %+v, want NOT READY naming the unencrypted volume", rep)
	}
	if _, err := os.Stat(filepath.Join(dir, stage.ExternalDirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("prepare made an exports folder on an unencrypted volume")
	}
}

func TestPrepareStageWritesExportsToThePrivateFolderOnTheVolume(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	var seen module.Env
	a := &App{Runner: stagingMac(t, home, dir, "true"), Home: home, Version: "test", Now: time.Now, Modules: []module.Module{exportModule{&seen}}}
	rep, err := a.Prepare(ctx, PrepareOptions{StageDir: dir})
	if err != nil || !rep.Ready {
		t.Fatalf("prepare = %+v, %v", rep, err)
	}
	want := filepath.Join(dir, stage.ExternalDirName)
	if got := loadManifest(t, home).ExportsDir; got != want {
		t.Errorf("ExportsDir = %q, want %q", got, want)
	}
	info, err := os.Stat(filepath.Join(want, "exp", "data"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("export = %v, %v; want a 0600 file in %s", info, err, want)
	}
}

func TestPrepareStageNeedsAFullPath(t *testing.T) {
	home := t.TempDir()
	var seen module.Env
	a := &App{Runner: fakeMac(home, oldUUID), Home: home, Version: "test", Now: time.Now, Modules: []module.Module{exportModule{&seen}}}
	rep, err := a.Prepare(ctx, PrepareOptions{StageDir: "Volumes/X"})
	if err != nil || rep.Ready || !strings.Contains(rep.Problems[0].Detail, "full path") {
		t.Errorf("prepare = %+v, %v; want a blocker asking for a full path", rep, err)
	}
}

func TestPrepareOnlyKeepsExportsInOneFolder(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	var seen module.Env
	a := &App{Runner: stagingMac(t, home, dir, "true"), Home: home, Version: "test", Now: time.Now, Modules: []module.Module{exportModule{&seen}}}
	if rep, err := a.Prepare(ctx, PrepareOptions{StageDir: dir}); err != nil || !rep.Ready {
		t.Fatalf("prepare --stage = %+v, %v", rep, err)
	}
	rep, err := a.Prepare(ctx, PrepareOptions{Only: []string{"exp"}, StageDir: dir})
	if err != nil || !rep.Ready {
		t.Fatalf("prepare --only with the same --stage = %+v, %v", rep, err)
	}
	rep, err = a.Prepare(ctx, PrepareOptions{Only: []string{"exp"}})
	if err != nil || rep.Ready || rep.Problems[0].ID != "exports" {
		t.Errorf("prepare --only without --stage = %+v, %v; want a blocker", rep, err)
	}
}

func TestFinishFindsTheExportsPrepareWrote(t *testing.T) {
	old := t.TempDir()
	var seen module.Env
	mod := exportModule{&seen}
	mustPrepare(t, stepApp(old, oldUUID, mod))
	newHome := migrate(t, old)
	rep, err := stepApp(newHome, newUUID, mod).Finish(ctx, FinishOptions{})
	if err != nil || !slices.Equal(rep.Done, []string{"exp: checked the export"}) {
		t.Errorf("finish = %+v, %v", rep, err)
	}
	if want := filepath.Join(newHome, stage.DirName, stage.ExportsDirName); seen.Exports == nil || seen.Exports.Root != want {
		t.Errorf("Exports = %+v, want %s", seen.Exports, want)
	}
}

func TestFinishOnlyRunsTheNamedModulesWithTheirFlags(t *testing.T) {
	old := t.TempDir()
	var seen module.Env
	var calls []string
	exp, step := exportModule{&seen}, stepModule{name: "svc", calls: &calls}
	mustPrepare(t, stepApp(old, oldUUID, exp, step))
	newHome := migrate(t, old)
	_, err := stepApp(newHome, newUUID, exp, step).Finish(ctx, FinishOptions{Only: []string{"exp"}, Force: true, RestoreDump: true})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(calls, "finish") {
		t.Error("finish --only exp also finished svc")
	}
	if !seen.Force || !seen.RestoreDump {
		t.Errorf("env = %+v, want Force and RestoreDump", seen)
	}
}

func TestFinishRejectsAnUnknownModule(t *testing.T) {
	if _, err := stepApp(t.TempDir(), newUUID).Finish(ctx, FinishOptions{Only: []string{"nope"}}); err == nil {
		t.Error("finish --only nope succeeded")
	}
}

func stagedRecord(t *testing.T, home, exportsDir string) {
	t.Helper()
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.New("test")
	m.MachineID, m.ExportsDir = "x", exportsDir
	if err := manifest.Save(st, m); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupDeletesTheStageFolder(t *testing.T) {
	home := t.TempDir()
	external, err := stage.OpenDir(filepath.Join(t.TempDir(), stage.ExternalDirName))
	if err != nil {
		t.Fatal(err)
	}
	stagedRecord(t, home, external.Root)
	rep, err := newTestApp(home, fakeMac(home, newUUID)).Cleanup(true)
	if err != nil || !rep.Removed || !rep.ExternalRemoved || rep.External != external.Root {
		t.Fatalf("cleanup = %+v, %v", rep, err)
	}
	if _, err := os.Stat(external.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the --stage folder still exists")
	}
}

func TestCleanupWithTheStageDriveDetached(t *testing.T) {
	home := t.TempDir()
	gone := filepath.Join(t.TempDir(), "detached", stage.ExternalDirName)
	stagedRecord(t, home, gone)
	rep, err := newTestApp(home, fakeMac(home, newUUID)).Cleanup(true)
	if err != nil || !rep.Removed || rep.ExternalRemoved || rep.External != gone {
		t.Errorf("cleanup = %+v, %v; want ~/.mtm deleted and the detached folder named", rep, err)
	}
}

func TestCleanupDeletesOnlyAFolderMtmMade(t *testing.T) {
	home := t.TempDir()
	other, err := stage.OpenDir(filepath.Join(t.TempDir(), "Documents"))
	if err != nil {
		t.Fatal(err)
	}
	stagedRecord(t, home, other.Root)
	if _, err := newTestApp(home, fakeMac(home, newUUID)).Cleanup(true); err == nil {
		t.Error("cleanup accepted a folder not named mtm-exports")
	}
	if _, err := os.Stat(other.Root); err != nil {
		t.Errorf("cleanup deleted %s: %v", other.Root, err)
	}
}
