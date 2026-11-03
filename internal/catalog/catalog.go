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
