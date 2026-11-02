// Package apps lists the applications installed on a Mac.
package apps

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/runner"
)

type App struct {
	Path     string   `json:"path"`
	BundleID string   `json:"bundle_id"`
	Version  string   `json:"version,omitempty"`
	Archs    []string `json:"archs,omitempty"`
}

// IntelOnly reports an app that needs Rosetta on Apple Silicon. arm64e,
// the pointer-authentication variant, runs natively too.
func (a App) IntelOnly() bool {
	return len(a.Archs) > 0 && !slices.Contains(a.Archs, "arm64") && !slices.Contains(a.Archs, "arm64e")
}

// Root is a folder of applications. Display replaces Dir in recorded paths,
// so ~/Applications compares equal across home folders.
type Root struct {
	Dir     string
	Display string
}

// List finds .app bundles in each root and one level of subfolders. It
// skips symlinks and Apple's own apps, which come with macOS.
func List(ctx context.Context, r runner.Runner, roots []Root) ([]App, error) {
	var out []App
	for _, root := range roots {
		bundles, err := bundlesIn(root.Dir)
		if err != nil {
			return nil, err
		}
		for _, rel := range bundles {
			app, ok, err := inspect(ctx, r, root, rel)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, app)
			}
		}
	}
	return out, nil
}

func bundlesIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var rels []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".app") {
			rels = append(rels, e.Name())
			continue
		}
		sub, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			// A vendor folder we can't read holds no app we could inspect.
			continue
		}
		for _, s := range sub {
			if s.IsDir() && strings.HasSuffix(s.Name(), ".app") {
				rels = append(rels, e.Name()+"/"+s.Name())
			}
		}
	}
	return rels, nil
}

func inspect(ctx context.Context, r runner.Runner, root Root, rel string) (App, bool, error) {
	dir := filepath.Join(root.Dir, filepath.FromSlash(rel))
	plist := filepath.Join(dir, "Contents", "Info.plist")
	id, err := macos.PlistValue(ctx, r, plist, "CFBundleIdentifier")
	if err != nil {
		return App{}, false, err
	}
	if id == "" || strings.HasPrefix(id, "com.apple.") {
		return App{}, false, nil
	}
	version, err := macos.PlistValue(ctx, r, plist, "CFBundleShortVersionString")
	if err != nil {
		return App{}, false, err
	}
	exe, err := macos.PlistValue(ctx, r, plist, "CFBundleExecutable")
	if err != nil {
		return App{}, false, err
	}
	app := App{Path: root.Display + "/" + rel, BundleID: id, Version: version}
	if exe != "" {
		app.Archs = macos.Archs(ctx, r, filepath.Join(dir, "Contents", "MacOS", exe))
	}
	return app, true, nil
}

var digits = regexp.MustCompile(`\d+`)

// Compare orders version strings by their numeric parts, so "1.10" is newer
// than "1.9". ok is false when either has no digits; then only equality of
// the raw strings means anything.
func Compare(a, b string) (int, bool) {
	pa, pb := digits.FindAllString(a, -1), digits.FindAllString(b, -1)
	if len(pa) == 0 || len(pb) == 0 {
		return 0, false
	}
	for i := range max(len(pa), len(pb)) {
		x, y := part(pa, i), part(pb, i)
		if len(x) != len(y) {
			return cmp.Compare(len(x), len(y)), true
		}
		if c := strings.Compare(x, y); c != 0 {
			return c, true
		}
	}
	return 0, true
}

func part(parts []string, i int) string {
	if i < len(parts) {
		return strings.TrimLeft(parts[i], "0")
	}
	return ""
}
