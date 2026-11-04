package toolchains

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/testutil"
)

type fixture struct {
	home, bin1, bin2 string
	fake             *runner.Fake
}

// newFixture puts node in two PATH folders (the first must win) and go in
// the second, under a home folder.
func newFixture(t *testing.T, shell string) fixture {
	t.Helper()
	home := t.TempDir()
	f := fixture{home: home, bin1: filepath.Join(home, ".asdf", "shims"), bin2: filepath.Join(home, "bin"), fake: &runner.Fake{Responses: map[string]runner.Response{}}}
	for _, exe := range []string{filepath.Join(f.bin1, "node"), filepath.Join(f.bin2, "node"), filepath.Join(f.bin2, "go")} {
		testutil.WriteFile(t, exe, "#!/bin/sh\n")
		if err := os.Chmod(exe, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	testutil.WriteFile(t, filepath.Join(f.bin2, "java"), "not executable")
	f.fake.Responses["/usr/bin/dscl . -read /Users/dev UserShell"] = runner.Response{Stdout: "UserShell: " + shell + "\n"}
	// No system folders in this PATH: macOS ships git, python3, and java
	// stubs in /usr/bin, which would make the expected tool list depend on
	// the test machine.
	f.fake.Responses[pathCommand(shell).String()] = runner.Response{Stdout: pathMarker + f.bin1 + ":" + f.bin2 + "\n"}
	f.fake.Responses[filepath.Join(f.bin1, "node")+" --version"] = runner.Response{Stdout: "v22.1.0\n"}
	f.fake.Responses[filepath.Join(f.bin2, "go")+" version"] = runner.Response{Stdout: "go version go1.26.1 darwin/arm64\n"}
	return f
}

func (f fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func TestPathCommandPerShell(t *testing.T) {
	tests := map[string][]string{
		"/opt/homebrew/bin/fish": {"-l", "-c"},
		"/bin/zsh":               {"-l", "-i", "-c"},
		"/bin/bash":              {"-l", "-i", "-c"},
		"/bin/tcsh":              {"-l", "-c"},
	}
	for shell, flags := range tests {
		cmd := pathCommand(shell)
		if cmd.Name != shell || !slices.Equal(cmd.Args[:len(cmd.Args)-1], flags) || !strings.Contains(cmd.Args[len(cmd.Args)-1], pathMarker) {
			t.Errorf("pathCommand(%s) = %+v", shell, cmd)
		}
	}
	if fish := pathCommand("/opt/homebrew/bin/fish"); !strings.Contains(fish.Args[2], "string join : $PATH") {
		t.Errorf("fish command = %q", fish.Args[2])
	}
}

func TestParsePath(t *testing.T) {
	out := []byte("Last login: today\n" + pathMarker + "/old\nnoise\n" + pathMarker + "/a:/b\n")
	if p, ok := parsePath(out); !ok || p != "/a:/b" {
		t.Errorf("parsePath = %q, %v", p, ok)
	}
	if _, ok := parsePath([]byte("no marker here\n")); ok {
		t.Error("parsePath found a PATH without the marker")
	}
}

func TestCollectFindsToolsInPathOrder(t *testing.T) {
	f := newFixture(t, "/opt/homebrew/bin/fish")
	inv, err := Module{User: "dev"}.collect(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []Tool{
		{Name: "node", Path: "~/.asdf/shims/node", Version: "v22.1.0"},
		{Name: "go", Path: "~/bin/go", Version: "go version go1.26.1 darwin/arm64"},
	}
	if inv.Shell != "/opt/homebrew/bin/fish" || !slices.Equal(inv.Tools, want) {
		t.Errorf("inventory = %+v, want tools %+v", inv, want)
	}
}

func TestCollectToleratesNoisyShell(t *testing.T) {
	f := newFixture(t, "/bin/zsh")
	f.fake.Responses[pathCommand("/bin/zsh").String()] = runner.Response{
		Stdout: "/Users/dev/.zprofile:3: no such file\n" + pathMarker + f.bin1 + ":" + f.bin2 + "\n",
		Code:   1,
	}
	inv, err := Module{User: "dev"}.collect(f.env(nil))
	if err != nil || len(inv.Tools) != 2 {
		t.Errorf("collect = %+v, %v; want both tools despite the noisy, failing shell", inv, err)
	}
}

func TestCollectFailsWithoutPATH(t *testing.T) {
	f := newFixture(t, "/bin/zsh")
	f.fake.Responses[pathCommand("/bin/zsh").String()] = runner.Response{Stdout: "zsh: broken\n", Code: 1}
	if _, err := (Module{User: "dev"}).collect(f.env(nil)); err == nil || !strings.Contains(err.Error(), "read PATH") {
		t.Errorf("err = %v, want a read PATH error", err)
	}
}

func TestVersionFallsBackToStderr(t *testing.T) {
	f := newFixture(t, "/opt/homebrew/bin/fish")
	java := filepath.Join(f.bin2, "java")
	os.Chmod(java, 0o755)
	f.fake.Responses[java+" -version"] = runner.Response{Stderr: "openjdk version \"21.0.2\" 2024-01-16\nOpenJDK Runtime\n"}
	inv, err := Module{User: "dev"}.collect(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(inv.Tools, Tool{Name: "java", Path: "~/bin/java", Version: `openjdk version "21.0.2" 2024-01-16`}) {
		t.Errorf("tools = %+v", inv.Tools)
	}
}

// brew --version is one of these commands, and it must not update
// Homebrew or send analytics on mtm's behalf.
func TestVersionCommandsKeepHomebrewQuiet(t *testing.T) {
	f := newFixture(t, "/opt/homebrew/bin/fish")
	if _, err := (Module{User: "dev"}).collect(f.env(nil)); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(f.bin1, "node")
	i := slices.IndexFunc(f.fake.Calls(), func(c runner.Command) bool { return c.Name == node })
	if i < 0 {
		t.Fatal("node --version did not run")
	}
	env := f.fake.Calls()[i].Env
	for _, want := range []string{"PATH=" + f.bin1 + ":" + f.bin2, "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ANALYTICS=1", "HOMEBREW_NO_ENV_HINTS=1", "HOMEBREW_NO_INSTALL_CLEANUP=1"} {
		if !slices.Contains(env, want) {
			t.Errorf("env = %q, want it to contain %q", env, want)
		}
	}
}

func TestVerify(t *testing.T) {
	old := Inventory{Shell: "/opt/homebrew/bin/fish", Tools: []Tool{
		{Name: "node", Path: "~/.asdf/shims/node", Version: "v22.1.0"},
		{Name: "go", Path: "~/bin/go", Version: "go version go1.26.1 darwin/arm64"},
		{Name: "rustc", Path: "~/.cargo/bin/rustc", Version: "rustc 1.91.0"},
	}}
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), old); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, "/opt/homebrew/bin/fish")
	f.fake.Responses[filepath.Join(f.bin2, "go")+" version"] = runner.Response{Stdout: "go version go1.25.0 darwin/arm64\n"}
	checks, err := Module{User: "dev"}.Verify(f.env(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]module.Status{}
	for _, c := range checks {
		status[c.ID] = c.Status
	}
	want := map[string]module.Status{"toolchains:node": module.StatusPass, "toolchains:go": module.StatusFail, "toolchains:rustc": module.StatusFail}
	for id, s := range want {
		if status[id] != s {
			t.Errorf("%s = %s, want %s", id, status[id], s)
		}
	}
}

func TestVerifyShellFailure(t *testing.T) {
	m := manifest.New("test")
	m.SetModule(Name, time.Now(), Inventory{Shell: "/bin/zsh", Tools: []Tool{{Name: "node", Path: "~/bin/node", Version: "v22"}}})
	f := newFixture(t, "/bin/zsh")
	f.fake.Responses[pathCommand("/bin/zsh").String()] = runner.Response{Stdout: "nothing\n"}
	checks, err := Module{User: "dev"}.Verify(f.env(m), nil)
	if err != nil || len(checks) != 1 || checks[0].ID != "toolchains:shell" || checks[0].Status != module.StatusFail {
		t.Errorf("Verify = %+v, %v", checks, err)
	}
}

func TestScanListsTools(t *testing.T) {
	f := newFixture(t, "/opt/homebrew/bin/fish")
	findings, err := Module{User: "dev"}.Scan(f.env(nil))
	if err != nil || len(findings) != 1 || !strings.Contains(findings[0].Detail, "node v22.1.0") {
		t.Errorf("Scan = %+v, %v", findings, err)
	}
}

// TestApplePlaceholdersCountAsNotInstalled: macOS's /usr/bin/java with no
// JDK fails to report a version. That's a tool that isn't installed, so it
// isn't recorded, while a working tool in the same folder is.
func TestApplePlaceholdersCountAsNotInstalled(t *testing.T) {
	f := newFixture(t, "/opt/homebrew/bin/fish")
	apple := t.TempDir()
	old := appleToolsDir
	appleToolsDir = apple
	t.Cleanup(func() { appleToolsDir = old })
	for _, name := range []string{"java", "python3"} {
		testutil.WriteFile(t, filepath.Join(apple, name), "#!/bin/sh\n")
		os.Chmod(filepath.Join(apple, name), 0o755)
	}
	f.fake.Responses[pathCommand("/opt/homebrew/bin/fish").String()] = runner.Response{Stdout: pathMarker + apple + "\n"}
	f.fake.Responses[filepath.Join(apple, "java")+" -version"] = runner.Response{Code: 1, Stderr: "The operation couldn’t be completed. Unable to locate a Java Runtime.\n"}
	f.fake.Responses[filepath.Join(apple, "python3")+" --version"] = runner.Response{Stdout: "Python 3.9.6\n"}
	inv, err := Module{User: "dev"}.collect(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := []Tool{{Name: "python3", Path: filepath.Join(apple, "python3"), Version: "Python 3.9.6"}}; !slices.Equal(inv.Tools, want) {
		t.Errorf("tools = %+v, want only python3", inv.Tools)
	}
}

// TestAFailingToolOutsideAppleFolderStaysUnknown keeps a real tool that
// can't report its version, so verify still requires it.
func TestAFailingToolOutsideAppleFolderStaysUnknown(t *testing.T) {
	f := newFixture(t, "/opt/homebrew/bin/fish")
	f.fake.Responses[filepath.Join(f.bin2, "go")+" version"] = runner.Response{Code: 1}
	inv, err := Module{User: "dev"}.collect(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(inv.Tools, Tool{Name: "go", Path: "~/bin/go", Version: "unknown"}) {
		t.Errorf("tools = %+v, want go kept as unknown", inv.Tools)
	}
}
