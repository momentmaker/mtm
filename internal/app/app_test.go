package app

import (
	"context"
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
	"github.com/momentmaker/mtm/internal/modules/files"
	"github.com/momentmaker/mtm/internal/modules/git"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
	"github.com/momentmaker/mtm/internal/testutil"
)

const (
	oldUUID = "OLD-MAC-UUID"
	newUUID = "NEW-MAC-UUID"
)

var ctx = context.Background()

func fakeMac(home, uuid string) *runner.Fake {
	responses := testutil.MacResponses(uuid)
	responses["/usr/bin/tmutil addexclusion "+filepath.Join(home, stage.DirName)] = runner.Response{}
	return &runner.Fake{Responses: responses, Fallback: runner.Exec{}}
}

func newTestApp(home string, r runner.Runner) *App {
	return &App{Runner: r, Home: home, Version: "test", Now: time.Now, Modules: []module.Module{files.New(), git.New()}}
}

func seedHome(t *testing.T) string {
	t.Helper()
	testutil.IsolateGit(t)
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	testutil.WriteFile(t, filepath.Join(home, ".ssh/id_ed25519"), "key\n")
	testutil.WriteFile(t, filepath.Join(home, "Documents/notes.md"), "notes\n")
	testutil.WriteFile(t, filepath.Join(home, "Library/Preferences/p.plist"), "plist\n")
	repo := filepath.Join(home, "GitHub", "app")
	os.MkdirAll(repo, 0o755)
	testutil.Git(t, repo, "init", "-q", "-b", "main")
	testutil.WriteFile(t, filepath.Join(repo, ".gitignore"), ".env\n")
	testutil.WriteFile(t, filepath.Join(repo, "main.go"), "package main\n")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-q", "-m", "first")
	testutil.WriteFile(t, filepath.Join(repo, ".env"), "SECRET=1\n")
	return home
}

func prepareOld(t *testing.T, home string) {
	t.Helper()
	rep, err := newTestApp(home, fakeMac(home, oldUUID)).Prepare(ctx, PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Ready {
		t.Fatalf("not ready: %+v", rep.Problems)
	}
}

func migrate(t *testing.T, oldHome string) string {
	t.Helper()
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, oldHome, newHome)
	return newHome
}

func verifyOn(t *testing.T, home, uuid string, opts VerifyOptions) VerifyReport {
	t.Helper()
	rep, err := newTestApp(home, fakeMac(home, uuid)).Verify(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func blockingIDs(rep VerifyReport) []string {
	var ids []string
	for _, c := range rep.Verdict.Blocking {
		ids = append(ids, c.ID)
	}
	return ids
}

// keepTime rewrites path with same-length content and restores its old
// modification time: corruption that a timestamp check alone would miss.
func keepTime(t *testing.T, path, content string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(path, info.ModTime(), info.ModTime())
}

func TestSimulatedMigrationIsSafe(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	rep := verifyOn(t, migrate(t, old), newUUID, VerifyOptions{})
	if !rep.Verdict.Safe {
		t.Fatalf("not safe; blocking: %v", blockingIDs(rep))
	}
}

func TestVerifyCatchesFaults(t *testing.T) {
	tests := []struct {
		name   string
		damage func(t *testing.T, home string)
		want   string
	}{
		{"missing ssh key", func(t *testing.T, home string) {
			os.Remove(filepath.Join(home, ".ssh/id_ed25519"))
		}, "files:.ssh"},
		{"dotfile corrupted with original time", func(t *testing.T, home string) {
			keepTime(t, filepath.Join(home, ".zshrc"), "export B=2\n")
		}, "files:" + files.HomeGroup},
		{"repo missing", func(t *testing.T, home string) {
			os.RemoveAll(filepath.Join(home, "GitHub", "app"))
		}, "git:GitHub/app"},
		{"ignored secret corrupted with original time", func(t *testing.T, home string) {
			keepTime(t, filepath.Join(home, "GitHub", "app", ".env"), "SECRET=2\n")
		}, "git:GitHub/app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := seedHome(t)
			prepareOld(t, old)
			newHome := migrate(t, old)
			tt.damage(t, newHome)
			rep := verifyOn(t, newHome, newUUID, VerifyOptions{})
			if rep.Verdict.Safe {
				t.Fatal("verdict is SAFE TO WIPE, want NOT SAFE")
			}
			if !slices.Contains(blockingIDs(rep), tt.want) {
				t.Errorf("blocking = %v, want it to include %q", blockingIDs(rep), tt.want)
			}
		})
	}
}

func TestChangesAfterPrepareStaySafe(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	notes := filepath.Join(newHome, "Documents/notes.md")
	os.WriteFile(notes, []byte("edited on the new Mac\n"), 0o644)
	later := time.Now().Add(time.Minute)
	os.Chtimes(notes, later, later)
	if rep := verifyOn(t, newHome, newUUID, VerifyOptions{}); !rep.Verdict.Safe {
		t.Errorf("not safe after a normal edit; blocking: %v", blockingIDs(rep))
	}
}

func TestVerifyOnSameMacIsNotSafe(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	rep := verifyOn(t, old, oldUUID, VerifyOptions{})
	if rep.Verdict.Safe || !slices.Contains(blockingIDs(rep), "same-mac") {
		t.Errorf("blocking = %v, want same-mac", blockingIDs(rep))
	}
}

func TestVerifyWithoutRecord(t *testing.T) {
	home := t.TempDir()
	_, err := newTestApp(home, fakeMac(home, newUUID)).Verify(ctx, VerifyOptions{})
	if !errors.Is(err, stage.ErrNotFound) {
		t.Errorf("err = %v, want stage.ErrNotFound", err)
	}
}

func TestPrepareRefusesWithoutFileVault(t *testing.T) {
	home := seedHome(t)
	fake := fakeMac(home, oldUUID)
	fake.Responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
	rep, err := newTestApp(home, fake).Prepare(ctx, PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ready {
		t.Error("ready without FileVault")
	}
	if _, err := os.Stat(filepath.Join(home, stage.DirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("~/.mtm created without FileVault: %v", err)
	}
}

func TestPrepareOnlyKeepsOtherModules(t *testing.T) {
	home := seedHome(t)
	prepareOld(t, home)
	rep, err := newTestApp(home, fakeMac(home, oldUUID)).Prepare(ctx, PrepareOptions{Only: []string{"git"}})
	if err != nil || !rep.Ready {
		t.Fatalf("prepare --only git: %+v, %v", rep, err)
	}
	st, _ := stage.Existing(home)
	m, err := manifest.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Modules[files.Name]; !ok {
		t.Error("files module dropped from the manifest")
	}
}

func TestPrepareRejectsUnknownModule(t *testing.T) {
	home := seedHome(t)
	_, err := newTestApp(home, fakeMac(home, oldUUID)).Prepare(ctx, PrepareOptions{Only: []string{"bogus"}})
	if err == nil || !strings.Contains(err.Error(), `unknown module "bogus"`) {
		t.Errorf("err = %v, want unknown module", err)
	}
}

func TestPrepareRefusesToOverwriteAnotherMacsRecord(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	_, err := newTestApp(newHome, fakeMac(newHome, newUUID)).Prepare(ctx, PrepareOptions{})
	if !errors.Is(err, ErrOtherMacsRecord) || !strings.Contains(err.Error(), "mtm verify") {
		t.Errorf("err = %v, want ErrOtherMacsRecord pointing to mtm verify", err)
	}
}

func TestPrepareWritesRedactedLog(t *testing.T) {
	home := seedHome(t)
	prepareOld(t, home)
	logs, err := os.ReadDir(filepath.Join(home, stage.DirName, "logs"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs = %v, %v; want one log file", logs, err)
	}
	data, _ := os.ReadFile(filepath.Join(home, stage.DirName, "logs", logs[0].Name()))
	if !strings.Contains(string(data), "$ /usr/sbin/ioreg") || !strings.Contains(string(data), "$ git -C") {
		t.Errorf("log missing expected commands:\n%s", data)
	}
	if strings.Contains(string(data), "SECRET=1") {
		t.Error("log contains file contents")
	}
}

func TestScanIsReadOnlyAndCombinesFindings(t *testing.T) {
	home := seedHome(t)
	fake := fakeMac(home, oldUUID)
	fake.Responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
	rep := newTestApp(home, fake).Scan(ctx)
	var ids []string
	for _, f := range rep.Findings {
		ids = append(ids, f.ID)
	}
	for _, want := range []string{"filevault", "home", "repos"} {
		if !slices.Contains(ids, want) {
			t.Errorf("finding ids %v missing %q", ids, want)
		}
	}
	if _, err := os.Stat(filepath.Join(home, stage.DirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("scan created ~/.mtm")
	}
}

func TestCleanupRules(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	a := newTestApp(newHome, fakeMac(newHome, newUUID))

	if _, err := a.Cleanup(false); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("cleanup before verify: %v, want ErrNotVerified", err)
	}
	verifyOn(t, newHome, newUUID, VerifyOptions{Only: []string{"git"}})
	if _, err := a.Cleanup(false); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("cleanup after verify --only: %v, want ErrNotVerified", err)
	}
	if rep := verifyOn(t, newHome, newUUID, VerifyOptions{}); !rep.Verdict.Safe {
		t.Fatalf("full verify not safe: %v", blockingIDs(rep))
	}
	rep, err := a.Cleanup(false)
	if err != nil || !rep.Removed {
		t.Fatalf("cleanup after SAFE: %+v, %v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(newHome, stage.DirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("~/.mtm still exists")
	}
	if rep, err := a.Cleanup(false); err != nil || rep.Removed {
		t.Errorf("second cleanup = %+v, %v; want nothing to remove", rep, err)
	}
}

func TestCleanupForce(t *testing.T) {
	home := seedHome(t)
	prepareOld(t, home)
	rep, err := newTestApp(home, fakeMac(home, oldUUID)).Cleanup(true)
	if err != nil || !rep.Removed {
		t.Errorf("forced cleanup = %+v, %v", rep, err)
	}
}

func TestVerifyBlocksOnModuleThatPrepareNeverRecorded(t *testing.T) {
	old := seedHome(t)
	rep, err := newTestApp(old, fakeMac(old, oldUUID)).Prepare(ctx, PrepareOptions{Only: []string{"files"}})
	if err != nil || !rep.Ready {
		t.Fatalf("prepare --only files: %+v, %v", rep, err)
	}
	got := verifyOn(t, migrate(t, old), newUUID, VerifyOptions{})
	if got.Verdict.Safe || !slices.Contains(blockingIDs(got), "git:not-prepared") {
		t.Errorf("safe = %v, blocking = %v; want git:not-prepared blocking", got.Verdict.Safe, blockingIDs(got))
	}
}

func lockDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o755) })
}

func TestUnreadableDocumentsOnNewMacIsNotSafe(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	lockDir(t, filepath.Join(newHome, "Documents"))
	rep := verifyOn(t, newHome, newUUID, VerifyOptions{})
	if rep.Verdict.Safe || !slices.Contains(blockingIDs(rep), "files:Documents") {
		t.Errorf("safe = %v, blocking = %v; want files:Documents blocking", rep.Verdict.Safe, blockingIDs(rep))
	}
}

func TestDocumentsUnreadableAtPrepareThenMissingIsNotSafe(t *testing.T) {
	old := seedHome(t)
	docs := filepath.Join(old, "Documents")
	lockDir(t, docs)
	rep, err := newTestApp(old, fakeMac(old, oldUUID)).Prepare(ctx, PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ready {
		t.Error("prepare is READY although ~/Documents could not be read")
	}
	os.Chmod(docs, 0o755)
	newHome := migrate(t, old)
	os.RemoveAll(filepath.Join(newHome, "Documents"))
	got := verifyOn(t, newHome, newUUID, VerifyOptions{})
	if got.Verdict.Safe || !slices.Contains(blockingIDs(got), "files:Documents") {
		t.Errorf("safe = %v, blocking = %v; want files:Documents blocking", got.Verdict.Safe, blockingIDs(got))
	}
}

func TestFailedPrepareRerunDropsTheStaleRecord(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	testutil.WriteFile(t, filepath.Join(old, "NewProject/only-here.txt"), "precious\n")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	rep, err := newTestApp(old, fakeMac(old, oldUUID)).Prepare(cancelled, PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ready {
		t.Fatal("cancelled prepare reported READY")
	}
	newHome := migrate(t, old)
	os.RemoveAll(filepath.Join(newHome, "NewProject"))
	got := verifyOn(t, newHome, newUUID, VerifyOptions{})
	if got.Verdict.Safe || !slices.Contains(blockingIDs(got), "files:not-prepared") {
		t.Errorf("safe = %v, blocking = %v; want files:not-prepared blocking", got.Verdict.Safe, blockingIDs(got))
	}
}

func TestVerifyFailsOnModuleRecordedByNewerMtm(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	st, err := stage.Existing(old)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetModule("docker", m.PreparedAt, map[string]int{"volumes": 2}); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(st, m); err != nil {
		t.Fatal(err)
	}
	newHome := migrate(t, old)

	rep := verifyOn(t, newHome, newUUID, VerifyOptions{})
	if rep.Verdict.Safe || !slices.Contains(blockingIDs(rep), "docker:unknown") {
		t.Fatalf("safe = %v, blocking = %v; want docker:unknown blocking", rep.Verdict.Safe, blockingIDs(rep))
	}
	i := slices.IndexFunc(rep.Checks, func(c module.Check) bool { return c.ID == "docker:unknown" })
	if c := rep.Checks[i]; !c.Critical || c.Title != "docker was recorded by a newer mtm" || !strings.Contains(c.Detail, "upgrade mtm") {
		t.Errorf("check = %+v", c)
	}
	only := verifyOn(t, newHome, newUUID, VerifyOptions{Only: []string{"files"}})
	if slices.ContainsFunc(only.Checks, func(c module.Check) bool { return c.ID == "docker:unknown" }) {
		t.Error("verify --only files reported the unknown module")
	}
}

func TestLinkToDataOutsideHomeThatDidNotArriveIsNotSafe(t *testing.T) {
	old := seedHome(t)
	outside := filepath.Join(t.TempDir(), "Work", "code")
	testutil.WriteFile(t, filepath.Join(outside, "precious.go"), "package p\n")
	if err := os.Symlink(outside, filepath.Join(old, "code")); err != nil {
		t.Fatal(err)
	}
	prepareOld(t, old)
	newHome := migrate(t, old)
	os.RemoveAll(outside)
	rep := verifyOn(t, newHome, newUUID, VerifyOptions{})
	if rep.Verdict.Safe || !slices.Contains(blockingIDs(rep), "files:"+files.HomeGroup) {
		t.Errorf("safe = %v, blocking = %v; want files:%s blocking", rep.Verdict.Safe, blockingIDs(rep), files.HomeGroup)
	}
}

// stepModule records which steps ran, to test the orchestration of
// prepare, finish, and undo without touching the machine.
type stepModule struct {
	name  string
	calls *[]string
}

func (m stepModule) Name() string                            { return m.name }
func (stepModule) Scan(module.Env) ([]module.Finding, error) { return nil, nil }

func (m stepModule) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	call := "prepare"
	if _, ok := env.Manifest.Modules[m.name]; ok {
		call = "prepare-again"
	}
	*m.calls = append(*m.calls, call)
	return module.Result{Inventory: map[string]bool{"ok": true}}, nil
}

func (m stepModule) Finish(module.Env, *stage.Stage) (module.Result, error) {
	*m.calls = append(*m.calls, "finish")
	return module.Result{
		Done:     []string{"started things"},
		Problems: []module.Finding{{Module: m.name, ID: "by-hand", Severity: module.SeverityWarn, Handler: module.Manual, Title: "Do a thing"}},
	}, nil
}

func (m stepModule) Verify(module.Env, *stage.Stage) ([]module.Check, error) {
	return []module.Check{{Module: m.name, ID: m.name + ":ok", Status: module.StatusPass, Title: "ok"}}, nil
}

func (m stepModule) Undo(module.Env, *stage.Stage) (module.Result, error) {
	*m.calls = append(*m.calls, "undo")
	return module.Result{Done: []string{"restarted things"}}, nil
}

// finishOnly has no Undo, like the files and git modules.
type finishOnly struct{ name string }

func (m finishOnly) Name() string                            { return m.name }
func (finishOnly) Scan(module.Env) ([]module.Finding, error) { return nil, nil }
func (finishOnly) Prepare(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{Inventory: true}, nil
}
func (finishOnly) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}
func (m finishOnly) Verify(module.Env, *stage.Stage) ([]module.Check, error) {
	return []module.Check{{Module: m.name, ID: m.name + ":ok", Status: module.StatusPass, Title: "ok"}}, nil
}

func stepApp(home, uuid string, mods ...module.Module) *App {
	return &App{Runner: fakeMac(home, uuid), Home: home, Version: "test", Now: time.Now, Modules: mods}
}

func mustPrepare(t *testing.T, a *App) {
	t.Helper()
	rep, err := a.Prepare(ctx, PrepareOptions{})
	if err != nil || !rep.Ready {
		t.Fatalf("prepare = %+v, %v", rep, err)
	}
}

func TestPrepareShowsModulesTheirPreviousRecord(t *testing.T) {
	home := t.TempDir()
	var calls []string
	a := stepApp(home, oldUUID, stepModule{name: "svc", calls: &calls})
	mustPrepare(t, a)
	mustPrepare(t, a)
	if want := []string{"prepare", "prepare-again"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %q, want %q", calls, want)
	}
}

func TestFinishRunsRecordedModulesOnTheNewMac(t *testing.T) {
	old := t.TempDir()
	var calls []string
	step := stepModule{name: "svc", calls: &calls}
	mustPrepare(t, stepApp(old, oldUUID, step))
	newHome := migrate(t, old)
	unrecorded := stepModule{name: "extra", calls: &calls}
	rep, err := stepApp(newHome, newUUID, step, unrecorded).Finish(ctx, FinishOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Done, []string{"svc: started things"}) || len(rep.Problems) != 1 {
		t.Errorf("report = %+v", rep)
	}
	if want := []string{"prepare", "finish"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %q, want %q (an unrecorded module must not run)", calls, want)
	}
}

func TestFinishRefusesOnTheOldMac(t *testing.T) {
	home := t.TempDir()
	var calls []string
	a := stepApp(home, oldUUID, stepModule{name: "svc", calls: &calls})
	mustPrepare(t, a)
	if _, err := a.Finish(ctx, FinishOptions{}); !errors.Is(err, ErrFinishOnOldMac) {
		t.Errorf("err = %v, want ErrFinishOnOldMac", err)
	}
	if slices.Contains(calls, "finish") {
		t.Error("finish ran on the old Mac")
	}
}

func TestFinishWithADamagedStateChangesNothing(t *testing.T) {
	old := t.TempDir()
	var calls []string
	step := stepModule{name: "svc", calls: &calls}
	mustPrepare(t, stepApp(old, oldUUID, step))
	newHome := migrate(t, old)
	st, err := stage.Existing(newHome)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.WriteFile(state.FileName, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if _, err := stepApp(newHome, newUUID, step).Finish(ctx, FinishOptions{}); err == nil {
		t.Error("finish succeeded with a damaged state.json")
	}
	if slices.Contains(calls, "finish") {
		t.Error("a module's finish ran before the damaged state.json stopped finish")
	}
}

func TestFinishNeedsFileVault(t *testing.T) {
	old := t.TempDir()
	var calls []string
	step := stepModule{name: "svc", calls: &calls}
	mustPrepare(t, stepApp(old, oldUUID, step))
	newHome := migrate(t, old)
	a := stepApp(newHome, newUUID, step)
	a.Runner.(*runner.Fake).Responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
	rep, err := a.Finish(ctx, FinishOptions{})
	if err != nil || !module.HasBlocker(rep.Problems) {
		t.Errorf("Finish = %+v, %v; want a FileVault blocker", rep, err)
	}
	if slices.Contains(calls, "finish") {
		t.Error("finish ran without FileVault")
	}
}

func TestUndoRestoresAndForgetsTheRecord(t *testing.T) {
	old := t.TempDir()
	var calls []string
	step := stepModule{name: "svc", calls: &calls}
	keep := finishOnly{name: "plain"}
	a := stepApp(old, oldUUID, step, keep)
	mustPrepare(t, a)
	rep, err := a.Undo(ctx)
	if err != nil || !slices.Equal(rep.Done, []string{"svc: restarted things"}) {
		t.Fatalf("Undo = %+v, %v", rep, err)
	}
	st, err := stage.Existing(old)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Modules["svc"]; ok {
		t.Error("undone module still has a record")
	}
	if _, ok := m.Modules["plain"]; !ok {
		t.Error("a module without Undo lost its record")
	}
	vrep, err := stepApp(migrate(t, old), newUUID, step, keep).Verify(ctx, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(blockingIDs(vrep), "svc:not-prepared") {
		t.Errorf("blocking = %v, want svc:not-prepared after undo", blockingIDs(vrep))
	}
}

func TestUndoRefusesOnTheNewMac(t *testing.T) {
	old := t.TempDir()
	var calls []string
	step := stepModule{name: "svc", calls: &calls}
	mustPrepare(t, stepApp(old, oldUUID, step))
	newHome := migrate(t, old)
	if _, err := stepApp(newHome, newUUID, step).Undo(ctx); !errors.Is(err, ErrUndoOnNewMac) {
		t.Errorf("err = %v, want ErrUndoOnNewMac", err)
	}
	if slices.Contains(calls, "undo") {
		t.Error("undo ran on the new Mac")
	}
}

func TestUndoWithoutRecord(t *testing.T) {
	home := t.TempDir()
	var calls []string
	if _, err := stepApp(home, oldUUID, stepModule{name: "svc", calls: &calls}).Undo(ctx); !errors.Is(err, stage.ErrNotFound) {
		t.Errorf("err = %v, want stage.ErrNotFound", err)
	}
}
