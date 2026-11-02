// Package loginitems records launch agents and login items, loads the
// agents again on the new Mac, and lists the login items to add back.
package loginitems

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const (
	Name             = "loginitems"
	loginItemsScript = `tell application "System Events" to get the name of every login item`
)

type Module struct {
	UID int
}

func New(uid int) Module { return Module{UID: uid} }

func (Module) Name() string { return Name }

type Agent struct {
	File   string `json:"file"`
	Label  string `json:"label"`
	Loaded bool   `json:"loaded"`
}

type Inventory struct {
	Agents           []Agent  `json:"agents,omitempty"`
	LoginItems       []string `json:"login_items,omitempty"`
	LoginItemsListed bool     `json:"login_items_listed"`
}

func agentsDir(home string) string { return filepath.Join(home, "Library", "LaunchAgents") }

func agents(env module.Env) ([]Agent, error) {
	loaded, err := loadedLabels(env)
	if err != nil {
		return nil, err
	}
	dir := agentsDir(env.Home)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []Agent
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".plist") {
			continue
		}
		label, err := macos.PlistValue(env.Ctx, env.Run, filepath.Join(dir, e.Name()), "Label")
		if err != nil {
			return nil, err
		}
		if label == "" {
			label = strings.TrimSuffix(e.Name(), ".plist")
		}
		out = append(out, Agent{File: e.Name(), Label: label, Loaded: loaded[label]})
	}
	return out, nil
}

func loadedLabels(env module.Env) (map[string]bool, error) {
	out, err := env.Run.Run(env.Ctx, runner.Command{Name: "/bin/launchctl", Args: []string{"list"}})
	if err != nil {
		return nil, fmt.Errorf("list loaded launch agents: %w", err)
	}
	labels := map[string]bool{}
	for i, line := range strings.Split(string(out.Stdout), "\n") {
		fields := strings.Split(line, "\t")
		if i == 0 || len(fields) != 3 {
			continue
		}
		labels[fields[2]] = true
	}
	return labels, nil
}

func loginItems(env module.Env) ([]string, error) {
	out, err := env.Run.Run(env.Ctx, runner.Command{Name: "/usr/bin/osascript", Args: []string{"-e", loginItemsScript}})
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(string(out.Stdout))
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, ", "), nil
}

func countLoaded(list []Agent) int {
	n := 0
	for _, a := range list {
		if a.Loaded {
			n++
		}
	}
	return n
}

// Scan doesn't ask System Events, so it never brings up a permission
// prompt; it explains the one prepare will.
func (Module) Scan(env module.Env) ([]module.Finding, error) {
	list, err := agents(env)
	if err != nil {
		return nil, err
	}
	return []module.Finding{
		{
			Module: Name, ID: "agents", Severity: module.SeverityInfo, Handler: module.ByMtm,
			Title:  fmt.Sprintf("%s in ~/Library/LaunchAgents (%d loaded)", words.Count(len(list), "launch agent", "launch agents"), countLoaded(list)),
			Detail: "finish loads them again on the new Mac.",
		},
		{
			Module: Name, ID: "login-items", Severity: module.SeverityInfo, Handler: module.Manual,
			Title:  "Login items",
			Detail: "prepare asks System Events for your login items, so macOS may ask to let your terminal control System Events. finish lists them so you can add them back.",
		},
	}, nil
}

func (Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	list, err := agents(env)
	if err != nil {
		return module.Result{}, err
	}
	inv := Inventory{Agents: list}
	var res module.Result
	items, err := loginItems(env)
	if err != nil {
		res.Problems = append(res.Problems, module.Finding{
			Module: Name, ID: "login-items", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Could not list your login items", Detail: err.Error(),
			Fix: "Allow your terminal to control System Events when macOS asks and run mtm prepare again, or note them in System Settings > General > Login Items.",
		})
	} else {
		inv.LoginItems, inv.LoginItemsListed = items, true
	}
	res.Inventory = inv
	res.Done = []string{fmt.Sprintf("recorded %s (%d loaded) and %s", words.Count(len(list), "launch agent", "launch agents"),
		countLoaded(list), words.Count(len(inv.LoginItems), "login item", "login items"))}
	return res, nil
}

func (m Module) Finish(env module.Env, _ *stage.Stage) (module.Result, error) {
	var old Inventory
	if _, err := module.LoadInventory(env, Name, &old); err != nil {
		return module.Result{}, err
	}
	loaded, err := loadedLabels(env)
	if err != nil {
		return module.Result{}, err
	}
	domain := "gui/" + strconv.Itoa(m.UID)
	var res module.Result
	var ok []string
	for _, a := range old.Agents {
		if !a.Loaded || loaded[a.Label] {
			continue
		}
		plist := filepath.Join(agentsDir(env.Home), a.File)
		if _, err := env.Run.Run(env.Ctx, runner.Command{Name: "/bin/launchctl", Args: []string{"bootstrap", domain, plist}}); err != nil {
			res.Problems = append(res.Problems, module.Finding{
				Module: Name, ID: "agent:" + a.Label, Severity: module.SeverityWarn, Handler: module.Manual,
				Title: "Could not load " + a.Label, Detail: err.Error(),
				Fix: "Allow it in System Settings > General > Login Items, or run: launchctl bootstrap " + domain + " " + macos.ShellQuote(plist),
			})
			continue
		}
		ok = append(ok, a.Label)
	}
	if len(ok) > 0 {
		res.Done = append(res.Done, "loaded "+strings.Join(ok, ", "))
	}
	switch {
	case !old.LoginItemsListed:
		res.Problems = append(res.Problems, addBack("mtm couldn't list them on the old Mac; check the list there before you erase it."))
	case len(old.LoginItems) > 0:
		res.Problems = append(res.Problems, addBack("Add: "+strings.Join(old.LoginItems, ", ")))
	}
	return res, nil
}

func addBack(detail string) module.Finding {
	return module.Finding{
		Module: Name, ID: "login-items", Severity: module.SeverityWarn, Handler: module.Manual,
		Title: "Add your login items back", Detail: detail,
		Fix: "System Settings > General > Login Items > Open at Login",
	}
}

func (Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var old Inventory
	if _, err := module.LoadInventory(env, Name, &old); err != nil {
		return nil, err
	}
	loaded, err := loadedLabels(env)
	if err != nil {
		return nil, err
	}
	c := module.Check{Module: Name, ID: Name + ":agents", Status: module.StatusPass, Title: "launch agents"}
	var down []string
	want := 0
	for _, a := range old.Agents {
		if !a.Loaded {
			continue
		}
		want++
		if !loaded[a.Label] {
			down = append(down, a.Label)
		}
	}
	c.Detail = words.Count(want-len(down), "agent is", "agents are") + " loaded"
	if len(down) > 0 {
		c.Status, c.Paths = module.StatusWarn, down
		c.Detail = "not loaded: " + strings.Join(down, ", ") + "; run mtm finish"
	}
	checks := []module.Check{c}
	if len(old.LoginItems) > 0 {
		checks = append(checks, module.Check{
			Module: Name, ID: Name + ":login-items", Status: module.StatusInfo, Title: "login items",
			Detail: "add back by hand: " + strings.Join(old.LoginItems, ", "),
		})
	}
	return checks, nil
}
