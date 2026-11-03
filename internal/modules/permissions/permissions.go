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
