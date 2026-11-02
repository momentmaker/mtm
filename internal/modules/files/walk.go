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
	resolves   bool
	cloud      bool
	unreadable bool
	mount      bool
}

// walkHome visits everything under home in sorted order without following
// symlinks. It skips paths in skip and non-regular files such as sockets.
// It never opens cloud-only items, and it reports folders it cannot read as
// unreadable instead of failing. It never crosses into another volume: a
// network mount can be slow or hang, and a missing one would read as lost
// data on the new Mac.
func walkHome(ctx context.Context, home string, skip pathset.Set, emit func(item) error) error {
	info, err := os.Stat(home)
	if err != nil {
		return fmt.Errorf("read home folder %s: %w", home, err)
	}
	w := walker{ctx: ctx, home: home, skip: skip, emit: emit, otherVolume: fsinfo.OtherVolume(info)}
	return w.walk()
}

// walker holds one walk. otherVolume is a field so unit tests can stand in
// a mount point without attaching a real volume.
type walker struct {
	ctx         context.Context
	home        string
	skip        pathset.Set
	emit        func(item) error
	otherVolume func(fs.FileInfo) bool
}

func (w walker) walk() error {
	entries, err := os.ReadDir(w.home)
	if err != nil {
		return fmt.Errorf("read home folder %s: %w", w.home, err)
	}
	return w.visitEntries("", entries)
}

func (w walker) visitEntries(rel string, entries []fs.DirEntry) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	for _, e := range entries {
		childRel := path.Join(rel, e.Name())
		if w.skip.Match(childRel) {
			continue
		}
		if err := w.visit(childRel); err != nil {
			return err
		}
	}
	return nil
}

func (w walker) visit(rel string) error {
	abs := filepath.Join(w.home, filepath.FromSlash(rel))
	info, err := os.Lstat(abs)
	if err != nil {
		return skipOrUnreadable(err, rel, abs, w.emit)
	}
	it := item{rel: rel, abs: abs, info: info}
	switch mode := info.Mode(); {
	case fsinfo.IsDataless(info):
		it.cloud = true
		return w.emit(it)
	case mode&fs.ModeSymlink != 0:
		if it.target, err = os.Readlink(abs); err != nil {
			return skipOrUnreadable(err, rel, abs, w.emit)
		}
		_, err = os.Stat(abs)
		it.resolves = err == nil
		return w.emit(it)
	case mode.IsDir() && w.otherVolume(info):
		it.mount = true
		return w.emit(it)
	case mode.IsDir():
		entries, err := os.ReadDir(abs)
		if err != nil {
			return skipOrUnreadable(err, rel, abs, w.emit)
		}
		if err := w.emit(it); err != nil {
			return err
		}
		return w.visitEntries(rel, entries)
	case mode.IsRegular():
		return w.emit(it)
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
