package preflight

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/testutil"
)

func fakeWith(overrides map[string]runner.Response) *runner.Fake {
	responses := testutil.MacResponses("AAAA-1111")
	for k, v := range overrides {
		responses[k] = v
	}
	return &runner.Fake{Responses: responses}
}

func ids(findings []module.Finding) []string {
	var out []string
	for _, f := range findings {
		if f.Severity != module.SeverityBlocker {
			panic("preflight findings must all be blockers")
		}
		out = append(out, f.ID)
	}
	return out
}

func TestCheckPassesOnSupportedMac(t *testing.T) {
	if got := Check(context.Background(), fakeWith(nil)); len(got) != 0 {
		t.Errorf("findings = %+v, want none", got)
	}
}

func TestCheckBlocks(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]runner.Response
		want     string
	}{
		{"FileVault off", map[string]runner.Response{"/usr/bin/fdesetup status": {Stdout: "FileVault is Off.\n"}}, "filevault"},
		{"FileVault unknown", map[string]runner.Response{"/usr/bin/fdesetup status": {Code: 1}}, "filevault"},
		{"old macOS", map[string]runner.Response{"/usr/bin/sw_vers -productVersion": {Stdout: "15.6\n"}}, "macos-version"},
		{"Intel Mac", map[string]runner.Response{"/usr/sbin/sysctl -n hw.optional.arm64": {Code: 1}}, "apple-silicon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(Check(context.Background(), fakeWith(tt.override)))
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("finding ids = %q, want [%q]", got, tt.want)
			}
		})
	}
}

func TestGatherHashesHardwareUUID(t *testing.T) {
	info, err := Gather(context.Background(), fakeWith(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("AAAA-1111")))
	if info.MachineID != want || strings.Contains(info.MachineID, "AAAA") {
		t.Errorf("MachineID = %q, want %q", info.MachineID, want)
	}
	if info.MacOSVersion != "26.7.1" || info.MacOSBuild != "25G241" {
		t.Errorf("info = %+v", info)
	}
}

func TestGatherFailsWithoutUUID(t *testing.T) {
	fake := fakeWith(map[string]runner.Response{"/usr/sbin/ioreg -rd1 -c IOPlatformExpertDevice": {Stdout: "nothing here\n"}})
	if _, err := Gather(context.Background(), fake); err == nil {
		t.Error("Gather succeeded without a hardware UUID")
	}
}
