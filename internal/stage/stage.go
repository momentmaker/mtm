// Package stage manages ~/.mtm, the private folder holding mtm's record,
// exports, state, and logs.
package stage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/runner"
)

const (
	DirName = ".mtm"
	// ExportsDirName is the exports folder inside ~/.mtm.
	ExportsDirName = "exports"
	// ExternalDirName is the exports folder prepare --stage makes on an
	// external volume. cleanup deletes only a folder of this name.
	ExternalDirName = "mtm-exports"
)

var ErrNotFound = errors.New("no ~/.mtm folder")

type Stage struct {
	Root string
}

// Open returns the stage folder under home, creating it with mode 0700
// when it does not exist.
func Open(home string) (*Stage, error) {
	return OpenDir(filepath.Join(home, DirName))
}

// OpenDir returns the private folder root, creating it with mode 0700 when
// it does not exist. Exports live in one: ~/.mtm/exports, or the folder
// prepare --stage makes on an external volume.
func OpenDir(root string) (*Stage, error) {
	if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("create %s: %w", root, err)
	}
	if err := checkSafe(root); err != nil {
		return nil, err
	}
	return &Stage{Root: root}, nil
}

// Existing returns the stage folder under home only if it already exists.
func Existing(home string) (*Stage, error) {
	root := filepath.Join(home, DirName)
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w; run mtm prepare on the old Mac first", root, ErrNotFound)
	}
	return ExistingDir(root)
}

// ExistingDir returns the folder root if it exists and is private to this
// user.
func ExistingDir(root string) (*Stage, error) {
	if err := checkSafe(root); err != nil {
		return nil, err
	}
	return &Stage{Root: root}, nil
}

func checkSafe(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", root, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to use it. Remove it and re-run", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a folder; refusing to use it", root)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect %s: no owner information", root)
	}
	if int(st.Uid) != os.Getuid() {
		return wrongOwner(root, st.Uid)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%s has permissions %04o but must be private. Fix with: chmod 700 %s", root, perm, macos.ShellQuote(root))
	}
	return nil
}

// wrongOwner explains a stage folder another uid owns, as happens when
// Migration Assistant gives the account a new uid on the new Mac.
func wrongOwner(root string, uid uint32) error {
	return fmt.Errorf("%s is owned by uid %d, not you (uid %d). If Migration Assistant gave your account a new uid, run: sudo chown -R $(id -un) %s", root, uid, os.Getuid(), macos.ShellQuote(root))
}

func (s *Stage) Path(elem ...string) string {
	return filepath.Join(append([]string{s.Root}, elem...)...)
}

func (s *Stage) MkdirAll(name string) error {
	if err := os.MkdirAll(s.Path(name), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", s.Path(name), err)
	}
	return nil
}

// File is a private (0600) file that appears at its final path only after Commit.
type File struct {
	*os.File
	final string
	done  bool
}

func (s *Stage) Create(name string) (*File, error) {
	final := s.Path(name)
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(final)+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", final, err)
	}
	return &File{File: f, final: final}, nil
}

func (f *File) Commit() error {
	if err := f.File.Sync(); err != nil {
		f.Abort()
		return fmt.Errorf("sync %s: %w", f.final, err)
	}
	if err := f.File.Close(); err != nil {
		os.Remove(f.Name())
		f.done = true
		return fmt.Errorf("close %s: %w", f.final, err)
	}
	if err := os.Rename(f.Name(), f.final); err != nil {
		os.Remove(f.Name())
		f.done = true
		return fmt.Errorf("save %s: %w", f.final, err)
	}
	f.done = true
	return nil
}

func (f *File) Abort() {
	if f.done {
		return
	}
	f.File.Close()
	os.Remove(f.Name())
	f.done = true
}

func (s *Stage) WriteFile(name string, data []byte) error {
	f, err := s.Create(name)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Abort()
		return fmt.Errorf("write %s: %w", f.final, err)
	}
	return f.Commit()
}

// ExcludeFromBackups keeps database dumps and the record out of Time Machine.
func (s *Stage) ExcludeFromBackups(ctx context.Context, r runner.Runner) error {
	if _, err := r.Run(ctx, runner.Command{Name: "/usr/bin/tmutil", Args: []string{"addexclusion", s.Root}}); err != nil {
		return fmt.Errorf("exclude %s from Time Machine: %w", s.Root, err)
	}
	return nil
}

func (s *Stage) Remove() error {
	if err := os.RemoveAll(s.Root); err != nil {
		return fmt.Errorf("delete %s: %w", s.Root, err)
	}
	return nil
}
