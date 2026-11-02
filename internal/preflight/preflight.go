// Package preflight checks what every mtm run needs from the Mac itself.
package preflight

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
)

const (
	Module        = "preflight"
	MinMacOSMajor = 26
)

type Info struct {
	MacOSVersion string
	MacOSBuild   string
	MachineID    string
}

// Check returns a blocker for each requirement this Mac does not meet:
// Apple Silicon, macOS 26 or later, and FileVault turned on.
func Check(ctx context.Context, r runner.Runner) []module.Finding {
	var findings []module.Finding
	for _, check := range []func(context.Context, runner.Runner) *module.Finding{appleSilicon, macOSVersion, fileVault} {
		if f := check(ctx, r); f != nil {
			findings = append(findings, *f)
		}
	}
	return findings
}

func blocker(id, title, detail, fix string) *module.Finding {
	return &module.Finding{Module: Module, ID: id, Severity: module.SeverityBlocker, Handler: module.Manual, Title: title, Detail: detail, Fix: fix}
}

func appleSilicon(ctx context.Context, r runner.Runner) *module.Finding {
	out, err := r.Run(ctx, runner.Command{Name: "/usr/sbin/sysctl", Args: []string{"-n", "hw.optional.arm64"}})
	if err != nil || strings.TrimSpace(string(out.Stdout)) != "1" {
		return blocker("apple-silicon", "This is not an Apple Silicon Mac", "", "mtm supports Apple Silicon Macs only.")
	}
	return nil
}

func macOSVersion(ctx context.Context, r runner.Runner) *module.Finding {
	version, err := productVersion(ctx, r)
	if err != nil {
		return blocker("macos-version", "Could not read the macOS version", err.Error(), "")
	}
	major, _, _ := strings.Cut(version, ".")
	if n, err := strconv.Atoi(major); err != nil || n < MinMacOSMajor {
		return blocker("macos-version", "macOS "+version+" is too old", "", fmt.Sprintf("Update to macOS %d or later in System Settings > General > Software Update.", MinMacOSMajor))
	}
	return nil
}

func fileVault(ctx context.Context, r runner.Runner) *module.Finding {
	out, err := r.Run(ctx, runner.Command{Name: "/usr/bin/fdesetup", Args: []string{"status"}})
	if err != nil {
		return blocker("filevault", "Could not check FileVault", err.Error(), "")
	}
	if !strings.Contains(string(out.Stdout), "FileVault is On") {
		return blocker("filevault", "FileVault is off", "mtm's record and exports must sit on an encrypted disk.", "Turn on FileVault in System Settings > Privacy & Security > FileVault, then re-run.")
	}
	return nil
}

func productVersion(ctx context.Context, r runner.Runner) (string, error) {
	out, err := r.Run(ctx, runner.Command{Name: "/usr/bin/sw_vers", Args: []string{"-productVersion"}})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out.Stdout)), nil
}

// Gather reads the facts prepare stores in the manifest.
func Gather(ctx context.Context, r runner.Runner) (Info, error) {
	var info Info
	var err error
	if info.MacOSVersion, err = productVersion(ctx, r); err != nil {
		return Info{}, err
	}
	build, err := r.Run(ctx, runner.Command{Name: "/usr/bin/sw_vers", Args: []string{"-buildVersion"}})
	if err != nil {
		return Info{}, err
	}
	info.MacOSBuild = strings.TrimSpace(string(build.Stdout))
	if info.MachineID, err = machineID(ctx, r); err != nil {
		return Info{}, err
	}
	return info, nil
}

var platformUUID = regexp.MustCompile(`"IOPlatformUUID" = "([^"]+)"`)

// machineID hashes the hardware UUID so the manifest can tell Macs apart
// without storing the identifier itself.
func machineID(ctx context.Context, r runner.Runner) (string, error) {
	out, err := r.Run(ctx, runner.Command{Name: "/usr/sbin/ioreg", Args: []string{"-rd1", "-c", "IOPlatformExpertDevice"}})
	if err != nil {
		return "", fmt.Errorf("read hardware UUID: %w", err)
	}
	m := platformUUID.FindSubmatch(out.Stdout)
	if m == nil {
		return "", errors.New("read hardware UUID: IOPlatformUUID not found in ioreg output")
	}
	sum := sha256.Sum256(m[1])
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
