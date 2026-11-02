// Package system records the parts of the Mac outside the home folder that
// a developer relies on: the login shell, edited /etc files, and installed
// apps, including whether they need Rosetta.
package system

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/apps"
	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const (
	Name      = "system"
	etcExport = "exports/etc"
)

// etcFiles are the /etc files developers commonly edit. Every file in
// /etc/paths.d is added when recording.
var etcFiles = []string{"hosts", "shells", "pam.d/sudo_local"}

type Module struct {
	User        string
	EtcDir      string
	AppRoots    func(home string) []apps.Root
	RosettaPath string
}

func New(user string) Module {
	return Module{User: user, EtcDir: "/etc", AppRoots: DefaultAppRoots, RosettaPath: macos.RosettaPath}
}

func DefaultAppRoots(home string) []apps.Root {
	return []apps.Root{
		{Dir: "/Applications", Display: "/Applications"},
		{Dir: filepath.Join(home, "Applications"), Display: "~/Applications"},
	}
}

func (Module) Name() string { return Name }

// Inventory maps /etc files, relative to /etc, to their fingerprints.
type Inventory struct {
	Shell       string            `json:"shell"`
	ShellListed bool              `json:"shell_listed"`
	Etc         map[string]string `json:"etc"`
	Apps        []apps.App        `json:"apps"`
}

type snapshot struct {
	Inventory
	etcData map[string][]byte
}

func (m Module) snapshot(env module.Env) (snapshot, error) {
	shell, err := macos.LoginShell(env.Ctx, env.Run, m.User)
	if err != nil {
		return snapshot{}, err
	}
	listed, err := m.shellListed(shell)
	if err != nil {
		return snapshot{}, err
	}
	etc, data, err := m.readEtc()
	if err != nil {
		return snapshot{}, err
	}
	list, err := apps.List(env.Ctx, env.Run, m.AppRoots(env.Home))
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{Inventory: Inventory{Shell: shell, ShellListed: listed, Etc: etc, Apps: list}, etcData: data}, nil
}

func (m Module) shellListed(shell string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(m.EtcDir, "shells"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read /etc/shells: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == shell {
			return true, nil
		}
	}
	return false, nil
}

func (m Module) readEtc() (map[string]string, map[string][]byte, error) {
	rels := slices.Clone(etcFiles)
	entries, err := os.ReadDir(filepath.Join(m.EtcDir, "paths.d"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("read /etc/paths.d: %w", err)
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			rels = append(rels, "paths.d/"+e.Name())
		}
	}
	hashes, data := map[string]string{}, map[string][]byte{}
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(m.EtcDir, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read /etc/%s: %w", rel, err)
		}
		hashes[rel], data[rel] = fsinfo.HashBytes(b), b
	}
	return hashes, data, nil
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	snap, err := m.snapshot(env)
	if err != nil {
		return nil, err
	}
	findings := []module.Finding{
		{
			Module: Name, ID: "apps", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
			Title:  words.Count(len(snap.Apps), "app", "apps") + " in /Applications and ~/Applications",
			Detail: "Migration Assistant copies them. mtm records each one and checks it arrived.",
		},
		{
			Module: Name, ID: "shell", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
			Title: "Login shell: " + snap.Shell,
		},
	}
	if f, ok := intelOnly(snap.Apps); ok {
		findings = append(findings, f)
	}
	return findings, nil
}

func (m Module) Prepare(env module.Env, st *stage.Stage) (module.Result, error) {
	snap, err := m.snapshot(env)
	if err != nil {
		return module.Result{}, err
	}
	for rel, b := range snap.etcData {
		if err := st.WriteFile(filepath.Join(etcExport, filepath.FromSlash(rel)), b); err != nil {
			return module.Result{}, err
		}
	}
	res := module.Result{
		Inventory: snap.Inventory,
		Done: []string{fmt.Sprintf("recorded login shell %s, %s, and %s", snap.Shell,
			words.Count(len(snap.Etc), "/etc file", "/etc files"), words.Count(len(snap.Apps), "app", "apps"))},
	}
	if f, ok := intelOnly(snap.Apps); ok {
		res.Problems = append(res.Problems, f)
	}
	return res, nil
}

func (m Module) Finish(env module.Env, st *stage.Stage) (module.Result, error) {
	var old Inventory
	if _, err := module.LoadInventory(env, Name, &old); err != nil {
		return module.Result{}, err
	}
	now, err := m.snapshot(env)
	if err != nil {
		return module.Result{}, err
	}
	var res module.Result
	for _, rel := range slices.Sorted(maps.Keys(old.Etc)) {
		if now.Etc[rel] == old.Etc[rel] {
			continue
		}
		res.Problems = append(res.Problems, byHand("etc:"+rel, "/etc/"+rel+" differs from the old Mac",
			"Review the old copy, then restore it with: sudo cp "+macos.ShellQuote(st.Path(etcExport, filepath.FromSlash(rel)))+" /etc/"+rel))
	}
	if hasIntelOnly(old.Apps) && !m.rosettaInstalled() {
		res.Problems = append(res.Problems, byHand("rosetta", "Intel-only apps need Rosetta", "Run: sudo softwareupdate --install-rosetta --agree-to-license"))
	}
	if old.Shell != "" && now.Shell != old.Shell {
		listed, err := m.shellListed(old.Shell)
		if err != nil {
			return module.Result{}, err
		}
		fix := "Run: chsh -s " + macos.ShellQuote(old.Shell)
		if !listed {
			fix = "Run: echo " + macos.ShellQuote(old.Shell) + " | sudo tee -a /etc/shells, then: chsh -s " + macos.ShellQuote(old.Shell)
		}
		res.Problems = append(res.Problems, byHand("shell", "Your login shell is "+now.Shell+", not "+old.Shell, fix))
	}
	if len(res.Problems) == 0 {
		res.Done = []string{"login shell, /etc files, and Rosetta are as they were"}
	}
	return res, nil
}

func intelOnly(list []apps.App) (module.Finding, bool) {
	var names []string
	for _, a := range list {
		if a.IntelOnly() {
			names = append(names, appName(a))
		}
	}
	if len(names) == 0 {
		return module.Finding{}, false
	}
	return module.Finding{
		Module: Name, ID: "intel-only", Severity: module.SeverityWarn, Handler: module.Manual,
		Title:  words.Count(len(names), "app is Intel-only and needs", "apps are Intel-only and need") + " Rosetta on the new Mac",
		Detail: strings.Join(names, ", "),
		Fix:    "mtm finish prints the Rosetta install command. Rosetta is limited after macOS 27, so look for Apple Silicon versions.",
	}, true
}

func hasIntelOnly(list []apps.App) bool { return slices.ContainsFunc(list, apps.App.IntelOnly) }

func appName(a apps.App) string { return strings.TrimSuffix(path.Base(a.Path), ".app") }

func (m Module) rosettaInstalled() bool {
	_, err := os.Stat(m.RosettaPath)
	return err == nil
}

func byHand(id, title, fix string) module.Finding {
	return module.Finding{Module: Name, ID: id, Severity: module.SeverityWarn, Handler: module.Manual, Title: title, Fix: fix}
}
