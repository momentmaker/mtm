package files

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/momentmaker/mtm/internal/pathset"
	"github.com/momentmaker/mtm/internal/testutil"
)

func TestSkipOrUnreadable(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantUnreadable bool
	}{
		{"missing item is skipped", fs.ErrNotExist, false},
		{"not a directory is skipped", syscall.ENOTDIR, false},
		{"wrapped missing item is skipped", &fs.PathError{Op: "readlink", Path: "x", Err: syscall.ENOENT}, false},
		{"permission denied is unreadable", syscall.EACCES, true},
		{"I/O error is unreadable", syscall.EIO, true},
		{"wrapped I/O error is unreadable", &fs.PathError{Op: "lstat", Path: "x", Err: syscall.EIO}, true},
		{"timeout is unreadable", fmt.Errorf("stat: %w", syscall.ETIMEDOUT), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []item
			err := skipOrUnreadable(tt.err, "a/b", "/home/a/b", func(it item) error {
				got = append(got, it)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantUnreadable {
				if len(got) != 1 || !got[0].unreadable || got[0].rel != "a/b" {
					t.Errorf("emitted %+v, want one unreadable item", got)
				}
			} else if len(got) != 0 {
				t.Errorf("emitted %+v, want nothing", got)
			}
		})
	}
}

func TestWalkRecordsMountPointWithoutDescending(t *testing.T) {
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, "mnt/inside.txt"), "x")
	testutil.WriteFile(t, filepath.Join(home, "Docs/a.txt"), "y")
	var got []string
	var mounts []string
	w := walker{
		ctx:         context.Background(),
		home:        home,
		skip:        pathset.MustNew(),
		otherVolume: func(info fs.FileInfo) bool { return info.Name() == "mnt" },
		emit: func(it item) error {
			got = append(got, it.rel)
			if it.mount {
				mounts = append(mounts, it.rel)
			}
			return nil
		},
	}
	if err := w.walk(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Docs", "Docs/a.txt", "mnt"}; !slices.Equal(got, want) {
		t.Errorf("walked %q, want %q", got, want)
	}
	if !slices.Equal(mounts, []string{"mnt"}) {
		t.Errorf("mounts = %q, want [mnt]", mounts)
	}
}
