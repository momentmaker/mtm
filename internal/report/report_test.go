package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/verdict"
)

func TestVerifyNeverPrintsFingerprintsOrCredentials(t *testing.T) {
	checks := []module.Check{{
		Module: "git", ID: "git:x", Status: module.StatusFail, Critical: true, Title: "~/x",
		Detail: "content differs sha256:9f2c01ab; remote https://me:pw@host/r.git",
	}}
	var out bytes.Buffer
	Verify(&out, checks, verdict.Decide(checks, nil), false)
	if s := out.String(); strings.Contains(s, "sha256:") || strings.Contains(s, "me:pw") {
		t.Errorf("output leaks a fingerprint or credential:\n%s", s)
	}
}

func TestVerifyBanners(t *testing.T) {
	pass := []module.Check{{Module: "files", ID: "files:GitHub", Status: module.StatusPass, Title: "GitHub", Detail: "3 match"}}
	var safe bytes.Buffer
	Verify(&safe, pass, verdict.Decide(pass, nil), false)
	if !strings.Contains(safe.String(), "SAFE TO WIPE") || strings.Contains(safe.String(), "NOT SAFE") {
		t.Errorf("safe output:\n%s", safe.String())
	}
	fail := []module.Check{{Module: "files", ID: "files:.ssh", Status: module.StatusFail, Critical: true, Title: ".ssh", Detail: "1 missing"}}
	var notSafe bytes.Buffer
	Verify(&notSafe, fail, verdict.Decide(fail, nil), false)
	if !strings.Contains(notSafe.String(), "NOT SAFE TO WIPE") || !strings.Contains(notSafe.String(), "files .ssh: 1 missing") {
		t.Errorf("not-safe output:\n%s", notSafe.String())
	}
}

func TestVerifyTruncatesPathsUnlessDetails(t *testing.T) {
	c := module.Check{Module: "files", ID: "files:Docs", Status: module.StatusFail, Title: "Docs",
		Paths: []string{"a", "b", "c", "d", "e", "f", "g", "odd\nname"}}
	checks := []module.Check{c}
	var short, long bytes.Buffer
	Verify(&short, checks, verdict.Decide(checks, nil), false)
	Verify(&long, checks, verdict.Decide(checks, nil), true)
	if !strings.Contains(short.String(), "3 more (use --details)") {
		t.Errorf("short output:\n%s", short.String())
	}
	if !strings.Contains(long.String(), `"odd\nname"`) {
		t.Errorf("details output should quote the odd path:\n%s", long.String())
	}
}

func TestFindingsPutsBlockersFirst(t *testing.T) {
	findings := []module.Finding{
		{Module: "files", ID: "home", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant, Title: "Home folder"},
		{Module: "preflight", ID: "filevault", Severity: module.SeverityBlocker, Handler: module.Manual, Title: "FileVault is off", Fix: "Turn it on"},
	}
	var out bytes.Buffer
	Findings(&out, findings)
	s := out.String()
	if strings.Index(s, "FileVault is off") > strings.Index(s, "Home folder") {
		t.Errorf("blocker not listed first:\n%s", s)
	}
	if !strings.Contains(s, "→ Turn it on") {
		t.Errorf("fix not shown:\n%s", s)
	}
}

func TestPrepareReadyShowsNextSteps(t *testing.T) {
	var out bytes.Buffer
	Prepare(&out, true, []string{"files: recorded 3 items, 1 fingerprinted"}, nil)
	for _, want := range []string{"READY FOR MIGRATION ASSISTANT", "not from a Time Machine backup", "mtm verify"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	var notReady bytes.Buffer
	Prepare(&notReady, false, nil, []module.Finding{{Module: "git", Severity: module.SeverityBlocker, Title: "broken"}})
	if !strings.Contains(notReady.String(), "NOT READY FOR MIGRATION ASSISTANT") {
		t.Errorf("not-ready output:\n%s", notReady.String())
	}
}

func TestJSONScrubsAndStaysValid(t *testing.T) {
	var out bytes.Buffer
	if err := JSON(&out, map[string]string{"detail": "fingerprint sha256:abcdef"}); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) || strings.Contains(out.String(), "sha256:") {
		t.Errorf("JSON output = %s", out.String())
	}
}
