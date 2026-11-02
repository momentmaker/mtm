// Package macos reads facts from macOS command-line tools and formats
// commands the user runs by hand.
package macos

import (
	"context"
	"fmt"
	"strings"

	"github.com/momentmaker/mtm/internal/runner"
)

// RosettaPath exists once Rosetta 2 is installed.
const RosettaPath = "/Library/Apple/usr/libexec/oah/libRosettaRuntime"

// LoginShell returns the login shell Directory Services records for user.
// It can differ from $SHELL, which the terminal sets.
func LoginShell(ctx context.Context, r runner.Runner, user string) (string, error) {
	out, err := r.Run(ctx, runner.Command{Name: "/usr/bin/dscl", Args: []string{".", "-read", "/Users/" + user, "UserShell"}})
	if err != nil {
		return "", fmt.Errorf("read login shell: %w", err)
	}
	text := strings.TrimSpace(string(out.Stdout))
	shell, ok := strings.CutPrefix(text, "UserShell:")
	if !ok {
		return "", fmt.Errorf("read login shell: unexpected dscl output %q", text)
	}
	return strings.TrimSpace(shell), nil
}

// PlistValue returns one value from a property list, or "" when the key is
// absent (plutil exits 1).
func PlistValue(ctx context.Context, r runner.Runner, plist, key string) (string, error) {
	out, err := r.Run(ctx, runner.Command{Name: "/usr/bin/plutil", Args: []string{"-extract", key, "raw", "-o", "-", plist}})
	if code, ok := runner.ExitCode(err); ok && code == 1 {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s from %s: %w", key, plist, err)
	}
	return strings.TrimSpace(string(out.Stdout)), nil
}

// Archs returns the CPU architectures of an executable. It returns nil when
// lipo can't read the file, as for a script, which then counts as neither
// Intel-only nor Apple Silicon.
func Archs(ctx context.Context, r runner.Runner, exe string) []string {
	out, err := r.Run(ctx, runner.Command{Name: "/usr/bin/lipo", Args: []string{"-archs", exe}})
	if err != nil {
		return nil
	}
	return strings.Fields(string(out.Stdout))
}

// ShellQuote makes s safe to paste into a shell as one word.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
