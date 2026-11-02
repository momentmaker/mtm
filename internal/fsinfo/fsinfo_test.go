package fsinfo

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type fakeInfo struct{ sys any }

func (fakeInfo) Name() string       { return "x" }
func (fakeInfo) Size() int64        { return 0 }
func (fakeInfo) Mode() fs.FileMode  { return 0 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any         { return f.sys }

func TestIsDataless(t *testing.T) {
	if !IsDataless(fakeInfo{sys: &syscall.Stat_t{Flags: sfDataless}}) {
		t.Error("dataless flag not detected")
	}
	if IsDataless(fakeInfo{sys: &syscall.Stat_t{}}) {
		t.Error("regular file reported dataless")
	}
	if IsDataless(fakeInfo{}) {
		t.Error("missing stat reported dataless")
	}
}

func TestOtherVolume(t *testing.T) {
	onHome := fakeInfo{sys: &syscall.Stat_t{Dev: 5}}
	onOther := fakeInfo{sys: &syscall.Stat_t{Dev: 6}}
	other := OtherVolume(onHome)
	if other(onHome) {
		t.Error("item on the same volume reported as other")
	}
	if !other(onOther) {
		t.Error("item on a different volume not detected")
	}
	if other(fakeInfo{}) {
		t.Error("item without a volume id reported as other")
	}
	if OtherVolume(fakeInfo{})(onOther) {
		t.Error("unknown reference volume reported an item as other")
	}
}

func TestHashFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	os.WriteFile(path, []byte("hello\n"), 0o600)
	got, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("hello\n"))); got != want {
		t.Errorf("HashFile = %q, want %q", got, want)
	}
}
