//go:build integration

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/testutil"
)

// denyNetwork blocks every outbound IP connection but leaves local sockets,
// which launchctl and brew services need.
const denyNetwork = "(version 1)(allow default)(deny network-outbound (remote ip))"

func requireIntegrationHost(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("needs an Apple Silicon Mac")
	}
	if out, err := exec.Command("/usr/bin/fdesetup", "status").Output(); err != nil || !strings.Contains(string(out), "FileVault is On") {
		t.Skip("needs FileVault on, as prepare does")
	}
	const brew = "/opt/homebrew/bin/brew"
	if _, err := os.Stat(brew); err != nil {
		t.Skip("needs Homebrew in /opt/homebrew")
	}
	cacheCmd := exec.Command(brew, "--cache")
	cacheCmd.Env = append(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1")
	cacheOut, err := cacheCmd.Output()
	if err != nil {
		t.Fatalf("brew --cache: %v", err)
	}
	cache := strings.TrimSpace(string(cacheOut))
	cmd := exec.Command(brew, "services", "list", "--json")
	cmd.Env = append(os.Environ(), "HOMEBREW_NO_AUTO_UPDATE=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("brew services list: %v", err)
	}
	var services []struct {
		Status string  `json:"status"`
		User   *string `json:"user"`
	}
	if err := json.Unmarshal(out, &services); err != nil {
		t.Fatal(err)
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range services {
		if s.Status == "started" && s.User != nil && *s.User == me.Username {
			t.Skip("Homebrew services are running; prepare would stop them on this Mac")
		}
	}
	return cache
}

// TestNoNetwork runs mtm under a sandbox that denies network access, with
// a temporary home folder, so every git, brew, and shell command mtm runs
// must work offline. loginitems is left out because it asks System Events,
// which would show a permission prompt.
func TestNoNetwork(t *testing.T) {
	cache := requireIntegrationHost(t)
	bin := filepath.Join(t.TempDir(), "mtm")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	steps := []struct {
		args  []string
		codes []int
	}{
		{[]string{"scan"}, []int{0, 1}},
		{[]string{"prepare", "--only", "files,git,system,homebrew,toolchains"}, []int{0, 1}},
		{[]string{"prepare", "--undo"}, []int{0, 1}},
		{[]string{"prepare", "--only", "files,git,system,homebrew,toolchains"}, []int{0, 1}},
		{[]string{"verify"}, []int{1}},
	}
	for _, s := range steps {
		cmd := exec.Command("/usr/bin/sandbox-exec", append([]string{"-p", denyNetwork, bin}, s.args...)...)
		// The temp home would hide Homebrew's API cache, which every real
		// install has; with no cache brew downloads formula data. That
		// empty-cache case is tracked separately.
		cmd.Env = append(os.Environ(), "HOME="+home, "HOMEBREW_CACHE="+cache)
		out, err := cmd.CombinedOutput()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("mtm %s: %v", strings.Join(s.args, " "), err)
		}
		if !slices.Contains(s.codes, code) {
			t.Errorf("mtm %s exited %d, want one of %v:\n%s", strings.Join(s.args, " "), code, s.codes, out)
		}
		for _, sign := range []string{"Could not resolve", "nodename nor servname", "Network is unreachable", "network is unreachable", "Failed to connect"} {
			if strings.Contains(string(out), sign) {
				t.Errorf("mtm %s tried the network (%q):\n%s", strings.Join(s.args, " "), sign, out)
			}
		}
	}
}
