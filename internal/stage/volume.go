package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/runner"
)

// Volume is the file system holding a folder, as statfs reports it.
type Volume struct {
	MountPoint    string
	Type          string
	IgnoresOwners bool
}

// mntIgnoreOwnership is MNT_IGNORE_OWNERSHIP from <sys/mount.h>, which Go's
// syscall package doesn't define.
const mntIgnoreOwnership = 0x00200000

var ErrUnsafeVolume = errors.New("prepare --stage needs an encrypted APFS volume that honors file ownership")

func VolumeOf(path string) (Volume, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return Volume{}, fmt.Errorf("inspect the volume holding %s: %w", path, err)
	}
	return Volume{
		MountPoint:    cString(fs.Mntonname[:]),
		Type:          cString(fs.Fstypename[:]),
		IgnoresOwners: fs.Flags&mntIgnoreOwnership != 0,
	}, nil
}

func cString(b []int8) string {
	var sb strings.Builder
	for _, c := range b {
		if c == 0 {
			break
		}
		sb.WriteByte(byte(c))
	}
	return sb.String()
}

// CheckExternal refuses a volume that would leave exports readable: one
// that isn't APFS, isn't encrypted, or ignores file ownership, which makes
// a 0700 folder open to every account. statfs can't tell encryption;
// diskutil can.
func CheckExternal(ctx context.Context, r runner.Runner, vol Volume) error {
	if vol.Type != "apfs" {
		return fmt.Errorf("%w: %s is %s. Erase a drive as APFS (Encrypted) in Disk Utility, or leave out --stage", ErrUnsafeVolume, vol.MountPoint, vol.Type)
	}
	if vol.IgnoresOwners {
		return fmt.Errorf("%w: %s ignores file ownership, so other accounts could read the exports. Run: sudo diskutil enableOwnership %s", ErrUnsafeVolume, vol.MountPoint, macos.ShellQuote(vol.MountPoint))
	}
	encrypted, err := encrypted(ctx, r, vol.MountPoint)
	if err != nil {
		return err
	}
	if !encrypted {
		return fmt.Errorf("%w: %s is not encrypted. Encrypt it (Finder: right-click the drive > Encrypt), or leave out --stage", ErrUnsafeVolume, vol.MountPoint)
	}
	return nil
}

// encrypted reads the Encryption key of diskutil's report. plutil exits 1
// when the key is absent, which diskutil does for file systems that can't
// be encrypted.
func encrypted(ctx context.Context, r runner.Runner, mount string) (bool, error) {
	info, err := r.Run(ctx, runner.Command{Name: "/usr/sbin/diskutil", Args: []string{"info", "-plist", mount}})
	if err != nil {
		return false, fmt.Errorf("ask diskutil about %s: %w", mount, err)
	}
	out, err := r.Run(ctx, runner.Command{Name: "/usr/bin/plutil", Args: []string{"-extract", "Encryption", "raw", "-o", "-", "-"}, Stdin: string(info.Stdout)})
	if code, ok := runner.ExitCode(err); ok && code == 1 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read diskutil's report on %s: %w", mount, err)
	}
	return strings.TrimSpace(string(out.Stdout)) == "true", nil
}
