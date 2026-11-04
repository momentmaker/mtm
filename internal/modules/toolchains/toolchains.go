// Package toolchains records which developer tools the login shell finds,
// and at which versions, so verify can tell the new Mac's terminal behaves
// the same.
package toolchains

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const (
	Name           = "toolchains"
	pathMarker     = "__MTM_PATH__="
	shellTimeout   = 20 * time.Second
	versionTimeout = 10 * time.Second
	unknownVersion = "unknown"
)

type tool struct {
	name string
	args []string
}

// appleToolsDir holds macOS's placeholders for java, python3, and git,
// which only offer to install the real tool. A variable so tests can stand
// in for it.
var appleToolsDir = "/usr/bin"

// applePlaceholders are the only tools in appleToolsDir that are stand-ins.
// Other tools there, such as ruby, are real, so a failed version stays
// "unknown" and verify still requires them.
var applePlaceholders = map[string]bool{"java": true, "python3": true, "git": true}

var known = []tool{
	{"brew", []string{"--version"}},
	{"git", []string{"--version"}},
	{"node", []string{"--version"}},
	{"npm", []string{"--version"}},
	{"pnpm", []string{"--version"}},
	{"bun", []string{"--version"}},
	{"deno", []string{"--version"}},
	{"python3", []string{"--version"}},
	{"uv", []string{"--version"}},
	{"go", []string{"version"}},
	{"rustc", []string{"--version"}},
	{"cargo", []string{"--version"}},
	{"ruby", []string{"--version"}},
	{"java", []string{"-version"}},
	{"asdf", []string{"--version"}},
	{"mise", []string{"--version"}},
}

// quietHomebrew goes to every version command. brew --version is one of
// them, and like every brew command mtm runs it must not update Homebrew
// or send analytics; other tools ignore these variables.
var quietHomebrew = []string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ANALYTICS=1", "HOMEBREW_NO_ENV_HINTS=1", "HOMEBREW_NO_INSTALL_CLEANUP=1"}

type Module struct {
	User string
}

func New(user string) Module { return Module{User: user} }

func (Module) Name() string { return Name }

// Tool paths inside the home folder are recorded as "~/…", so they compare
// across usernames.
type Tool struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

type Inventory struct {
	Shell string `json:"shell"`
	Tools []Tool `json:"tools,omitempty"`
}

func (m Module) collect(env module.Env) (Inventory, error) {
	shell, err := macos.LoginShell(env.Ctx, env.Run, m.User)
	if err != nil {
		return Inventory{}, err
	}
	path, err := loginPath(env, shell)
	if err != nil {
		return Inventory{}, err
	}
	inv := Inventory{Shell: shell}
	for _, t := range known {
		abs := lookPath(t.name, path)
		if abs == "" {
			continue
		}
		v := version(env, abs, t.args, path)
		if v == unknownVersion && applePlaceholders[t.name] && filepath.Dir(abs) == appleToolsDir {
			// A placeholder that can't report a version is a tool that isn't
			// installed, not one to require on the new Mac.
			continue
		}
		inv.Tools = append(inv.Tools, Tool{Name: t.name, Path: homeRelative(env.Home, abs), Version: v})
	}
	return inv, nil
}

func pathCommand(shell string) runner.Command {
	posix := `printf '` + pathMarker + `%s\n' "$PATH"`
	switch filepath.Base(shell) {
	case "fish":
		return runner.Command{Name: shell, Args: []string{"-l", "-c", `printf '` + pathMarker + `%s\n' (string join : $PATH)`}}
	case "zsh", "bash":
		// Many developers set PATH in .zshrc or .bashrc, which only
		// interactive shells read.
		return runner.Command{Name: shell, Args: []string{"-l", "-i", "-c", posix}}
	}
	return runner.Command{Name: shell, Args: []string{"-l", "-c", posix}}
}

func loginPath(env module.Env, shell string) (string, error) {
	ctx, cancel := context.WithTimeout(env.Ctx, shellTimeout)
	defer cancel()
	cmd := pathCommand(shell)
	cmd.Dir = env.Home
	out, err := env.Run.Run(ctx, cmd)
	// Startup files may print noise, or make the shell exit non-zero after
	// it printed the PATH; the marker line is what counts.
	if p, ok := parsePath(out.Stdout); ok {
		return p, nil
	}
	if err != nil {
		return "", fmt.Errorf("read PATH from login shell %s: %w", shell, err)
	}
	return "", fmt.Errorf("read PATH from login shell %s: it printed no PATH", shell)
}

func parsePath(out []byte) (string, bool) {
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if p, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), pathMarker); ok {
			return p, true
		}
	}
	return "", false
}

func lookPath(name, path string) string {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// version runs the tool with the login shell's PATH, so shims such as
// asdf's find their real tools. A tool that fails to report reads
// "unknown", which verify compares like any other version; collect drops
// the Apple placeholders from that rule.
func version(env module.Env, abs string, args []string, path string) string {
	ctx, cancel := context.WithTimeout(env.Ctx, versionTimeout)
	defer cancel()
	out, err := env.Run.Run(ctx, runner.Command{Name: abs, Args: args, Dir: env.Home, Env: append([]string{"PATH=" + path}, quietHomebrew...)})
	if err != nil {
		return unknownVersion
	}
	if line := firstLine(out.Stdout); line != "" {
		return line
	}
	if line := firstLine(out.Stderr); line != "" {
		return line
	}
	return unknownVersion
}

func firstLine(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func homeRelative(home, p string) string {
	if p == home || strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func summary(tools []Tool) string {
	parts := make([]string, len(tools))
	for i, t := range tools {
		parts[i] = t.Name + " " + t.Version
	}
	return strings.Join(parts, "; ")
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	inv, err := m.collect(env)
	if err != nil {
		return []module.Finding{{
			Module: Name, ID: "shell", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Could not read PATH from your login shell", Detail: err.Error(),
			Fix: "Check that a new terminal window starts cleanly, then run mtm scan again.",
		}}, nil
	}
	return []module.Finding{{
		Module: Name, ID: "tools", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
		Title:  words.Count(len(inv.Tools), "toolchain", "toolchains") + " found through " + inv.Shell,
		Detail: summary(inv.Tools),
	}}, nil
}

func (m Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	inv, err := m.collect(env)
	if err != nil {
		return module.Result{}, err
	}
	return module.Result{Inventory: inv, Done: []string{"recorded " + words.Count(len(inv.Tools), "tool", "tools") + " found through " + inv.Shell}}, nil
}

// Finish has nothing to restore: the tools live in folders Migration
// Assistant copies.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}

func (m Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var old Inventory
	if _, err := module.LoadInventory(env, Name, &old); err != nil {
		return nil, err
	}
	cur, err := m.collect(env)
	if err != nil {
		return []module.Check{{Module: Name, ID: Name + ":shell", Status: module.StatusFail, Title: "login shell", Detail: err.Error()}}, nil
	}
	now := map[string]Tool{}
	for _, t := range cur.Tools {
		now[t.Name] = t
	}
	var checks []module.Check
	for _, t := range old.Tools {
		c := module.Check{Module: Name, ID: Name + ":" + t.Name, Status: module.StatusPass, Title: t.Name, Detail: t.Version + " at " + t.Path}
		n, ok := now[t.Name]
		switch {
		case !ok:
			c.Status, c.Detail = module.StatusFail, fmt.Sprintf("missing (was %s at %s)", t.Version, t.Path)
		case n.Path != t.Path || n.Version != t.Version:
			c.Status, c.Detail = module.StatusFail, fmt.Sprintf("now %s at %s (was %s at %s)", n.Version, n.Path, t.Version, t.Path)
		}
		checks = append(checks, c)
	}
	return checks, nil
}
