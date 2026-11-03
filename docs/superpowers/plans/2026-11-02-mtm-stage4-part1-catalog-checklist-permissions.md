# mtm Stage 4, Part 1: Catalog, Checklist, and Permissions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Track the steps no tool can do for the user on the new Mac, and make SAFE TO WIPE wait for them. Those steps include privacy permissions, licenses, signing in again, and login items.
- The steps are shown with System Settings deep links.
- They are walked through with `mtm checklist`.
- Each must be done, or skipped with a reason.

**Architecture:**
- **The app catalog:** one TOML file per app in `catalog/`, built into the binary. `internal/catalog` reads and checks it, and holds the table of privacy panes with their deep links.
- **The checklist package:** `internal/checklist` holds the steps and what the user did with each, persisted in `~/.mtm/state.json`.
- **The module contract** gains two things:
  - `Result.Checklist`, the steps a module's `Finish` leaves to the user;
  - `Check.Resolves`, the steps a passing verify check proves done.
- **`finish` and `verify`:** `finish` merges the steps into the state, and `verify` resolves steps and counts the open ones in the verdict.
- **The `permissions` module** reads macOS's privacy databases, read-only, when it has Full Disk Access, and falls back to the catalog when it doesn't.
- **Other modules:**
  - `system` adds license and note steps from the catalog;
  - `loginitems` turns its login items into steps;
  - `files` reads the catalog's volatile paths.

**Tech Stack:**
- Go 1.26, plus the one dependency the spec allows: `github.com/BurntSushi/toml` v1.6.0.
- macOS tools: `/usr/bin/sqlite3` (read-only) and `/usr/bin/open` (System Settings links only).

**Spec:** `docs/superpowers/specs/2026-11-02-mtm-design.md`. Part 1 covers:
- sections 5 (the `mtm checklist` row) and 7.2–7.4 (packages, the module contract, the catalog);
- 8.1 (`state.json`) and 8.5 (the verdict's third rule);
- 9.7 (login items as checklist steps), 9.8 `permissions`, and 9.11 (the catalog's steps for apps);
- 12 (unverified deep links).

These sections were updated while planning. Stage 4 is split into two parts in `IMPLEMENTATION_PLAN.md`. Part 2 adds `browsers`, `secrets`, and `mtm retire`.

**How this plan was checked:**
- Every task's code was written and run in a scratch copy of the repository before the plan was written.
- Replayed in order on a clean copy, each task leaves `gofmt -l .`, `go vet ./...`, `go vet -tags integration ./...`, and `go test ./...` clean.
- `TestNoNetwork` passed with `permissions` included.
- The author's terminal has no Full Disk Access, so the code path that reads the privacy databases is tested only with fixtures of `sqlite3 -json` output.

**Diffs:** a modified file's change is a unified diff against the file as the previous task left it. Apply it with `git apply`, or by hand. A new file is given in full.

## Global Constraints

- **Module and dependencies:**
  - Module path `github.com/momentmaker/mtm`, `go 1.26`.
  - The only third-party dependency is `github.com/BurntSushi/toml` v1.6.0 (spec 6.6).
- **Network and secrets (spec 6.2):** `mtm` makes no network connections and never reads secret contents.
- **What reports and logs hold (spec 6.3):** never fingerprints, file contents, or environment values.
- **Privacy databases:**
  - Read only with `/usr/bin/sqlite3 -readonly -json <db> <query>`.
  - The query selects only granted rows (`auth_value = 2`), and only `service`, `client`, `client_type`, and the indirect object.
  - `mtm` never writes to a privacy database, and never reads its other columns.
- **Links:** `mtm` opens only links that start with `x-apple.systempreferences:`, with `/usr/bin/open`, and only from `mtm checklist` (the walk-through or `open <id>`).
- **Apple tools by absolute path:** this part adds `/usr/bin/sqlite3` and `/usr/bin/open`.
- **Checklist steps:**
  - A step's id is stable, `<module>:<key>`.
  - Skipping a step needs a non-empty reason.
  - Steps and their status live in `~/.mtm/state.json`, written through `stage` (0600, atomic).
- **The developer's Mac is never changed by tests:**
  - No test may stop, start, load, or open anything on the developer's Mac. Unit tests use `runner.Fake`.
  - No test runs `/usr/bin/open` for real.
  - Never run `mtm prepare`, `finish`, `verify`, `cleanup`, or `checklist` against the real home folder.
- **Code style:** modules never print; all output goes through `internal/report`. Comments explain why, never restate code. No commented-out code.
- **Exit codes:**
  - `verify`: `0` safe, `1` not safe, `2` mtm failed.
  - Other commands: `0` success, `1` completed with problems or refused, `2` mtm failed or a usage error.
  - In `mtm checklist`, an unknown step id or a missing reason is a usage error (`2`).
- **Before every commit:** `gofmt -l .` prints nothing, and `go vet ./...`, `go vet -tags integration ./...`, and `go test ./...` pass.
- **Commit messages** explain why and end with a `Co-Authored-By:` trailer naming the model that wrote the commit, as your harness instructs.

## Review Focus

These are the five conditions most likely to bite a real user. Each has a pinned test in the task that owns the code.

1. **Running `finish` again after the user closed steps.** Done and skipped steps stay closed, and their text is refreshed.
   - Task 2 `TestAddKeepsWhatTheUserDidAndRefreshesText`
   - Task 3 `TestFinishKeepsWhatTheUserDidWhenItRunsAgain`
2. **No Full Disk Access**, which is the author's own case. `scan` and `prepare` warn and fall back to the catalog. `verify` leaves the steps to the user and doesn't fail.
   - Task 5 `TestScanWithoutFullDiskAccessWarns`, `TestPrepareWithoutFullDiskAccessFallsBackToTheCatalog`, `TestVerifyWithoutFullDiskAccessLeavesTheStepsToTheUser`
3. **A `state.json` someone edited to point a step at another URL or app.** `mtm checklist` opens only System Settings links.
   - Task 4 `TestOpenStepRefusesALinkMtmDidntMake`
4. **A walk-through cut short: quit, later, or the end of input.** Every answer is saved as it is given, and the rest stay open.
   - Task 4 `TestChecklistWalkQuitsAndLaterLeavesStepsOpen`, `TestChecklistWalkEndsAtTheEndOfInput`
5. **A macOS version mtm hasn't checked links for.** Steps name the pane and give no link, rather than a wrong one.
   - Task 1 `TestServiceLinksAndLookups`
   - Task 5 `TestFinishOnAnUncheckedMacOSGivesNoLinks`

## File Structure

```
go.mod, go.sum                              (modify/create) require github.com/BurntSushi/toml v1.6.0
catalog/embed.go                            builds catalog/*.toml into the binary
catalog/*.toml                              24 starter app definitions
catalog/README.md                           the file format, how to add an app, credit to tape
internal/catalog/catalog.go                 App, Load, Default, Parse (validation), ByBundleID, Volatile
internal/catalog/services.go                privacy panes: name, privacy-database id, deep link per macOS major
internal/checklist/checklist.go             Item, Entry, List: Add (merge), Done, Skip, Pending, Find
internal/state/state.go                     (modify) State.Checklist
internal/module/module.go                   (modify) Result.Checklist, Check.Resolves
internal/app/app.go                         (modify) finish saves steps; verify resolves and counts them
internal/app/checklist.go                   Checklist, MarkDone, Skip, OpenStep
internal/report/report.go                   (modify) Finish takes the open-step count; Verify's hint
internal/report/checklist.go                Checklist, Step
cmd/mtm/checklist.go                        mtm checklist: walk, list, done, skip, open
cmd/mtm/main.go                             (modify) report.Finish call; checklist command and usage; catalog and module wiring
internal/modules/permissions/permissions.go privacy permissions: Scan, Prepare, Finish, Verify
internal/modules/system/system.go           (modify) Catalog; license and note steps
internal/modules/loginitems/loginitems.go   (modify) login items as checklist steps
internal/modules/files/module.go, verify.go (modify) the catalog's volatile paths
cmd/mtm/nonetwork_integration_test.go       (modify) also prepare permissions offline
```

---
### Task 1: The app catalog

**Files:**
- Modify: `go.mod`. Create: `go.sum` (both through `go get` and `go mod tidy`).
- Create: `catalog/embed.go`, `catalog/README.md`, and the 24 `catalog/*.toml` files.
- Create: `internal/catalog/catalog.go`, `internal/catalog/services.go`.
- Test: `internal/catalog/catalog_test.go`

**Interfaces:**
- Consumes: `pathset.New` (Stage 1).
- Produces:
  - `catalog.Files embed.FS`, in the root package `github.com/momentmaker/mtm/catalog`.
  - Types in `internal/catalog`:
    - `App{ID, Name, BundleID; Permissions []Permission; License License; Notes []string; State State}`;
    - `Permission{Service, Client}` with `ClientFor(App) string`;
    - `License{Reactivate bool; Note string}`;
    - `State{Volatile []string}`;
    - `Catalog{Apps []App}`.
  - Functions and methods:
    - `catalog.Default() (*Catalog, error)`, `catalog.Load(fs.FS)`, and `catalog.Parse(name string, data []byte) (App, error)`;
    - `(*Catalog).ByBundleID(id) (App, bool)` and `(*Catalog).Volatile() []string`.
  - The privacy panes:
    - `catalog.Service{Name, TCC}`, with `Where() string` and `Link(major string) string`;
    - `catalog.ServiceNamed`, `catalog.ServiceForTCC`, and `catalog.LoginItemsLink(major)`.
    - Links exist only for macOS major `"26"`.

- [ ] **Step 1: Add the TOML dependency**

Run: `go get github.com/BurntSushi/toml@v1.6.0`
Expected: `go: added github.com/BurntSushi/toml v1.6.0`. `go.mod` gains a `require` line, marked `// indirect` until Step 6's `go mod tidy`.

- [ ] **Step 2: Write the failing tests**

`internal/catalog/catalog_test.go`:
```go
package catalog

import (
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
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/catalog/`
Expected: the build fails with `undefined: Default`, `undefined: Parse`, and `undefined: ServiceNamed`.

- [ ] **Step 4: Implement the catalog package**

`internal/catalog/services.go`:
```go
package catalog

import "fmt"

// Service is a privacy permission as System Settings names it, with the
// id macOS's privacy database records it under.
type Service struct {
	Name   string
	TCC    string
	anchor string
}

var services = []Service{
	{"Accessibility", "kTCCServiceAccessibility", "Privacy_Accessibility"},
	{"Input Monitoring", "kTCCServiceListenEvent", "Privacy_ListenEvent"},
	{"Screen Recording", "kTCCServiceScreenCapture", "Privacy_ScreenCapture"},
	{"Full Disk Access", "kTCCServiceSystemPolicyAllFiles", "Privacy_AllFiles"},
	{"Automation", "kTCCServiceAppleEvents", "Privacy_Automation"},
	{"Camera", "kTCCServiceCamera", "Privacy_Camera"},
	{"Microphone", "kTCCServiceMicrophone", "Privacy_Microphone"},
	{"Developer Tools", "kTCCServiceDeveloperTool", "Privacy_DevTools"},
	{"Calendars", "kTCCServiceCalendar", "Privacy_Calendars"},
	{"Contacts", "kTCCServiceAddressBook", "Privacy_Contacts"},
	{"Photos", "kTCCServicePhotos", "Privacy_Photos"},
	{"Reminders", "kTCCServiceReminders", "Privacy_Reminders"},
	{"Bluetooth", "kTCCServiceBluetoothAlways", "Privacy_Bluetooth"},
}

// linkFormats holds, per macOS major, the System Settings URL a privacy
// pane's anchor completes. Each is checked by hand before a release (spec
// 12); a major without one gets no link, only the pane's name.
var linkFormats = map[string]string{
	"26": "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?%s",
}

var loginItemsLinks = map[string]string{
	"26": "x-apple.systempreferences:com.apple.LoginItems-Settings.extension",
}

func ServiceNamed(name string) (Service, bool) {
	for _, s := range services {
		if s.Name == name {
			return s, true
		}
	}
	return Service{}, false
}

func ServiceForTCC(id string) (Service, bool) {
	for _, s := range services {
		if s.TCC == id {
			return s, true
		}
	}
	return Service{}, false
}

func (s Service) Where() string { return "System Settings > Privacy & Security > " + s.Name }

// Link is the deep link to s's pane on macOS major, or "".
func (s Service) Link(major string) string {
	format, ok := linkFormats[major]
	if !ok {
		return ""
	}
	return fmt.Sprintf(format, s.anchor)
}

// LoginItemsLink is the deep link to Login Items on macOS major, or "".
func LoginItemsLink(major string) string { return loginItemsLinks[major] }
```

`internal/catalog/catalog.go`:
```go
// Package catalog reads mtm's app definitions: the privacy permissions an
// app needs, whether its license needs reactivating, notes for the
// checklist, and paths that come and go on their own.
package catalog

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	embedded "github.com/momentmaker/mtm/catalog"
	"github.com/momentmaker/mtm/internal/pathset"
)

type App struct {
	ID          string       `toml:"id"`
	Name        string       `toml:"name"`
	BundleID    string       `toml:"bundle_id"`
	Permissions []Permission `toml:"permissions"`
	License     License      `toml:"license"`
	Notes       []string     `toml:"notes"`
	State       State        `toml:"state"`
}

// Permission is a privacy permission the app needs. Client is who macOS
// records it for when that isn't the app itself, such as a helper's
// bundle id or path.
type Permission struct {
	Service string `toml:"service"`
	Client  string `toml:"client"`
}

func (p Permission) ClientFor(a App) string {
	if p.Client != "" {
		return p.Client
	}
	return a.BundleID
}

type License struct {
	Reactivate bool   `toml:"reactivate"`
	Note       string `toml:"note"`
}

// State holds volatile: home-folder paths, as pathset patterns, that the
// app creates and deletes on its own, so a missing one is no loss.
type State struct {
	Volatile []string `toml:"volatile"`
}

type Catalog struct {
	Apps []App
}

// Default is the catalog built into mtm.
func Default() (*Catalog, error) { return Load(embedded.Files) }

// Load reads every *.toml file at the top of fsys.
func Load(fsys fs.FS) (*Catalog, error) {
	names, err := fs.Glob(fsys, "*.toml")
	if err != nil {
		return nil, err
	}
	c := &Catalog{}
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read catalog file %s: %w", name, err)
		}
		app, err := Parse(name, data)
		if err != nil {
			return nil, err
		}
		if _, dup := c.ByBundleID(app.BundleID); dup {
			return nil, fmt.Errorf("catalog file %s: bundle_id %s is already in another file", name, app.BundleID)
		}
		c.Apps = append(c.Apps, app)
	}
	return c, nil
}

// Parse decodes one catalog file and checks it, so a mistake in a
// contributed file fails the tests instead of misleading a user.
func Parse(name string, data []byte) (App, error) {
	var a App
	md, err := toml.Decode(string(data), &a)
	if err != nil {
		return App{}, fmt.Errorf("catalog file %s: %w", name, err)
	}
	if extra := md.Undecoded(); len(extra) > 0 {
		return App{}, fmt.Errorf("catalog file %s: unknown key %s", name, extra[0])
	}
	if err := a.validate(strings.TrimSuffix(path.Base(name), ".toml")); err != nil {
		return App{}, fmt.Errorf("catalog file %s: %w", name, err)
	}
	return a, nil
}

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	bundlePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)+$`)
)

func (a App) validate(file string) error {
	switch {
	case a.ID != file:
		return fmt.Errorf("id %q must match the file name %q", a.ID, file)
	case !idPattern.MatchString(a.ID):
		return fmt.Errorf("id %q must be lowercase words joined by hyphens", a.ID)
	case strings.TrimSpace(a.Name) == "":
		return fmt.Errorf("name is missing")
	case !bundlePattern.MatchString(a.BundleID):
		return fmt.Errorf("bundle_id %q is not a reverse-DNS id", a.BundleID)
	}
	for _, p := range a.Permissions {
		if _, ok := ServiceNamed(p.Service); !ok {
			return fmt.Errorf("permission service %q is not one mtm knows (see internal/catalog/services.go)", p.Service)
		}
		if p.Client != "" && !bundlePattern.MatchString(p.Client) && !filepath.IsAbs(p.Client) {
			return fmt.Errorf("permission client %q is neither a bundle id nor an absolute path", p.Client)
		}
	}
	if slices.Contains(a.Notes, "") {
		return fmt.Errorf("a note is empty")
	}
	for _, v := range a.State.Volatile {
		if !strings.HasPrefix(v, "~/") {
			return fmt.Errorf("volatile path %q must start with ~/", v)
		}
	}
	if _, err := pathset.New(a.State.Volatile...); err != nil {
		return fmt.Errorf("volatile path: %w", err)
	}
	return nil
}

func (c *Catalog) ByBundleID(id string) (App, bool) {
	for _, a := range c.Apps {
		if a.BundleID == id {
			return a, true
		}
	}
	return App{}, false
}

// Volatile lists every app's volatile patterns.
func (c *Catalog) Volatile() []string {
	var out []string
	for _, a := range c.Apps {
		out = append(out, a.State.Volatile...)
	}
	return out
}
```

- [ ] **Step 5: Add the catalog files**

`catalog/embed.go`:
```go
// Package catalog holds mtm's app definitions, one TOML file per app,
// built into the binary. internal/catalog reads and checks them; the
// format is described in README.md here.
package catalog

import "embed"

//go:embed *.toml
var Files embed.FS
```

`catalog/README.md` (the copyright notice at its end is tape's own, copied verbatim as the BSD 2-Clause License requires):
````markdown
# mtm's app catalog

One TOML file per app, named after its `id`. mtm builds every file here into its binary. The tests in `internal/catalog` check each one, and a file they reject fails CI.

```toml
id = "raycast"                 # lowercase words joined by hyphens; must match the file name
name = "Raycast"               # as the app names itself
bundle_id = "com.raycast.macos"

[[permissions]]                # privacy permissions the app needs to work
service = "Accessibility"      # a name from internal/catalog/services.go
# client = "com.example.helper"  # only when macOS records the grant for a helper, not the app

[license]
reactivate = true              # the license must be entered again on a new Mac
note = "Enter your license again if it asks."

notes = ["Sign in to your account again if it asks."]   # extra checklist steps

[state]
volatile = ["~/Library/Application Support/Raycast/logs/"]   # paths the app deletes on its own
```

mtm turns these into checklist steps on the new Mac:
- one per permission, unless it can read the old Mac's privacy permissions directly;
- one for a license to reactivate;
- one per note.

Volatile paths that are missing after Migration Assistant are reported as info, not warnings.

## Adding an app

1. Find its bundle id: `plutil -extract CFBundleIdentifier raw /Applications/<App>.app/Contents/Info.plist`.
2. Add `<id>.toml` with only the fields you know to be true.
3. Run `go test ./internal/catalog/`.

## Credits

Several apps' names and bundle ids were first taken from the definitions of [tape](https://github.com/vitorgalvao/tape) by Vítor Galvão, under the BSD 2-Clause License:

```
Copyright (c) 2022, Vítor Galvão
Copyright (c) 2022, Vítor Galvão
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```
````

The 24 starter app files. Their bundle ids were read from the author's installed apps, or taken from tape's definitions.
`catalog/aerospace.toml`:
```toml
id = "aerospace"
name = "AeroSpace"
bundle_id = "bobko.aerospace"

[[permissions]]
service = "Accessibility"
```

`catalog/alfred.toml`:
```toml
id = "alfred"
name = "Alfred"
bundle_id = "com.runningwithcrayons.Alfred"

[[permissions]]
service = "Accessibility"
```

`catalog/bartender.toml`:
```toml
id = "bartender"
name = "Bartender"
bundle_id = "com.surteesstudios.Bartender"

[[permissions]]
service = "Accessibility"

[[permissions]]
service = "Screen Recording"

[license]
reactivate = true
note = "Enter your Bartender license again if it asks."
```

`catalog/bettertouchtool.toml`:
```toml
id = "bettertouchtool"
name = "BetterTouchTool"
bundle_id = "com.hegenberg.BetterTouchTool"

[[permissions]]
service = "Accessibility"

[[permissions]]
service = "Input Monitoring"

[license]
reactivate = true
note = "Enter your BetterTouchTool license again if it asks."
```

`catalog/cleanshot-x.toml`:
```toml
id = "cleanshot-x"
name = "CleanShot X"
bundle_id = "pl.maketheweb.cleanshotx"

[[permissions]]
service = "Screen Recording"

[[permissions]]
service = "Accessibility"

[license]
reactivate = true
note = "Activate CleanShot X with your license key; it counts Macs, so remove the old one from your account."
```

`catalog/dash.toml`:
```toml
id = "dash"
name = "Dash"
bundle_id = "com.kapeli.dashdoc"

[license]
reactivate = true
note = "Enter your Dash license again if it asks."
```

`catalog/hammerspoon.toml`:
```toml
id = "hammerspoon"
name = "Hammerspoon"
bundle_id = "org.hammerspoon.Hammerspoon"

[[permissions]]
service = "Accessibility"
```

`catalog/handy.toml`:
```toml
id = "handy"
name = "Handy"
bundle_id = "com.pais.handy"

[[permissions]]
service = "Microphone"

[[permissions]]
service = "Accessibility"
```

`catalog/itsycal.toml`:
```toml
id = "itsycal"
name = "Itsycal"
bundle_id = "com.mowglii.ItsycalApp"

[[permissions]]
service = "Calendars"
```

`catalog/jetbrains-toolbox.toml`:
```toml
id = "jetbrains-toolbox"
name = "JetBrains Toolbox"
bundle_id = "com.jetbrains.toolbox"

[license]
reactivate = true
note = "JetBrains licenses belong to your account: sign in again in each IDE if it asks."
```

`catalog/karabiner-elements.toml`:
```toml
id = "karabiner-elements"
name = "Karabiner-Elements"
bundle_id = "org.pqrs.Karabiner-Elements.Settings"

notes = [
  "Approve Karabiner's driver extension (System Settings > General > Login Items & Extensions > Driver Extensions), and give its core service Input Monitoring when it asks.",
]
```

`catalog/malwarebytes.toml`:
```toml
id = "malwarebytes"
name = "Malwarebytes"
bundle_id = "com.malwarebytes.mbam.frontend.application"

[[permissions]]
service = "Full Disk Access"

[license]
reactivate = true
note = "Malwarebytes Premium counts devices: sign in on the new Mac, and remove the old Mac from your account."
```

`catalog/monitorcontrol.toml`:
```toml
id = "monitorcontrol"
name = "MonitorControl"
bundle_id = "app.monitorcontrol.MonitorControl"

[[permissions]]
service = "Accessibility"
```

`catalog/mullvad-vpn.toml`:
```toml
id = "mullvad-vpn"
name = "Mullvad VPN"
bundle_id = "net.mullvad.vpn"

notes = ["Check that Mullvad VPN connects; if it asks, log in with your account number."]
```

`catalog/raycast.toml`:
```toml
id = "raycast"
name = "Raycast"
bundle_id = "com.raycast.macos"

[[permissions]]
service = "Accessibility"
```

`catalog/rectangle.toml`:
```toml
id = "rectangle"
name = "Rectangle"
bundle_id = "com.knollsoft.Rectangle"

[[permissions]]
service = "Accessibility"
```

`catalog/signal.toml`:
```toml
id = "signal"
name = "Signal"
bundle_id = "org.whispersystems.signal-desktop"

notes = [
  "Open Signal and check your chats are there. If it asks to be linked again, link the new Mac from your phone (Settings > Linked Devices), then remove the old Mac there.",
]
```

`catalog/slack.toml`:
```toml
id = "slack"
name = "Slack"
bundle_id = "com.tinyspeck.slackmacgap"

notes = ["Sign in to your Slack workspaces again if it asks."]

[state]
volatile = ["~/Library/Application Support/Slack/logs/"]
```

`catalog/tailscale.toml`:
```toml
id = "tailscale"
name = "Tailscale"
bundle_id = "io.tailscale.ipn.macsys"

notes = [
  "Check that Tailscale is connected. If the new Mac shows up as the old Mac's machine, log out and in again so each Mac has its own node.",
]
```

`catalog/termius.toml`:
```toml
id = "termius"
name = "Termius"
bundle_id = "com.termius-dmg.mac"

notes = ["Sign in to Termius again if it asks; your hosts and keys sync from your account."]
```

`catalog/visual-studio-code.toml`:
```toml
id = "visual-studio-code"
name = "Visual Studio Code"
bundle_id = "com.microsoft.VSCode"

notes = ["If you use Settings Sync, sign in to it again."]

[state]
volatile = [
  "~/Library/Application Support/Code/logs/",
  "~/Library/Application Support/Code/CachedData/",
]
```

`catalog/whatsapp.toml`:
```toml
id = "whatsapp"
name = "WhatsApp"
bundle_id = "net.whatsapp.WhatsApp"

notes = ["Open WhatsApp and check it is still linked; if not, link it again from your phone."]
```

`catalog/zoom.toml`:
```toml
id = "zoom"
name = "Zoom"
bundle_id = "us.zoom.xos"

[[permissions]]
service = "Camera"

[[permissions]]
service = "Microphone"

[[permissions]]
service = "Screen Recording"
```

- [ ] **Step 6: Tidy the module and run the tests**

Run: `go mod tidy && cat go.mod go.sum`
Expected:
```
module github.com/momentmaker/mtm

go 1.26

require github.com/BurntSushi/toml v1.6.0
github.com/BurntSushi/toml v1.6.0 h1:dRaEfpa2VI55EwlIW72hMRHdWouJeRF7TPYhI+AUQjk=
github.com/BurntSushi/toml v1.6.0/go.mod h1:ukJfTF/6rtPPRCnwkur4qwRxa8vTRFBF0uk2lLoLwho=
```

Run: `go test ./internal/catalog/`
Expected: PASS. `TestEveryCatalogFileIsValid` checks all 24 built-in files.

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./... && go vet -tags integration ./... && go test ./...
git add go.mod go.sum catalog internal/catalog
git commit -m "Add the app catalog

One TOML file per app says what macOS won't carry over for it: the
privacy permissions it needs, a license to enter again, and steps such
as signing in again. Every file is checked by the tests, so a mistake in
a contributed file fails CI instead of misleading a user. The first
apps' names and bundle ids come partly from tape, credited in the
catalog's README."
```

---

### Task 2: Checklist steps and their state

**Files:**
- Create: `internal/checklist/checklist.go`
- Modify: `internal/state/state.go`
- Test: `internal/checklist/checklist_test.go`, `internal/state/state_test.go` (modify)

**Interfaces:**
- Consumes: `state.Load` and `state.Save` (Stage 1).
- Produces:
  - `checklist.Item{ID, Module, Title, Detail, Where, Link string}`.
  - `checklist.Status`: `Pending`, `Done`, or `Skipped`.
  - `checklist.Entry{Item; Status; Reason string; At time.Time}`.
  - `checklist.List{Entries []Entry}`, with these methods:
    - `Add(items ...Item) int`: new ids are appended as pending; known ids keep their status and get the new text. It returns how many were new.
    - `Done(id, at) error` and `Skip(id, reason, at) error`. Both wrap `ErrUnknownItem`, and `Skip` returns `ErrNoReason` when the reason is blank.
    - `Pending() []Entry` and `Find(id) (Entry, bool)`.
  - `state.State.Checklist checklist.List`, with json `checklist,omitzero`.

- [ ] **Step 1: Write the failing tests**

`internal/checklist/checklist_test.go`:
```go
package checklist

import (
	"errors"
	"testing"
	"time"
)

var (
	raycast = Item{ID: "permissions:com.raycast.macos:Accessibility", Module: "permissions", Title: "Give Raycast Accessibility access"}
	license = Item{ID: "system:license:dash", Module: "system", Title: "Reactivate Dash"}
	now     = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
)

func TestAddKeepsWhatTheUserDidAndRefreshesText(t *testing.T) {
	var l List
	if n := l.Add(raycast, license); n != 2 {
		t.Fatalf("added %d, want 2", n)
	}
	if err := l.Done(raycast.ID, now); err != nil {
		t.Fatal(err)
	}
	renamed := raycast
	renamed.Title = "Give Raycast Accessibility access again"
	if n := l.Add(renamed, license); n != 0 {
		t.Errorf("added %d on a re-run, want 0", n)
	}
	e, ok := l.Find(raycast.ID)
	if !ok || e.Status != Done || e.Title != renamed.Title || !e.At.Equal(now) {
		t.Errorf("entry = %+v", e)
	}
	if p := l.Pending(); len(p) != 1 || p[0].ID != license.ID {
		t.Errorf("pending = %+v", p)
	}
}

func TestSkipNeedsAReason(t *testing.T) {
	var l List
	l.Add(license)
	if err := l.Skip(license.ID, "  ", now); !errors.Is(err, ErrNoReason) {
		t.Errorf("err = %v, want ErrNoReason", err)
	}
	if err := l.Skip(license.ID, " sold my Dash license ", now); err != nil {
		t.Fatal(err)
	}
	if e, _ := l.Find(license.ID); e.Status != Skipped || e.Reason != "sold my Dash license" {
		t.Errorf("entry = %+v", e)
	}
	if len(l.Pending()) != 0 {
		t.Error("a skipped step is still pending")
	}
}

func TestDoneAfterSkipClearsTheReason(t *testing.T) {
	var l List
	l.Add(license)
	l.Skip(license.ID, "later", now)
	l.Done(license.ID, now)
	if e, _ := l.Find(license.ID); e.Status != Done || e.Reason != "" {
		t.Errorf("entry = %+v", e)
	}
}

func TestUnknownIDsAreErrors(t *testing.T) {
	var l List
	if err := l.Done("nope", now); !errors.Is(err, ErrUnknownItem) {
		t.Errorf("Done err = %v", err)
	}
	if err := l.Skip("nope", "why", now); !errors.Is(err, ErrUnknownItem) {
		t.Errorf("Skip err = %v", err)
	}
}
```

`internal/state/state_test.go`:
```diff
--- a/internal/state/state_test.go
+++ b/internal/state/state_test.go
@@ -4,6 +4,7 @@
 	"testing"
 	"time"
 
+	"github.com/momentmaker/mtm/internal/checklist"
 	"github.com/momentmaker/mtm/internal/stage"
 )
 
@@ -26,3 +27,21 @@
 		t.Errorf("Load = %+v, %v", s, err)
 	}
 }
+
+func TestSaveLoadKeepsTheChecklist(t *testing.T) {
+	st, _ := stage.Open(t.TempDir())
+	var s State
+	s.Checklist.Add(checklist.Item{ID: "system:license:dash", Module: "system", Title: "Reactivate Dash"})
+	s.Checklist.Skip("system:license:dash", "no longer use it", time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
+	if err := Save(st, s); err != nil {
+		t.Fatal(err)
+	}
+	got, err := Load(st)
+	if err != nil {
+		t.Fatal(err)
+	}
+	e, ok := got.Checklist.Find("system:license:dash")
+	if !ok || e.Status != checklist.Skipped || e.Reason != "no longer use it" {
+		t.Errorf("entry = %+v, %v", e, ok)
+	}
+}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/checklist/ ./internal/state/`
Expected: the build fails with `undefined: Item` and `s.Checklist undefined`.

- [ ] **Step 3: Implement**

`internal/checklist/checklist.go`:
```go
// Package checklist tracks the steps no tool can do for the user, such as
// granting a privacy permission, until each is done or skipped with a
// reason. verify counts the rest as open (spec 8.5).
package checklist

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Item is one step. ID is stable across runs, "<module>:<key>", so finish
// can run again without losing what the user already did. Where names the
// place to do it, and Link opens it.
type Item struct {
	ID     string `json:"id"`
	Module string `json:"module"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Where  string `json:"where,omitempty"`
	Link   string `json:"link,omitempty"`
}

type Status string

const (
	Pending Status = "pending"
	Done    Status = "done"
	Skipped Status = "skipped"
)

type Entry struct {
	Item
	Status Status    `json:"status"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at,omitzero"`
}

type List struct {
	Entries []Entry `json:"entries,omitempty"`
}

var (
	ErrUnknownItem = errors.New("no checklist item has that id")
	ErrNoReason    = errors.New("skipping a step needs a reason")
)

// Add records new items as pending and refreshes the text of items it
// already has, keeping what the user did with them. It returns how many
// were new.
func (l *List) Add(items ...Item) int {
	added := 0
	for _, it := range items {
		if i := l.index(it.ID); i >= 0 {
			l.Entries[i].Item = it
			continue
		}
		l.Entries = append(l.Entries, Entry{Item: it, Status: Pending})
		added++
	}
	return added
}

func (l *List) Done(id string, at time.Time) error {
	i := l.index(id)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrUnknownItem, id)
	}
	l.Entries[i].Status, l.Entries[i].Reason, l.Entries[i].At = Done, "", at
	return nil
}

// Skip closes a step the user won't do. The reason is recorded because a
// skipped step still counts for the verdict only with one.
func (l *List) Skip(id, reason string, at time.Time) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrNoReason
	}
	i := l.index(id)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrUnknownItem, id)
	}
	l.Entries[i].Status, l.Entries[i].Reason, l.Entries[i].At = Skipped, reason, at
	return nil
}

func (l List) Pending() []Entry {
	var out []Entry
	for _, e := range l.Entries {
		if e.Status == Pending {
			out = append(out, e)
		}
	}
	return out
}

func (l List) Find(id string) (Entry, bool) {
	if i := l.index(id); i >= 0 {
		return l.Entries[i], true
	}
	return Entry{}, false
}

func (l List) index(id string) int {
	for i, e := range l.Entries {
		if e.ID == id {
			return i
		}
	}
	return -1
}
```

`internal/state/state.go`:
```diff
--- a/internal/state/state.go
+++ b/internal/state/state.go
@@ -9,6 +9,7 @@
 	"os"
 	"time"
 
+	"github.com/momentmaker/mtm/internal/checklist"
 	"github.com/momentmaker/mtm/internal/stage"
 )
 
@@ -19,8 +20,11 @@
 	Safe bool      `json:"safe"`
 }
 
+// State holds the new Mac's checklist, with what the user did with each
+// step, and the last verify result.
 type State struct {
-	LastVerify *VerifyResult `json:"last_verify,omitempty"`
+	LastVerify *VerifyResult  `json:"last_verify,omitempty"`
+	Checklist  checklist.List `json:"checklist,omitzero"`
 }
 
 func Load(st *stage.Stage) (State, error) {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/checklist/ ./internal/state/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go vet -tags integration ./... && go test ./...
git add internal/checklist internal/state
git commit -m "Add checklist steps, kept in state.json

A step stays open until the user marks it done or skips it with a
reason. Adding steps again keeps what the user already did, so finish
can run any number of times."
```

---

### Task 3: `finish` saves steps, and `verify` counts them

**Files:**
- Modify: `internal/module/module.go`, `internal/app/app.go`, `internal/report/report.go`, `cmd/mtm/main.go`
- Test: `internal/app/checklist_test.go` (create), `internal/report/report_test.go` (modify)

**Interfaces:**
- Consumes: Task 2 (`checklist.List`, `state.State.Checklist`); `verdict.Decide(checks, pending []string)` (Stage 1, which until now got `nil`).
- Produces:
  - `module.Result.Checklist []checklist.Item` and `module.Check.Resolves []string` (json `resolves,omitempty`).
  - `app.StepReport.Pending int` (json `pending_checklist`): the open steps after `finish`.
  - `App.Finish` merges every module's `Checklist` into `state.json`.
  - `App.Verify`:
    - marks done the `Resolves` ids of every passing check, but only ids the checklist has;
    - passes the open steps' titles to the verdict;
    - saves the state.
  - `report.Finish(w, done, problems, pending int)`. With open steps, it ends with "N steps are waiting in the checklist." and "Next: run mtm checklist, then mtm verify".
  - `report.Verify` adds "Run mtm checklist to do or skip the open steps." when steps are open.

- [ ] **Step 1: Write the failing tests**

`internal/app/checklist_test.go`:
```go
package app

import (
	"slices"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
)

var (
	grantStep   = checklist.Item{ID: "steps:grant", Module: "steps", Title: "Give Raycast Accessibility access"}
	licenseStep = checklist.Item{ID: "steps:license", Module: "steps", Title: "Reactivate Dash"}
)

// stepsModule leaves two checklist steps in finish. Its verify check
// passes and proves the grant step done when grants is set.
type stepsModule struct{ grants bool }

func (stepsModule) Name() string                              { return "steps" }
func (stepsModule) Scan(module.Env) ([]module.Finding, error) { return nil, nil }
func (stepsModule) Prepare(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{Inventory: true}, nil
}
func (stepsModule) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{Checklist: []checklist.Item{grantStep, licenseStep}}, nil
}
func (m stepsModule) Verify(module.Env, *stage.Stage) ([]module.Check, error) {
	c := module.Check{Module: "steps", ID: "steps:grants", Status: module.StatusWarn, Title: "grants"}
	if m.grants {
		c.Status, c.Resolves = module.StatusPass, []string{grantStep.ID}
	}
	return []module.Check{c}, nil
}

func finishedNewMac(t *testing.T, mod module.Module) string {
	t.Helper()
	old := t.TempDir()
	mustPrepare(t, stepApp(old, oldUUID, mod))
	newHome := migrate(t, old)
	rep, err := stepApp(newHome, newUUID, mod).Finish(ctx, FinishOptions{})
	if err != nil || rep.Pending != 2 {
		t.Fatalf("finish = %+v, %v; want 2 pending steps", rep, err)
	}
	return newHome
}

func loadState(t *testing.T, home string) state.State {
	t.Helper()
	st, err := stage.Existing(home)
	if err != nil {
		t.Fatal(err)
	}
	s, err := state.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFinishKeepsWhatTheUserDidWhenItRunsAgain(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	st, _ := stage.Existing(newHome)
	s := loadState(t, newHome)
	if err := s.Checklist.Skip(licenseStep.ID, "sold the license", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(st, s); err != nil {
		t.Fatal(err)
	}
	rep, err := stepApp(newHome, newUUID, stepsModule{}).Finish(ctx, FinishOptions{})
	if err != nil || rep.Pending != 1 {
		t.Errorf("second finish = %+v, %v; want 1 pending step", rep, err)
	}
}

func TestVerifyIsNotSafeWhileStepsAreOpen(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	rep, err := stepApp(newHome, newUUID, stepsModule{}).Verify(ctx, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict.Safe || !slices.Contains(rep.Verdict.Pending, licenseStep.Title) {
		t.Errorf("verdict = %+v, want NOT SAFE with the open steps", rep.Verdict)
	}
}

func TestVerifyMarksDoneTheStepsAPassingCheckProves(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	rep, err := stepApp(newHome, newUUID, stepsModule{grants: true}).Verify(ctx, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Verdict.Pending, []string{licenseStep.Title}) {
		t.Errorf("pending = %q, want only the license step", rep.Verdict.Pending)
	}
	if e, _ := loadState(t, newHome).Checklist.Find(grantStep.ID); e.Status != checklist.Done {
		t.Errorf("grant step = %+v, want done", e)
	}
}

func TestVerifyIsSafeOnceEveryStepIsClosed(t *testing.T) {
	newHome := finishedNewMac(t, stepsModule{})
	st, _ := stage.Existing(newHome)
	s := loadState(t, newHome)
	s.Checklist.Skip(licenseStep.ID, "sold the license", time.Now())
	state.Save(st, s)
	rep, err := stepApp(newHome, newUUID, stepsModule{grants: true}).Verify(ctx, VerifyOptions{})
	if err != nil || !rep.Verdict.Safe {
		t.Errorf("verify = %+v, %v; want SAFE", rep.Verdict, err)
	}
}
```

`internal/report/report_test.go`:
```diff
--- a/internal/report/report_test.go
+++ b/internal/report/report_test.go
@@ -166,7 +166,7 @@
 	Finish(&out, []string{"homebrew: started postgresql@16"}, []module.Finding{{
 		Module: "system", Severity: module.SeverityWarn, Handler: module.Manual,
 		Title: "Intel-only apps need Rosetta", Fix: "Run: sudo softwareupdate --install-rosetta --agree-to-license",
-	}})
+	}}, 0)
 	for _, want := range []string{"✓ homebrew: started postgresql@16", "Do these by hand", "→ Run: sudo softwareupdate", "Next: run mtm verify"} {
 		if !strings.Contains(out.String(), want) {
 			t.Errorf("output missing %q:\n%s", want, out.String())
@@ -176,7 +176,7 @@
 
 func TestFinishBlockedSaysToRunAgain(t *testing.T) {
 	var out bytes.Buffer
-	Finish(&out, nil, []module.Finding{{Module: "preflight", Severity: module.SeverityBlocker, Title: "FileVault is off"}})
+	Finish(&out, nil, []module.Finding{{Module: "preflight", Severity: module.SeverityBlocker, Title: "FileVault is off"}}, 0)
 	if !strings.Contains(out.String(), "run mtm finish again") || strings.Contains(out.String(), "Next: run mtm verify") {
 		t.Errorf("blocked output:\n%s", out.String())
 	}
@@ -222,3 +222,23 @@
 		t.Errorf("output without --stage mentions the drive:\n%s", plain.String())
 	}
 }
+
+func TestFinishPointsToTheChecklist(t *testing.T) {
+	var out bytes.Buffer
+	Finish(&out, nil, nil, 3)
+	for _, want := range []string{"3 steps are waiting in the checklist.", "Next: run mtm checklist, then mtm verify"} {
+		if !strings.Contains(out.String(), want) {
+			t.Errorf("output missing %q:\n%s", want, out.String())
+		}
+	}
+}
+
+func TestVerifyNotSafeListsOpenStepsAndTheCommand(t *testing.T) {
+	var out bytes.Buffer
+	Verify(&out, nil, verdict.Decide(nil, []string{"Give Raycast Accessibility access"}), false)
+	for _, want := range []string{"NOT SAFE TO WIPE", "✗ checklist: Give Raycast Accessibility access", "Run mtm checklist to do or skip the open steps."} {
+		if !strings.Contains(out.String(), want) {
+			t.Errorf("output missing %q:\n%s", want, out.String())
+		}
+	}
+}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ ./internal/report/`
Expected: the build fails with `unknown field Checklist in struct literal of type module.Result`, `unknown field Resolves`, and `too many arguments in call to Finish`.

- [ ] **Step 3: Implement**

`internal/module/module.go`:
```diff
--- a/internal/module/module.go
+++ b/internal/module/module.go
@@ -7,6 +7,7 @@
 	"fmt"
 	"time"
 
+	"github.com/momentmaker/mtm/internal/checklist"
 	"github.com/momentmaker/mtm/internal/manifest"
 	"github.com/momentmaker/mtm/internal/runner"
 	"github.com/momentmaker/mtm/internal/stage"
@@ -73,6 +74,8 @@
 	StatusInfo Status = "info"
 )
 
+// Check is one verify result. Resolves names checklist steps a passing
+// check proves done, such as a privacy permission found granted.
 type Check struct {
 	Module   string   `json:"module"`
 	ID       string   `json:"id"`
@@ -81,15 +84,18 @@
 	Title    string   `json:"title"`
 	Detail   string   `json:"detail,omitempty"`
 	Paths    []string `json:"paths,omitempty"`
+	Resolves []string `json:"resolves,omitempty"`
 }
 
 // Result is what Prepare and Finish return. Inventory is stored in the
 // manifest under the module's name. A Problems entry with SeverityBlocker
-// makes the run NOT READY.
+// makes the run NOT READY. Checklist holds the steps Finish leaves to the
+// user, which verify counts until each is done or skipped.
 type Result struct {
 	Inventory any
 	Done      []string
 	Problems  []Finding
+	Checklist []checklist.Item
 }
 
 type Module interface {
```

`internal/app/app.go`:
```diff
--- a/internal/app/app.go
+++ b/internal/app/app.go
@@ -14,6 +14,7 @@
 	"strings"
 	"time"
 
+	"github.com/momentmaker/mtm/internal/checklist"
 	"github.com/momentmaker/mtm/internal/macos"
 	"github.com/momentmaker/mtm/internal/manifest"
 	"github.com/momentmaker/mtm/internal/module"
@@ -89,10 +90,11 @@
 }
 
 // StepReport is what finish or prepare --undo did, and what is left to do
-// by hand.
+// by hand. Pending counts the checklist steps still open after finish.
 type StepReport struct {
 	Done     []string         `json:"done"`
 	Problems []module.Finding `json:"problems"`
+	Pending  int              `json:"pending_checklist"`
 }
 
 func (r *StepReport) add(name string, res module.Result) {
@@ -279,11 +281,12 @@
 		checks = append(checks, a.unknownModules(m)...)
 	}
 
-	v := verdict.Decide(checks, nil)
 	s, err := state.Load(st)
 	if err != nil {
 		return VerifyReport{}, err
 	}
+	resolve(&s.Checklist, checks, a.Now())
+	v := verdict.Decide(checks, pendingTitles(s.Checklist))
 	s.LastVerify = &state.VerifyResult{At: a.Now(), Safe: v.Safe && len(opts.Only) == 0}
 	if err := state.Save(st, s); err != nil {
 		return VerifyReport{}, err
@@ -324,6 +327,7 @@
 	env := a.env(ctx, module.NewMac, false, m, run, exportsFor(st, m))
 	env.Force, env.RestoreDump = opts.Force, opts.RestoreDump
 	var rep StepReport
+	var items []checklist.Item
 	for _, mod := range mods {
 		if _, ok := m.Modules[mod.Name()]; !ok {
 			continue
@@ -334,10 +338,43 @@
 			continue
 		}
 		rep.add(mod.Name(), res)
+		items = append(items, res.Checklist...)
+	}
+	s, err := state.Load(st)
+	if err != nil {
+		return rep, err
 	}
+	s.Checklist.Add(items...)
+	if err := state.Save(st, s); err != nil {
+		return rep, err
+	}
+	rep.Pending = len(s.Checklist.Pending())
 	return rep, nil
 }
 
+// resolve marks done the checklist steps a passing check proved done. A
+// step finish never added, because finish hasn't run, has nothing to mark.
+func resolve(l *checklist.List, checks []module.Check, at time.Time) {
+	for _, c := range checks {
+		if c.Status != module.StatusPass {
+			continue
+		}
+		for _, id := range c.Resolves {
+			if _, ok := l.Find(id); ok {
+				l.Done(id, at)
+			}
+		}
+	}
+}
+
+func pendingTitles(l checklist.List) []string {
+	var out []string
+	for _, e := range l.Pending() {
+		out = append(out, e.Title)
+	}
+	return out
+}
+
 // Undo puts the old Mac back in use when the migration is called off.
 // Undone modules lose their record: it describes a state that no longer
 // holds, so verify must not trust it.
```

`internal/report/report.go`:
```diff
--- a/internal/report/report.go
+++ b/internal/report/report.go
@@ -18,6 +18,7 @@
 	"github.com/momentmaker/mtm/internal/preflight"
 	"github.com/momentmaker/mtm/internal/redact"
 	"github.com/momentmaker/mtm/internal/verdict"
+	"github.com/momentmaker/mtm/internal/words"
 )
 
 const shownPaths = 5
@@ -187,7 +188,9 @@
 }
 
 // Finish prints what finish did and what is left to do by hand.
-func Finish(w io.Writer, done []string, problems []module.Finding) {
+// Finish prints what finish did and what is left to do by hand: problems,
+// and pending, the checklist steps still open.
+func Finish(w io.Writer, done []string, problems []module.Finding, pending int) {
 	writeDone(w, done)
 	if module.HasBlocker(problems) {
 		writeProblems(w, "Problems", problems)
@@ -195,6 +198,11 @@
 		return
 	}
 	writeProblems(w, "Do these by hand", problems)
+	if pending > 0 {
+		fmt.Fprintf(w, "%s in the checklist.\n", words.Count(pending, "step is waiting", "steps are waiting"))
+		fmt.Fprintln(w, "Next: run mtm checklist, then mtm verify")
+		return
+	}
 	fmt.Fprintln(w, "Next: run mtm verify")
 }
 
@@ -239,6 +247,9 @@
 	for _, item := range v.Pending {
 		fmt.Fprintf(w, "  ✗ checklist: %s\n", clean(item))
 	}
+	if len(v.Pending) > 0 {
+		fmt.Fprintln(w, "Run mtm checklist to do or skip the open steps.")
+	}
 }
 
 // checkedModules names the modules whose checks the verdict rests on, so
```

`cmd/mtm/main.go`:
```diff
--- a/cmd/mtm/main.go
+++ b/cmd/mtm/main.go
@@ -219,7 +219,7 @@
 		fmt.Fprintln(stderr, "mtm finish:", err)
 		return exitFailed
 	}
-	report.Finish(stdout, rep.Done, rep.Problems)
+	report.Finish(stdout, rep.Done, rep.Problems, rep.Pending)
 	if len(rep.Problems) > 0 {
 		return exitProblems
 	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/app/ ./internal/report/ ./cmd/mtm/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go vet -tags integration ./... && go test ./...
git add internal/module internal/app internal/report cmd/mtm
git commit -m "Make SAFE TO WIPE wait for the checklist

Modules can now leave steps for the user, and a passing check can prove
some done, as a granted permission will. finish keeps the steps in
state.json, and verify reports each open step and isn't SAFE until
every one is done or skipped with a reason (spec 8.5)."
```

---

### Task 4: `mtm checklist`

**Files:**
- Create: `internal/app/checklist.go`, `internal/report/checklist.go`, `cmd/mtm/checklist.go`
- Modify: `cmd/mtm/main.go`
- Test: `internal/app/steps_test.go`, `internal/report/checklist_test.go`, `cmd/mtm/checklist_test.go`

**Interfaces:**
- Consumes: Tasks 2 and 3; `stage.Existing`; `runner.Command`.
- Produces:
  - `App.Checklist() (checklist.List, error)`, `App.MarkDone(ids ...string) error`, and `App.Skip(id, reason string) error`.
  - `App.OpenStep(ctx, id) error`:
    - it runs `/usr/bin/open <link>` only for links starting `x-apple.systempreferences:`;
    - it returns `app.ErrNoLink` for a step without a link.
  - `report.Checklist(w, list, all bool)` and `report.Step(w, n, total, entry)`.
  - The `mtm checklist` command:
    - without arguments, it walks the open steps when stdin is a terminal, and lists them otherwise;
    - subcommands: `list [--all] [--json]`, `done <id>...`, `skip <id> --reason "..."` (flags may come after the id), and `open <id>`.
  - Package variables `stdin` and `interactive` in `cmd/mtm`, which tests replace.
  - The walk-through:
    - it opens each step's link;
    - it asks `[d]one, [s]kip, [l]ater, [q]uit?`, and a reason for a skip;
    - it saves every answer as it is given;
    - the end of input acts like quit.

- [ ] **Step 1: Write the failing tests**

`internal/app/steps_test.go`:
```go
package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
)

func savedSteps(t *testing.T, items ...checklist.Item) string {
	t.Helper()
	home := t.TempDir()
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	var s state.State
	s.Checklist.Add(items...)
	if err := state.Save(st, s); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestMarkDoneAndSkipAreSaved(t *testing.T) {
	home := savedSteps(t, grantStep, licenseStep)
	a := stepApp(home, newUUID)
	if err := a.MarkDone(grantStep.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.Skip(licenseStep.ID, "sold it"); err != nil {
		t.Fatal(err)
	}
	l, err := a.Checklist()
	if err != nil || len(l.Pending()) != 0 {
		t.Errorf("checklist = %+v, %v; want nothing open", l, err)
	}
	if err := a.MarkDone("steps:nope"); !errors.Is(err, checklist.ErrUnknownItem) {
		t.Errorf("err = %v, want ErrUnknownItem", err)
	}
}

func TestOpenStepOpensItsSettingsPane(t *testing.T) {
	linked := grantStep
	linked.Link = "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?Privacy_Accessibility"
	home := savedSteps(t, linked)
	a := stepApp(home, newUUID)
	fake := a.Runner.(*runner.Fake)
	fake.Responses["/usr/bin/open "+linked.Link] = runner.Response{}
	if err := a.OpenStep(ctx, linked.ID); err != nil {
		t.Fatal(err)
	}
	if calls := fake.Calls(); calls[len(calls)-1].String() != "/usr/bin/open "+linked.Link {
		t.Errorf("last command = %s", calls[len(calls)-1].String())
	}
}

func TestOpenStepRefusesALinkMtmDidntMake(t *testing.T) {
	odd := grantStep
	odd.Link = "file:///Applications/Calculator.app"
	home := savedSteps(t, odd)
	a := stepApp(home, newUUID)
	if err := a.OpenStep(ctx, odd.ID); err == nil || !strings.Contains(err.Error(), "only System Settings links") {
		t.Errorf("err = %v", err)
	}
	if len(a.Runner.(*runner.Fake).Calls()) != 0 {
		t.Error("open ran for a link mtm didn't make")
	}
}

func TestOpenStepWithoutALinkNamesThePlace(t *testing.T) {
	plain := licenseStep
	plain.Where = "Dash > Settings > License"
	home := savedSteps(t, plain)
	if err := stepApp(home, newUUID).OpenStep(ctx, plain.ID); !errors.Is(err, ErrNoLink) || !strings.Contains(err.Error(), "Dash > Settings") {
		t.Errorf("err = %v", err)
	}
}
```

`internal/report/checklist_test.go`:
```go
package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/checklist"
)

func sampleList() checklist.List {
	var l checklist.List
	l.Add(
		checklist.Item{ID: "permissions:com.raycast.macos:Accessibility", Title: "Give Raycast Accessibility access", Where: "System Settings > Privacy & Security > Accessibility"},
		checklist.Item{ID: "system:license:dash", Title: "Reactivate the Dash license", Detail: "Enter your Dash license again if it asks."},
	)
	l.Skip("system:license:dash", "sold it", time.Now())
	return l
}

func TestChecklistShowsOpenStepsWithTheirCommand(t *testing.T) {
	var out bytes.Buffer
	Checklist(&out, sampleList(), false)
	for _, want := range []string{"1 step open", "☐ Give Raycast Accessibility access", "System Settings > Privacy & Security > Accessibility", "→ mtm checklist done permissions:com.raycast.macos:Accessibility"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Dash") {
		t.Errorf("a skipped step showed without --all:\n%s", out.String())
	}
}

func TestChecklistAllShowsSkippedStepsWithTheirReason(t *testing.T) {
	var out bytes.Buffer
	Checklist(&out, sampleList(), true)
	if !strings.Contains(out.String(), "– Reactivate the Dash license") || !strings.Contains(out.String(), "skipped: sold it") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestChecklistEmptyAndFinished(t *testing.T) {
	var out bytes.Buffer
	Checklist(&out, checklist.List{}, false)
	if !strings.Contains(out.String(), "mtm finish fills it") {
		t.Errorf("empty output:\n%s", out.String())
	}
	l := sampleList()
	l.Done("permissions:com.raycast.macos:Accessibility", time.Now())
	out.Reset()
	Checklist(&out, l, false)
	if !strings.Contains(out.String(), "Every checklist step is done or skipped.") {
		t.Errorf("finished output:\n%s", out.String())
	}
}

func TestStepEscapesControlCharacters(t *testing.T) {
	var out bytes.Buffer
	Step(&out, 1, 2, checklist.Entry{Item: checklist.Item{Title: "Give \x1b[2JEvil access"}})
	assertNoRawControls(t, out.String())
}
```

`cmd/mtm/checklist_test.go`:
```go
package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
	"github.com/momentmaker/mtm/internal/testutil"
)

const raycastLink = "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?Privacy_Accessibility"

var (
	raycastStep = checklist.Item{ID: "permissions:com.raycast.macos:Accessibility", Module: "permissions",
		Title: "Give Raycast Accessibility access", Where: "System Settings > Privacy & Security > Accessibility", Link: raycastLink}
	dashStep = checklist.Item{ID: "system:license:dash", Module: "system", Title: "Reactivate the Dash license"}
)

// checklistHome is a new Mac whose finish left two steps, with HOME set
// to it. The app it returns answers open from a fake and records it.
func checklistHome(t *testing.T) (*runner.Fake, func(string) *app.App) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	var s state.State
	s.Checklist.Add(raycastStep, dashStep)
	if err := state.Save(st, s); err != nil {
		t.Fatal(err)
	}
	fake := &runner.Fake{Responses: testutil.MacResponses("NEW-MAC")}
	fake.Responses["/usr/bin/open "+raycastLink] = runner.Response{}
	return fake, func(home string) *app.App {
		return &app.App{Runner: fake, Home: home, Version: "test", Now: time.Now}
	}
}

func asTerminal(t *testing.T, input string) {
	t.Helper()
	oldIn, oldInteractive := stdin, interactive
	stdin, interactive = strings.NewReader(input), func() bool { return true }
	t.Cleanup(func() { stdin, interactive = oldIn, oldInteractive })
}

func notTerminal(t *testing.T) {
	t.Helper()
	old := interactive
	interactive = func() bool { return false }
	t.Cleanup(func() { interactive = old })
}

func TestChecklistWithoutATerminalListsTheOpenSteps(t *testing.T) {
	_, newApp := checklistHome(t)
	notTerminal(t)
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"checklist"}, &out, &errOut, newApp)
	if code != exitOK || !strings.Contains(out.String(), "2 steps open") || !strings.Contains(out.String(), "mtm checklist done system:license:dash") {
		t.Errorf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
}

func TestChecklistDoneAndSkip(t *testing.T) {
	_, newApp := checklistHome(t)
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"checklist", "done", raycastStep.ID}, &out, &errOut, newApp); code != exitOK {
		t.Fatalf("done exit %d: %s", code, errOut.String())
	}
	if code := run(ctx, []string{"checklist", "skip", dashStep.ID}, &out, &errOut, newApp); code != exitFailed || !strings.Contains(errOut.String(), "needs a reason") {
		t.Errorf("skip without a reason: exit %d, stderr %q", code, errOut.String())
	}
	if code := run(ctx, []string{"checklist", "skip", dashStep.ID, "--reason", "sold it"}, &out, &errOut, newApp); code != exitOK {
		t.Fatalf("skip exit %d: %s", code, errOut.String())
	}
	out.Reset()
	run(ctx, []string{"checklist", "list", "--all"}, &out, &errOut, newApp)
	if !strings.Contains(out.String(), "0 steps open") || !strings.Contains(out.String(), "skipped: sold it") {
		t.Errorf("list --all:\n%s", out.String())
	}
}

func TestChecklistDoneRejectsAnUnknownStep(t *testing.T) {
	_, newApp := checklistHome(t)
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"checklist", "done", "nope"}, &out, &errOut, newApp); code != exitFailed || !strings.Contains(errOut.String(), "no checklist item has that id") {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}

func TestChecklistWalkOpensEachPaneAndRecordsTheAnswers(t *testing.T) {
	fake, newApp := checklistHome(t)
	asTerminal(t, "d\ns\nI sold my Dash license\n")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"checklist"}, &out, &errOut, newApp); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	opened := false
	for _, c := range fake.Calls() {
		opened = opened || c.String() == "/usr/bin/open "+raycastLink
	}
	if !opened {
		t.Error("the walk didn't open Raycast's pane")
	}
	if !strings.Contains(out.String(), "[1/2] Give Raycast Accessibility access") || !strings.Contains(out.String(), "Every checklist step is done or skipped.") {
		t.Errorf("walk output:\n%s", out.String())
	}
}

func TestChecklistWalkQuitsAndLaterLeavesStepsOpen(t *testing.T) {
	_, newApp := checklistHome(t)
	asTerminal(t, "l\nq\n")
	var out, errOut bytes.Buffer
	run(ctx, []string{"checklist"}, &out, &errOut, newApp)
	if !strings.Contains(out.String(), "2 steps open") {
		t.Errorf("walk output:\n%s", out.String())
	}
}

func TestChecklistWalkEndsAtTheEndOfInput(t *testing.T) {
	_, newApp := checklistHome(t)
	asTerminal(t, "")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"checklist"}, &out, &errOut, newApp); code != exitOK {
		t.Errorf("exit %d: %s", code, errOut.String())
	}
}

func TestChecklistUnknownSubcommand(t *testing.T) {
	_, newApp := checklistHome(t)
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"checklist", "frob"}, &out, &errOut, newApp); code != exitFailed || !strings.Contains(errOut.String(), `unknown subcommand "frob"`) {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}

func TestChecklistWithoutAnMtmFolder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	notTerminal(t)
	var out, errOut bytes.Buffer
	newApp := func(home string) *app.App {
		return &app.App{Runner: &runner.Fake{}, Home: home, Version: "test", Now: time.Now}
	}
	if code := run(ctx, []string{"checklist"}, &out, &errOut, newApp); code != exitFailed || !strings.Contains(errOut.String(), "run mtm prepare") {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ ./internal/report/ ./cmd/mtm/`
Expected: the build fails with `a.MarkDone undefined`, `undefined: Checklist` (report), and `undefined: stdin` (cmd/mtm).

- [ ] **Step 3: Implement**

`internal/app/checklist.go`:
```go
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
)

// settingsLink is the only kind of link mtm opens: it made every
// checklist link itself, and state.json is a file anyone with the account
// could edit to point open at something else.
const settingsLink = "x-apple.systempreferences:"

var ErrNoLink = errors.New("this step has no link to open")

// Checklist returns the steps finish left on this Mac, with what the user
// did with each.
func (a *App) Checklist() (checklist.List, error) {
	st, err := stage.Existing(a.Home)
	if err != nil {
		return checklist.List{}, err
	}
	s, err := state.Load(st)
	if err != nil {
		return checklist.List{}, err
	}
	return s.Checklist, nil
}

func (a *App) MarkDone(ids ...string) error {
	return a.updateChecklist(func(l *checklist.List) error {
		for _, id := range ids {
			if err := l.Done(id, a.Now()); err != nil {
				return err
			}
		}
		return nil
	})
}

func (a *App) Skip(id, reason string) error {
	return a.updateChecklist(func(l *checklist.List) error { return l.Skip(id, reason, a.Now()) })
}

func (a *App) updateChecklist(change func(*checklist.List) error) error {
	st, err := stage.Existing(a.Home)
	if err != nil {
		return err
	}
	s, err := state.Load(st)
	if err != nil {
		return err
	}
	if err := change(&s.Checklist); err != nil {
		return err
	}
	return state.Save(st, s)
}

// OpenStep opens the System Settings pane a step happens in.
func (a *App) OpenStep(ctx context.Context, id string) error {
	l, err := a.Checklist()
	if err != nil {
		return err
	}
	e, ok := l.Find(id)
	if !ok {
		return fmt.Errorf("%w: %s", checklist.ErrUnknownItem, id)
	}
	if e.Link == "" {
		return fmt.Errorf("%w; do it in %s", ErrNoLink, e.Where)
	}
	if !strings.HasPrefix(e.Link, settingsLink) {
		return fmt.Errorf("refusing to open %q: mtm opens only System Settings links", e.Link)
	}
	if _, err := a.Runner.Run(ctx, runner.Command{Name: "/usr/bin/open", Args: []string{e.Link}}); err != nil {
		return fmt.Errorf("open %s: %w", e.Where, err)
	}
	return nil
}
```

`internal/report/checklist.go`:
```go
package report

import (
	"fmt"
	"io"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/words"
)

func stepSymbol(s checklist.Status) string {
	switch s {
	case checklist.Done:
		return "✓"
	case checklist.Skipped:
		return "–"
	}
	return "☐"
}

// Checklist prints the open steps, or every step with all, each with the
// command that closes it.
func Checklist(w io.Writer, l checklist.List, all bool) {
	open := len(l.Pending())
	if len(l.Entries) == 0 {
		fmt.Fprintln(w, "The checklist is empty. mtm finish fills it on the new Mac.")
		return
	}
	if open == 0 && !all {
		fmt.Fprintln(w, "Every checklist step is done or skipped.")
		return
	}
	fmt.Fprintf(w, "%s open\n", words.Count(open, "step", "steps"))
	for _, e := range l.Entries {
		if e.Status != checklist.Pending && !all {
			continue
		}
		fmt.Fprintf(w, "  %s %s\n", stepSymbol(e.Status), clean(e.Title))
		writeStepBody(w, e)
		switch e.Status {
		case checklist.Pending:
			fmt.Fprintf(w, "      → mtm checklist done %s\n", clean(e.ID))
		case checklist.Skipped:
			fmt.Fprintf(w, "      skipped: %s\n", clean(e.Reason))
		}
	}
}

func writeStepBody(w io.Writer, e checklist.Entry) {
	if e.Where != "" {
		fmt.Fprintf(w, "      %s\n", clean(e.Where))
	}
	if e.Detail != "" {
		fmt.Fprintf(w, "      %s\n", clean(e.Detail))
	}
}

// Step prints one step of the walk-through: the nth of total.
func Step(w io.Writer, n, total int, e checklist.Entry) {
	fmt.Fprintf(w, "\n[%d/%d] %s\n", n, total, clean(e.Title))
	writeStepBody(w, e)
}
```

`cmd/mtm/checklist.go`:
```go
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/report"
)

// stdin and interactive are the walk-through's input; tests replace them.
var (
	stdin       io.Reader = os.Stdin
	interactive           = func() bool {
		info, err := os.Stdin.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0
	}
)

func cmdChecklist(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if interactive() {
			return walk(ctx, a, stdin, stdout, stderr)
		}
		return listSteps(a, nil, stdout, stderr)
	}
	switch args[0] {
	case "list":
		return listSteps(a, args[1:], stdout, stderr)
	case "done":
		return markDone(a, args[1:], stdout, stderr)
	case "skip":
		return skipStep(a, args[1:], stdout, stderr)
	case "open":
		return openStep(ctx, a, args[1:], stderr)
	}
	fmt.Fprintf(stderr, "mtm checklist: unknown subcommand %q\n\n%s", args[0], usage)
	return exitFailed
}

// parse reads flags wherever they sit among the arguments, so
// "skip <id> --reason ..." works as well as "skip --reason ... <id>".
func parse(flags *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		if flags.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, flags.Arg(0))
		args = flags.Args()[1:]
	}
}

func listSteps(a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("checklist list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	all := flags.Bool("all", false, "also show steps already done or skipped")
	asJSON := flags.Bool("json", false, "print JSON")
	if _, err := parse(flags, args); err != nil {
		return exitFailed
	}
	l, err := a.Checklist()
	if err != nil {
		fmt.Fprintln(stderr, "mtm checklist:", err)
		return exitFailed
	}
	if *asJSON {
		if err := report.JSON(stdout, l); err != nil {
			fmt.Fprintln(stderr, "mtm checklist:", err)
			return exitFailed
		}
		return exitOK
	}
	report.Checklist(stdout, l, *all)
	return exitOK
}

func markDone(a *app.App, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "mtm checklist done: name the step ids to mark done")
		return exitFailed
	}
	if err := a.MarkDone(args...); err != nil {
		fmt.Fprintln(stderr, "mtm checklist done:", err)
		return exitFailed
	}
	fmt.Fprintf(stdout, "Marked done: %s\n", strings.Join(args, ", "))
	return exitOK
}

func skipStep(a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("checklist skip", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reason := flags.String("reason", "", "why this step doesn't apply (required)")
	ids, err := parse(flags, args)
	if err != nil {
		return exitFailed
	}
	if len(ids) != 1 {
		fmt.Fprintln(stderr, `mtm checklist skip: name one step id, with --reason "..."`)
		return exitFailed
	}
	if err := a.Skip(ids[0], *reason); err != nil {
		fmt.Fprintln(stderr, "mtm checklist skip:", err)
		return exitFailed
	}
	fmt.Fprintf(stdout, "Skipped: %s\n", ids[0])
	return exitOK
}

func openStep(ctx context.Context, a *app.App, args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "mtm checklist open: name one step id")
		return exitFailed
	}
	if err := a.OpenStep(ctx, args[0]); err != nil {
		fmt.Fprintln(stderr, "mtm checklist open:", err)
		return exitFailed
	}
	return exitOK
}

// walk goes through the open steps one at a time: it opens each step's
// System Settings pane and asks what happened, saving every answer at once
// so quitting loses nothing.
func walk(ctx context.Context, a *app.App, in io.Reader, stdout, stderr io.Writer) int {
	l, err := a.Checklist()
	if err != nil {
		fmt.Fprintln(stderr, "mtm checklist:", err)
		return exitFailed
	}
	steps := l.Pending()
	if len(steps) == 0 {
		report.Checklist(stdout, l, false)
		return exitOK
	}
	r := bufio.NewReader(in)
	for i, e := range steps {
		report.Step(stdout, i+1, len(steps), e)
		if e.Link != "" {
			if err := a.OpenStep(ctx, e.ID); err != nil {
				fmt.Fprintln(stdout, "  Could not open it:", err)
			}
		}
		if quit := ask(a, r, e, stdout); quit {
			break
		}
	}
	l, err = a.Checklist()
	if err != nil {
		fmt.Fprintln(stderr, "mtm checklist:", err)
		return exitFailed
	}
	fmt.Fprintln(stdout)
	report.Checklist(stdout, l, false)
	return exitOK
}

// ask reads answers until one settles the step, and reports a quit or the
// end of input.
func ask(a *app.App, r *bufio.Reader, e checklist.Entry, out io.Writer) bool {
	for {
		fmt.Fprint(out, "  [d]one, [s]kip, [l]ater, [q]uit? ")
		answer, err := readLine(r)
		if err != nil {
			return true
		}
		switch strings.ToLower(answer) {
		case "d", "done":
			if err := a.MarkDone(e.ID); err != nil {
				fmt.Fprintln(out, "  ", err)
				continue
			}
			return false
		case "s", "skip":
			fmt.Fprint(out, "  Why doesn't it apply? ")
			reason, err := readLine(r)
			if err != nil {
				return true
			}
			if err := a.Skip(e.ID, reason); err != nil {
				fmt.Fprintln(out, "  ", err)
				continue
			}
			return false
		case "l", "later", "":
			return false
		case "q", "quit":
			return true
		}
	}
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
```

`cmd/mtm/main.go` (the usage lines and the command):
```diff
--- a/cmd/mtm/main.go
+++ b/cmd/mtm/main.go
@@ -46,6 +46,8 @@
   mtm finish [--only a,b]                   on the new Mac: restore what prepare set aside
   mtm finish --only databases --restore-dump [--force]
                                             load the Postgres safety-net dump
+  mtm checklist                             on the new Mac: go through the steps left to do by hand
+  mtm checklist list [--all] [--json] | done <id>... | skip <id> --reason "..." | open <id>
   mtm verify [--only a,b] [--deep] [--details] [--json]
                                             check the new Mac against the record
   mtm cleanup [--force]                     delete ~/.mtm, and the --stage exports, after SAFE TO WIPE
@@ -55,11 +57,12 @@
 type command func(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int
 
 var commands = map[string]command{
-	"scan":    cmdScan,
-	"prepare": cmdPrepare,
-	"finish":  cmdFinish,
-	"verify":  cmdVerify,
-	"cleanup": cmdCleanup,
+	"scan":      cmdScan,
+	"prepare":   cmdPrepare,
+	"finish":    cmdFinish,
+	"checklist": cmdChecklist,
+	"verify":    cmdVerify,
+	"cleanup":   cmdCleanup,
 }
 
 func main() {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/app/ ./internal/report/ ./cmd/mtm/`
Expected: PASS. No test runs `/usr/bin/open` for real: the fakes answer it.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go vet -tags integration ./... && go test ./...
git add internal/app internal/report cmd/mtm
git commit -m "Add mtm checklist

In a terminal it walks through the open steps, opening each one's System
Settings pane and saving every answer at once. Elsewhere it lists them.
done, skip, and open act on a single step. mtm opens only System
Settings links, since state.json is a file anyone with the account
could edit."
```

---

### Task 5: The `permissions` module

**Files:**
- Create: `internal/modules/permissions/permissions.go`
- Test: `internal/modules/permissions/permissions_test.go`

**Interfaces:**
- Consumes:
  - Task 1 (`catalog.Catalog`, `ServiceForTCC`, `ServiceNamed`, `Service.Where`, `Service.Link`);
  - Task 2 (`checklist.Item`);
  - Task 3 (`Result.Checklist`, `Check.Resolves`);
  - `macos.MajorVersion`, `module.LoadInventory`, `words.Count`.
- Produces:
  - `permissions.Name = "permissions"`, `permissions.SystemDB`, `permissions.Module{Catalog, SystemDB}`, and `permissions.New(*catalog.Catalog)`.
  - `permissions.Grant{Service, Client string; Path bool; Target string}`.
  - `permissions.Inventory{Source string ("tcc" or "catalog"); Grants []Grant}`.
  - `Scan`, `Prepare`, `Finish`, and `Verify`, per spec 9.8. It is not an `Undoer`, because prepare changes nothing.
  - Step ids are `permissions:<client>:<pane>`, plus `:<target>` for Automation.
  - Without a record from the privacy databases, the expected grants come from the catalog, for the apps in the `system` module's record (`apps[].bundle_id`).

- [ ] **Step 1: Write the failing tests**

`internal/modules/permissions/permissions_test.go`:
```go
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
	return "/usr/bin/sqlite3 -readonly -json " + db + " " + query
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/permissions/`
Expected: the build fails with `undefined: Module`.

- [ ] **Step 3: Implement**

`internal/modules/permissions/permissions.go`:
```go
// Package permissions records the privacy permissions the old Mac granted
// (Accessibility, Screen Recording, and so on), which Migration Assistant
// doesn't bring along, and turns each into a checklist step on the new Mac.
package permissions

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/catalog"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const Name = "permissions"

// SystemDB is macOS's privacy database for the whole Mac; each user has
// another in their home folder. Reading either needs Full Disk Access.
const SystemDB = "/Library/Application Support/com.apple.TCC/TCC.db"

// query reads only granted permissions (auth_value 2), and only which
// service and client: the rest of a row says nothing mtm needs.
const query = "SELECT service, client, client_type, IFNULL(indirect_object_identifier, '') AS target FROM access WHERE auth_value = 2"

const automation = "kTCCServiceAppleEvents"

type Module struct {
	Catalog  *catalog.Catalog
	SystemDB string
}

func New(c *catalog.Catalog) Module { return Module{Catalog: c, SystemDB: SystemDB} }

func (Module) Name() string { return Name }

// Grant is a granted permission: the service, as the privacy database
// names it, and the client, a bundle id or, with Path, an executable's
// path. Target is the app an Automation grant lets the client control.
type Grant struct {
	Service string `json:"service"`
	Client  string `json:"client"`
	Path    bool   `json:"path,omitempty"`
	Target  string `json:"target,omitempty"`
}

// Inventory has Source "tcc" when prepare read the privacy databases, and
// "catalog" when macOS refused, without Full Disk Access; finish then
// expects what the catalog says the recorded apps need.
type Inventory struct {
	Source string  `json:"source"`
	Grants []Grant `json:"grants,omitempty"`
}

func userDB(home string) string {
	return filepath.Join(home, "Library", "Application Support", "com.apple.TCC", "TCC.db")
}

// read returns the grants in both privacy databases. readable is false
// when macOS refused to open either, as it does without Full Disk Access.
func (m Module) read(env module.Env) (grants []Grant, readable bool, err error) {
	for _, db := range []string{userDB(env.Home), m.SystemDB} {
		gs, ok, err := m.readDB(env, db)
		if err != nil || !ok {
			return nil, false, err
		}
		grants = append(grants, gs...)
	}
	return grants, true, nil
}

func (m Module) readDB(env module.Env, db string) ([]Grant, bool, error) {
	out, err := env.Run.Run(env.Ctx, runner.Command{Name: "/usr/bin/sqlite3", Args: []string{"-readonly", "-json", db, query}})
	var ee *runner.ExitError
	if errors.As(err, &ee) && (strings.Contains(ee.Stderr, "authorization denied") || strings.Contains(ee.Stderr, "unable to open")) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", db, err)
	}
	grants, err := parseGrants(out.Stdout)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", db, err)
	}
	return grants, true, nil
}

// parseGrants reads sqlite3's JSON, which is no output at all for no rows.
// The database fills target with "UNUSED" for every service but
// Automation.
func parseGrants(data []byte) ([]Grant, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var rows []struct {
		Service    string `json:"service"`
		Client     string `json:"client"`
		ClientType int    `json:"client_type"`
		Target     string `json:"target"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parse sqlite3 output: %w", err)
	}
	grants := make([]Grant, 0, len(rows))
	for _, r := range rows {
		g := Grant{Service: r.Service, Client: r.Client, Path: r.ClientType == 1}
		if r.Service == automation {
			g.Target = r.Target
		}
		grants = append(grants, g)
	}
	return grants, nil
}

func noAccess(severity module.Severity, step string) module.Finding {
	return module.Finding{
		Module: Name, ID: "full-disk-access", Severity: severity, Handler: module.Manual,
		Title:  "mtm can't read privacy permissions without Full Disk Access",
		Detail: "It will list the permissions its catalog knows your apps need instead.",
		Fix:    "For the exact list, give your terminal Full Disk Access (System Settings > Privacy & Security > Full Disk Access), then run mtm " + step + " again.",
	}
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	grants, readable, err := m.read(env)
	if err != nil {
		return nil, err
	}
	if !readable {
		return []module.Finding{noAccess(module.SeverityWarn, "scan")}, nil
	}
	return []module.Finding{{
		Module: Name, ID: "grants", Severity: module.SeverityInfo, Handler: module.ByMtm,
		Title:  words.Count(len(grants), "privacy permission", "privacy permissions") + " granted",
		Detail: "finish lists each one to grant again on the new Mac, and verify checks them.",
	}}, nil
}

func (m Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	grants, readable, err := m.read(env)
	if err != nil {
		return module.Result{}, err
	}
	if !readable {
		return module.Result{Inventory: Inventory{Source: "catalog"}, Problems: []module.Finding{noAccess(module.SeverityWarn, "prepare")}}, nil
	}
	return module.Result{
		Inventory: Inventory{Source: "tcc", Grants: grants},
		Done:      []string{"recorded " + words.Count(len(grants), "privacy permission", "privacy permissions")},
	}, nil
}

// recordedApp is the part of the system module's record this module reads:
// which apps the old Mac had, to name clients and to look them up in the
// catalog.
type recordedApp struct {
	Path     string `json:"path"`
	BundleID string `json:"bundle_id"`
}

func recordedApps(env module.Env) []recordedApp {
	var sys struct {
		Apps []recordedApp `json:"apps"`
	}
	// Without a system record there are no names or catalog matches, but
	// grants read from the privacy databases still make steps.
	if _, err := module.LoadInventory(env, "system", &sys); err != nil {
		return nil
	}
	return sys.Apps
}

// expected is the grants the new Mac should have: those recorded, or with
// a catalog record, what the catalog says the old Mac's apps need.
func (m Module) expected(env module.Env, inv Inventory, apps []recordedApp) []Grant {
	if inv.Source == "tcc" {
		return inv.Grants
	}
	var out []Grant
	for _, a := range apps {
		entry, ok := m.Catalog.ByBundleID(a.BundleID)
		if !ok {
			continue
		}
		for _, p := range entry.Permissions {
			svc, _ := catalog.ServiceNamed(p.Service)
			out = append(out, Grant{Service: svc.TCC, Client: p.ClientFor(entry)})
		}
	}
	return out
}

// step turns a grant into a checklist step. Grants for services System
// Settings doesn't show, which macOS uses internally, have none.
func (m Module) step(g Grant, apps []recordedApp, major string) (checklist.Item, bool) {
	svc, ok := catalog.ServiceForTCC(g.Service)
	if !ok {
		return checklist.Item{}, false
	}
	id := Name + ":" + g.Client + ":" + svc.Name
	title := "Give " + m.clientName(g.Client, g.Path, apps) + " " + svc.Name
	if !strings.HasSuffix(svc.Name, "Access") {
		title += " access"
	}
	if g.Target != "" {
		id += ":" + g.Target
		title = "Let " + m.clientName(g.Client, g.Path, apps) + " control " + m.clientName(g.Target, false, apps) + " (Automation)"
	}
	return checklist.Item{ID: id, Module: Name, Title: title, Where: svc.Where(), Link: svc.Link(major)}, true
}

func (m Module) clientName(client string, path bool, apps []recordedApp) string {
	if path {
		return filepath.Base(client)
	}
	if entry, ok := m.Catalog.ByBundleID(client); ok {
		return entry.Name
	}
	if i := slices.IndexFunc(apps, func(a recordedApp) bool { return a.BundleID == client }); i >= 0 {
		return strings.TrimSuffix(filepath.Base(apps[i].Path), ".app")
	}
	return client
}

// steps are the checklist steps for the expected grants, one per grant.
func (m Module) steps(env module.Env, inv Inventory) ([]checklist.Item, []Grant, error) {
	major, err := macos.MajorVersion(env.Ctx, env.Run)
	if err != nil {
		return nil, nil, err
	}
	apps := recordedApps(env)
	var items []checklist.Item
	var grants []Grant
	for _, g := range m.expected(env, inv, apps) {
		it, ok := m.step(g, apps, major)
		if !ok || slices.ContainsFunc(items, func(o checklist.Item) bool { return o.ID == it.ID }) {
			continue
		}
		items = append(items, it)
		grants = append(grants, g)
	}
	return items, grants, nil
}

func (m Module) Finish(env module.Env, _ *stage.Stage) (module.Result, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return module.Result{}, err
	}
	items, _, err := m.steps(env, inv)
	if err != nil || len(items) == 0 {
		return module.Result{}, err
	}
	return module.Result{
		Checklist: items,
		Done:      []string{"listed " + words.Count(len(items), "privacy permission", "privacy permissions") + " to grant again"},
	}, nil
}

// Verify can check the grants only with Full Disk Access on this Mac too.
// Granted ones mark their checklist steps done; the user marks the rest.
func (m Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	items, grants, err := m.steps(env, inv)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	now, readable, err := m.read(env)
	if err != nil {
		return nil, err
	}
	if !readable {
		return []module.Check{{Module: Name, ID: Name + ":unreadable", Status: module.StatusInfo, Title: "privacy permissions",
			Detail: "mtm can't read them without Full Disk Access; mark each step done in mtm checklist once you've granted it"}}, nil
	}
	var granted, missing []string
	for i, g := range grants {
		if slices.Contains(now, g) {
			granted = append(granted, items[i].ID)
		} else {
			missing = append(missing, items[i].Title)
		}
	}
	var checks []module.Check
	if len(granted) > 0 {
		checks = append(checks, module.Check{Module: Name, ID: Name + ":granted", Status: module.StatusPass, Title: "privacy permissions granted again",
			Detail: words.Count(len(granted), "permission is", "permissions are") + " granted on this Mac", Resolves: granted})
	}
	if len(missing) > 0 {
		checks = append(checks, module.Check{Module: Name, ID: Name + ":missing", Status: module.StatusWarn, Title: "privacy permissions not granted yet",
			Detail: words.Count(len(missing), "permission is", "permissions are") + " missing; run mtm checklist", Paths: missing})
	}
	return checks, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/modules/permissions/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go vet -tags integration ./... && go test ./...
git add internal/modules/permissions
git commit -m "Add the permissions module

Migration Assistant doesn't carry privacy permissions over, so each
granted one becomes a checklist step with its System Settings pane.
With Full Disk Access, mtm reads the old Mac's grants, read-only, and
verify marks steps done as it finds them granted again. Without it, the
catalog says what the old Mac's apps need."
```

---

### Task 6: Steps from the catalog for apps and login items, and its volatile paths

**Files:**
- Modify: `internal/modules/system/system.go`, `internal/modules/loginitems/loginitems.go`, `internal/modules/files/module.go`, `internal/modules/files/verify.go`, `cmd/mtm/main.go`
- Test: `internal/modules/system/system_test.go`, `internal/modules/loginitems/loginitems_test.go`, `internal/modules/files/module_test.go`

**Interfaces:**
- Consumes: Task 1 (`catalog.Catalog`, `ByBundleID`, `Volatile`, `LoginItemsLink`); Task 2 (`checklist.Item`); Task 3 (`Result.Checklist`).
- Produces:
  - `system.Module.Catalog *catalog.Catalog` and `system.New(user string, c *catalog.Catalog)`.
    - `system.Finish` adds `system:license:<id>`, titled "Reactivate the <Name> license", and `system:note:<id>:<n>`, titled "<Name>: <note>", for the recorded apps.
  - `loginitems.Finish` adds `loginitems:<name>`, titled "Add <name> to Login Items again", with the Login Items link.
    - When prepare couldn't list the login items, it adds `loginitems:from-old-mac` instead.
    - The earlier "Add your login items back" Problem is removed.
  - `files.New(volatile ...string)`: the extra volatile patterns. `files.New()` keeps working.
  - `cmd/mtm` `builtinCatalog()`, which panics when the built-in catalog is invalid: a broken build, which the Task 1 tests prevent.

- [ ] **Step 1: Write the failing tests**

`internal/modules/system/system_test.go`:
```diff
--- a/internal/modules/system/system_test.go
+++ b/internal/modules/system/system_test.go
@@ -8,9 +8,12 @@
 	"slices"
 	"strings"
 	"testing"
+	"testing/fstest"
 	"time"
 
 	"github.com/momentmaker/mtm/internal/apps"
+	"github.com/momentmaker/mtm/internal/catalog"
+	"github.com/momentmaker/mtm/internal/checklist"
 	"github.com/momentmaker/mtm/internal/macos"
 	"github.com/momentmaker/mtm/internal/manifest"
 	"github.com/momentmaker/mtm/internal/module"
@@ -318,5 +321,32 @@
 	}
 	if want := "sudo install -m 0644 -o root -g wheel " + macos.ShellQuote(st.Path("exports", "etc", "hosts")) + " '/etc/hosts'"; !strings.Contains(fixesByID(res)["etc:hosts"], want) {
 		t.Errorf("hosts fix = %q, want it to contain %q", fixesByID(res)["etc:hosts"], want)
+	}
+}
+
+func TestFinishListsTheCatalogsStepsForRecordedApps(t *testing.T) {
+	f := newFixture(t)
+	f.addApp(t, "Dash.app", "com.kapeli.dashdoc", "7.3", "Dash", "arm64")
+	f.addApp(t, "Signal.app", "org.whispersystems.signal-desktop", "7.80", "Signal", "arm64")
+	m, st, _ := f.prepare(t)
+	c, err := catalog.Load(fstest.MapFS{
+		"dash.toml":   {Data: []byte("id = \"dash\"\nname = \"Dash\"\nbundle_id = \"com.kapeli.dashdoc\"\n\n[license]\nreactivate = true\nnote = \"Enter your license.\"\n")},
+		"signal.toml": {Data: []byte("id = \"signal\"\nname = \"Signal\"\nbundle_id = \"org.whispersystems.signal-desktop\"\nnotes = [\"Link it again.\"]\n")},
+	})
+	if err != nil {
+		t.Fatal(err)
 	}
+	mod := f.module()
+	mod.Catalog = c
+	res, err := mod.Finish(f.env(m), st)
+	if err != nil {
+		t.Fatal(err)
+	}
+	want := []checklist.Item{
+		{ID: "system:license:dash", Module: Name, Title: "Reactivate the Dash license", Detail: "Enter your license.", Where: "Dash"},
+		{ID: "system:note:signal:1", Module: Name, Title: "Signal: Link it again.", Where: "Signal"},
+	}
+	if !slices.Equal(res.Checklist, want) {
+		t.Errorf("checklist = %+v\nwant %+v", res.Checklist, want)
+	}
 }
```

`internal/modules/loginitems/loginitems_test.go`:
```diff
--- a/internal/modules/loginitems/loginitems_test.go
+++ b/internal/modules/loginitems/loginitems_test.go
@@ -8,6 +8,7 @@
 	"testing"
 	"time"
 
+	"github.com/momentmaker/mtm/internal/checklist"
 	"github.com/momentmaker/mtm/internal/manifest"
 	"github.com/momentmaker/mtm/internal/module"
 	"github.com/momentmaker/mtm/internal/runner"
@@ -27,7 +28,8 @@
 func newFixture(t *testing.T) fixture {
 	t.Helper()
 	f := fixture{home: filepath.Join(t.TempDir(), "my home"), fake: &runner.Fake{Responses: map[string]runner.Response{
-		"/bin/launchctl list": {Stdout: launchctlList},
+		"/bin/launchctl list":              {Stdout: launchctlList},
+		"/usr/bin/sw_vers -productVersion": {Stdout: "26.7.1\n"},
 	}}}
 	dir := filepath.Join(f.home, "Library", "LaunchAgents")
 	for file, label := range map[string]string{"a.plist": "com.a", "b.plist": "com.b"} {
@@ -104,9 +106,14 @@
 	if !slices.Equal(res.Done, []string{"loaded com.a"}) {
 		t.Errorf("done = %q", res.Done)
 	}
-	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Detail, "Raycast") {
-		t.Errorf("problems = %+v", res.Problems)
+	if len(res.Problems) != 0 {
+		t.Errorf("problems = %+v, want login items as checklist steps instead", res.Problems)
 	}
+	want := []checklist.Item{{ID: "loginitems:Raycast", Module: Name, Title: "Add Raycast to Login Items again",
+		Where: "System Settings > General > Login Items & Extensions", Link: "x-apple.systempreferences:com.apple.LoginItems-Settings.extension"}}
+	if !slices.Equal(res.Checklist, want) {
+		t.Errorf("checklist = %+v, want %+v", res.Checklist, want)
+	}
 }
 
 func TestFinishBootstrapFailureIsByHand(t *testing.T) {
@@ -127,8 +134,8 @@
 func TestFinishWhenLoginItemsWereNotListed(t *testing.T) {
 	f := newFixture(t)
 	res, err := New(uid).Finish(f.env(record(t, Inventory{})), nil)
-	if err != nil || len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Detail, "couldn't list them") {
-		t.Errorf("Finish = %+v, %v", res, err)
+	if err != nil || len(res.Checklist) != 1 || res.Checklist[0].ID != "loginitems:from-old-mac" {
+		t.Errorf("Finish = %+v, %v; want one step to note the login items on the old Mac", res, err)
 	}
 }
 
```

`internal/modules/files/module_test.go`:
```diff
--- a/internal/modules/files/module_test.go
+++ b/internal/modules/files/module_test.go
@@ -389,5 +389,37 @@
 	}
 	if len(findings) != 1 || findings[0].ID != "home" {
 		t.Errorf("findings = %+v, want only the home info finding", findings)
+	}
+}
+
+func TestCatalogVolatilePathsMissingAfterMigrationAreInfo(t *testing.T) {
+	home := seedHome(t)
+	testutil.WriteFile(t, filepath.Join(home, "Library/Application Support/Code/logs/main.log"), "log\n")
+	m := prepareHome(t, home)
+	if err := os.Remove(filepath.Join(home, "Library/Application Support/Code/logs/main.log")); err != nil {
+		t.Fatal(err)
+	}
+	st, err := stage.Existing(home)
+	if err != nil {
+		t.Fatal(err)
+	}
+	group := func(mod Module) module.Check {
+		checks, err := mod.Verify(module.Env{Ctx: context.Background(), Home: home, Manifest: m}, st)
+		if err != nil {
+			t.Fatal(err)
+		}
+		for _, c := range checks {
+			if c.Title == "Library/Application Support" {
+				return c
+			}
+		}
+		t.Fatalf("no Library/Application Support check in %+v", checks)
+		return module.Check{}
 	}
+	if c := group(New()); c.Status != module.StatusWarn {
+		t.Errorf("without the catalog pattern: %+v, want warn", c)
+	}
+	if c := group(New("~/Library/Application Support/Code/logs/")); c.Status != module.StatusPass {
+		t.Errorf("with the catalog pattern: %+v, want pass", c)
+	}
 }
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/system/ ./internal/modules/loginitems/ ./internal/modules/files/`
Expected:
- the system tests don't build (`mod.Catalog undefined`);
- the loginitems Finish tests fail (a Problem where a step is expected);
- the files test doesn't build (too many arguments to `New`).

- [ ] **Step 3: Implement**

`internal/modules/system/system.go`:
```diff
--- a/internal/modules/system/system.go
+++ b/internal/modules/system/system.go
@@ -15,6 +15,8 @@
 	"strings"
 
 	"github.com/momentmaker/mtm/internal/apps"
+	"github.com/momentmaker/mtm/internal/catalog"
+	"github.com/momentmaker/mtm/internal/checklist"
 	"github.com/momentmaker/mtm/internal/fsinfo"
 	"github.com/momentmaker/mtm/internal/macos"
 	"github.com/momentmaker/mtm/internal/module"
@@ -31,15 +33,18 @@
 // /etc/paths.d is added when recording.
 var etcFiles = []string{"hosts", "shells", "pam.d/sudo_local"}
 
+// Module records the login shell, /etc files, and apps. Catalog supplies
+// the by-hand steps finish lists for the apps.
 type Module struct {
 	User        string
 	EtcDir      string
 	AppRoots    func(home string) []apps.Root
 	RosettaPath string
+	Catalog     *catalog.Catalog
 }
 
-func New(user string) Module {
-	return Module{User: user, EtcDir: "/etc", AppRoots: DefaultAppRoots, RosettaPath: macos.RosettaPath}
+func New(user string, c *catalog.Catalog) Module {
+	return Module{User: user, EtcDir: "/etc", AppRoots: DefaultAppRoots, RosettaPath: macos.RosettaPath, Catalog: c}
 }
 
 func DefaultAppRoots(home string) []apps.Root {
@@ -220,7 +225,32 @@
 	if len(res.Problems) == 0 {
 		res.Done = []string{"login shell, /etc files, and Rosetta are as they were"}
 	}
+	res.Checklist = m.appSteps(old.Apps)
 	return res, nil
+}
+
+// appSteps are what the catalog says the old Mac's apps need by hand on a
+// new Mac: a license to enter again, and notes such as signing in again.
+func (m Module) appSteps(installed []apps.App) []checklist.Item {
+	if m.Catalog == nil {
+		return nil
+	}
+	var items []checklist.Item
+	for _, a := range installed {
+		entry, ok := m.Catalog.ByBundleID(a.BundleID)
+		if !ok {
+			continue
+		}
+		if entry.License.Reactivate {
+			items = append(items, checklist.Item{ID: Name + ":license:" + entry.ID, Module: Name,
+				Title: "Reactivate the " + entry.Name + " license", Detail: entry.License.Note, Where: entry.Name})
+		}
+		for i, note := range entry.Notes {
+			items = append(items, checklist.Item{ID: fmt.Sprintf("%s:note:%s:%d", Name, entry.ID, i+1), Module: Name,
+				Title: entry.Name + ": " + note, Where: entry.Name})
+		}
+	}
+	return items
 }
 
 // restoreEtc copies an exported /etc file back as root-owned and readable
```

`internal/modules/loginitems/loginitems.go`:
```diff
--- a/internal/modules/loginitems/loginitems.go
+++ b/internal/modules/loginitems/loginitems.go
@@ -11,6 +11,8 @@
 	"strconv"
 	"strings"
 
+	"github.com/momentmaker/mtm/internal/catalog"
+	"github.com/momentmaker/mtm/internal/checklist"
 	"github.com/momentmaker/mtm/internal/macos"
 	"github.com/momentmaker/mtm/internal/module"
 	"github.com/momentmaker/mtm/internal/runner"
@@ -20,6 +22,7 @@
 
 const (
 	Name             = "loginitems"
+	loginItemsPane   = "System Settings > General > Login Items & Extensions"
 	loginItemsScript = `tell application "System Events" to get the name of every login item`
 )
 
@@ -187,21 +190,33 @@
 	if len(ok) > 0 {
 		res.Done = append(res.Done, "loaded "+strings.Join(ok, ", "))
 	}
-	switch {
-	case !old.LoginItemsListed:
-		res.Problems = append(res.Problems, addBack("mtm couldn't list them on the old Mac; check the list there before you erase it."))
-	case len(old.LoginItems) > 0:
-		res.Problems = append(res.Problems, addBack("Add: "+strings.Join(old.LoginItems, ", ")))
+	items, err := loginItemSteps(env, old)
+	if err != nil {
+		return module.Result{}, err
 	}
+	res.Checklist = items
 	return res, nil
 }
 
-func addBack(detail string) module.Finding {
-	return module.Finding{
-		Module: Name, ID: "login-items", Severity: module.SeverityWarn, Handler: module.Manual,
-		Title: "Add your login items back", Detail: detail,
-		Fix: "System Settings > General > Login Items > Open at Login",
+// loginItemSteps asks the user to add each login item back: macOS keeps
+// the list outside the files Migration Assistant copies. When prepare
+// couldn't read the list, the step is to read it on the old Mac first.
+func loginItemSteps(env module.Env, old Inventory) ([]checklist.Item, error) {
+	major, err := macos.MajorVersion(env.Ctx, env.Run)
+	if err != nil {
+		return nil, err
 	}
+	step := func(id, title string) checklist.Item {
+		return checklist.Item{ID: Name + ":" + id, Module: Name, Title: title, Where: loginItemsPane, Link: catalog.LoginItemsLink(major)}
+	}
+	if !old.LoginItemsListed {
+		return []checklist.Item{step("from-old-mac", "Note your login items on the old Mac and add them here")}, nil
+	}
+	var items []checklist.Item
+	for _, name := range old.LoginItems {
+		items = append(items, step(name, "Add "+name+" to Login Items again"))
+	}
+	return items, nil
 }
 
 func (Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
```

`internal/modules/files/module.go`:
```diff
--- a/internal/modules/files/module.go
+++ b/internal/modules/files/module.go
@@ -8,6 +8,7 @@
 
 	"github.com/momentmaker/mtm/internal/manifest"
 	"github.com/momentmaker/mtm/internal/module"
+	"github.com/momentmaker/mtm/internal/pathset"
 	"github.com/momentmaker/mtm/internal/stage"
 )
 
@@ -16,10 +17,21 @@
 	maxListed = 20
 )
 
-type Module struct{}
+// Module records and checks the home folder. extraVolatile holds the
+// catalog's paths that apps create and delete on their own, on top of the
+// built-in volatile list.
+type Module struct {
+	extraVolatile pathset.Set
+}
 
-func New() Module { return Module{} }
+// New takes the catalog's volatile patterns, which its tests check, so
+// they always compile.
+func New(volatile ...string) Module { return Module{extraVolatile: pathset.MustNew(volatile...)} }
 
+func (m Module) isVolatile(rel string) bool {
+	return volatile.Match(rel) || m.extraVolatile.Match(rel)
+}
+
 func (Module) Name() string { return Name }
 
 func (Module) Prepare(env module.Env, st *stage.Stage) (module.Result, error) {
```

`internal/modules/files/verify.go`:
```diff
--- a/internal/modules/files/verify.go
+++ b/internal/modules/files/verify.go
@@ -21,7 +21,7 @@
 	maxPathsPerGroup = 200
 )
 
-func (Module) Verify(env module.Env, st *stage.Stage) ([]module.Check, error) {
+func (m Module) Verify(env module.Env, st *stage.Stage) ([]module.Check, error) {
 	var stats Stats
 	preparedAt, err := module.LoadInventory(env, Name, &stats)
 	if err != nil {
@@ -55,7 +55,7 @@
 		if err != nil {
 			return nil, err
 		}
-		t.add(rec, Classify(rec, obs, prepared, volatile.Match))
+		t.add(rec, Classify(rec, obs, prepared, m.isVolatile))
 	}
 	checks := t.checks()
 	if read != stats.Entries {
```

`cmd/mtm/main.go` (`system.New` now takes the catalog, and `files.New` takes its volatile paths):
```diff
--- a/cmd/mtm/main.go
+++ b/cmd/mtm/main.go
@@ -14,6 +14,7 @@
 	"time"
 
 	"github.com/momentmaker/mtm/internal/app"
+	"github.com/momentmaker/mtm/internal/catalog"
 	"github.com/momentmaker/mtm/internal/module"
 	"github.com/momentmaker/mtm/internal/modules/databases"
 	"github.com/momentmaker/mtm/internal/modules/docker"
@@ -76,18 +77,30 @@
 // running, and homebrew's prepare stops it.
 func defaultApp(home string) *app.App {
 	name := currentUser()
+	cat := builtinCatalog()
 	return &app.App{
 		Runner:  runner.Exec{},
 		Home:    home,
 		Version: version,
 		Now:     time.Now,
 		Modules: []module.Module{
-			files.New(), git.New(), system.New(name), databases.New(), docker.New(),
+			files.New(cat.Volatile()...), git.New(), system.New(name, cat), databases.New(), docker.New(),
 			homebrew.New(name), toolchains.New(name), loginitems.New(os.Getuid()),
 		},
 	}
 }
 
+// builtinCatalog loads the app catalog built into mtm. internal/catalog's
+// tests check every file in it, so a failure here is a broken build, not
+// something a user can fix.
+func builtinCatalog() *catalog.Catalog {
+	c, err := catalog.Default()
+	if err != nil {
+		panic("mtm's built-in app catalog is invalid: " + err.Error())
+	}
+	return c
+}
+
 // currentUser is the account name Directory Services knows the user by.
 func currentUser() string {
 	if u, err := user.Current(); err == nil {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/modules/system/ ./internal/modules/loginitems/ ./internal/modules/files/ ./cmd/mtm/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go vet -tags integration ./... && go test ./...
git add internal/modules/system internal/modules/loginitems internal/modules/files cmd/mtm
git commit -m "Turn app licenses, notes, and login items into checklist steps

The catalog's steps for the old Mac's apps (a license to enter again,
signing in again) and each login item to add back are now checklist
steps that verify waits for, not lines printed once by finish. Paths the
catalog says apps delete on their own count as info when missing."
```

---

### Task 7: Wire the permissions module

**Files:**
- Modify: `cmd/mtm/main.go`, `cmd/mtm/main_test.go`, `cmd/mtm/nonetwork_integration_test.go`

**Interfaces:**
- Consumes: `permissions.New` (Task 5); `builtinCatalog` (Task 6).
- Produces: `defaultApp` registers modules in this order: `files`, `git`, `system`, `databases`, `docker`, `homebrew`, `toolchains`, `loginitems`, `permissions`.

- [ ] **Step 1: Write the failing test**

`cmd/mtm/main_test.go`:
```diff
--- a/cmd/mtm/main_test.go
+++ b/cmd/mtm/main_test.go
@@ -247,7 +247,7 @@
 	for _, m := range defaultApp(t.TempDir()).Modules {
 		names = append(names, m.Name())
 	}
-	if want := []string{"files", "git", "system", "databases", "docker", "homebrew", "toolchains", "loginitems"}; !slices.Equal(names, want) {
+	if want := []string{"files", "git", "system", "databases", "docker", "homebrew", "toolchains", "loginitems", "permissions"}; !slices.Equal(names, want) {
 		t.Errorf("modules = %q, want %q", names, want)
 	}
 }
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/mtm/ -run TestDefaultAppModules`
Expected: FAIL. The list has no `permissions`.

- [ ] **Step 3: Wire it**

`cmd/mtm/main.go`:
```diff
--- a/cmd/mtm/main.go
+++ b/cmd/mtm/main.go
@@ -22,6 +22,7 @@
 	"github.com/momentmaker/mtm/internal/modules/git"
 	"github.com/momentmaker/mtm/internal/modules/homebrew"
 	"github.com/momentmaker/mtm/internal/modules/loginitems"
+	"github.com/momentmaker/mtm/internal/modules/permissions"
 	"github.com/momentmaker/mtm/internal/modules/system"
 	"github.com/momentmaker/mtm/internal/modules/toolchains"
 	"github.com/momentmaker/mtm/internal/report"
@@ -85,7 +86,7 @@
 		Now:     time.Now,
 		Modules: []module.Module{
 			files.New(cat.Volatile()...), git.New(), system.New(name, cat), databases.New(), docker.New(),
-			homebrew.New(name), toolchains.New(name), loginitems.New(os.Getuid()),
+			homebrew.New(name), toolchains.New(name), loginitems.New(os.Getuid()), permissions.New(cat),
 		},
 	}
 }
```

`cmd/mtm/nonetwork_integration_test.go` (prepare now covers `permissions` too):
```diff
--- a/cmd/mtm/nonetwork_integration_test.go
+++ b/cmd/mtm/nonetwork_integration_test.go
@@ -129,9 +129,9 @@
 		brewfile bool
 	}{
 		{[]string{"scan"}, []int{0, 1}, false},
-		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains"}, []int{0, 1}, true},
+		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions"}, []int{0, 1}, true},
 		{[]string{"prepare", "--undo"}, []int{0, 1}, false},
-		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains"}, []int{0, 1}, true},
+		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions"}, []int{0, 1}, true},
 		{[]string{"verify"}, []int{1}, false},
 	}
 	for _, s := range steps {
```

- [ ] **Step 4: Run every check**

```bash
gofmt -l .
go vet ./... && go vet -tags integration ./...
go test -race ./...
go test -tags integration -run TestNoNetwork -v ./cmd/mtm/
```
Expected:
- `gofmt` prints nothing, and `go vet` and `go test` pass.
- `TestNoNetwork` passes, or skips with its stated reason. Report which. It passed on the author's Mac in about 36 seconds.
- Inside the sandbox, with a temporary home folder, macOS refuses the privacy databases. `permissions` then records the catalog source and warns, which the test allows.

- [ ] **Step 5: Commit**

```bash
git add cmd/mtm
git commit -m "Wire the permissions module into mtm

It runs last, after loginitems, so its steps follow the others in the
checklist, and the no-network test now prepares it offline too."
```

Don't edit `IMPLEMENTATION_PLAN.md`. The controller records Part 1's status and known gaps after the final review.
