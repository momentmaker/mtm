package git

import (
	"cmp"
	"context"
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
// state, caches, package-manager checkouts, and the clones version
// managers, shell frameworks, and editor and agent plugin managers keep
// and download again. Their files are still recorded and checked by the
// files module; only the git checks skip them.
var skipDirs = pathset.MustNew(
	"Library/", ".Trash/", ".mtm/", ".cache/", ".npm/", ".rustup/",
	".cargo/registry/", ".cargo/git/", "go/pkg/",
	".asdf/", ".nvm/", ".pyenv/", ".rbenv/",
	".oh-my-zsh/", ".tmux/plugins/", ".vim/plugged/", ".local/share/nvim/",
	".claude/plugins/",
	"node_modules", ".git",
)

// discover returns home-relative paths of git repos, in sorted walk order,
// down to maxDepth folders below home. Symlinks are not followed, and the
// search stays on the home folder's volume, like the files walk.
func discover(ctx context.Context, home string, maxDepth int) ([]string, error) {
	info, err := os.Stat(home)
	if err != nil {
		return nil, fmt.Errorf("read home folder %s: %w", home, err)
	}
	f := &finder{ctx: ctx, home: home, maxDepth: maxDepth, readDir: os.ReadDir, otherVolume: fsinfo.OtherVolume(info)}
	return f.find()
}

// finder holds one discovery walk. readDir and otherVolume are fields so
// tests can make a folder fail to read or stand in a mount point.
type finder struct {
	ctx         context.Context
	home        string
	maxDepth    int
	readDir     func(string) ([]fs.DirEntry, error)
	otherVolume func(fs.FileInfo) bool
	repos       []string
}

func (f *finder) find() ([]string, error) {
	if err := f.visit("", 0); err != nil {
		return nil, err
	}
	return f.repos, nil
}

// visit skips a folder below home that can't be read, as the files walk
// does, because the files module records it as unreadable. Only the home
// folder itself is required.
func (f *finder) visit(rel string, depth int) error {
	if err := f.ctx.Err(); err != nil {
		return err
	}
	abs := filepath.Join(f.home, filepath.FromSlash(rel))
	entries, err := f.readDir(abs)
	switch {
	case err != nil && rel == "":
		return fmt.Errorf("read home folder %s: %w", abs, err)
	case err != nil:
		return nil
	}
	if slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == ".git" }) {
		f.repos = append(f.repos, cmp.Or(rel, "."))
	}
	if depth == f.maxDepth {
		return nil
	}
	for _, e := range entries {
		child := path.Join(rel, e.Name())
		if !e.IsDir() || skipDirs.Match(child) {
			continue
		}
		info, err := e.Info()
		if err != nil || fsinfo.IsDataless(info) || f.otherVolume(info) {
			continue
		}
		if err := f.visit(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}
