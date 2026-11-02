package system

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/apps"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

func (m Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var old Inventory
	if _, err := module.LoadInventory(env, Name, &old); err != nil {
		return nil, err
	}
	now, err := m.snapshot(env)
	if err != nil {
		return nil, err
	}
	checks := []module.Check{shellCheck(old, now.Inventory), etcCheck(old, now.Inventory), appsCheck(old, now.Inventory)}
	if c, ok := m.rosettaCheck(old); ok {
		checks = append(checks, c)
	}
	return checks, nil
}

func check(id, title string) module.Check {
	return module.Check{Module: Name, ID: Name + ":" + id, Status: module.StatusPass, Title: title}
}

func shellCheck(old, now Inventory) module.Check {
	c := check("shell", "login shell")
	c.Detail = now.Shell
	switch {
	case old.Shell == "":
	case !executable(old.Shell):
		c.Status, c.Detail = module.StatusFail, old.Shell+" is missing; reinstall it, or you can't open a terminal with your shell"
	case now.Shell != old.Shell:
		c.Status, c.Detail = module.StatusWarn, fmt.Sprintf("now %s (was %s); mtm finish prints the command to switch back", now.Shell, old.Shell)
	case !now.ShellListed:
		c.Status, c.Detail = module.StatusWarn, now.Shell+" is not listed in /etc/shells; mtm finish prints the fix"
	}
	return c
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func etcCheck(old, now Inventory) module.Check {
	c := check("etc", "/etc files")
	var changed []string
	for _, rel := range slices.Sorted(maps.Keys(old.Etc)) {
		if now.Etc[rel] != old.Etc[rel] {
			changed = append(changed, "/etc/"+rel)
		}
	}
	c.Detail = words.Count(len(old.Etc), "file matches", "files match")
	if len(changed) > 0 {
		c.Status, c.Paths = module.StatusWarn, changed
		c.Detail = words.Count(len(changed), "file differs", "files differ") + " from the old Mac; mtm finish prints the commands to restore them"
	}
	return c
}

// appsCheck fails on a missing app: a discontinued or paid app may be
// impossible to get again once the old Mac is erased.
func appsCheck(old, now Inventory) module.Check {
	c := check("apps", "apps")
	byID := map[string]apps.App{}
	for _, a := range now.Apps {
		byID[a.BundleID] = a
	}
	var missing, older []string
	for _, a := range old.Apps {
		cur, ok := byID[a.BundleID]
		if !ok {
			missing = append(missing, a.Path)
			continue
		}
		if order, ok := apps.Compare(cur.Version, a.Version); ok && order < 0 {
			older = append(older, fmt.Sprintf("%s (%s, was %s)", appName(a), cur.Version, a.Version))
		}
	}
	c.Detail = words.Count(len(old.Apps)-len(missing)-len(older), "app matches", "apps match")
	if len(older) > 0 {
		c.Status = module.StatusWarn
		c.Detail += "; older: " + strings.Join(older, ", ")
	}
	if len(missing) > 0 {
		c.Status, c.Paths = module.StatusFail, missing
		c.Detail += "; missing: " + words.Count(len(missing), "app", "apps") + " (copy or reinstall them before you erase the old Mac)"
	}
	return c
}

func (m Module) rosettaCheck(old Inventory) (module.Check, bool) {
	if !hasIntelOnly(old.Apps) {
		return module.Check{}, false
	}
	c := check("rosetta", "Rosetta")
	c.Detail = "installed"
	if !m.rosettaInstalled() {
		c.Status, c.Detail = module.StatusWarn, "not installed, so Intel-only apps won't open; mtm finish prints the install command"
	}
	return c, true
}
