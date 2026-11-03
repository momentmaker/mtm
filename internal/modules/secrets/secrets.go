// Package secrets records the old Mac's SSH and GPG identities, never their
// key material, so verify can prove they arrived. It also finds what no
// copy can move: keys in the Secure Enclave, password managers and their
// SSH agents to turn on again, and Touch ID fingerprints.
package secrets

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/momentmaker/mtm/internal/catalog"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const Name = "secrets"

// gpgPaths are where Homebrew and GPG Suite install gpg.
var gpgPaths = []string{"/opt/homebrew/bin/gpg", "/usr/local/bin/gpg", "/usr/local/MacGPG2/bin/gpg"}

// Module reads the environment through Getenv and looks for gpg at the
// GPG paths, so tests can stand in for both.
type Module struct {
	Catalog *catalog.Catalog
	Getenv  func(string) string
	GPG     []string
}

func New(c *catalog.Catalog) Module { return Module{Catalog: c, Getenv: os.Getenv, GPG: gpgPaths} }

func (Module) Name() string { return Name }

// Inventory is the module's record.
//   - GPGUnlisted: a GnuPG folder existed but there was no gpg to list it.
//   - Agent: the catalog id of the SSH agent SSH used, when mtm knows it.
//   - SecureEnclave: SSH used keys kept in the Secure Enclave.
//   - Fingerprints: how many Touch ID fingerprints were enrolled, or nil
//     when bioutil couldn't say.
type Inventory struct {
	SSH           []SSHKey `json:"ssh,omitempty"`
	GPG           []GPGKey `json:"gpg,omitempty"`
	GPGUnlisted   bool     `json:"gpg_unlisted,omitempty"`
	Agent         string   `json:"agent,omitempty"`
	SecureEnclave bool     `json:"secure_enclave,omitempty"`
	Fingerprints  *int     `json:"fingerprints,omitempty"`
}

func (m Module) look(env module.Env) (Inventory, error) {
	var inv Inventory
	var err error
	if inv.SSH, err = sshKeys(env.Home); err != nil {
		return Inventory{}, err
	}
	inv.GPG, err = m.gpgKeys(env)
	switch {
	case errors.Is(err, errNoGPG):
		inv.GPGUnlisted = true
	case err != nil:
		return Inventory{}, err
	}
	if inv.Agent, inv.SecureEnclave, err = m.agent(env.Home); err != nil {
		return Inventory{}, err
	}
	inv.SecureEnclave = inv.SecureEnclave || slices.ContainsFunc(inv.SSH, SSHKey.hardware)
	inv.Fingerprints = fingerprints(env)
	return inv, nil
}

// agent finds the SSH agent SSH uses, from SSH_AUTH_SOCK and the
// IdentityAgent lines in ~/.ssh/config, and whether it, or a
// SecurityKeyProvider, keeps keys in the Secure Enclave.
func (m Module) agent(home string) (string, bool, error) {
	cfg, err := readSSHConfig(home)
	if err != nil {
		return "", false, err
	}
	secureEnclave := cfg.secureEnclave || strings.Contains(m.Getenv("SSH_SK_PROVIDER"), secureEnclaveProvider)
	sockets := cfg.agents
	if s := m.Getenv("SSH_AUTH_SOCK"); s != "" {
		sockets = append([]string{s}, sockets...)
	}
	for _, s := range sockets {
		if a, ok := agentApp(m.Catalog, s, home); ok {
			return a.ID, secureEnclave || a.Kind == catalog.KindSecureEnclaveAgent, nil
		}
	}
	return "", secureEnclave, nil
}

var templates = regexp.MustCompile(`(\d+) biometric template`)

// fingerprints counts the enrolled Touch ID fingerprints with bioutil's
// read-only --count. Any failure, as on a Mac without Touch ID, means
// unknown.
func fingerprints(env module.Env) *int {
	out, err := env.Run.Run(env.Ctx, runner.Command{Name: "/usr/bin/bioutil", Args: []string{"-c"}})
	if err != nil {
		return nil
	}
	m := templates.FindStringSubmatch(string(out.Stdout))
	if m == nil {
		return nil
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return nil
	}
	return &n
}

func (m Module) findings(inv Inventory, severity module.Severity) []module.Finding {
	var out []module.Finding
	if inv.SecureEnclave {
		out = append(out, module.Finding{
			Module: Name, ID: "secure-enclave", Severity: severity, Handler: module.Manual,
			Title:  "SSH keys in this Mac's Secure Enclave can't move to the new Mac",
			Detail: "A key made in the Secure Enclave stays in it. A key on a hardware security key keeps working once it's plugged in.",
			Fix:    "On the new Mac, make new keys and add them to GitHub and your servers; mtm checklist reminds you. Keep this Mac until they work.",
		})
	}
	if inv.GPGUnlisted {
		out = append(out, module.Finding{
			Module: Name, ID: "gpg", Severity: severity, Handler: module.Manual,
			Title:  "mtm can't list your GPG keys: gpg isn't installed where it looks",
			Detail: "Looked in " + strings.Join(m.GPG, ", ") + ".",
			Fix:    "On the new Mac, mtm checklist asks you to check them with gpg -K.",
		})
	}
	if a, ok := m.app(inv.Agent); ok && a.Kind == catalog.KindPasswordManager {
		out = append(out, module.Finding{
			Module: Name, ID: "ssh-agent", Severity: module.SeverityInfo, Handler: module.Manual,
			Title:  "SSH uses " + a.Name + "'s SSH agent",
			Detail: "On the new Mac, sign in to " + a.Name + " and turn its SSH agent on; mtm checklist lists it.",
		})
	}
	return out
}

func (m Module) app(id string) (catalog.App, bool) {
	if id == "" {
		return catalog.App{}, false
	}
	i := slices.IndexFunc(m.Catalog.Apps, func(a catalog.App) bool { return a.ID == id })
	if i < 0 {
		return catalog.App{}, false
	}
	return m.Catalog.Apps[i], true
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	inv, err := m.look(env)
	if err != nil {
		return nil, err
	}
	var out []module.Finding
	if len(inv.SSH) > 0 || len(inv.GPG) > 0 {
		out = append(out, module.Finding{
			Module: Name, ID: "identities", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
			Title:  words.Count(len(inv.SSH), "SSH key", "SSH keys") + " and " + words.Count(len(inv.GPG), "GPG key", "GPG keys"),
			Detail: "Migration Assistant copies them. verify checks each one arrived.",
		})
	}
	return append(out, m.findings(inv, module.SeverityWarn)...), nil
}

func (m Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	inv, err := m.look(env)
	if err != nil {
		return module.Result{}, err
	}
	return module.Result{
		Inventory: inv,
		Done:      []string{"recorded " + words.Count(len(inv.SSH), "SSH key", "SSH keys") + " and " + words.Count(len(inv.GPG), "GPG key", "GPG keys")},
		Problems:  m.findings(inv, module.SeverityWarn),
	}, nil
}

// Finish has nothing to restore: Migration Assistant copies ~/.ssh,
// ~/.gnupg, and the login keychain.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}

// recordedApps are the catalog entries of the apps in the system module's
// record, where the password managers and Secure Enclave agents are found.
func (m Module) recordedApps(env module.Env) ([]catalog.App, error) {
	var sys struct {
		Apps []struct {
			BundleID string `json:"bundle_id"`
		} `json:"apps"`
	}
	if _, err := module.LoadInventory(env, "system", &sys); err != nil {
		if errors.Is(err, module.ErrNotPrepared) {
			return nil, nil
		}
		return nil, err
	}
	var out []catalog.App
	for _, a := range sys.Apps {
		if entry, ok := m.Catalog.ByBundleID(a.BundleID); ok {
			out = append(out, entry)
		}
	}
	return out, nil
}

// usesSecureEnclave is true when SSH used Secure Enclave keys, or the old
// Mac had an app that keeps them there.
func usesSecureEnclave(inv Inventory, apps []catalog.App) bool {
	return inv.SecureEnclave || slices.ContainsFunc(apps, func(a catalog.App) bool { return a.Kind == catalog.KindSecureEnclaveAgent })
}

const touchIDStep = Name + ":touch-id"

func (m Module) Steps(env module.Env) ([]checklist.Item, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	apps, err := m.recordedApps(env)
	if err != nil {
		return nil, err
	}
	items := []checklist.Item{{
		ID: Name + ":icloud-keychain", Module: Name, Where: "System Settings > your name > iCloud > Passwords",
		Title:  "Turn on iCloud Keychain, and check your passwords and passkeys are there",
		Detail: "Passkeys, and items saved only in this Mac's keychain, come back only through iCloud Keychain.",
	}}
	for _, a := range apps {
		if a.Kind == catalog.KindPasswordManager {
			items = append(items, checklist.Item{ID: Name + ":password-manager:" + a.ID, Module: Name, Where: a.Name, Title: "Sign in to " + a.Name})
		}
	}
	if a, ok := m.app(inv.Agent); ok && a.Kind == catalog.KindPasswordManager {
		items = append(items, checklist.Item{ID: Name + ":ssh-agent:" + a.ID, Module: Name, Where: a.Name,
			Title:  "Turn on " + a.Name + "'s SSH agent",
			Detail: "SSH used it on the old Mac, through SSH_AUTH_SOCK or IdentityAgent in ~/.ssh/config."})
	}
	if usesSecureEnclave(inv, apps) {
		items = append(items, checklist.Item{ID: Name + ":secure-enclave", Module: Name, Where: "Terminal",
			Title:  "Make new Secure Enclave SSH keys and add them to GitHub and your servers",
			Detail: "Keys in the old Mac's Secure Enclave can't leave it. Keys on a hardware security key keep working once it's plugged in." + hardwareKeys(inv.SSH)})
	}
	if inv.GPGUnlisted {
		items = append(items, checklist.Item{ID: Name + ":gpg-check", Module: Name, Where: "Terminal",
			Title:  "Check your GPG secret keys arrived: run gpg -K",
			Detail: "mtm couldn't list them on the old Mac, because gpg wasn't installed where it looks."})
	}
	if inv.Fingerprints == nil || *inv.Fingerprints > 0 {
		items = append(items, checklist.Item{ID: touchIDStep, Module: Name, Where: "System Settings > Touch ID & Password", Title: "Add your fingerprints to Touch ID"})
	}
	return items, nil
}

// Retire asks, for mtm retire on the old Mac, for its Secure Enclave keys
// to be removed from the services that trust them. They die with this Mac,
// but stay trusted until removed.
func (m Module) Retire(env module.Env, _ *stage.Stage) (module.Retirement, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return module.Retirement{}, err
	}
	apps, err := m.recordedApps(env)
	if err != nil || !usesSecureEnclave(inv, apps) {
		return module.Retirement{}, err
	}
	return module.Retirement{Steps: []checklist.Item{{
		ID: Name + ":retire-secure-enclave", Module: Name, Where: "GitHub > Settings > SSH and GPG keys, and each server's ~/.ssh/authorized_keys",
		Title:  "Remove this Mac's Secure Enclave SSH keys from GitHub and your servers",
		Detail: "Do it once the new Mac's keys work. Remove any passkey you made only on this Mac the same way, in each site's security settings. Keys on a hardware security key keep working; leave those." + hardwareKeys(inv.SSH),
	}}}, nil
}

func hardwareKeys(keys []SSHKey) string {
	var names []string
	for _, k := range keys {
		if k.hardware() {
			names = append(names, "~/.ssh/"+k.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " Keys of this kind: " + strings.Join(names, ", ") + "."
}

func (m Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	var checks []module.Check
	if len(inv.SSH) > 0 {
		c, err := verifySSH(env.Home, inv.SSH)
		if err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	if len(inv.GPG) > 0 || inv.GPGUnlisted {
		checks = append(checks, m.verifyGPG(env, inv))
	}
	if inv.Fingerprints == nil || *inv.Fingerprints > 0 {
		checks = append(checks, touchIDCheck(env))
	}
	return checks, nil
}

// verifySSH fails when a recorded key is missing or is a different key
// now: the old Mac holds the only other copy.
func verifySSH(home string, recorded []SSHKey) (module.Check, error) {
	now, err := sshKeys(home)
	if err != nil {
		return module.Check{}, err
	}
	var problems []string
	for _, k := range recorded {
		i := slices.IndexFunc(now, func(n SSHKey) bool { return n.Name == k.Name })
		var got SSHKey
		if i >= 0 {
			got = now[i]
		}
		switch {
		case k.Public && !got.Public:
			problems = append(problems, "~/.ssh/"+k.Name+".pub: missing")
		case k.Fingerprint != "" && got.Fingerprint != k.Fingerprint:
			problems = append(problems, "~/.ssh/"+k.Name+".pub: a different key")
		}
		if k.Private && !got.Private {
			problems = append(problems, "~/.ssh/"+k.Name+": private key missing")
		}
	}
	c := module.Check{Module: Name, ID: Name + ":ssh", Status: module.StatusPass, Critical: true, Title: "SSH keys",
		Detail: words.Count(len(recorded), "key", "keys") + " arrived"}
	if len(problems) > 0 {
		c.Status = module.StatusFail
		c.Detail = "copy them from the old Mac's ~/.ssh"
		c.Paths = problems
	}
	return c, nil
}

func (m Module) verifyGPG(env module.Env, inv Inventory) module.Check {
	c := module.Check{Module: Name, ID: Name + ":gpg", Status: module.StatusPass, Critical: true, Title: "GPG keys"}
	if inv.GPGUnlisted {
		c.Status = module.StatusInfo
		c.Critical = false
		c.Detail = "mtm couldn't list them on the old Mac; mtm checklist has a step to check them with gpg -K"
		return c
	}
	now, err := m.gpgKeys(env)
	if err != nil {
		c.Status = module.StatusFail
		c.Detail = err.Error()
		return c
	}
	var missing []string
	for _, k := range inv.GPG {
		i := slices.IndexFunc(now, func(n GPGKey) bool { return n.Fingerprint == k.Fingerprint })
		if i < 0 || (now[i].stub() && !k.stub()) {
			missing = append(missing, k.describe())
		}
	}
	c.Detail = words.Count(len(inv.GPG), "secret key", "secret keys") + " arrived"
	if len(missing) > 0 {
		c.Status = module.StatusFail
		c.Detail = "secret keys are missing; copy ~/.gnupg from the old Mac, or export them there with gpg --export-secret-keys"
		c.Paths = missing
	}
	return c
}

// touchIDCheck closes the Touch ID step once fingerprints are enrolled
// on this Mac.
func touchIDCheck(env module.Env) module.Check {
	c := module.Check{Module: Name, ID: touchIDStep, Status: module.StatusInfo, Title: "Touch ID",
		Detail: "no fingerprints yet; mtm checklist has the step"}
	if n := fingerprints(env); n != nil && *n > 0 {
		c.Status, c.Detail, c.Resolves = module.StatusPass, words.Count(*n, "fingerprint", "fingerprints")+" enrolled", []string{touchIDStep}
	}
	return c
}
