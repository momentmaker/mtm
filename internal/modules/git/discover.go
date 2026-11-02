package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/pathset"
)

// skipDirs holds folders that never contain the user's own repos: app
// state, caches, and package-manager checkouts.
var skipDirs = pathset.MustNew(
	"Library/", ".Trash/", ".mtm/", ".cache/", ".npm/", ".rustup/",
	".cargo/registry/", ".cargo/git/", "go/pkg/",
	"node_modules", ".git",
)

// discover returns home-relative paths of git repos, in sorted walk order,
// down to maxDepth folders below home. Symlinks are not followed.
func discover(ctx context.Context, home string, maxDepth int) ([]string, error) {
	var repos []string
	var visit func(rel string, depth int) error
	visit = func(rel string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		abs := filepath.Join(home, filepath.FromSlash(rel))
		entries, err := os.ReadDir(abs)
		switch {
		case errors.Is(err, fs.ErrPermission), errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return fmt.Errorf("read %s: %w", abs, err)
		}
		if slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == ".git" }) {
			repos = append(repos, cmp.Or(rel, "."))
		}
		if depth == maxDepth {
			return nil
		}
		for _, e := range entries {
			child := path.Join(rel, e.Name())
			if !e.IsDir() || skipDirs.Match(child) {
				continue
			}
			info, err := e.Info()
			if err != nil || fsinfo.IsDataless(info) {
				continue
			}
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit("", 0); err != nil {
		return nil, err
	}
	return repos, nil
}
