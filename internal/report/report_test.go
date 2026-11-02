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
	if !strings.Contains(safe.String(), "Everything mtm checked arrived (files).") || strings.Contains(safe.String(), "recorded arrived") {
		t.Errorf("safe banner should name what was checked without over-claiming:\n%s", safe.String())
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

func TestSafeBannerNamesCheckedModules(t *testing.T) {
	checks := []module.Check{
		{Module: "preflight", ID: "version", Status: module.StatusWarn, Title: "Different mtm version"},
		{Module: "files", ID: "files:GitHub", Status: module.StatusPass, Title: "GitHub"},
		{Module: "git", ID: "git:GitHub/app", Status: module.StatusPass, Critical: true, Title: "~/GitHub/app"},
		{Module: "files", ID: "files:record", Status: module.StatusPass, Title: "record"},
	}
	var out bytes.Buffer
	Verify(&out, checks, verdict.Decide(checks, nil), false)
	if !strings.Contains(out.String(), "Everything mtm checked arrived (files, git).") {
		t.Errorf("safe banner should list files, git once each and skip preflight:\n%s", out.String())
	}
}

func assertNoRawControls(t *testing.T, s string) {
	t.Helper()
	for _, c := range []string{"\x1b", "\r", "\u202e"} {
		if strings.Contains(s, c) {
			t.Errorf("output contains raw %q:\n%q", c, s)
		}
	}
}

func TestVerifyEscapesControlCharacters(t *testing.T) {
	checks := []module.Check{{
		Module: "files", ID: "files:x", Status: module.StatusFail, Critical: true,
		Title: "Docs\x1b[2J", Detail: "1 missing\rSAFE TO WIPE",
		Paths: []string{"Docs/Icon\r", "Docs/evil\x1b[2Jname", "Docs/rtl\u202etxt.exe"},
	}}
	var out bytes.Buffer
	Verify(&out, checks, verdict.Decide(checks, nil), true)
	s := out.String()
	assertNoRawControls(t, s)
	for _, want := range []string{`Docs\x1b[2J`, `1 missing\rSAFE TO WIPE`, `"Docs/Icon\r"`, `"Docs/evil\x1b[2Jname"`, `"Docs/rtl\u202etxt.exe"`, `files Docs\x1b[2J: 1 missing\rSAFE TO WIPE`} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %s:\n%s", want, s)
		}
	}
}

func TestFindingsAndPrepareEscapeControlCharacters(t *testing.T) {
	findings := []module.Finding{{Module: "git", ID: "x", Severity: module.SeverityBlocker, Handler: module.Manual,
		Title: "~/GitHub/Icon\r", Detail: "bad\x1b[2J", Fix: "run\x1b]0;title\x07"}}
	var scan bytes.Buffer
	Findings(&scan, findings)
	assertNoRawControls(t, scan.String())
	if !strings.Contains(scan.String(), `~/GitHub/Icon\r`) || strings.Contains(scan.String(), "\x07") {
		t.Errorf("scan output:\n%q", scan.String())
	}
	var prep bytes.Buffer
	Prepare(&prep, false, []string{"files: recorded Icon\r"}, findings)
	assertNoRawControls(t, prep.String())
}

func TestJSONKeepsControlCharactersEscapedAndValid(t *testing.T) {
	var out bytes.Buffer
	if err := JSON(&out, map[string]string{"path": "Docs/Icon\r", "title": "x\x1b[2J"}); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) {
		t.Fatalf("not valid JSON:\n%s", out.String())
	}
	assertNoRawControls(t, out.String())
}

func TestFinishListsByHandStepsThenVerify(t *testing.T) {
	var out bytes.Buffer
	Finish(&out, []string{"homebrew: started postgresql@16"}, []module.Finding{{
		Module: "system", Severity: module.SeverityWarn, Handler: module.Manual,
		Title: "Intel-only apps need Rosetta", Fix: "Run: sudo softwareupdate --install-rosetta --agree-to-license",
	}})
	for _, want := range []string{"✓ homebrew: started postgresql@16", "Do these by hand", "→ Run: sudo softwareupdate", "Next: run mtm verify"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestFinishBlockedSaysToRunAgain(t *testing.T) {
	var out bytes.Buffer
	Finish(&out, nil, []module.Finding{{Module: "preflight", Severity: module.SeverityBlocker, Title: "FileVault is off"}})
	if !strings.Contains(out.String(), "run mtm finish again") || strings.Contains(out.String(), "Next: run mtm verify") {
		t.Errorf("blocked output:\n%s", out.String())
	}
}

func TestUndoSaysToPrepareAgain(t *testing.T) {
	var out bytes.Buffer
	Undo(&out, []string{"homebrew: started postgresql@16"}, nil)
	if !strings.Contains(out.String(), "Run mtm prepare again") {
		t.Errorf("undo output:\n%s", out.String())
	}
}

func TestReadyTextNamesFinish(t *testing.T) {
	var out bytes.Buffer
	Prepare(&out, true, nil, nil)
	if !strings.Contains(out.String(), "mtm finish, then mtm verify") {
		t.Errorf("ready text:\n%s", out.String())
	}
}
