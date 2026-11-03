package runner

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecCapturesStdout(t *testing.T) {
	out, err := Exec{}.Run(context.Background(), Command{Name: "/bin/echo", Args: []string{"hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out.Stdout); got != "hello\n" {
		t.Errorf("stdout = %q, want %q", got, "hello\n")
	}
}

func TestExecReportsExitCodeAndStderr(t *testing.T) {
	_, err := Exec{}.Run(context.Background(), Command{Name: "/bin/sh", Args: []string{"-c", "echo oops >&2; exit 3"}})
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want *ExitError", err)
	}
	if ee.Code != 3 || ee.Stderr != "oops" {
		t.Errorf("code %d stderr %q, want 3 and %q", ee.Code, ee.Stderr, "oops")
	}
	if code, ok := ExitCode(err); !ok || code != 3 {
		t.Errorf("ExitCode = %d, %v; want 3, true", code, ok)
	}
}

func TestExecRunsInDirWithEnvAndStdin(t *testing.T) {
	dir := t.TempDir()
	out, err := Exec{}.Run(context.Background(), Command{
		Name:  "/bin/sh",
		Args:  []string{"-c", `pwd -P; printf '%s\n' "$MTM_TEST"; cat`},
		Dir:   dir,
		Env:   []string{"MTM_TEST=yes"},
		Stdin: "from stdin\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	lines := strings.Split(strings.TrimSpace(string(out.Stdout)), "\n")
	if len(lines) != 3 || lines[0] != want || lines[1] != "yes" || lines[2] != "from stdin" {
		t.Errorf("stdout lines = %q, want [%q yes \"from stdin\"]", lines, want)
	}
}

func TestExecUnsetsVariablesBeforeAddingEnv(t *testing.T) {
	t.Setenv("MTM_UNSET_ME", "inherited")
	t.Setenv("MTM_KEEP", "kept")
	out, err := Exec{}.Run(context.Background(), Command{
		Name:  "/bin/sh",
		Args:  []string{"-c", `printf '%s|%s|%s' "${MTM_UNSET_ME-unset}" "$MTM_KEEP" "$MTM_ADDED"`},
		Unset: []string{"MTM_UNSET_ME"},
		Env:   []string{"MTM_ADDED=added"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out.Stdout); got != "unset|kept|added" {
		t.Errorf("stdout = %q, want %q", got, "unset|kept|added")
	}
}

func TestExecDoesNotWaitForABackgroundChildHoldingStdout(t *testing.T) {
	start := time.Now()
	out, err := Exec{}.Run(context.Background(), Command{Name: "/bin/sh", Args: []string{"-c", "sleep 5 & echo hi"}})
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("Run took %s, want it to return soon after the shell exits", elapsed)
	}
	if err != nil || string(out.Stdout) != "hi\n" {
		t.Errorf("Run = %q, %v; want \"hi\\n\", nil", out.Stdout, err)
	}
}

func TestFakeReturnsCannedResponse(t *testing.T) {
	f := &Fake{Responses: map[string]Response{"/usr/bin/fdesetup status": {Stdout: "FileVault is On.\n"}}}
	out, err := f.Run(context.Background(), Command{Name: "/usr/bin/fdesetup", Args: []string{"status"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Stdout) != "FileVault is On.\n" {
		t.Errorf("stdout = %q", out.Stdout)
	}
	if len(f.Calls()) != 1 {
		t.Errorf("calls = %d, want 1", len(f.Calls()))
	}
}

func TestFakeNonZeroCodeIsExitError(t *testing.T) {
	f := &Fake{Responses: map[string]Response{"tool": {Code: 1, Stderr: "nope"}}}
	_, err := f.Run(context.Background(), Command{Name: "tool"})
	if code, ok := ExitCode(err); !ok || code != 1 {
		t.Errorf("ExitCode = %d, %v; want 1, true", code, ok)
	}
}

func TestFakeUnknownCommandFails(t *testing.T) {
	_, err := (&Fake{}).Run(context.Background(), Command{Name: "surprise"})
	if err == nil || !strings.Contains(err.Error(), "unexpected command: surprise") {
		t.Errorf("err = %v, want unexpected command error", err)
	}
}

func TestFakeFallsBack(t *testing.T) {
	f := &Fake{Fallback: Exec{}}
	out, err := f.Run(context.Background(), Command{Name: "/bin/echo", Args: []string{"hi"}})
	if err != nil || string(out.Stdout) != "hi\n" {
		t.Errorf("got %q, %v; want \"hi\\n\", nil", out.Stdout, err)
	}
}

func TestLoggedRedactsSecrets(t *testing.T) {
	var log bytes.Buffer
	inner := &Fake{Responses: map[string]Response{
		"tool --token abc": {Code: 1, Stderr: "bad https://u:p@host/x"},
	}}
	l := &Logged{Inner: inner, W: &log}
	l.Run(context.Background(), Command{Name: "tool", Args: []string{"--token", "abc"}})
	got := log.String()
	if strings.Contains(got, "abc") || strings.Contains(got, "u:p@") {
		t.Errorf("log leaks a secret:\n%s", got)
	}
	if !strings.Contains(got, "$ tool --token [REDACTED]") || !strings.Contains(got, "exit 1") {
		t.Errorf("log missing command or exit status:\n%s", got)
	}
}

func TestExecStreamsInputAndOutput(t *testing.T) {
	var got bytes.Buffer
	out, err := Exec{}.Run(context.Background(), Command{
		Name:   "/bin/cat",
		Input:  strings.NewReader("streamed\n"),
		Stdout: &got,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "streamed\n" || len(out.Stdout) != 0 {
		t.Errorf("streamed %q, Output.Stdout %q; want %q and nothing", got.String(), out.Stdout, "streamed\n")
	}
}

func TestExecRefusesStdinAndInputTogether(t *testing.T) {
	_, err := Exec{}.Run(context.Background(), Command{Name: "/bin/cat", Stdin: "a", Input: strings.NewReader("b")})
	if err == nil || !strings.Contains(err.Error(), "both Stdin and Input") {
		t.Errorf("err = %v, want both Stdin and Input", err)
	}
}

func TestExecFailsAStreamCutOffByABackgroundChild(t *testing.T) {
	var got bytes.Buffer
	_, err := Exec{}.Run(context.Background(), Command{Name: "/bin/sh", Args: []string{"-c", "sleep 5 & echo hi"}, Stdout: &got})
	if err == nil {
		t.Error("a streamed command whose output was cut off reported success")
	}
}

func TestFakeStreamsOutputAndRecordsInput(t *testing.T) {
	f := &Fake{Responses: map[string]Response{"/bin/cat": {Stdout: "canned"}}}
	var got bytes.Buffer
	out, err := f.Run(context.Background(), Command{Name: "/bin/cat", Input: strings.NewReader("sent"), Stdout: &got})
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "canned" || len(out.Stdout) != 0 {
		t.Errorf("streamed %q, Output.Stdout %q; want %q and nothing", got.String(), out.Stdout, "canned")
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Stdin != "sent" || calls[0].Input != nil {
		t.Errorf("calls = %+v, want one with Stdin %q and no Input", calls, "sent")
	}
}
