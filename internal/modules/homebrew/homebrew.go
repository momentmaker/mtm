// Package homebrew stops running Homebrew services so their data copies
// whole, starts them again afterwards, and checks every package arrived.
package homebrew

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const (
	Name         = "homebrew"
	BrewfileName = "Brewfile"
)

// brewEnv stops Homebrew updating itself, sending analytics, printing
// hints, and cleaning up, so brew changes nothing on mtm's behalf. It
// doesn't stop brew downloading package data it lacks; checkCache covers
// that before any other brew command runs.
var brewEnv = []string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ANALYTICS=1", "HOMEBREW_NO_ENV_HINTS=1", "HOMEBREW_NO_INSTALL_CLEANUP=1"}

type Module struct {
	Brew string
	User string
}

func New(user string) Module { return Module{Brew: "/opt/homebrew/bin/brew", User: user} }

func (Module) Name() string { return Name }

type Service struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	User   string `json:"user,omitempty"`
	File   string `json:"file,omitempty"`
}

// Inventory records which services prepare stopped (Stopped) and which ran
// as root and must be stopped and started by hand (Root). RootLabels maps
// each root service to its launchd label, so verify can ask launchd
// whether it runs again.
type Inventory struct {
	Installed  bool              `json:"installed"`
	Services   []Service         `json:"services,omitempty"`
	Stopped    []string          `json:"stopped,omitempty"`
	Root       []string          `json:"root,omitempty"`
	RootLabels map[string]string `json:"root_labels,omitempty"`
	Brewfile   bool              `json:"brewfile"`
}

func (m Module) installed() bool {
	_, err := os.Stat(m.Brew)
	return err == nil
}

func (m Module) brew(env module.Env, args ...string) (runner.Output, error) {
	return env.Run.Run(env.Ctx, runner.Command{Name: m.Brew, Args: args, Env: brewEnv})
}

const noCacheTitle = "Homebrew's package data isn't cached on this Mac, so brew would download it"

func noCacheFix(step string) string {
	return "Run brew update yourself (it uses the network), then run mtm " + step + " again."
}

func noCache(severity module.Severity, step string, err error) module.Finding {
	return module.Finding{
		Module: Name, ID: "cache", Severity: severity, Handler: module.Manual,
		Title: noCacheTitle, Detail: err.Error(), Fix: noCacheFix(step),
	}
}

// describe is a finding as one sentence, for steps that report a problem
// as an error or a check's detail.
func describe(f module.Finding) string {
	return fmt.Sprintf("%s (%s). %s", f.Title, f.Detail, f.Fix)
}

// checkCache runs before any other brew command. With its package data
// missing, brew downloads it, even with auto-update off, and mtm must not
// reach the network (spec 6.2). brew --cache reads no package data.
func (m Module) checkCache(env module.Env) error {
	out, err := m.brew(env, "--cache")
	if err != nil {
		return fmt.Errorf("find Homebrew's cache: %w", err)
	}
	cache := strings.TrimSpace(string(out.Stdout))
	if !filepath.IsAbs(cache) {
		return fmt.Errorf("brew --cache printed %q, not a folder", cache)
	}
	api := filepath.Join(cache, "api")
	if !hasPackageData(api) {
		return fmt.Errorf("no package data in %s", api)
	}
	return nil
}

// hasPackageData looks for a non-empty *.jws.json in api or one folder
// below it, where Homebrew 7 keeps internal/packages.<tag>.jws.json. A
// folder it can't read counts as empty, which mtm reports.
func hasPackageData(api string) bool {
	dirs := []string{api}
	entries, _ := os.ReadDir(api)
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(api, e.Name()))
		}
	}
	return slices.ContainsFunc(dirs, hasSignedData)
}

func hasSignedData(dir string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jws.json") {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return true
		}
	}
	return false
}

func (m Module) services(env module.Env) ([]Service, error) {
	out, err := m.brew(env, "services", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("list Homebrew services: %w", err)
	}
	return parseServices(out.Stdout)
}

func parseServices(out []byte) ([]Service, error) {
	var raw []struct {
		Name   string  `json:"name"`
		Status string  `json:"status"`
		User   *string `json:"user"`
		File   string  `json:"file"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse brew services list: %w", err)
	}
	services := make([]Service, 0, len(raw))
	for _, r := range raw {
		s := Service{Name: r.Name, Status: r.Status, File: r.File}
		if r.User != nil {
			s.User = *r.User
		}
		services = append(services, s)
	}
	return services, nil
}

func started(services []Service, name string) bool {
	return slices.ContainsFunc(services, func(s Service) bool { return s.Name == name && s.Status == "started" })
}

// owns reports a service that runs as this user. brew services run leaves
// no plist behind to name a user, so Homebrew reports none.
func (m Module) owns(s Service) bool { return s.User == "" || s.User == m.User }

// active reports a service launchd has loaded: Homebrew says "started"
// while it runs, "scheduled" between timed runs, and "error" once it has
// exited, when KeepAlive can start it again mid-copy.
func active(s Service) bool { return s.Status != "none" && s.Status != "stopped" }

// mine lists the active services that run as this user, which mtm can
// stop itself.
func (m Module) mine(services []Service) []string {
	var names []string
	for _, s := range services {
		if m.owns(s) && active(s) {
			names = append(names, s.Name)
		}
	}
	return names
}

// label is a service's launchd label, which Homebrew uses as its plist's
// file name. It is "" when brew reports no plist.
func label(s Service) string {
	if s.File == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(s.File), ".plist")
}

// loadedAsRoot asks launchd whether it has label loaded in the system
// domain. brew services list, run as the user, can't see that domain, so
// the status it reports for a root service means nothing.
func loadedAsRoot(env module.Env, label string) bool {
	if label == "" {
		return false
	}
	_, err := env.Run.Run(env.Ctx, runner.Command{Name: "/bin/launchctl", Args: []string{"print", "system/" + label}})
	return err == nil
}

// loadedRoot returns the services that run as root or another user and
// that launchd has loaded, with their labels. One without a plist path
// can't be looked up, so it counts as loaded rather than be copied
// mid-write.
func (m Module) loadedRoot(env module.Env, services []Service) ([]string, map[string]string) {
	var names []string
	labels := map[string]string{}
	for _, s := range services {
		if m.owns(s) {
			continue
		}
		l := label(s)
		if l == "" || loadedAsRoot(env, l) {
			names = append(names, s.Name)
			labels[s.Name] = l
		}
	}
	return names, labels
}

// stillStopped keeps names an earlier prepare recorded that aren't running
// now, so finish restarts them even after prepare runs again.
func stillStopped(earlier, running []string) []string {
	var out []string
	for _, name := range earlier {
		if !slices.Contains(running, name) {
			out = append(out, name)
		}
	}
	return out
}

func sudo(verb string, names []string) string {
	cmds := make([]string, len(names))
	for i, n := range names {
		cmds[i] = "sudo brew services " + verb + " " + macos.ShellQuote(n)
	}
	return strings.Join(cmds, "; ")
}

func rootRunning(severity module.Severity, name string) module.Finding {
	return module.Finding{
		Module: Name, ID: "root:" + name, Severity: severity, Handler: module.Manual,
		Title: name + " runs as root; stop it before Migration Assistant",
		Fix:   "Run: sudo brew services stop " + macos.ShellQuote(name) + ", then run mtm prepare again",
	}
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	if !m.installed() {
		return []module.Finding{{Module: Name, ID: "not-installed", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant, Title: "Homebrew is not installed"}}, nil
	}
	findings := []module.Finding{{
		Module: Name, ID: "homebrew", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
		Title:  "Homebrew in " + filepath.Dir(filepath.Dir(m.Brew)),
		Detail: "Migration Assistant copies it. mtm saves a Brewfile and checks every package arrived.",
	}}
	if err := m.checkCache(env); err != nil {
		return append(findings, noCache(module.SeverityWarn, "scan", err)), nil
	}
	services, err := m.services(env)
	if err != nil {
		return nil, err
	}
	if mine := m.mine(services); len(mine) > 0 {
		findings = append(findings, module.Finding{
			Module: Name, ID: "services", Severity: module.SeverityInfo, Handler: module.ByMtm,
			Title:  words.Count(len(mine), "running service", "running services") + ": " + strings.Join(mine, ", "),
			Detail: "prepare stops them so their data is copied whole; finish starts them on the new Mac.",
		})
	}
	root, _ := m.loadedRoot(env, services)
	for _, name := range root {
		findings = append(findings, rootRunning(module.SeverityWarn, name))
	}
	return findings, nil
}

func (m Module) Prepare(env module.Env, st *stage.Stage) (module.Result, error) {
	if !m.installed() {
		return module.Result{Inventory: Inventory{}, Done: []string{"Homebrew is not installed"}}, nil
	}
	var previous Inventory
	_, err := module.LoadInventory(env, Name, &previous)
	if err != nil && !errors.Is(err, module.ErrNotPrepared) {
		return module.Result{}, err
	}
	recorded := err == nil
	if err := m.checkCache(env); err != nil {
		return keepPrevious(previous, recorded, noCache(module.SeverityBlocker, "prepare", err))
	}
	services, err := m.services(env)
	if err != nil {
		return keepPrevious(previous, recorded, module.Finding{
			Module: Name, ID: "services", Severity: module.SeverityBlocker, Handler: module.Manual,
			Title: "Could not list Homebrew services", Detail: err.Error(),
			Fix: "mtm can't tell which services to stop. Check that brew services list works, then run mtm prepare again.",
		})
	}
	inv := Inventory{Installed: true, Services: services}
	var res module.Result
	if err := m.saveBrewfile(env, st); err != nil {
		res.Problems = append(res.Problems, module.Finding{
			Module: Name, ID: "brewfile", Severity: module.SeverityWarn, Handler: module.ByMtm,
			Title: "Could not save a Brewfile", Detail: err.Error(),
			Fix: "verify can't list missing packages, but Migration Assistant still copies Homebrew.",
		})
	} else {
		inv.Brewfile = true
		res.Done = append(res.Done, "saved a Brewfile")
	}
	mine := m.mine(services)
	inv.Stopped = stillStopped(previous.Stopped, mine)
	for _, name := range mine {
		if _, err := m.brew(env, "services", "stop", name); err != nil {
			res.Problems = append(res.Problems, module.Finding{
				Module: Name, ID: "stop:" + name, Severity: module.SeverityBlocker, Handler: module.Manual,
				Title: "Could not stop " + name, Detail: err.Error(),
				Fix: "Run: brew services stop " + macos.ShellQuote(name) + ", then run mtm prepare again. A running service's data could be copied mid-write.",
			})
			continue
		}
		inv.Stopped = append(inv.Stopped, name)
	}
	if len(inv.Stopped) > 0 {
		res.Done = append(res.Done, "stopped "+strings.Join(inv.Stopped, ", "))
	}
	root, labels := m.loadedRoot(env, services)
	carried := stillStopped(previous.Root, root)
	for _, name := range carried {
		labels[name] = previous.RootLabels[name]
	}
	inv.Root, inv.RootLabels = append(carried, root...), labels
	for _, name := range root {
		res.Problems = append(res.Problems, rootRunning(module.SeverityBlocker, name))
	}
	res.Inventory = inv
	return res, nil
}

// keepPrevious ends prepare with problem but keeps the earlier record, so
// finish still starts what an earlier run stopped. With no earlier record
// it fails instead: an empty record would read as "Homebrew not
// installed", and verify would skip Homebrew.
func keepPrevious(previous Inventory, recorded bool, problem module.Finding) (module.Result, error) {
	if !recorded {
		return module.Result{}, errors.New(describe(problem))
	}
	return module.Result{Inventory: previous, Problems: []module.Finding{problem}}, nil
}

// saveBrewfile passes the type flags so brew bundle doesn't ask cargo, go,
// npm, or VS Code for their packages; cargo's rustup reaches the network.
func (m Module) saveBrewfile(env module.Env, st *stage.Stage) error {
	out, err := m.brew(env, "bundle", "dump", "--file=-", "--formula", "--cask", "--tap", "--mas")
	if err != nil {
		return err
	}
	return st.WriteFile(BrewfileName, out.Stdout)
}

// Undo fails without package data rather than report it: an error keeps
// the record, so prepare --undo can start the services once brew is ready.
func (m Module) Undo(env module.Env, _ *stage.Stage) (module.Result, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return module.Result{}, err
	}
	if err := m.checkCache(env); err != nil {
		return module.Result{}, errors.New(describe(noCache(module.SeverityBlocker, "prepare --undo", err)))
	}
	return m.restart(env, inv), nil
}

func (m Module) Finish(env module.Env, st *stage.Stage) (module.Result, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return module.Result{}, err
	}
	if !inv.Installed {
		return module.Result{}, nil
	}
	if !m.installed() {
		return module.Result{Problems: []module.Finding{{
			Module: Name, ID: "missing", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Homebrew is missing on this Mac",
			Fix:   "Migration Assistant should have copied it. Copy it from the old Mac before you erase it, or reinstall Homebrew and run: brew bundle install --file " + macos.ShellQuote(st.Path(BrewfileName)),
		}}}, nil
	}
	if err := m.checkCache(env); err != nil {
		return module.Result{Problems: []module.Finding{noCache(module.SeverityWarn, "finish", err)}}, nil
	}
	return m.restart(env, inv), nil
}

func (m Module) restart(env module.Env, inv Inventory) module.Result {
	var res module.Result
	var ok []string
	for _, name := range inv.Stopped {
		if _, err := m.brew(env, "services", "start", name); err != nil {
			res.Problems = append(res.Problems, module.Finding{
				Module: Name, ID: "start:" + name, Severity: module.SeverityWarn, Handler: module.Manual,
				Title: "Could not start " + name, Detail: err.Error(), Fix: "Run: brew services start " + macos.ShellQuote(name),
			})
			continue
		}
		ok = append(ok, name)
	}
	if len(ok) > 0 {
		res.Done = append(res.Done, "started "+strings.Join(ok, ", "))
	}
	if len(inv.Root) > 0 {
		res.Problems = append(res.Problems, module.Finding{
			Module: Name, ID: "root-services", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Start the services that ran as root", Fix: "Run: " + sudo("start", inv.Root),
		})
	}
	return res
}

func (m Module) Verify(env module.Env, st *stage.Stage) ([]module.Check, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	if !inv.Installed {
		return nil, nil
	}
	if !m.installed() {
		return []module.Check{{Module: Name, ID: Name + ":installed", Status: module.StatusFail, Title: "Homebrew",
			Detail: m.Brew + " is missing; Migration Assistant should have copied Homebrew"}}, nil
	}
	if err := m.checkCache(env); err != nil {
		return []module.Check{{Module: Name, ID: Name + ":cache", Status: module.StatusWarn, Title: "package data",
			Detail: describe(noCache(module.SeverityWarn, "verify", err))}}, nil
	}
	var checks []module.Check
	if inv.Brewfile {
		checks = append(checks, m.packagesCheck(env, st))
	}
	services, err := m.services(env)
	if err != nil {
		return nil, err
	}
	return append(checks, servicesCheck(env, inv, services)), nil
}

func (m Module) packagesCheck(env module.Env, st *stage.Stage) module.Check {
	c := module.Check{Module: Name, ID: Name + ":packages", Status: module.StatusPass, Title: "packages", Detail: "every Brewfile package is installed"}
	brewfile := st.Path(BrewfileName)
	out, err := m.brew(env, "bundle", "check", "--file="+brewfile, "--no-upgrade", "--verbose")
	if err == nil {
		return c
	}
	missing := parseMissing(slices.Concat(out.Stdout, []byte("\n"), out.Stderr))
	if code, ok := runner.ExitCode(err); !ok || code != 1 || len(missing) == 0 {
		c.Status, c.Detail = module.StatusWarn, "could not check packages: "+err.Error()
		return c
	}
	c.Status, c.Paths = module.StatusWarn, missing
	c.Detail = words.Count(len(missing), "package is", "packages are") + " missing; run: brew bundle install --file " + macos.ShellQuote(brewfile)
	return c
}

// parseMissing reads brew bundle check --verbose lines such as
// "→ Formula wget needs to be installed.", which brew writes to stderr.
func parseMissing(out []byte) []string {
	var missing []string
	for _, line := range strings.Split(string(out), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "→ ")
		if !ok {
			continue
		}
		if name, _, ok := strings.Cut(rest, " needs to be"); ok {
			missing = append(missing, name)
		}
	}
	return missing
}

// servicesCheck asks brew about the services prepare stopped, and launchd
// about the root ones, which brew can't see when run as the user.
func servicesCheck(env module.Env, inv Inventory, services []Service) module.Check {
	c := module.Check{Module: Name, ID: Name + ":services", Status: module.StatusPass, Title: "services"}
	var down []string
	for _, name := range inv.Stopped {
		if !started(services, name) {
			down = append(down, name)
		}
	}
	for _, name := range inv.Root {
		if !loadedAsRoot(env, inv.RootLabels[name]) {
			down = append(down, name)
		}
	}
	want := len(inv.Stopped) + len(inv.Root)
	c.Detail = words.Count(want-len(down), "service is", "services are") + " running again"
	if len(down) > 0 {
		c.Status, c.Paths = module.StatusWarn, down
		c.Detail = "not running: " + strings.Join(down, ", ") + "; run mtm finish"
	}
	return c
}
