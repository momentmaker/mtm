package permissions

import (
	"context"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/momentmaker/mtm/internal/catalog"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
)

const (
	home     = "/Users/dev"
	systemDB = "/Library/Application Support/com.apple.TCC/TCC.db"
	link26   = "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?"
)

// userRows and systemRows are sqlite3 -json output for this module's query:
// a granted Accessibility for Raycast, Automation from iTerm to System
// Events, and an internal service System Settings doesn't show.
const userRows = `[{"service":"kTCCServiceAppleEvents","client":"com.googlecode.iterm2","client_type":0,"target":"com.apple.systemevents"},
{"service":"kTCCServiceUbiquity","client":"com.apple.notes","client_type":0,"target":"UNUSED"}]`

const systemRows = `[{"service":"kTCCServiceAccessibility","client":"com.raycast.macos","client_type":0,"target":"UNUSED"},
{"service":"kTCCServiceSystemPolicyAllFiles","client":"/usr/local/bin/backup-tool","client_type":1,"target":"UNUSED"}]`

var denied = runner.Response{Code: 1, Stderr: `Error: unable to open database "` + systemDB + `": authorization denied`}

var missing = runner.Response{Code: 1, Stderr: `Error: unable to open database "/Users/dev/Library/Application Support/com.apple.TCC/TCC.db": unable to open database file`}

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(fstest.MapFS{
		"raycast.toml": {Data: []byte("id = \"raycast\"\nname = \"Raycast\"\nbundle_id = \"com.raycast.macos\"\n\n[[permissions]]\nservice = \"Accessibility\"\n")},
		"zoom.toml":    {Data: []byte("id = \"zoom\"\nname = \"Zoom\"\nbundle_id = \"us.zoom.xos\"\n\n[[permissions]]\nservice = \"Camera\"\n\n[[permissions]]\nservice = \"Microphone\"\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type fixture struct {
	mod  Module
	fake *runner.Fake
}

func newFixture(t *testing.T) fixture {
	f := fixture{mod: Module{Catalog: testCatalog(t), SystemDB: systemDB}, fake: &runner.Fake{Responses: map[string]runner.Response{
		"/usr/bin/sw_vers -productVersion": {Stdout: "26.7.1\n"},
	}}}
	f.answer(runner.Response{Stdout: userRows}, runner.Response{Stdout: systemRows})
	return f
}

func sqlite(db string) string {
	return "/usr/bin/sqlite3 -init /dev/null -readonly -json " + db + " " + query
}

func (f fixture) answer(user, system runner.Response) {
	f.fake.Responses[sqlite(userDB(home))] = user
	f.fake.Responses[sqlite(systemDB)] = system
}

func (f fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: home, Manifest: m}
}

// record is a manifest holding this module's inventory and the system
// module's apps.
func record(t *testing.T, inv Inventory, apps ...recordedApp) *manifest.Manifest {
	t.Helper()
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), inv); err != nil {
		t.Fatal(err)
	}
	if err := m.SetModule("system", time.Now(), map[string]any{"apps": apps}); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestScanCountsGrantsWithFullDiskAccess(t *testing.T) {
	f := newFixture(t)
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil || len(findings) != 1 || findings[0].Title != "4 privacy permissions granted" {
		t.Errorf("Scan = %+v, %v", findings, err)
	}
}

func TestScanWithoutFullDiskAccessWarns(t *testing.T) {
	f := newFixture(t)
	f.answer(denied, denied)
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil || len(findings) != 1 || findings[0].Severity != module.SeverityWarn || !strings.Contains(findings[0].Fix, "Full Disk Access") {
		t.Errorf("Scan = %+v, %v", findings, err)
	}
}

func TestPrepareRecordsTheGrants(t *testing.T) {
	f := newFixture(t)
	res, err := f.mod.Prepare(f.env(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	if inv.Source != "tcc" || len(inv.Grants) != 4 {
		t.Fatalf("inventory = %+v", inv)
	}
	if g := inv.Grants[0]; g.Service != automation || g.Target != "com.apple.systemevents" {
		t.Errorf("automation grant = %+v", g)
	}
	if g := inv.Grants[1]; g.Target != "" {
		t.Errorf("a non-Automation grant kept target %q", g.Target)
	}
	if g := inv.Grants[3]; !g.Path || g.Client != "/usr/local/bin/backup-tool" {
		t.Errorf("path grant = %+v", g)
	}
}

func TestPrepareWithoutFullDiskAccessFallsBackToTheCatalog(t *testing.T) {
	f := newFixture(t)
	f.answer(runner.Response{Stdout: userRows}, denied)
	res, err := f.mod.Prepare(f.env(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if inv := res.Inventory.(Inventory); inv.Source != "catalog" || len(inv.Grants) != 0 {
		t.Errorf("inventory = %+v", inv)
	}
	if len(res.Problems) != 1 || res.Problems[0].ID != "full-disk-access" {
		t.Errorf("problems = %+v", res.Problems)
	}
}

func TestPrepareFailsOnAnyOtherSqliteError(t *testing.T) {
	f := newFixture(t)
	f.answer(runner.Response{Code: 1, Stderr: "Error: database disk image is malformed"}, runner.Response{Stdout: systemRows})
	if _, err := f.mod.Prepare(f.env(nil), nil); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Errorf("err = %v", err)
	}
}

func TestParseGrantsReadsNoOutputAsNoRows(t *testing.T) {
	if g, err := parseGrants([]byte("\n")); err != nil || len(g) != 0 {
		t.Errorf("parseGrants = %+v, %v", g, err)
	}
}

func finishSteps(t *testing.T, f fixture, m *manifest.Manifest) []checklist.Item {
	t.Helper()
	res, err := f.mod.Finish(f.env(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.Checklist
}

func TestFinishListsAStepPerGrantSystemSettingsShows(t *testing.T) {
	f := newFixture(t)
	res, _ := f.mod.Prepare(f.env(nil), nil)
	m := record(t, res.Inventory.(Inventory), recordedApp{Path: "/Applications/iTerm.app", BundleID: "com.googlecode.iterm2"})
	items := finishSteps(t, f, m)
	want := []checklist.Item{
		{ID: "permissions:com.googlecode.iterm2:Automation:com.apple.systemevents", Module: Name, Title: "Let iTerm control com.apple.systemevents (Automation)",
			Where: "System Settings > Privacy & Security > Automation", Link: link26 + "Privacy_Automation"},
		{ID: "permissions:com.raycast.macos:Accessibility", Module: Name, Title: "Give Raycast Accessibility access",
			Where: "System Settings > Privacy & Security > Accessibility", Link: link26 + "Privacy_Accessibility"},
		{ID: "permissions:/usr/local/bin/backup-tool:Full Disk Access", Module: Name, Title: "Give backup-tool Full Disk Access",
			Where: "System Settings > Privacy & Security > Full Disk Access", Link: link26 + "Privacy_AllFiles"},
	}
	if !slices.Equal(items, want) {
		t.Errorf("steps =\n%+v\nwant\n%+v", items, want)
	}
}

func TestFinishFromTheCatalogCoversTheRecordedApps(t *testing.T) {
	f := newFixture(t)
	m := record(t, Inventory{Source: "catalog"},
		recordedApp{Path: "/Applications/zoom.us.app", BundleID: "us.zoom.xos"},
		recordedApp{Path: "/Applications/Notes.app", BundleID: "com.example.notes"})
	var titles []string
	for _, it := range finishSteps(t, f, m) {
		titles = append(titles, it.Title)
	}
	if want := []string{"Give Zoom Camera access", "Give Zoom Microphone access"}; !slices.Equal(titles, want) {
		t.Errorf("titles = %q, want %q", titles, want)
	}
}

func TestFinishOnAnUncheckedMacOSGivesNoLinks(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses["/usr/bin/sw_vers -productVersion"] = runner.Response{Stdout: "27.0\n"}
	m := record(t, Inventory{Source: "tcc", Grants: []Grant{{Service: "kTCCServiceAccessibility", Client: "com.raycast.macos"}}})
	items := finishSteps(t, f, m)
	if len(items) != 1 || items[0].Link != "" || items[0].Where == "" {
		t.Errorf("steps = %+v, want a place but no link", items)
	}
}

func TestVerifyResolvesGrantedStepsAndWarnsAboutTheRest(t *testing.T) {
	f := newFixture(t)
	m := record(t, Inventory{Source: "tcc", Grants: []Grant{
		{Service: "kTCCServiceAccessibility", Client: "com.raycast.macos"},
		{Service: "kTCCServiceScreenCapture", Client: "com.raycast.macos"},
	}})
	checks, err := f.mod.Verify(f.env(m), nil)
	if err != nil || len(checks) != 2 {
		t.Fatalf("Verify = %+v, %v", checks, err)
	}
	if c := checks[0]; c.Status != module.StatusPass || !slices.Equal(c.Resolves, []string{"permissions:com.raycast.macos:Accessibility"}) {
		t.Errorf("granted check = %+v", c)
	}
	if c := checks[1]; c.Status != module.StatusWarn || c.Critical || !slices.Equal(c.Paths, []string{"Give Raycast Screen Recording access"}) {
		t.Errorf("missing check = %+v", c)
	}
}

func TestVerifyWithoutFullDiskAccessLeavesTheStepsToTheUser(t *testing.T) {
	f := newFixture(t)
	f.answer(denied, denied)
	m := record(t, Inventory{Source: "tcc", Grants: []Grant{{Service: "kTCCServiceAccessibility", Client: "com.raycast.macos"}}})
	checks, err := f.mod.Verify(f.env(m), nil)
	if err != nil || len(checks) != 1 || checks[0].Status != module.StatusInfo || len(checks[0].Resolves) != 0 {
		t.Errorf("Verify = %+v, %v", checks, err)
	}
}

func TestVerifyWithNothingExpectedHasNoChecks(t *testing.T) {
	f := newFixture(t)
	checks, err := f.mod.Verify(f.env(record(t, Inventory{Source: "catalog"})), nil)
	if err != nil || len(checks) != 0 {
		t.Errorf("Verify = %+v, %v", checks, err)
	}
}

func TestPrepareTreatsAMissingUserDatabaseAsNoGrants(t *testing.T) {
	f := newFixture(t)
	f.answer(missing, runner.Response{Stdout: systemRows})
	res, err := f.mod.Prepare(f.env(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if inv := res.Inventory.(Inventory); inv.Source != "tcc" || len(inv.Grants) != 2 {
		t.Errorf("inventory = %+v", inv)
	}
}

func TestFinishFailsOnAnUnreadableSystemRecord(t *testing.T) {
	f := newFixture(t)
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), Inventory{Source: "catalog"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetModule("system", time.Now(), map[string]any{"apps": "nope"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mod.Finish(f.env(m), nil); err == nil {
		t.Error("Finish succeeded on a malformed system record")
	}
}

func TestFinishFromTheCatalogKeepsAbsoluteClientsAsPaths(t *testing.T) {
	f := newFixture(t)
	f.mod.Catalog = pathCatalog(t)
	m := record(t, Inventory{Source: "catalog"}, recordedApp{Path: "/Applications/Tool.app", BundleID: "com.example.tool"})
	items := finishSteps(t, f, m)
	if len(items) != 1 || items[0].Title != "Give tool Full Disk Access" || items[0].ID != "permissions:/usr/local/bin/tool:Full Disk Access" {
		t.Errorf("steps = %+v", items)
	}
}

func pathCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(fstest.MapFS{
		"tool.toml": {Data: []byte("id = \"tool\"\nname = \"Tool\"\nbundle_id = \"com.example.tool\"\n\n[[permissions]]\nservice = \"Full Disk Access\"\nclient = \"/usr/local/bin/tool\"\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
