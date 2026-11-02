package macos

import (
	"context"
	"slices"
	"testing"

	"github.com/momentmaker/mtm/internal/runner"
)

var ctx = context.Background()

func TestLoginShell(t *testing.T) {
	f := &runner.Fake{Responses: map[string]runner.Response{
		"/usr/bin/dscl . -read /Users/dev UserShell": {Stdout: "UserShell: /opt/homebrew/bin/fish\n"},
	}}
	got, err := LoginShell(ctx, f, "dev")
	if err != nil || got != "/opt/homebrew/bin/fish" {
		t.Errorf("LoginShell = %q, %v", got, err)
	}
}

func TestLoginShellRejectsUnexpectedOutput(t *testing.T) {
	f := &runner.Fake{Responses: map[string]runner.Response{
		"/usr/bin/dscl . -read /Users/dev UserShell": {Stdout: "No such key: UserShell\n"},
	}}
	if _, err := LoginShell(ctx, f, "dev"); err == nil {
		t.Error("LoginShell accepted output without UserShell")
	}
}

func TestPlistValue(t *testing.T) {
	key := func(k string) string { return "/usr/bin/plutil -extract " + k + " raw -o - /A/Info.plist" }
	f := &runner.Fake{Responses: map[string]runner.Response{
		key("CFBundleIdentifier"):         {Stdout: "com.mitchellh.ghostty\n"},
		key("CFBundleShortVersionString"): {Code: 1, Stderr: "Could not extract value"},
		key("Broken"):                     {Code: 2, Stderr: "boom"},
	}}
	if got, err := PlistValue(ctx, f, "/A/Info.plist", "CFBundleIdentifier"); err != nil || got != "com.mitchellh.ghostty" {
		t.Errorf("present key = %q, %v", got, err)
	}
	if got, err := PlistValue(ctx, f, "/A/Info.plist", "CFBundleShortVersionString"); err != nil || got != "" {
		t.Errorf("absent key = %q, %v; want empty, nil", got, err)
	}
	if _, err := PlistValue(ctx, f, "/A/Info.plist", "Broken"); err == nil {
		t.Error("plutil failure was not reported")
	}
}

func TestArchs(t *testing.T) {
	f := &runner.Fake{Responses: map[string]runner.Response{
		"/usr/bin/lipo -archs /A/exe":    {Stdout: "x86_64 arm64\n"},
		"/usr/bin/lipo -archs /A/script": {Code: 1, Stderr: "not a Mach-O file"},
	}}
	if got := Archs(ctx, f, "/A/exe"); !slices.Equal(got, []string{"x86_64", "arm64"}) {
		t.Errorf("Archs = %q", got)
	}
	if got := Archs(ctx, f, "/A/script"); got != nil {
		t.Errorf("Archs(script) = %q, want nil", got)
	}
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"/Users/dev/.mtm":       "'/Users/dev/.mtm'",
		"/Users/my home/.mtm":   "'/Users/my home/.mtm'",
		"/Users/o'brien/.mtm/x": `'/Users/o'\''brien/.mtm/x'`,
		`/tmp/a\b`:              `'/tmp/a'\\'b'`,
		`/tmp/a\`:               `'/tmp/a'\\''`,
		`/tmp/a\'b`:             `'/tmp/a'\\''\''b'`,
	}
	for in, want := range tests {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestMajorVersion(t *testing.T) {
	tests := []struct {
		name string
		resp runner.Response
		want string
		fail bool
	}{
		{"patch release", runner.Response{Stdout: "26.7.1\n"}, "26", false},
		{"no patch", runner.Response{Stdout: "15.6\n"}, "15", false},
		{"sw_vers fails", runner.Response{Code: 1, Stderr: "boom"}, "", true},
		{"empty output", runner.Response{Stdout: "\n"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &runner.Fake{Responses: map[string]runner.Response{"/usr/bin/sw_vers -productVersion": tt.resp}}
			got, err := MajorVersion(ctx, f)
			if got != tt.want || (err != nil) != tt.fail {
				t.Errorf("MajorVersion = %q, %v; want %q, fail=%v", got, err, tt.want, tt.fail)
			}
		})
	}
}
