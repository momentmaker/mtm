package homebrew

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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

// listJSON is brew services list --json in Homebrew 7's format, run as the
// user dev. redis was started with brew services run, so it has no user.
// mysql exited with an error, and launchd may restart it. unbound is a root
// daemon: brew run as the user can't see the system domain, so it always
// reads "none" there, loaded or not.
const listJSON = `[
  {"name": "atuin", "status": "none", "user": null, "file": "/opt/homebrew/opt/atuin/sh.brew.atuin.plist", "exit_code": null},
  {"name": "mysql", "status": "error", "user": "dev", "file": "/Users/dev/Library/LaunchAgents/homebrew.mxcl.mysql.plist", "exit_code": 1},
  {"name": "postgresql@16", "status": "started", "user": "dev", "file": "/Users/dev/Library/LaunchAgents/homebrew.mxcl.postgresql@16.plist", "exit_code": 0},
  {"name": "redis", "status": "started", "user": null, "file": "/opt/homebrew/opt/redis/sh.brew.redis.plist", "exit_code": 0},
  {"name": "unbound", "status": "none", "user": "root", "file": "/Library/LaunchDaemons/homebrew.mxcl.unbound.plist", "exit_code": null}
]`

// allStoppedJSON is the same Mac after brew services stop, which removes a
// service's plist from ~/Library/LaunchAgents, so brew reports no user.
const allStoppedJSON = `[
  {"name": "atuin", "status": "none", "user": null, "file": "/opt/homebrew/opt/atuin/sh.brew.atuin.plist", "exit_code": null},
  {"name": "mysql", "status": "none", "user": null, "file": "/opt/homebrew/opt/mysql/homebrew.mxcl.mysql.plist", "exit_code": null},
  {"name": "postgresql@16", "status": "none", "user": null, "file": "/opt/homebrew/opt/postgresql@16/homebrew.mxcl.postgresql@16.plist", "exit_code": null},
  {"name": "redis", "status": "none", "user": null, "file": "/opt/homebrew/opt/redis/sh.brew.redis.plist", "exit_code": null},
  {"name": "unbound", "status": "none", "user": "root", "file": "/Library/LaunchDaemons/homebrew.mxcl.unbound.plist", "exit_code": null}
]`

const postgresStartedJSON = `[
  {"name": "postgresql@16", "status": "started", "user": "dev", "file": "/Users/dev/Library/LaunchAgents/homebrew.mxcl.postgresql@16.plist", "exit_code": 0},
  {"name": "unbound", "status": "none", "user": "root", "file": "/Library/LaunchDaemons/homebrew.mxcl.unbound.plist", "exit_code": null}
]`

const (
	unboundLabel = "homebrew.mxcl.unbound"
	printUnbound = "/bin/launchctl print system/" + unboundLabel
)

var (
	loaded    = runner.Response{Stdout: "system/" + unboundLabel + " = {\n\tactive count = 1\n\tstate = running\n}\n"}
	notLoaded = runner.Response{Code: 113, Stderr: "Bad request.\nCould not find service \"" + unboundLabel + "\" in domain for system\n"}
)

type fixture struct {
	home  string
	cache string
	mod   Module
	fake  *runner.Fake
	st    *stage.Stage
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	brew := filepath.Join(root, "homebrew", "bin", "brew")
	testutil.WriteFile(t, brew, "")
	cache := filepath.Join(root, "Caches", "Homebrew")
	testutil.WriteFile(t, filepath.Join(cache, "api", "formula.jws.json"), `{"payload":"[]"}`)
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{home: home, cache: cache, mod: Module{Brew: brew, User: "dev"}, fake: &runner.Fake{Responses: map[string]runner.Response{}}, st: st}
	f.respond("--cache", runner.Response{Stdout: cache + "\n"})
	f.respond("services list --json", runner.Response{Stdout: listJSON})
	f.respond("bundle dump --file=- --formula --cask --tap --mas", runner.Response{Stdout: "tap \"homebrew/core\"\nbrew \"git\"\n"})
	for _, name := range []string{"mysql", "postgresql@16", "redis"} {
		f.respond("services stop "+name, runner.Response{})
		f.respond("services start "+name, runner.Response{})
	}
	f.fake.Responses[printUnbound] = notLoaded
	return f
}

func (f fixture) respond(args string, r runner.Response) {
	f.fake.Responses[f.mod.Brew+" "+args] = r
}

func (f fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func (f fixture) called(line string) bool {
	return slices.ContainsFunc(f.fake.Calls(), func(c runner.Command) bool { return c.String() == line })
}

func (f fixture) ran(args string) bool { return f.called(f.mod.Brew + " " + args) }

// ranBrewBeyondCache reports any brew command other than brew --cache,
// which could download the package data the cache lacks.
func (f fixture) ranBrewBeyondCache() bool {
	return slices.ContainsFunc(f.fake.Calls(), func(c runner.Command) bool {
		return c.Name == f.mod.Brew && !slices.Equal(c.Args, []string{"--cache"})
	})
}

func (f fixture) emptyCache(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(f.cache, "api")); err != nil {
		t.Fatal(err)
	}
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

func byID(findings []module.Finding) map[string]module.Finding {
	out := map[string]module.Finding{}
	for _, f := range findings {
		out[f.ID] = f
	}
	return out
}

func stoppedEarlier() Inventory {
	return Inventory{Installed: true, Stopped: []string{"postgresql@16"}, Root: []string{"unbound"}, RootLabels: map[string]string{"unbound": unboundLabel}, Brewfile: true}
}

func TestScanReportsActiveAndRootServices(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses[printUnbound] = loaded
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"homebrew", "services", "root:unbound"}; !slices.Equal(ids(findings), want) {
		t.Fatalf("ids = %q, want %q", ids(findings), want)
	}
	if want := "3 running services: mysql, postgresql@16, redis"; findings[1].Title != want {
		t.Errorf("services title = %q, want %q", findings[1].Title, want)
	}
	root := findings[2]
	if root.Severity != module.SeverityWarn || root.Fix != "Run: sudo brew services stop 'unbound', then run mtm prepare again" {
		t.Errorf("root finding = %+v", root)
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

func TestPrepareStopsEveryActiveUserServiceAndSavesBrewfile(t *testing.T) {
	f := newFixture(t)
	res, err := f.mod.Prepare(f.env(manifest.New("test")), f.st)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	// redis was started with brew services run (no user); mysql is in the
	// error state, which launchd's KeepAlive can restart mid-copy.
	if want := []string{"mysql", "postgresql@16", "redis"}; !slices.Equal(inv.Stopped, want) {
		t.Errorf("stopped = %q, want %q", inv.Stopped, want)
	}
	for _, name := range []string{"mysql", "postgresql@16", "redis"} {
		if !f.ran("services stop " + name) {
			t.Errorf("%s was not stopped", name)
		}
	}
	if f.ran("services stop atuin") || f.ran("services stop unbound") {
		t.Error("mtm stopped a service that wasn't running as the user")
	}
	if !inv.Brewfile {
		t.Error("Brewfile not recorded")
	}
	brewfile, err := os.ReadFile(f.st.Path(BrewfileName))
	if err != nil || !strings.Contains(string(brewfile), `brew "git"`) {
		t.Errorf("Brewfile = %q, %v", brewfile, err)
	}
	if len(res.Problems) != 0 {
		t.Errorf("problems = %+v, want none", res.Problems)
	}
}

func TestPrepareBlocksOnALoadedRootService(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses[printUnbound] = loaded
	res, err := f.mod.Prepare(f.env(manifest.New("test")), f.st)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := byID(res.Problems)["root:unbound"]
	if !ok || p.Severity != module.SeverityBlocker || p.Title != "unbound runs as root; stop it before Migration Assistant" ||
		p.Fix != "Run: sudo brew services stop 'unbound', then run mtm prepare again" {
		t.Errorf("problems = %+v, want a root:unbound blocker", res.Problems)
	}
	inv := res.Inventory.(Inventory)
	if !slices.Equal(inv.Root, []string{"unbound"}) || inv.RootLabels["unbound"] != unboundLabel {
		t.Errorf("root = %q, labels = %v", inv.Root, inv.RootLabels)
	}
	if f.ran("services stop unbound") {
		t.Error("mtm tried to stop a root service itself")
	}
}

func TestPrepareAsksLaunchdAndIgnoresAnUnloadedRootService(t *testing.T) {
	f := newFixture(t)
	res, err := f.mod.Prepare(f.env(manifest.New("test")), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if !f.called(printUnbound) {
		t.Error("launchd was not asked about unbound")
	}
	if inv := res.Inventory.(Inventory); len(inv.Root) != 0 || module.HasBlocker(res.Problems) {
		t.Errorf("root = %q, problems = %+v; want neither for a root service launchd hasn't loaded", inv.Root, res.Problems)
	}
}

func TestPrepareStopFailureBlocks(t *testing.T) {
	f := newFixture(t)
	f.respond("services stop postgresql@16", runner.Response{Code: 1, Stderr: "Error: Failure while executing"})
	res, err := f.mod.Prepare(f.env(manifest.New("test")), f.st)
	if err != nil {
		t.Fatal(err)
	}
	p := byID(res.Problems)["stop:postgresql@16"]
	if p.Severity != module.SeverityBlocker || slices.Contains(res.Inventory.(Inventory).Stopped, "postgresql@16") {
		t.Errorf("Prepare = %+v, want a blocker and postgresql@16 not recorded as stopped", res)
	}
	if !strings.HasPrefix(p.Fix, "Run: brew services stop 'postgresql@16', then") {
		t.Errorf("fix = %q, want the service name quoted", p.Fix)
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
	res, err := f.mod.Prepare(f.env(record(t, stoppedEarlier())), f.st)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	if !slices.Equal(inv.Stopped, []string{"postgresql@16"}) || !slices.Equal(inv.Root, []string{"unbound"}) || inv.RootLabels["unbound"] != unboundLabel {
		t.Errorf("inventory = %+v, want the earlier stops carried forward", inv)
	}
	if f.ran("services stop postgresql@16") {
		t.Error("stopped an already stopped service")
	}
}

func TestPrepareAgainRecordsARestartedServiceOnce(t *testing.T) {
	f := newFixture(t)
	res, err := f.mod.Prepare(f.env(record(t, stoppedEarlier())), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Inventory.(Inventory).Stopped; !slices.Equal(got, []string{"mysql", "postgresql@16", "redis"}) {
		t.Errorf("stopped = %q, want each service once", got)
	}
}

func TestPrepareKeepsThePreviousRecordWhenServicesCannotBeListed(t *testing.T) {
	f := newFixture(t)
	f.respond("services list --json", runner.Response{Code: 1, Stderr: "Error: boom"})
	previous := stoppedEarlier()
	res, err := f.mod.Prepare(f.env(record(t, previous)), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Inventory, previous) {
		t.Errorf("inventory = %+v, want the previous one %+v", res.Inventory, previous)
	}
	if p := byID(res.Problems)["services"]; p.Severity != module.SeverityBlocker {
		t.Errorf("problems = %+v, want a services blocker", res.Problems)
	}
}

func TestPrepareWithoutARecordFailsWhenServicesCannotBeListed(t *testing.T) {
	f := newFixture(t)
	f.respond("services list --json", runner.Response{Code: 1, Stderr: "Error: boom"})
	if _, err := f.mod.Prepare(f.env(manifest.New("test")), f.st); err == nil || !strings.Contains(err.Error(), "Could not list Homebrew services") {
		t.Errorf("err = %v, want a services error, so nothing claims Homebrew isn't installed", err)
	}
}

func TestUndoAndFinishStartServices(t *testing.T) {
	for _, step := range []string{"undo", "finish"} {
		t.Run(step, func(t *testing.T) {
			f := newFixture(t)
			m := record(t, stoppedEarlier())
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
			if len(res.Problems) != 1 || res.Problems[0].Fix != "Run: sudo brew services start 'unbound'" {
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
	if len(res.Problems) != 1 || res.Problems[0].Fix != "Run: brew services start 'postgresql@16'" {
		t.Errorf("problems = %+v", res.Problems)
	}
}

func TestVerifyPackagesAndServices(t *testing.T) {
	f := newFixture(t)
	f.st.WriteFile(BrewfileName, []byte("brew \"wget\"\n"))
	check := "bundle check --file=" + f.st.Path(BrewfileName) + " --no-upgrade --verbose"
	f.respond(check, runner.Response{Code: 1, Stderr: "brew bundle can't satisfy your Brewfile's dependencies.\n→ Formula wget needs to be installed.\nSatisfy missing dependencies with `brew bundle install`.\n"})
	f.respond("services list --json", runner.Response{Stdout: allStoppedJSON})
	m := record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}, Brewfile: true})
	checks, err := f.mod.Verify(f.env(m), f.st)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]module.Check{}
	for _, c := range checks {
		got[c.ID] = c
	}
	if c := got["homebrew:packages"]; c.Status != module.StatusWarn || !slices.Equal(c.Paths, []string{"Formula wget"}) || !strings.Contains(c.Detail, "brew bundle install") {
		t.Errorf("packages = %+v", c)
	}
	if c := got["homebrew:services"]; c.Status != module.StatusWarn || !slices.Equal(c.Paths, []string{"postgresql@16"}) {
		t.Errorf("services = %+v", c)
	}
}

// A stdout whose last line has no newline must not swallow the first
// stderr line, which is where brew writes the missing packages.
func TestVerifyPackagesReadsStderrAfterUnterminatedStdout(t *testing.T) {
	f := newFixture(t)
	f.st.WriteFile(BrewfileName, []byte("brew \"wget\"\n"))
	f.respond("bundle check --file="+f.st.Path(BrewfileName)+" --no-upgrade --verbose", runner.Response{
		Code: 1, Stdout: "Using git", Stderr: "→ Formula wget needs to be installed.\n",
	})
	f.respond("services list --json", runner.Response{Stdout: postgresStartedJSON})
	checks, err := f.mod.Verify(f.env(record(t, Inventory{Installed: true, Brewfile: true})), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if c := checks[0]; c.ID != "homebrew:packages" || !slices.Equal(c.Paths, []string{"Formula wget"}) {
		t.Errorf("packages = %+v, want wget listed as missing", c)
	}
}

func TestVerifyAsksLaunchdAboutRootServices(t *testing.T) {
	tests := []struct {
		name   string
		resp   runner.Response
		status module.Status
	}{
		{"loaded", loaded, module.StatusPass},
		{"not loaded", notLoaded, module.StatusWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.fake.Responses[printUnbound] = tt.resp
			f.respond("services list --json", runner.Response{Stdout: postgresStartedJSON})
			inv := stoppedEarlier()
			inv.Brewfile = false
			checks, err := f.mod.Verify(f.env(record(t, inv)), f.st)
			if err != nil {
				t.Fatal(err)
			}
			if len(checks) != 1 || checks[0].ID != "homebrew:services" || checks[0].Status != tt.status {
				t.Fatalf("checks = %+v, want services %s", checks, tt.status)
			}
			if tt.status == module.StatusWarn && !slices.Equal(checks[0].Paths, []string{"unbound"}) {
				t.Errorf("paths = %q, want unbound", checks[0].Paths)
			}
			if !f.called(printUnbound) {
				t.Error("launchd was not asked about unbound")
			}
		})
	}
}

func TestVerifyPassesWhenAllIsWell(t *testing.T) {
	f := newFixture(t)
	f.st.WriteFile(BrewfileName, []byte("brew \"git\"\n"))
	f.respond("bundle check --file="+f.st.Path(BrewfileName)+" --no-upgrade --verbose", runner.Response{Stdout: "The Brewfile's dependencies are satisfied.\n"})
	f.respond("services list --json", runner.Response{Stdout: postgresStartedJSON})
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

const wantNoCacheTitle = "Homebrew's package data isn't cached on this Mac, so brew would download it"

func TestScanWithoutPackageDataWarnsAndRunsNoOtherBrew(t *testing.T) {
	f := newFixture(t)
	f.emptyCache(t)
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	p := byID(findings)["cache"]
	if p.Severity != module.SeverityWarn || p.Title != wantNoCacheTitle || p.Fix != "Run brew update yourself (it uses the network), then run mtm scan again." {
		t.Errorf("findings = %+v, want a cache warning", findings)
	}
	if f.ranBrewBeyondCache() {
		t.Errorf("ran %v; want only brew --cache", f.fake.Calls())
	}
}

func TestPrepareWithoutPackageDataBlocksAndKeepsThePreviousRecord(t *testing.T) {
	f := newFixture(t)
	f.emptyCache(t)
	previous := stoppedEarlier()
	res, err := f.mod.Prepare(f.env(record(t, previous)), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Inventory, previous) {
		t.Errorf("inventory = %+v, want the previous one %+v", res.Inventory, previous)
	}
	p := byID(res.Problems)["cache"]
	if p.Severity != module.SeverityBlocker || p.Title != wantNoCacheTitle || p.Fix != "Run brew update yourself (it uses the network), then run mtm prepare again." {
		t.Errorf("problems = %+v, want a cache blocker", res.Problems)
	}
	if f.ranBrewBeyondCache() {
		t.Errorf("ran %v; want only brew --cache", f.fake.Calls())
	}
}

func TestPrepareWithoutPackageDataOrARecordFails(t *testing.T) {
	f := newFixture(t)
	f.emptyCache(t)
	if _, err := f.mod.Prepare(f.env(manifest.New("test")), f.st); err == nil || !strings.Contains(err.Error(), wantNoCacheTitle) {
		t.Errorf("err = %v, want the cache error, so nothing claims Homebrew isn't installed", err)
	}
}

func TestFinishWithoutPackageDataWarnsAndStartsNothing(t *testing.T) {
	f := newFixture(t)
	f.emptyCache(t)
	res, err := f.mod.Finish(f.env(record(t, stoppedEarlier())), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || res.Problems[0].ID != "cache" || res.Problems[0].Severity != module.SeverityWarn ||
		res.Problems[0].Fix != "Run brew update yourself (it uses the network), then run mtm finish again." {
		t.Errorf("problems = %+v, want only the cache warning", res.Problems)
	}
	if f.ranBrewBeyondCache() || len(res.Done) != 0 {
		t.Errorf("ran %v, done %q; want nothing started", f.fake.Calls(), res.Done)
	}
}

func TestUndoWithoutPackageDataFailsSoTheRecordStays(t *testing.T) {
	f := newFixture(t)
	f.emptyCache(t)
	_, err := f.mod.Undo(f.env(record(t, stoppedEarlier())), f.st)
	if err == nil || !strings.Contains(err.Error(), "then run mtm prepare --undo again") {
		t.Errorf("err = %v, want the cache error", err)
	}
	if f.ranBrewBeyondCache() {
		t.Errorf("ran %v; want nothing started", f.fake.Calls())
	}
}

func TestVerifyWithoutPackageDataWarnsOnce(t *testing.T) {
	f := newFixture(t)
	f.emptyCache(t)
	checks, err := f.mod.Verify(f.env(record(t, stoppedEarlier())), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].ID != "homebrew:cache" || checks[0].Status != module.StatusWarn || !strings.Contains(checks[0].Detail, "then run mtm verify again") {
		t.Errorf("checks = %+v, want only a homebrew:cache warning", checks)
	}
	if f.ranBrewBeyondCache() {
		t.Errorf("ran %v; want only brew --cache", f.fake.Calls())
	}
}

func TestHasPackageData(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"formula data", map[string]string{"formula.jws.json": "{}"}, true},
		{"one folder below", map[string]string{"internal/packages.arm64_tahoe.jws.json": "{}"}, true},
		{"empty file", map[string]string{"formula.jws.json": ""}, false},
		{"two folders below", map[string]string{"a/b/formula.jws.json": "{}"}, false},
		{"names only", map[string]string{"formula_names.txt": "git\n"}, false},
		{"no api folder", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := filepath.Join(t.TempDir(), "api")
			for rel, content := range tt.files {
				testutil.WriteFile(t, filepath.Join(api, filepath.FromSlash(rel)), content)
			}
			if got := hasPackageData(api); got != tt.want {
				t.Errorf("hasPackageData = %v, want %v", got, tt.want)
			}
		})
	}
}
