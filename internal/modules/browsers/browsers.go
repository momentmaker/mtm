// Package browsers records the old Mac's browser profiles and extensions,
// and whether each Chromium browser's Safe Storage key is in the keychain:
// without that key, the saved passwords and cookies Migration Assistant
// copies can't be decrypted on the new Mac.
package browsers

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const Name = "browsers"

type family int

const (
	chromium family = iota
	firefox
)

// browser is one browser mtm knows: its data folder, relative to the home
// folder, and for Chromium browsers the keychain item holding the key
// that encrypts saved passwords and cookies.
type browser struct {
	id          string
	name        string
	family      family
	dir         string
	safeStorage string
}

var known = []browser{
	{"chrome", "Chrome", chromium, "Library/Application Support/Google/Chrome", "Chrome Safe Storage"},
	{"brave", "Brave", chromium, "Library/Application Support/BraveSoftware/Brave-Browser", "Brave Safe Storage"},
	{"arc", "Arc", chromium, "Library/Application Support/Arc/User Data", "Arc Safe Storage"},
	{"firefox", "Firefox", firefox, "Library/Application Support/Firefox", ""},
}

func byID(id string) (browser, bool) {
	for _, b := range known {
		if b.id == id {
			return b, true
		}
	}
	return browser{}, false
}

// What prepare could see of Safari's data, which needs Full Disk Access.
const (
	SafariReadable   = "readable"
	SafariUnreadable = "unreadable"
	SafariAbsent     = "absent"
)

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return Name }

// Inventory lists the browsers that had profiles. Safari is one of the
// Safari constants.
type Inventory struct {
	Browsers []Browser `json:"browsers,omitempty"`
	Safari   string    `json:"safari"`
}

// Browser is one browser's profiles. SafeStorage is set when its Safe
// Storage keychain item existed.
type Browser struct {
	ID          string    `json:"id"`
	Profiles    []Profile `json:"profiles"`
	SafeStorage bool      `json:"safe_storage,omitempty"`
}

// Profile is a profile's folder, relative to the browser's data folder (or
// absolute, for a Firefox profile kept elsewhere), the name the browser
// shows for it, and its extension ids.
type Profile struct {
	Dir        string   `json:"dir"`
	Name       string   `json:"name,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
}

func (p Profile) label() string {
	if p.Name == "" || p.Name == p.Dir {
		return p.Dir
	}
	return p.Name + " (" + p.Dir + ")"
}

func (b Browser) extensions() int {
	n := 0
	for _, p := range b.Profiles {
		n += len(p.Extensions)
	}
	return n
}

// profiles reads a browser's profiles, or none when it has never run.
func profiles(home string, b browser) ([]Profile, error) {
	root := filepath.Join(home, filepath.FromSlash(b.dir))
	if b.family == firefox {
		return firefoxProfiles(root)
	}
	return chromiumProfiles(root)
}

// look records the browsers that have profiles. A browser whose data mtm
// can't read becomes a warning rather than an error, so it can't keep the
// rest of the migration from going ahead.
func look(env module.Env) (Inventory, []module.Finding, error) {
	inv := Inventory{Safari: safari(env.Home)}
	var problems []module.Finding
	for _, b := range known {
		ps, err := profiles(env.Home, b)
		if err != nil {
			problems = append(problems, module.Finding{
				Module: Name, ID: b.id + ":unreadable", Severity: module.SeverityWarn, Handler: module.Manual,
				Title:  "mtm can't read " + b.name + "'s profiles, so verify won't check them",
				Detail: err.Error(),
				Fix:    "Open " + b.name + " on the new Mac and check your profiles are there before you erase this Mac.",
			})
			continue
		}
		if len(ps) == 0 {
			continue
		}
		rec := Browser{ID: b.id, Profiles: ps}
		if b.safeStorage != "" {
			if rec.SafeStorage, err = hasKeychainItem(env, b.safeStorage); err != nil {
				return Inventory{}, nil, err
			}
		}
		inv.Browsers = append(inv.Browsers, rec)
	}
	return inv, problems, nil
}

// hasKeychainItem asks the keychain whether an item exists. Without -w or
// -g, security prints only the item's attributes, never its secret (spec
// 6.2). It exits 44 when there is no such item.
func hasKeychainItem(env module.Env, service string) (bool, error) {
	_, err := env.Run.Run(env.Ctx, runner.Command{Name: "/usr/bin/security", Args: []string{"find-generic-password", "-s", service}})
	if code, ok := runner.ExitCode(err); ok && code == 44 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look for %q in the keychain: %w", service, err)
	}
	return true, nil
}

// safari says whether Safari's data folder could be read. macOS protects
// it, so without Full Disk Access it can't.
func safari(home string) string {
	_, err := os.ReadDir(filepath.Join(home, "Library", "Safari"))
	switch {
	case err == nil:
		return SafariReadable
	case errors.Is(err, fs.ErrNotExist):
		return SafariAbsent
	}
	return SafariUnreadable
}

func (Module) Scan(env module.Env) ([]module.Finding, error) {
	inv, findings, err := look(env)
	if err != nil {
		return nil, err
	}
	for _, rec := range inv.Browsers {
		b, _ := byID(rec.ID)
		detail := "Migration Assistant copies them. verify checks they arrived."
		if b.family == chromium {
			detail += " On the new Mac you'll sign in to Google again; mtm checklist lists it."
		}
		findings = append(findings, module.Finding{
			Module: Name, ID: rec.ID, Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
			Title:  b.name + ": " + words.Count(len(rec.Profiles), "profile", "profiles") + ", " + words.Count(rec.extensions(), "extension", "extensions"),
			Detail: detail,
		})
	}
	if inv.Safari == SafariUnreadable {
		findings = append(findings, module.Finding{
			Module: Name, ID: "safari", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
			Title:  "Safari: mtm can't read its data without Full Disk Access",
			Detail: "Migration Assistant copies it, and its bookmarks, tabs, and passwords also sync through iCloud.",
		})
	}
	return findings, nil
}

func (Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	inv, problems, err := look(env)
	if err != nil {
		return module.Result{}, err
	}
	n := 0
	for _, b := range inv.Browsers {
		n += len(b.Profiles)
	}
	return module.Result{
		Inventory: inv,
		Done:      []string{"recorded " + words.Count(len(inv.Browsers), "browser", "browsers") + " with " + words.Count(n, "profile", "profiles")},
		Problems:  problems,
	}, nil
}

// Finish has nothing to restore: Migration Assistant copies the profiles,
// and the login keychain with the Safe Storage keys.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}

// Steps asks the user to sign in to Google again in each Chromium browser,
// because Google binds its sessions to the old Mac's hardware, and to check
// that other sites still know them.
func (Module) Steps(env module.Env) ([]checklist.Item, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	var items []checklist.Item
	for _, rec := range inv.Browsers {
		b, ok := byID(rec.ID)
		if !ok || len(rec.Profiles) == 0 {
			continue
		}
		if b.family == chromium {
			var names []string
			for _, p := range rec.Profiles {
				names = append(names, p.label())
			}
			items = append(items, checklist.Item{
				ID: Name + ":google:" + b.id, Module: Name, Where: b.name,
				Title:  "Sign in to Google again in " + b.name,
				Detail: "Google ties its sign-in to the Mac it happened on, so Gmail, YouTube, and Docs ask again. Do it in each profile that uses Google: " + strings.Join(names, ", ") + ".",
			})
		}
		items = append(items, checklist.Item{
			ID: Name + ":sites:" + b.id, Module: Name, Where: b.name,
			Title:  "Check you're still signed in to the sites you use most in " + b.name,
			Detail: "Your cookies came across with Migration Assistant, so most sites should still know you.",
		})
	}
	return items, nil
}

func (Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	var checks []module.Check
	for _, rec := range inv.Browsers {
		b, ok := byID(rec.ID)
		if !ok {
			checks = append(checks, module.Check{Module: Name, ID: Name + ":" + rec.ID, Status: module.StatusFail, Critical: true,
				Title: rec.ID, Detail: "recorded by a newer mtm; upgrade mtm on this Mac and run mtm verify again"})
			continue
		}
		cs, err := verifyBrowser(env, b, rec)
		if err != nil {
			return nil, err
		}
		checks = append(checks, cs...)
	}
	if c, ok := safariCheck(inv.Safari); ok {
		checks = append(checks, c)
	}
	return checks, nil
}

// verifyBrowser fails a missing profile or Safe Storage key: either loses
// data that only the old Mac still has. A missing extension only warns,
// since the extension store has it.
func verifyBrowser(env module.Env, b browser, rec Browser) ([]module.Check, error) {
	id := Name + ":" + b.id
	now, err := profiles(env.Home, b)
	if err != nil {
		return []module.Check{{Module: Name, ID: id + ":profiles", Status: module.StatusFail, Critical: true,
			Title: b.name + " profiles", Detail: "can't read them: " + err.Error()}}, nil
	}
	present := map[string]Profile{}
	for _, p := range now {
		present[p.Dir] = p
	}
	var missing, missingExt []string
	exts := 0
	for _, p := range rec.Profiles {
		got, ok := present[p.Dir]
		if !ok {
			missing = append(missing, p.label())
			continue
		}
		for _, e := range p.Extensions {
			exts++
			if !slices.Contains(got.Extensions, e) {
				missingExt = append(missingExt, p.label()+": "+e)
			}
		}
	}
	profilesCheck := module.Check{Module: Name, ID: id + ":profiles", Status: module.StatusPass, Critical: true,
		Title: b.name + " profiles", Detail: words.Count(len(rec.Profiles), "profile", "profiles") + " arrived"}
	if len(missing) > 0 {
		profilesCheck.Status = module.StatusFail
		profilesCheck.Detail = words.Count(len(missing), "profile is", "profiles are") + " missing; copy them from the old Mac's ~/" + b.dir
		profilesCheck.Paths = missing
	}
	checks := []module.Check{profilesCheck}
	if exts > 0 {
		c := module.Check{Module: Name, ID: id + ":extensions", Status: module.StatusPass,
			Title: b.name + " extensions", Detail: words.Count(exts, "extension", "extensions") + " arrived"}
		if len(missingExt) > 0 {
			c.Status = module.StatusWarn
			c.Detail = words.Count(len(missingExt), "extension is", "extensions are") + " missing; add them again from the extension store"
			c.Paths = missingExt
		}
		checks = append(checks, c)
	}
	if rec.SafeStorage {
		c, err := safeStorageCheck(env, b)
		if err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	return checks, nil
}

func safeStorageCheck(env module.Env, b browser) (module.Check, error) {
	c := module.Check{Module: Name, ID: Name + ":" + b.id + ":safe-storage", Status: module.StatusPass, Critical: true,
		Title: b.name + " Safe Storage key", Detail: "in the keychain"}
	found, err := hasKeychainItem(env, b.safeStorage)
	if err != nil {
		return module.Check{}, err
	}
	if !found {
		c.Status = module.StatusFail
		c.Detail = "missing from the keychain, so " + b.name + " can't read the saved passwords and cookies from the old Mac. " +
			"Before you erase it, copy the password of its \"" + b.safeStorage + "\" item in Keychain Access to the same item here, or export " + b.name + "'s passwords there"
	}
	return c, nil
}

func safariCheck(seen string) (module.Check, bool) {
	c := module.Check{Module: Name, ID: Name + ":safari", Status: module.StatusInfo, Title: "Safari"}
	switch seen {
	case SafariReadable:
		c.Detail = "its data was copied with your files; files checks it"
	case SafariUnreadable:
		c.Detail = "mtm couldn't read it on the old Mac; open Safari and check your bookmarks, which also sync through iCloud"
	default:
		return module.Check{}, false
	}
	return c, true
}
