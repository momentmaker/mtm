package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"syscall"

	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/pathset"
)

type item struct {
	rel        string
	abs        string
	info       fs.FileInfo
	target     string
	cloud      bool
	unreadable bool
}

// walkHome visits everything under home in sorted order without following
// symlinks. It skips paths in skip and non-regular files such as sockets.
// It never opens cloud-only items, and it reports folders it cannot read as
// unreadable instead of failing.
func walkHome(ctx context.Context, home string, skip pathset.Set, emit func(item) error) error {
	entries, err := os.ReadDir(home)
	if err != nil {
		return fmt.Errorf("read home folder %s: %w", home, err)
	}
	return visitEntries(ctx, home, "", entries, skip, emit)
}

func visitEntries(ctx context.Context, home, rel string, entries []fs.DirEntry, skip pathset.Set, emit func(item) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, e := range entries {
		childRel := path.Join(rel, e.Name())
		if skip.Match(childRel) {
			continue
		}
		if err := visit(ctx, home, childRel, skip, emit); err != nil {
			return err
		}
	}
	return nil
}

func visit(ctx context.Context, home, rel string, skip pathset.Set, emit func(item) error) error {
	abs := filepath.Join(home, filepath.FromSlash(rel))
	info, err := os.Lstat(abs)
	if err != nil {
		return skipOrUnreadable(err, rel, abs, emit)
	}
	it := item{rel: rel, abs: abs, info: info}
	switch mode := info.Mode(); {
	case fsinfo.IsDataless(info):
		it.cloud = true
		return emit(it)
	case mode&fs.ModeSymlink != 0:
		if it.target, err = os.Readlink(abs); err != nil {
			return skipOrUnreadable(err, rel, abs, emit)
		}
		return emit(it)
	case mode.IsDir():
		entries, err := os.ReadDir(abs)
		if err != nil {
			return skipOrUnreadable(err, rel, abs, emit)
		}
		if err := emit(it); err != nil {
			return err
		}
		return visitEntries(ctx, home, rel, entries, skip, emit)
	case mode.IsRegular():
		return emit(it)
	}
	return nil
}

// skipOrUnreadable keeps one bad item from aborting the walk. An item that
// vanished mid-walk is dropped; any other failure (permissions, a stale
// mount, an I/O error) is recorded as unreadable.
func skipOrUnreadable(err error, rel, abs string, emit func(item) error) error {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil
	}
	return emit(item{rel: rel, abs: abs, unreadable: true})
}
