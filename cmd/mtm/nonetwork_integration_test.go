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

// networkSigns are what curl, git, and the resolver print when the sandbox
// refuses a connection. A clean run prints none of them.
var networkSigns = []string{
	"Could not resolve", "nodename nor servname", "Network is unreachable", "network is unreachable",
	"Failed to connect", "curl: (", "Operation not permitted",
}

// homebrewFailures mean a brew command failed, which a refused download
// can cause without printing any network sign.
var homebrewFailures = []string{
	"Could not save a Brewfile", "could not check packages", "Homebrew's package data isn't cached",
	"homebrew: scan failed", "homebrew: prepare failed", "homebrew: undo failed", "homebrew: verify failed",
}

type brewService struct {
	Name   string  `json:"name"`
	Status string  `json:"status"`
	User   *string `json:"user"`
	File   string  `json:"file"`
}

// requireIntegrationHost skips unless prepare can run here without
// stopping anything or blocking, and returns Homebrew's cache folder.
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
	var services []brewService
	if err := json.Unmarshal(out, &services); err != nil {
		t.Fatal(err)
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range services {
		// prepare stops every loaded service of the user's, including those
		// started with brew services run (no user) and those in the error
		// state, so any status but none or stopped would change this Mac.
		if s.Status != "none" && s.Status != "stopped" {
			t.Skipf("Homebrew service %s is %s; prepare would stop it on this Mac", s.Name, s.Status)
		}
		if s.User != nil && *s.User != me.Username && loadedAsRoot(s.File) {
			t.Skipf("Homebrew service %s runs as %s; prepare would block on it", s.Name, *s.User)
		}
	}
	return cache
}

// loadedAsRoot asks launchd, as prepare does, because brew run as the user
// can't see root services running. Without a plist prepare assumes loaded.
func loadedAsRoot(plist string) bool {
	if plist == "" {
		return true
	}
	label := strings.TrimSuffix(filepath.Base(plist), ".plist")
	return exec.Command("/bin/launchctl", "print", "system/"+label).Run() == nil
}

// TestNoNetwork runs mtm under a sandbox that denies network access, with
// a temporary home folder, so every git, brew, and shell command mtm runs
// must work offline. Homebrew must also have done its work, since a brew
// command that couldn't download what it wanted may fail without saying
// why.
//
// loginitems is not covered. prepare leaves it out because it asks System
// Events, which would show a permission prompt, and its scan and verify
// fail here because launchctl list doesn't work inside sandbox-exec.
//
// docker runs but reaches no engine: it talks to Docker Desktop through
// the socket in the home folder, and this home folder has none, so the
// developer's containers are never stopped. Its prepare fails, asking for
// Docker Desktop, which leaves prepare NOT READY (exit 1).
func TestNoNetwork(t *testing.T) {
	cache := requireIntegrationHost(t)
	bin := filepath.Join(t.TempDir(), "mtm")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	steps := []struct {
		args     []string
		codes    []int
		brewfile bool
	}{
		{[]string{"scan"}, []int{0, 1}, false},
		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions"}, []int{0, 1}, true},
		{[]string{"prepare", "--undo"}, []int{0, 1}, false},
		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions"}, []int{0, 1}, true},
		{[]string{"verify"}, []int{1}, false},
	}
	for _, s := range steps {
		name := "mtm " + strings.Join(s.args, " ")
		cmd := exec.Command("/usr/bin/sandbox-exec", append([]string{"-p", denyNetwork, bin}, s.args...)...)
		// The temp home would hide Homebrew's package data, which every real
		// install keeps in its cache; mtm itself checks that it is there.
		cmd.Env = append(os.Environ(), "HOME="+home, "HOMEBREW_CACHE="+cache)
		out, err := cmd.CombinedOutput()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		text := string(out)
		if !slices.Contains(s.codes, code) {
			t.Errorf("%s exited %d, want one of %v:\n%s", name, code, s.codes, text)
		}
		if s.brewfile && !strings.Contains(text, "homebrew: saved a Brewfile") {
			t.Errorf("%s did not save a Brewfile:\n%s", name, text)
		}
		for _, sign := range networkSigns {
			if strings.Contains(text, sign) {
				t.Errorf("%s tried the network (%q):\n%s", name, sign, text)
			}
		}
		for _, failure := range homebrewFailures {
			if strings.Contains(text, failure) {
				t.Errorf("%s: Homebrew failed (%q):\n%s", name, failure, text)
			}
		}
	}
}
