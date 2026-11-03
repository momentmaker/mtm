package stage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/runner"
)

func TestVolumeOfATempFolder(t *testing.T) {
	vol, err := VolumeOf(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if vol.Type != "apfs" || vol.MountPoint == "" || vol.IgnoresOwners {
		t.Errorf("volume = %+v, want an APFS volume that honors owners", vol)
	}
}

const diskutilInfo = "/usr/sbin/diskutil info -plist /Volumes/Stage"
const plutilEncryption = "/usr/bin/plutil -extract Encryption raw -o - -"

func diskutilSays(encryption runner.Response) *runner.Fake {
	return &runner.Fake{Responses: map[string]runner.Response{
		diskutilInfo:     {Stdout: "<plist>report</plist>"},
		plutilEncryption: encryption,
	}}
}

func TestCheckExternal(t *testing.T) {
	apfs := Volume{MountPoint: "/Volumes/Stage", Type: "apfs"}
	tests := []struct {
		name string
		vol  Volume
		r    runner.Runner
		want string
	}{
		{"encrypted APFS", apfs, diskutilSays(runner.Response{Stdout: "true\n"}), ""},
		{"not APFS", Volume{MountPoint: "/Volumes/Stage", Type: "exfat"}, &runner.Fake{}, "is exfat"},
		{"ignores owners", Volume{MountPoint: "/Volumes/Stage", Type: "apfs", IgnoresOwners: true}, &runner.Fake{}, "sudo diskutil enableOwnership '/Volumes/Stage'"},
		{"not encrypted", apfs, diskutilSays(runner.Response{Stdout: "false\n"}), "is not encrypted"},
		{"no Encryption key", apfs, diskutilSays(runner.Response{Code: 1}), "is not encrypted"},
	}
	for _, tt := range tests {
		err := CheckExternal(context.Background(), tt.r, tt.vol)
		if tt.want == "" {
			if err != nil {
				t.Errorf("%s: err = %v, want nil", tt.name, err)
			}
			continue
		}
		if !errors.Is(err, ErrUnsafeVolume) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want ErrUnsafeVolume with %q", tt.name, err, tt.want)
		}
	}
}

func TestCheckExternalHandsDiskutilsReportToPlutil(t *testing.T) {
	f := diskutilSays(runner.Response{Stdout: "true\n"})
	if err := CheckExternal(context.Background(), f, Volume{MountPoint: "/Volumes/Stage", Type: "apfs"}); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[1].Stdin != "<plist>report</plist>" {
		t.Errorf("calls = %+v, want plutil to read diskutil's report", calls)
	}
}

func TestCheckExternalFailsWhenDiskutilFails(t *testing.T) {
	f := &runner.Fake{Responses: map[string]runner.Response{diskutilInfo: {Code: 1, Stderr: "Could not find disk"}}}
	err := CheckExternal(context.Background(), f, Volume{MountPoint: "/Volumes/Stage", Type: "apfs"})
	if err == nil || errors.Is(err, ErrUnsafeVolume) {
		t.Errorf("err = %v, want diskutil's failure", err)
	}
}
