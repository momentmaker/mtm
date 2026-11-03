package stage

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/momentmaker/mtm/internal/words"
)

// Reserve is the free space every export leaves on its volume, so a
// migration never fills the disk that macOS and the user's apps need.
const Reserve uint64 = 10_000_000_000

var ErrNoRoom = errors.New("not enough free space")

// Free returns the bytes available to this user on the volume holding path.
func Free(path string) (uint64, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, fmt.Errorf("check free space on %s: %w", path, err)
	}
	return fs.Bavail * uint64(fs.Bsize), nil
}

// Room fails with ErrNoRoom unless need bytes fit on path's volume with
// Reserve to spare. free is Free, or a stand-in in tests.
func Room(free func(string) (uint64, error), path string, need uint64) error {
	avail, err := free(path)
	if err != nil {
		return err
	}
	if avail < Reserve || avail-Reserve < need {
		return fmt.Errorf("%w: the exports need %s, %s has %s free, and mtm keeps %s free", ErrNoRoom, words.Bytes(need), path, words.Bytes(avail), words.Bytes(Reserve))
	}
	return nil
}
