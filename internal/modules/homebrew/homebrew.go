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

// brewEnv keeps Homebrew from updating itself, sending analytics, or
// cleaning up, so it makes no network calls and changes nothing on mtm's
// behalf.
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
}

// Inventory records which services prepare stopped (Stopped) and which ran
// as root and must be stopped and started by hand (Root).
type Inventory struct {
	Installed bool      `json:"installed"`
	Services  []Service `json:"services,omitempty"`
	Stopped   []string  `json:"stopped,omitempty"`
	Root      []string  `json:"root,omitempty"`
	Brewfile  bool      `json:"brewfile"`
}

func (m Module) installed() bool {
	_, err := os.Stat(m.Brew)
	return err == nil
}

func (m Module) brew(env module.Env, args ...string) (runner.Output, error) {
	return env.Run.Run(env.Ctx, runner.Command{Name: m.Brew, Args: args, Env: brewEnv})
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
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse brew services list: %w", err)
	}
	services := make([]Service, 0, len(raw))
	for _, r := range raw {
		s := Service{Name: r.Name, Status: r.Status}
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

// running splits started services into those this user can stop and those
// that need sudo.
func (m Module) running(services []Service) (mine, others []string) {
	for _, s := range services {
		switch {
		case s.Status != "started":
		case s.User == m.User:
			mine = append(mine, s.Name)
		default:
			others = append(others, s.Name)
		}
	}
	return mine, others
}

// stillStopped keeps names an earlier prepare recorded that are still not
// running, so finish restarts them even after prepare runs again.
func stillStopped(earlier []string, services []Service) []string {
	var out []string
	for _, name := range earlier {
		if !started(services, name) {
			out = append(out, name)
		}
	}
	return out
}

func sudo(verb string, names []string) string {
	cmds := make([]string, len(names))
	for i, n := range names {
		cmds[i] = "sudo brew services " + verb + " " + n
	}
	return strings.Join(cmds, "; ")
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	if !m.installed() {
		return []module.Finding{{Module: Name, ID: "not-installed", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant, Title: "Homebrew is not installed"}}, nil
	}
	services, err := m.services(env)
	if err != nil {
		return nil, err
	}
	findings := []module.Finding{{
		Module: Name, ID: "homebrew", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
		Title:  "Homebrew in " + filepath.Dir(filepath.Dir(m.Brew)),
		Detail: "Migration Assistant copies it. mtm saves a Brewfile and checks every package arrived.",
	}}
	mine, others := m.running(services)
	if len(mine) > 0 {
		findings = append(findings, module.Finding{
			Module: Name, ID: "services", Severity: module.SeverityInfo, Handler: module.ByMtm,
			Title:  words.Count(len(mine), "running service", "running services") + ": " + strings.Join(mine, ", "),
			Detail: "prepare stops them so their data is copied whole; finish starts them on the new Mac.",
		})
	}
	if len(others) > 0 {
		findings = append(findings, module.Finding{
			Module: Name, ID: "root-services", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: words.Count(len(others), "service runs", "services run") + " as root: " + strings.Join(others, ", "),
			Fix:   "Before Migration Assistant, run: " + sudo("stop", others),
		})
	}
	return findings, nil
}

func (m Module) Prepare(env module.Env, st *stage.Stage) (module.Result, error) {
	if !m.installed() {
		return module.Result{Inventory: Inventory{}, Done: []string{"Homebrew is not installed"}}, nil
	}
	var previous Inventory
	if _, err := module.LoadInventory(env, Name, &previous); err != nil && !errors.Is(err, module.ErrNotPrepared) {
		return module.Result{}, err
	}
	services, err := m.services(env)
	if err != nil {
		return module.Result{}, err
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
	mine, others := m.running(services)
	inv.Stopped = stillStopped(previous.Stopped, services)
	for _, name := range mine {
		if _, err := m.brew(env, "services", "stop", name); err != nil {
			res.Problems = append(res.Problems, module.Finding{
				Module: Name, ID: "stop:" + name, Severity: module.SeverityBlocker, Handler: module.Manual,
				Title: "Could not stop " + name, Detail: err.Error(),
				Fix: "Run: brew services stop " + name + ", then run mtm prepare again. A running service's data could be copied mid-write.",
			})
			continue
		}
		inv.Stopped = append(inv.Stopped, name)
	}
	if len(inv.Stopped) > 0 {
		res.Done = append(res.Done, "stopped "+strings.Join(inv.Stopped, ", "))
	}
	inv.Root = append(stillStopped(previous.Root, services), others...)
	if len(others) > 0 {
		res.Problems = append(res.Problems, module.Finding{
			Module: Name, ID: "root-services", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: words.Count(len(others), "service runs", "services run") + " as root and must be stopped by hand",
			Fix:   "Run: " + sudo("stop", others),
		})
	}
	res.Inventory = inv
	return res, nil
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

func (m Module) Undo(env module.Env, _ *stage.Stage) (module.Result, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return module.Result{}, err
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
	return m.restart(env, inv), nil
}

func (m Module) restart(env module.Env, inv Inventory) module.Result {
	var res module.Result
	var ok []string
	for _, name := range inv.Stopped {
		if _, err := m.brew(env, "services", "start", name); err != nil {
			res.Problems = append(res.Problems, module.Finding{
				Module: Name, ID: "start:" + name, Severity: module.SeverityWarn, Handler: module.Manual,
				Title: "Could not start " + name, Detail: err.Error(), Fix: "Run: brew services start " + name,
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
	var checks []module.Check
	if inv.Brewfile {
		checks = append(checks, m.packagesCheck(env, st))
	}
	services, err := m.services(env)
	if err != nil {
		return nil, err
	}
	return append(checks, servicesCheck(inv, services)), nil
}

func (m Module) packagesCheck(env module.Env, st *stage.Stage) module.Check {
	c := module.Check{Module: Name, ID: Name + ":packages", Status: module.StatusPass, Title: "packages", Detail: "every Brewfile package is installed"}
	brewfile := st.Path(BrewfileName)
	out, err := m.brew(env, "bundle", "check", "--file="+brewfile, "--no-upgrade", "--verbose")
	if err == nil {
		return c
	}
	missing := parseMissing(append(out.Stdout, out.Stderr...))
	if code, ok := runner.ExitCode(err); !ok || code != 1 || len(missing) == 0 {
		c.Status, c.Detail = module.StatusWarn, "could not check packages: "+err.Error()
		return c
	}
	c.Status, c.Paths = module.StatusWarn, missing
	c.Detail = words.Count(len(missing), "package is", "packages are") + " missing; run: brew bundle install --file " + macos.ShellQuote(brewfile)
	return c
}

// parseMissing reads brew bundle check --verbose lines such as
// "→ Formula wget needs to be installed."
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

func servicesCheck(inv Inventory, services []Service) module.Check {
	c := module.Check{Module: Name, ID: Name + ":services", Status: module.StatusPass, Title: "services"}
	want := append(slices.Clone(inv.Stopped), inv.Root...)
	var down []string
	for _, name := range want {
		if !started(services, name) {
			down = append(down, name)
		}
	}
	c.Detail = words.Count(len(want)-len(down), "service is", "services are") + " running again"
	if len(down) > 0 {
		c.Status, c.Paths = module.StatusWarn, down
		c.Detail = "not running: " + strings.Join(down, ", ") + "; run mtm finish"
	}
	return c
}
