package loginitems

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/testutil"
)

const uid = 501

// launchctlList is `launchctl list` output: com.a is loaded, com.b is not.
const launchctlList = "PID\tStatus\tLabel\n-\t0\tcom.a\n123\t0\tcom.other\n"

type fixture struct {
	home string
	fake *runner.Fake
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{home: filepath.Join(t.TempDir(), "my home"), fake: &runner.Fake{Responses: map[string]runner.Response{
		"/bin/launchctl list":              {Stdout: launchctlList},
		"/usr/bin/sw_vers -productVersion": {Stdout: "26.7.1\n"},
	}}}
	dir := filepath.Join(f.home, "Library", "LaunchAgents")
	for file, label := range map[string]string{"a.plist": "com.a", "b.plist": "com.b"} {
		testutil.WriteFile(t, filepath.Join(dir, file), "<plist/>")
		f.fake.Responses["/usr/bin/plutil -extract Label raw -o - "+filepath.Join(dir, file)] = runner.Response{Stdout: label + "\n"}
	}
	testutil.WriteFile(t, filepath.Join(dir, "c.plist.disabled"), "<plist/>")
	testutil.WriteFile(t, filepath.Join(dir, "notes.txt"), "x")
	return f
}

func (f fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func record(t *testing.T, inv Inventory) *manifest.Manifest {
	t.Helper()
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), inv); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPrepareRecordsAgentsAndLoginItems(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses["/usr/bin/osascript -e "+loginItemsScript] = runner.Response{Stdout: "Raycast, 1Password\n"}
	res, err := New(uid).Prepare(f.env(manifest.New("test")), nil)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	want := []Agent{{File: "a.plist", Label: "com.a", Loaded: true}, {File: "b.plist", Label: "com.b", Loaded: false}}
	if !slices.Equal(inv.Agents, want) || !inv.LoginItemsListed || !slices.Equal(inv.LoginItems, []string{"Raycast", "1Password"}) {
		t.Errorf("inventory = %+v", inv)
	}
}

func TestPrepareLoginItemsDenied(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses["/usr/bin/osascript -e "+loginItemsScript] = runner.Response{Code: 1, Stderr: "Not authorized to send Apple events to System Events. (-1743)"}
	res, err := New(uid).Prepare(f.env(manifest.New("test")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Inventory.(Inventory).LoginItemsListed || len(res.Problems) != 1 || module.HasBlocker(res.Problems) {
		t.Errorf("Prepare = %+v, want one non-blocking warning", res)
	}
}

func TestScanDoesNotAskSystemEvents(t *testing.T) {
	f := newFixture(t)
	findings, err := New(uid).Scan(f.env(nil))
	if err != nil || len(findings) != 2 || !strings.Contains(findings[0].Title, "2 launch agents") {
		t.Errorf("Scan = %+v, %v", findings, err)
	}
	for _, c := range f.fake.Calls() {
		if c.Name == "/usr/bin/osascript" {
			t.Error("scan asked System Events")
		}
	}
}

func TestFinishLoadsAgentsAndListsLoginItems(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses["/bin/launchctl list"] = runner.Response{Stdout: "PID\tStatus\tLabel\n"}
	plist := filepath.Join(f.home, "Library", "LaunchAgents", "a.plist")
	f.fake.Responses["/bin/launchctl bootstrap gui/501 "+plist] = runner.Response{}
	m := record(t, Inventory{Agents: []Agent{{File: "a.plist", Label: "com.a", Loaded: true}, {File: "b.plist", Label: "com.b"}}, LoginItems: []string{"Raycast"}, LoginItemsListed: true})
	res, err := New(uid).Finish(f.env(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Done, []string{"loaded com.a"}) {
		t.Errorf("done = %q", res.Done)
	}
	if len(res.Problems) != 0 {
		t.Errorf("problems = %+v, want login items as checklist steps instead", res.Problems)
	}
	want := []checklist.Item{{ID: "loginitems:Raycast", Module: Name, Title: "Add Raycast to Login Items again",
		Where: "System Settings > General > Login Items & Extensions", Link: "x-apple.systempreferences:com.apple.LoginItems-Settings.extension"}}
	if !slices.Equal(res.Checklist, want) {
		t.Errorf("checklist = %+v, want %+v", res.Checklist, want)
	}
}

func TestFinishBootstrapFailureIsByHand(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses["/bin/launchctl list"] = runner.Response{Stdout: "PID\tStatus\tLabel\n"}
	plist := filepath.Join(f.home, "Library", "LaunchAgents", "a.plist")
	f.fake.Responses["/bin/launchctl bootstrap gui/501 "+plist] = runner.Response{Code: 5, Stderr: "Bootstrap failed: 5: Input/output error"}
	m := record(t, Inventory{Agents: []Agent{{File: "a.plist", Label: "com.a", Loaded: true}}, LoginItemsListed: true})
	res, err := New(uid).Finish(f.env(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Fix, "launchctl bootstrap gui/501 '"+plist+"'") {
		t.Errorf("problems = %+v", res.Problems)
	}
}

func TestFinishWhenLoginItemsWereNotListed(t *testing.T) {
	f := newFixture(t)
	res, err := New(uid).Finish(f.env(record(t, Inventory{})), nil)
	if err != nil || len(res.Checklist) != 1 || res.Checklist[0].ID != "loginitems:from-old-mac" {
		t.Errorf("Finish = %+v, %v; want one step to note the login items on the old Mac", res, err)
	}
}

func TestVerifyAgents(t *testing.T) {
	f := newFixture(t)
	m := record(t, Inventory{Agents: []Agent{{File: "a.plist", Label: "com.a", Loaded: true}, {File: "b.plist", Label: "com.b", Loaded: true}}, LoginItems: []string{"Raycast"}, LoginItemsListed: true})
	checks, err := New(uid).Verify(f.env(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 || checks[0].Status != module.StatusWarn || !slices.Equal(checks[0].Paths, []string{"com.b"}) {
		t.Errorf("agents check = %+v", checks)
	}
	if checks[1].ID != "loginitems:login-items" || checks[1].Status != module.StatusInfo {
		t.Errorf("login items check = %+v", checks[1])
	}
}
