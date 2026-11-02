package homebrew

import (
	"context"
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
	"github.com/momentmaker/mtm/internal/testutil"
)

// listJSON is brew services list --json as captured on macOS 26, with
// postgresql@16 running as the user and unbound running as root.
const listJSON = `[
  {"name": "atuin", "status": "none", "user": null, "file": "/opt/homebrew/opt/atuin/sh.brew.atuin.plist", "exit_code": null},
  {"name": "postgresql@16", "status": "started", "user": "dev", "file": "/Users/dev/Library/LaunchAgents/homebrew.mxcl.postgresql@16.plist", "exit_code": 0},
  {"name": "unbound", "status": "started", "user": "root", "file": "/Library/LaunchDaemons/homebrew.mxcl.unbound.plist", "exit_code": 0}
]`

const allStoppedJSON = `[
  {"name": "atuin", "status": "none", "user": null},
  {"name": "postgresql@16", "status": "none", "user": null},
  {"name": "unbound", "status": "none", "user": null}
]`

type fixture struct {
	home string
	mod  Module
	fake *runner.Fake
	st   *stage.Stage
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	brew := filepath.Join(root, "homebrew", "bin", "brew")
	testutil.WriteFile(t, brew, "")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{home: home, mod: Module{Brew: brew, User: "dev"}, fake: &runner.Fake{Responses: map[string]runner.Response{}}, st: st}
	f.respond("services list --json", runner.Response{Stdout: listJSON})
	f.respond("bundle dump --file=- --formula --cask --tap --mas", runner.Response{Stdout: "tap \"homebrew/core\"\nbrew \"git\"\n"})
	f.respond("services stop postgresql@16", runner.Response{})
	f.respond("services start postgresql@16", runner.Response{})
	return f
}

func (f fixture) respond(args string, r runner.Response) {
	f.fake.Responses[f.mod.Brew+" "+args] = r
}

func (f fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func (f fixture) ran(args string) bool {
	return slices.ContainsFunc(f.fake.Calls(), func(c runner.Command) bool { return c.String() == f.mod.Brew+" "+args })
}

func record(t *testing.T, inv Inventory) *manifest.Manifest {
	t.Helper()
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), inv); err != nil {
		t.Fatal(err)
	}
	return m
}

func ids(findings []module.Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.ID)
	}
	return out
}

func TestScanReportsServices(t *testing.T) {
	f := newFixture(t)
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"homebrew", "services", "root-services"}; !slices.Equal(ids(findings), want) {
		t.Fatalf("ids = %q, want %q", ids(findings), want)
	}
	if !strings.Contains(findings[1].Title, "postgresql@16") || !strings.Contains(findings[2].Fix, "sudo brew services stop unbound") {
		t.Errorf("findings = %+v", findings)
	}
}

func TestScanWithoutHomebrew(t *testing.T) {
	f := newFixture(t)
	f.mod.Brew = filepath.Join(t.TempDir(), "nope")
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil || len(findings) != 1 || findings[0].ID != "not-installed" {
		t.Errorf("Scan = %+v, %v", findings, err)
	}
}

func TestPrepareStopsUserServicesAndSavesBrewfile(t *testing.T) {
	f := newFixture(t)
	res, err := f.mod.Prepare(f.env(manifest.New("test")), f.st)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	if !slices.Equal(inv.Stopped, []string{"postgresql@16"}) || !slices.Equal(inv.Root, []string{"unbound"}) || !inv.Brewfile {
		t.Errorf("inventory = %+v", inv)
	}
	if !f.ran("services stop postgresql@16") {
		t.Error("postgresql@16 was not stopped")
	}
	if f.ran("services stop unbound") {
		t.Error("mtm tried to stop a root service itself")
	}
	brewfile, err := os.ReadFile(f.st.Path(BrewfileName))
	if err != nil || !strings.Contains(string(brewfile), `brew "git"`) {
		t.Errorf("Brewfile = %q, %v", brewfile, err)
	}
	if !slices.Contains(ids(res.Problems), "root-services") || module.HasBlocker(res.Problems) {
		t.Errorf("problems = %+v, want only the root-services warning", res.Problems)
	}
}

func TestPrepareStopFailureBlocks(t *testing.T) {
	f := newFixture(t)
	f.respond("services stop postgresql@16", runner.Response{Code: 1, Stderr: "Error: Failure while executing"})
	res, err := f.mod.Prepare(f.env(manifest.New("test")), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if !module.HasBlocker(res.Problems) || slices.Contains(res.Inventory.(Inventory).Stopped, "postgresql@16") {
		t.Errorf("Prepare = %+v, want a blocker and postgresql@16 not recorded as stopped", res)
	}
}

func TestPrepareBrewfileFailureWarns(t *testing.T) {
	f := newFixture(t)
	f.respond("bundle dump --file=- --formula --cask --tap --mas", runner.Response{Code: 1, Stderr: "Error: boom"})
	res, err := f.mod.Prepare(f.env(manifest.New("test")), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if res.Inventory.(Inventory).Brewfile || !slices.Contains(ids(res.Problems), "brewfile") || module.HasBlocker(res.Problems) {
		t.Errorf("Prepare = %+v", res)
	}
}

func TestPrepareAgainKeepsServicesStoppedEarlier(t *testing.T) {
	f := newFixture(t)
	f.respond("services list --json", runner.Response{Stdout: allStoppedJSON})
	previous := record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}, Root: []string{"unbound"}})
	res, err := f.mod.Prepare(f.env(previous), f.st)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	if !slices.Equal(inv.Stopped, []string{"postgresql@16"}) || !slices.Equal(inv.Root, []string{"unbound"}) {
		t.Errorf("inventory = %+v, want the earlier stops carried forward", inv)
	}
	if f.ran("services stop postgresql@16") {
		t.Error("stopped an already stopped service")
	}
}

func TestUndoAndFinishStartServices(t *testing.T) {
	for _, step := range []string{"undo", "finish"} {
		t.Run(step, func(t *testing.T) {
			f := newFixture(t)
			m := record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}, Root: []string{"unbound"}})
			var res module.Result
			var err error
			if step == "undo" {
				res, err = f.mod.Undo(f.env(m), f.st)
			} else {
				res, err = f.mod.Finish(f.env(m), f.st)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !f.ran("services start postgresql@16") || !slices.Equal(res.Done, []string{"started postgresql@16"}) {
				t.Errorf("result = %+v", res)
			}
			if len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Fix, "sudo brew services start unbound") {
				t.Errorf("problems = %+v", res.Problems)
			}
		})
	}
}

func TestFinishStartFailureIsByHand(t *testing.T) {
	f := newFixture(t)
	f.respond("services start postgresql@16", runner.Response{Code: 1, Stderr: "Error: boom"})
	res, err := f.mod.Finish(f.env(record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}})), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Fix, "brew services start postgresql@16") {
		t.Errorf("problems = %+v", res.Problems)
	}
}

func TestVerifyPackagesAndServices(t *testing.T) {
	f := newFixture(t)
	f.st.WriteFile(BrewfileName, []byte("brew \"wget\"\n"))
	check := "bundle check --file=" + f.st.Path(BrewfileName) + " --no-upgrade --verbose"
	f.respond(check, runner.Response{Code: 1, Stdout: "brew bundle can't satisfy your Brewfile's dependencies.\n→ Formula wget needs to be installed.\n"})
	f.respond("services list --json", runner.Response{Stdout: allStoppedJSON})
	m := record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}, Brewfile: true})
	checks, err := f.mod.Verify(f.env(m), f.st)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]module.Check{}
	for _, c := range checks {
		byID[c.ID] = c
	}
	if c := byID["homebrew:packages"]; c.Status != module.StatusWarn || !slices.Equal(c.Paths, []string{"Formula wget"}) || !strings.Contains(c.Detail, "brew bundle install") {
		t.Errorf("packages = %+v", c)
	}
	if c := byID["homebrew:services"]; c.Status != module.StatusWarn || !slices.Equal(c.Paths, []string{"postgresql@16"}) {
		t.Errorf("services = %+v", c)
	}
}

func TestVerifyPassesWhenAllIsWell(t *testing.T) {
	f := newFixture(t)
	f.st.WriteFile(BrewfileName, []byte("brew \"git\"\n"))
	f.respond("bundle check --file="+f.st.Path(BrewfileName)+" --no-upgrade --verbose", runner.Response{Stdout: "The Brewfile's dependencies are satisfied.\n"})
	checks, err := f.mod.Verify(f.env(record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}, Brewfile: true})), f.st)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.Status != module.StatusPass {
			t.Errorf("%s = %s (%s)", c.ID, c.Status, c.Detail)
		}
	}
}

func TestVerifyHomebrewMissing(t *testing.T) {
	f := newFixture(t)
	f.mod.Brew = filepath.Join(t.TempDir(), "nope")
	checks, err := f.mod.Verify(f.env(record(t, Inventory{Installed: true})), f.st)
	if err != nil || len(checks) != 1 || checks[0].ID != "homebrew:installed" || checks[0].Status != module.StatusFail {
		t.Errorf("Verify = %+v, %v", checks, err)
	}
}
