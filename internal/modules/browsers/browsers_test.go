package browsers

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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

const (
	chromeDir  = "Library/Application Support/Google/Chrome"
	firefoxDir = "Library/Application Support/Firefox"
	extA       = "aapbdbdomjkkjkaonfhkkikfgjllcleb"
	extB       = "nngceckbapebfimnlniiiahkandclblb"
)

// localState is Chrome's Local State with two profiles, one listed
// without a folder, and a key that tries to leave Chrome's folder. The
// account fields are there because real files have them; mtm must not
// record them.
const localState = `{"profile":{"info_cache":{
"Default":{"name":"Person 1","user_name":"dev@example.com","gaia_id":"123"},
"Profile 1":{"name":"Work"},
"Profile 9":{"name":"Deleted"},
"../escape":{"name":"Elsewhere"}}}}`

const profilesINI = `[Install4F96D1932A9F858E]
Default=Profiles/abcd.default-release

[Profile0]
Name=default-release
IsRelative=1
Path=Profiles/abcd.default-release

[Profile1]
Name=escape
IsRelative=1
Path=../../Documents

[General]
Version=2
`

const extensionsJSON = `{"addons":[
{"id":"uBlock0@raymondhill.net","type":"extension","location":"app-profile"},
{"id":"formautofill@mozilla.org","type":"extension","location":"app-builtin"},
{"id":"en-US@dictionaries","type":"dictionary","location":"app-profile"}]}`

// seedHome is an old Mac's home folder with Chrome and Firefox profiles,
// and the leftovers of an Arc that never ran. The folders the escaping
// profile entries point at exist, so only mtm's own check keeps them out.
func seedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	chrome := filepath.Join(home, chromeDir)
	testutil.WriteFile(t, filepath.Join(chrome, "Local State"), localState)
	testutil.WriteFile(t, filepath.Join(chrome, "Default", "Extensions", extA, "1.0", "manifest.json"), "{}")
	testutil.WriteFile(t, filepath.Join(chrome, "Default", "Extensions", "Temp", "x"), "")
	testutil.WriteFile(t, filepath.Join(chrome, "Profile 1", "Extensions", extB, "2.0", "manifest.json"), "{}")
	testutil.WriteFile(t, filepath.Join(home, "Library/Application Support/Google/escape/Extensions", extB, "manifest.json"), "{}")
	testutil.WriteFile(t, filepath.Join(home, "Library/Application Support/Arc/User Data/Crashpad/settings.dat"), "")
	firefox := filepath.Join(home, firefoxDir)
	testutil.WriteFile(t, filepath.Join(firefox, "profiles.ini"), profilesINI)
	testutil.WriteFile(t, filepath.Join(firefox, "Profiles", "abcd.default-release", "extensions.json"), extensionsJSON)
	testutil.WriteFile(t, filepath.Join(home, "Library", "Documents", "extensions.json"), extensionsJSON)
	return home
}

const chromeKeychain = "/usr/bin/security find-generic-password -s Chrome Safe Storage"

// keychain answers the Safe Storage lookup: found, or exit 44, not found.
func keychain(found bool) *runner.Fake {
	resp := runner.Response{Stdout: `keychain: "/Users/dev/Library/Keychains/login.keychain-db"`}
	if !found {
		resp = runner.Response{Code: 44, Stderr: "security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain."}
	}
	return &runner.Fake{Responses: map[string]runner.Response{chromeKeychain: resp}}
}

func env(home string, r runner.Runner, m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: r, Home: home, Manifest: m}
}

var wantInventory = Inventory{
	Browsers: []Browser{
		{ID: "chrome", SafeStorage: true, Profiles: []Profile{
			{Dir: "Default", Name: "Person 1", Extensions: []string{extA}},
			{Dir: "Profile 1", Name: "Work", Extensions: []string{extB}},
		}},
		{ID: "firefox", Profiles: []Profile{
			{Dir: "Profiles/abcd.default-release", Name: "default-release", Extensions: []string{"uBlock0@raymondhill.net"}},
		}},
	},
	Safari: SafariAbsent,
}

func prepared(t *testing.T, home string) (Inventory, *manifest.Manifest) {
	t.Helper()
	res, err := New().Prepare(env(home, keychain(true), nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), res.Inventory); err != nil {
		t.Fatal(err)
	}
	return res.Inventory.(Inventory), m
}

func TestPrepareRecordsProfilesExtensionsAndSafeStorage(t *testing.T) {
	inv, _ := prepared(t, seedHome(t))
	if !reflect.DeepEqual(inv, wantInventory) {
		t.Errorf("inventory = %+v\nwant        %+v", inv, wantInventory)
	}
}

func TestPrepareAsksTheKeychainOnlyAboutBrowsersWithProfiles(t *testing.T) {
	fake := keychain(true)
	if _, err := New().Prepare(env(seedHome(t), fake, nil), nil); err != nil {
		t.Fatal(err)
	}
	if calls := fake.Calls(); len(calls) != 1 || calls[0].String() != chromeKeychain {
		t.Errorf("commands = %v, want only Chrome's Safe Storage lookup", calls)
	}
}

func TestPrepareWarnsAboutAProfileListItCannotRead(t *testing.T) {
	home := seedHome(t)
	testutil.WriteFile(t, filepath.Join(home, chromeDir, "Local State"), "{")
	res, err := New().Prepare(env(home, keychain(true), nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || res.Problems[0].ID != "chrome:unreadable" || res.Problems[0].Severity != module.SeverityWarn {
		t.Errorf("problems = %+v, want a warning for Chrome", res.Problems)
	}
	inv := res.Inventory.(Inventory)
	if len(inv.Browsers) != 1 || inv.Browsers[0].ID != "firefox" {
		t.Errorf("browsers = %+v, want Firefox still recorded", inv.Browsers)
	}
	if !slices.Equal(inv.Unreadable, []string{"chrome"}) {
		t.Errorf("unreadable = %q, want [chrome]", inv.Unreadable)
	}
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), inv); err != nil {
		t.Fatal(err)
	}
	items, err := New().Steps(env(t.TempDir(), keychain(true), m))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(items, func(it checklist.Item) bool { return it.ID == "browsers:check:chrome" }) {
		t.Errorf("steps = %+v, want browsers:check:chrome", items)
	}
	checks, err := New().Verify(env(home, keychain(true), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := checkByID(checks, "browsers:chrome:unreadable")
	if !ok || c.Status != module.StatusInfo {
		t.Errorf("unreadable check = %+v, want an info check", c)
	}
}

func writeAbsoluteProfilesINI(t *testing.T, home, profile string) {
	t.Helper()
	testutil.WriteFile(t, filepath.Join(home, firefoxDir, "profiles.ini"),
		"[Profile0]\nName=work\nIsRelative=0\nPath="+profile+"\n")
}

func TestAnAbsoluteFirefoxProfileInsideTheFirefoxFolderIsRecordedRelative(t *testing.T) {
	oldHome := t.TempDir()
	profile := filepath.Join(oldHome, firefoxDir, "Profiles", "abs.work")
	testutil.WriteFile(t, filepath.Join(profile, "extensions.json"), extensionsJSON)
	writeAbsoluteProfilesINI(t, oldHome, profile)
	inv, m := prepared(t, oldHome)
	if len(inv.Browsers) != 1 || len(inv.Browsers[0].Profiles) != 1 {
		t.Fatalf("browsers = %+v, want one Firefox profile", inv.Browsers)
	}
	p := inv.Browsers[0].Profiles[0]
	if p.Dir != "Profiles/abs.work" || !slices.Equal(p.Extensions, []string{"uBlock0@raymondhill.net"}) {
		t.Errorf("profile = %+v, want a relative Dir and its extension", p)
	}

	newHome := filepath.Join(t.TempDir(), "other-name")
	testutil.Copy(t, oldHome, newHome)
	writeAbsoluteProfilesINI(t, newHome, filepath.Join(newHome, firefoxDir, "Profiles", "abs.work"))
	checks, err := New().Verify(env(newHome, keychain(true), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := checkByID(checks, "browsers:firefox:profiles")
	if c.Status != module.StatusPass {
		t.Errorf("profiles check = %+v, want pass on the new home", c)
	}
}

func TestAnAbsoluteFirefoxProfileOutsideTheFirefoxFolderIsIgnored(t *testing.T) {
	elsewhere := t.TempDir()
	testutil.WriteFile(t, filepath.Join(elsewhere, "abs.work", "extensions.json"), extensionsJSON)
	home := t.TempDir()
	writeAbsoluteProfilesINI(t, home, filepath.Join(elsewhere, "abs.work"))
	inv, _ := prepared(t, home)
	if len(inv.Browsers) != 0 {
		t.Errorf("browsers = %+v, want the outside profile ignored", inv.Browsers)
	}
}

func TestPrepareFailsWhenTheKeychainCannotBeAsked(t *testing.T) {
	fake := &runner.Fake{Responses: map[string]runner.Response{chromeKeychain: {Code: 51, Stderr: "security: interaction not allowed"}}}
	if _, err := New().Prepare(env(seedHome(t), fake, nil), nil); err == nil || !strings.Contains(err.Error(), "Chrome Safe Storage") {
		t.Errorf("err = %v, want the failed lookup", err)
	}
}

func TestSafariWithoutFullDiskAccess(t *testing.T) {
	home := t.TempDir()
	safariDir := filepath.Join(home, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Mode 000 stands in for macOS's privacy protection, which refuses
	// the same way without Full Disk Access.
	if err := os.Chmod(safariDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(safariDir, 0o755) })
	findings, err := New().Scan(env(home, keychain(true), nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].ID != "safari" || !strings.Contains(findings[0].Title, "Full Disk Access") {
		t.Errorf("findings = %+v, want Safari's note", findings)
	}
}

func TestScanSummarizesEachBrowser(t *testing.T) {
	findings, err := New().Scan(env(seedHome(t), keychain(true), nil))
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, f := range findings {
		titles = append(titles, f.Title)
	}
	if want := []string{"Chrome: 2 profiles, 2 extensions", "Firefox: 1 profile, 1 extension"}; !slices.Equal(titles, want) {
		t.Errorf("titles = %q, want %q", titles, want)
	}
}

func checkByID(checks []module.Check, id string) (module.Check, bool) {
	i := slices.IndexFunc(checks, func(c module.Check) bool { return c.ID == id })
	if i < 0 {
		return module.Check{}, false
	}
	return checks[i], true
}

func TestVerifyPassesWhenEverythingArrived(t *testing.T) {
	old := seedHome(t)
	_, m := prepared(t, old)
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, old, newHome)
	checks, err := New().Verify(env(newHome, keychain(true), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range checks {
		if c.Status != module.StatusPass {
			t.Errorf("%s = %s (%s), want pass", c.ID, c.Status, c.Detail)
		}
		ids = append(ids, c.ID)
	}
	want := []string{"browsers:chrome:profiles", "browsers:chrome:extensions", "browsers:chrome:safe-storage", "browsers:firefox:profiles", "browsers:firefox:extensions"}
	if !slices.Equal(ids, want) {
		t.Errorf("checks = %q, want %q", ids, want)
	}
}

func TestVerifyFailsAMissingProfile(t *testing.T) {
	old := seedHome(t)
	_, m := prepared(t, old)
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, old, newHome)
	if err := os.RemoveAll(filepath.Join(newHome, chromeDir, "Profile 1")); err != nil {
		t.Fatal(err)
	}
	checks, err := New().Verify(env(newHome, keychain(true), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := checkByID(checks, "browsers:chrome:profiles")
	if c.Status != module.StatusFail || !c.Critical || !slices.Equal(c.Paths, []string{"Work (Profile 1)"}) {
		t.Errorf("profiles check = %+v, want a critical fail naming Work", c)
	}
}

func TestVerifyWarnsAboutAMissingExtension(t *testing.T) {
	old := seedHome(t)
	_, m := prepared(t, old)
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, old, newHome)
	if err := os.RemoveAll(filepath.Join(newHome, chromeDir, "Default", "Extensions", extA)); err != nil {
		t.Fatal(err)
	}
	checks, err := New().Verify(env(newHome, keychain(true), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := checkByID(checks, "browsers:chrome:extensions")
	if c.Status != module.StatusWarn || c.Critical || !slices.Equal(c.Paths, []string{"Person 1 (Default): " + extA}) {
		t.Errorf("extensions check = %+v, want a warning naming the extension", c)
	}
}

func TestVerifyFailsWhenTheSafeStorageKeyIsMissing(t *testing.T) {
	old := seedHome(t)
	_, m := prepared(t, old)
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, old, newHome)
	checks, err := New().Verify(env(newHome, keychain(false), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := checkByID(checks, "browsers:chrome:safe-storage")
	if c.Status != module.StatusFail || !c.Critical || !strings.Contains(c.Detail, "Keychain Access") || !strings.Contains(c.Detail, "Don't open Chrome") {
		t.Errorf("Safe Storage check = %+v, want a critical fail with the fix", c)
	}
}

func TestStepsAskForGoogleInChromiumAndSitesInEveryBrowser(t *testing.T) {
	_, m := prepared(t, seedHome(t))
	items, err := New().Steps(env(t.TempDir(), keychain(true), m))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if want := []string{"browsers:google:chrome", "browsers:sites:chrome", "browsers:sites:firefox"}; !slices.Equal(ids, want) {
		t.Errorf("steps = %q, want %q", ids, want)
	}
	if !strings.Contains(items[0].Detail, "Person 1 (Default), Work (Profile 1)") {
		t.Errorf("Google step detail = %q, want the profile names", items[0].Detail)
	}
}

func TestVerifyNotesSafari(t *testing.T) {
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), Inventory{Safari: SafariUnreadable}); err != nil {
		t.Fatal(err)
	}
	checks, err := New().Verify(env(t.TempDir(), keychain(true), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].ID != "browsers:safari" || checks[0].Status != module.StatusInfo {
		t.Errorf("checks = %+v, want Safari's info", checks)
	}
}
