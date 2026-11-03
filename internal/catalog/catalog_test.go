package catalog

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// TestEveryCatalogFileIsValid is the check CI runs on contributed files.
func TestEveryCatalogFileIsValid(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Apps) < 20 {
		t.Errorf("catalog has %d apps, want the starter set", len(c.Apps))
	}
}

// TestReadmeExampleIsAValidFile keeps the contributor guide's example a
// file the catalog accepts, so copying it can't start a contributor off
// with a rejected file.
func TestReadmeExampleIsAValidFile(t *testing.T) {
	data, err := os.ReadFile("../../catalog/README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(data), "```toml\n")
	if !ok {
		t.Fatal("catalog/README.md has no toml example")
	}
	example, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("catalog/README.md's toml example isn't closed")
	}
	if _, err := Parse("raycast.toml", []byte(example)); err != nil {
		t.Errorf("README example: %v", err)
	}
}

// raycastHead is a file's top-level keys; keys after a [table] belong to
// that table, so tests add top-level keys here.
const raycastHead = `id = "raycast"
name = "Raycast"
bundle_id = "com.raycast.macos"
`

const raycast = raycastHead + `
[[permissions]]
service = "Accessibility"
`

func TestParseReadsEveryField(t *testing.T) {
	data := raycastHead + `notes = ["Sign in."]

[[permissions]]
service = "Accessibility"

[[permissions]]
service = "Input Monitoring"
client = "com.raycast.helper"

[license]
reactivate = true
note = "Enter the key."
deactivate = true
deactivate_note = "Deactivate it in Settings."

[state]
volatile = ["~/Library/Application Support/Raycast/logs/"]
`
	a, err := Parse("raycast.toml", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Raycast" || a.BundleID != "com.raycast.macos" || len(a.Permissions) != 2 || !a.License.Reactivate || a.License.Note != "Enter the key." {
		t.Errorf("app = %+v", a)
	}
	if a.Permissions[0].ClientFor(a) != "com.raycast.macos" || a.Permissions[1].ClientFor(a) != "com.raycast.helper" {
		t.Errorf("clients = %q, %q", a.Permissions[0].ClientFor(a), a.Permissions[1].ClientFor(a))
	}
	if len(a.Notes) != 1 || len(a.State.Volatile) != 1 {
		t.Errorf("notes %q, volatile %q", a.Notes, a.State.Volatile)
	}
	if !a.License.Deactivate || a.License.DeactivateNote != "Deactivate it in Settings." {
		t.Errorf("license = %+v", a.License)
	}
}

func TestParseReadsAPasswordManager(t *testing.T) {
	data := `id = "1password"
name = "1Password"
bundle_id = "com.1password.1password"
kind = "password-manager"
ssh_agent = ["~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"]
`
	a, err := Parse("1password.toml", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != KindPasswordManager || len(a.SSHAgent) != 1 {
		t.Errorf("app = %+v", a)
	}
}

func TestParseRejectsMistakes(t *testing.T) {
	tests := []struct {
		name, file, data, want string
	}{
		{"unknown key", "raycast.toml", raycastHead + "homepage = \"x\"\n", "unknown key homepage"},
		{"id unlike file", "ray.toml", raycast, "must match the file name"},
		{"bad id", "Raycast.toml", strings.Replace(raycast, `"raycast"`, `"Raycast"`, 1), "lowercase"},
		{"no name", "raycast.toml", strings.Replace(raycast, `"Raycast"`, `""`, 1), "name is missing"},
		{"bad bundle id", "raycast.toml", strings.Replace(raycast, "com.raycast.macos", "raycast", 1), "reverse-DNS"},
		{"unknown service", "raycast.toml", strings.Replace(raycast, "Accessibility", "Telepathy", 1), "not one mtm knows"},
		{"bad client", "raycast.toml", raycast + "client = \"helper\"\n", "neither a bundle id"},
		{"empty note", "raycast.toml", raycastHead + "notes = [\"\"]\n", "note is empty"},
		{"volatile outside home", "raycast.toml", raycast + "\n[state]\nvolatile = [\"/tmp/x\"]\n", "must start with ~/"},
		{"broken pattern", "raycast.toml", raycast + "\n[state]\nvolatile = [\"~/[x\"]\n", "volatile path"},
		{"not TOML", "raycast.toml", "id = ", "catalog file raycast.toml"},
		{"unknown kind", "raycast.toml", raycastHead + "kind = \"launcher\"\n", "kind \"launcher\""},
		{"agent outside home", "raycast.toml", raycastHead + "ssh_agent = [\"/tmp/agent.sock\"]\n", "must start with ~/"},
		{"note for no deactivation", "raycast.toml", raycast + "\n[license]\ndeactivate_note = \"x\"\n", "needs license.deactivate"},
	}
	for _, tt := range tests {
		_, err := Parse(tt.file, []byte(tt.data))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestLoadRejectsADuplicateBundleID(t *testing.T) {
	fsys := fstest.MapFS{
		"raycast.toml":  {Data: []byte(raycast)},
		"raycast2.toml": {Data: []byte(strings.Replace(raycast, `"raycast"`, `"raycast2"`, 1))},
	}
	if _, err := Load(fsys); err == nil || !strings.Contains(err.Error(), "already in another file") {
		t.Errorf("err = %v, want a duplicate bundle_id", err)
	}
}

func TestByBundleIDAndVolatile(t *testing.T) {
	fsys := fstest.MapFS{"raycast.toml": {Data: []byte(raycast + "\n[state]\nvolatile = [\"~/a/\"]\n")}}
	c, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := c.ByBundleID("com.raycast.macos"); !ok || a.ID != "raycast" {
		t.Errorf("ByBundleID = %+v, %v", a, ok)
	}
	if _, ok := c.ByBundleID("com.example.none"); ok {
		t.Error("found an app that isn't in the catalog")
	}
	if v := c.Volatile(); len(v) != 1 || v[0] != "~/a/" {
		t.Errorf("Volatile = %q", v)
	}
}

func TestServiceLinksAndLookups(t *testing.T) {
	s, ok := ServiceNamed("Accessibility")
	if !ok || s.TCC != "kTCCServiceAccessibility" {
		t.Fatalf("ServiceNamed = %+v, %v", s, ok)
	}
	if got := s.Link("26"); got != "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?Privacy_Accessibility" {
		t.Errorf("Link(26) = %q", got)
	}
	if got := s.Link("99"); got != "" {
		t.Errorf("Link(99) = %q, want none for an unchecked macOS", got)
	}
	if s.Where() != "System Settings > Privacy & Security > Accessibility" {
		t.Errorf("Where = %q", s.Where())
	}
	if f, ok := ServiceForTCC("kTCCServiceScreenCapture"); !ok || f.Name != "Screen Recording" {
		t.Errorf("ServiceForTCC = %+v, %v", f, ok)
	}
	if _, ok := ServiceForTCC("kTCCServiceUbiquity"); ok {
		t.Error("an internal service mapped to a System Settings pane")
	}
	if LoginItemsLink("26") == "" || LoginItemsLink("99") != "" {
		t.Error("LoginItemsLink")
	}
}
