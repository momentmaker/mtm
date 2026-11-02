package apps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/momentmaker/mtm/internal/runner"
)

var ctx = context.Background()

type fakeApps struct {
	root string
	fake *runner.Fake
}

func newFakeApps(t *testing.T) fakeApps {
	return fakeApps{root: t.TempDir(), fake: &runner.Fake{Responses: map[string]runner.Response{}}}
}

// add creates an app bundle folder and the plutil and lipo answers for it.
// An empty value makes plutil report the key as absent.
func (f fakeApps) add(t *testing.T, rel, id, version, exe, archs string) {
	t.Helper()
	dir := filepath.Join(f.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Join(dir, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(dir, "Contents", "Info.plist")
	for key, val := range map[string]string{"CFBundleIdentifier": id, "CFBundleShortVersionString": version, "CFBundleExecutable": exe} {
		resp := runner.Response{Stdout: val + "\n"}
		if val == "" {
			resp = runner.Response{Code: 1, Stderr: "Could not extract value"}
		}
		f.fake.Responses["/usr/bin/plutil -extract "+key+" raw -o - "+plist] = resp
	}
	if exe != "" {
		resp := runner.Response{Stdout: archs + "\n"}
		if archs == "" {
			resp = runner.Response{Code: 1, Stderr: "not a Mach-O file"}
		}
		f.fake.Responses["/usr/bin/lipo -archs "+filepath.Join(dir, "Contents", "MacOS", exe)] = resp
	}
}

func TestListFindsAppsAndSkipsApple(t *testing.T) {
	f := newFakeApps(t)
	f.add(t, "Ghostty.app", "com.mitchellh.ghostty", "1.4.0", "ghostty", "x86_64 arm64")
	f.add(t, "Utilities/Tool.app", "com.example.tool", "2.0", "tool", "x86_64")
	f.add(t, "Safari.app", "com.apple.Safari", "26.0", "Safari", "arm64")
	got, err := List(ctx, f.fake, []Root{{Dir: f.root, Display: "/Applications"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("apps = %+v, want Ghostty and Tool", got)
	}
	if got[0].Path != "/Applications/Ghostty.app" || got[0].Version != "1.4.0" || got[0].IntelOnly() {
		t.Errorf("Ghostty = %+v", got[0])
	}
	if got[1].Path != "/Applications/Utilities/Tool.app" || !got[1].IntelOnly() {
		t.Errorf("Tool = %+v", got[1])
	}
}

func TestListHandlesOddBundles(t *testing.T) {
	f := newFakeApps(t)
	f.add(t, "NoVersion.app", "com.example.noversion", "", "nv", "arm64")
	f.add(t, "Script.app", "com.example.script", "1.0", "run.sh", "")
	f.add(t, "NoID.app", "", "1.0", "x", "arm64")
	f.add(t, "Vendor/Inner.app", "com.vendor.inner", "3.1", "inner", "arm64")
	locked := filepath.Join(f.root, "Locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(locked, 0)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if err := os.Symlink(filepath.Join(f.root, "NoVersion.app"), filepath.Join(f.root, "Linked.app")); err != nil {
		t.Fatal(err)
	}
	got, err := List(ctx, f.fake, []Root{{Dir: f.root, Display: "/Applications"}})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]App{}
	for _, a := range got {
		byPath[a.Path] = a
	}
	if len(got) != 3 {
		t.Errorf("apps = %+v, want NoVersion, Script, Vendor/Inner", got)
	}
	if a := byPath["/Applications/NoVersion.app"]; a.BundleID != "com.example.noversion" || a.Version != "" {
		t.Errorf("NoVersion = %+v", a)
	}
	if a := byPath["/Applications/Script.app"]; a.Archs != nil || a.IntelOnly() {
		t.Errorf("Script = %+v, want no archs and not Intel-only", a)
	}
	if _, ok := byPath["/Applications/Vendor/Inner.app"]; !ok {
		t.Error("app in a vendor folder not listed")
	}
}

func TestListMissingRootIsEmpty(t *testing.T) {
	got, err := List(ctx, &runner.Fake{}, []Root{{Dir: filepath.Join(t.TempDir(), "nope"), Display: "~/Applications"}})
	if err != nil || len(got) != 0 {
		t.Errorf("List = %+v, %v", got, err)
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"1.10", "1.9", 1, true},
		{"1.9", "1.10", -1, true},
		{"2.0", "2", 0, true},
		{"1.4.0 (build 7)", "1.4.0 (build 12)", -1, true},
		{"26.0.1", "026.0.1", 0, true},
		{"beta", "1.0", 0, false},
		{"", "1.0", 0, false},
	}
	for _, tt := range tests {
		got, ok := Compare(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}
