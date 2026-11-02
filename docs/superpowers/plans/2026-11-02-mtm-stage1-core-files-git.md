# mtm Stage 1: Core, Files, and Git Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `mtm scan`, `prepare`, `verify`, and `cleanup` for home-folder files and git repos. Includes the private `~/.mtm` folder, the preflight gate, redacted logs and reports, and the SAFE TO WIPE verdict.

**Architecture:**
- A thin CLI (`cmd/mtm`) calls an orchestrator (`internal/app`). The orchestrator runs the preflight checks, opens `~/.mtm`, and calls each module through one contract (`internal/module`).
- Two modules ship in this stage: `files`, which records the home folder and verifies a copy of it, and `git`, which records repos and verifies their commits and untracked files.
- Every system command goes through `internal/runner`, so tests can fake it.
- Every printed string goes through `internal/report`, which strips fingerprints and credentials.

**Tech Stack:**
- Go 1.26, standard library only.
- macOS system tools: `fdesetup`, `sw_vers`, `sysctl`, `ioreg`, `tmutil`, `ditto` (tests only).
- `git`.

**Spec:** `docs/superpowers/specs/2026-11-02-mtm-design.md`. Stage 1 covers sections 5 (rows `scan`, `prepare`, `verify`, `cleanup`), 6.2 rules 1–5, 6.3 (except external staging and export fingerprints), 6.4, 7.1–7.3, 8 (except `exports/` and checklist state), 9.1, 9.2, 10 (the bullets that apply to files and git), and 11 (unit, table, and integration tests). The stage list is in `IMPLEMENTATION_PLAN.md`.

## Global Constraints

- Module path `github.com/momentmaker/mtm`; `go 1.26` in `go.mod`.
- No third-party dependencies in this stage. The only one the project will ever allow is `github.com/BurntSushi/toml`, in Stage 4.
- `mtm` makes no network connections and never reads secret contents (spec 6.2 rules 1–2).
- Reports and logs never contain fingerprints, file contents, or environment values (spec 6.3).
- `~/.mtm` is mode `0700` and every file in it `0600`. Refuse a `~/.mtm` that is a symlink, is owned by someone else, or has looser permissions.
- Apple system tools are called by absolute path: `/usr/bin/fdesetup`, `/usr/bin/sw_vers`, `/usr/sbin/sysctl`, `/usr/sbin/ioreg`, `/usr/bin/tmutil`. `git` is found on `PATH`.
- Every git command `mtm` runs starts with `-C <dir> --no-optional-locks -c core.fsmonitor=false`.
- Modules never print. All output goes through `internal/report`.
- Exit codes:
  - `verify`: `0` safe, `1` not safe, `2` mtm failed.
  - Other commands: `0` success, `1` completed with problems, `2` mtm failed.
- Any test that runs git calls `testutil.IsolateGit(t)` first, so the developer's own git config (signing, hooks) can't affect it.
- Before every commit, all three must pass: `gofmt -l .` prints nothing, `go vet ./...`, and `go test ./...`.
- Commit messages explain why. Every commit message ends with:
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`

## Review Focus

These are the five inputs and failure modes most likely to bite a real user that the main tests don't exercise. Each has a pinned test in the task that owns the code.

1. **File names with spaces, Unicode, newlines, or a leading dash** must be recorded, parsed, and verified exactly. Tests: Task 7 `TestRecordKeepsUnusualNames`, Task 9 `TestParseStatus`, Task 10 `TestUnusualRepoStates`.
2. **Repos in unusual states** (empty, detached HEAD, linked worktree) must be recorded without problems and verify as pass. Test: Task 10 `TestUnusualRepoStates`.
3. **Folders the terminal can't read** (privacy-protected, or `chmod 000`) must be recorded as unreadable, the walk must continue, and verify must report them as not checked rather than failing. Tests: Task 7 `TestRecordMarksUnreadableFolders`, Task 8 `TestVerifyReportsUnreadableAsNotChecked`.
4. **Symlinks** (loops, dangling, to folders, outside home) must be recorded as links and never followed. A changed link target fails. Tests: Task 7 `TestRecordNeverFollowsSymlinks`, Task 6 `TestClassify` row "symlink target changed".
5. **Running mtm on the wrong Mac or in the wrong order** must be refused or handled safely:
   - `prepare` on the new Mac after migration refuses to overwrite the old Mac's record;
   - `verify` on the old Mac is not safe;
   - `cleanup` before SAFE TO WIPE is refused;
   - `prepare --only` keeps the other modules' records;
   - a `verify --only` result never unlocks `cleanup`.

   Tests: Task 13 `TestPrepareRefusesToOverwriteAnotherMacsRecord`, `TestVerifyOnSameMacIsNotSafe`, `TestCleanupRules`, `TestPrepareOnlyKeepsOtherModules`.

## File Structure

```
go.mod
.gitignore
cmd/mtm/main.go                    CLI: subcommands, flags, exit codes
cmd/mtm/main_test.go
internal/redact/redact.go          masks secrets in command lines and text
internal/runner/runner.go          Command, Output, ExitError, Runner, Display
internal/runner/exec.go            real command execution
internal/runner/fake.go            canned responses for tests
internal/runner/logged.go          decorator that writes a redacted log
internal/stage/stage.go            ~/.mtm: creation, safety checks, atomic private files, Time Machine exclusion
internal/pathset/pathset.go        home-relative path patterns
internal/manifest/manifest.go      manifest.json schema, load and save
internal/manifest/record.go        files.jsonl.gz entries, streaming writer and reader
internal/module/module.go          module contract, Env, Finding, Check, Result
internal/verdict/verdict.go        SAFE TO WIPE rules
internal/fsinfo/fsinfo.go          cloud-only detection, file fingerprints
internal/testutil/testutil.go      shared test helpers (files, git, copying, fake Mac responses)
internal/modules/files/policy.go   areas, exclusions, volatile paths, fingerprint policy
internal/modules/files/classify.go per-file verify rules (spec 8.4)
internal/modules/files/walk.go     home-folder walk
internal/modules/files/record.go   writing the file record with parallel fingerprints
internal/modules/files/module.go   Module: Prepare, Finish
internal/modules/files/verify.go   Module: Verify, grouping into checks
internal/modules/files/scan.go     Module: Scan
internal/modules/git/parse.go      parsing git output
internal/modules/git/discover.go   finding repos
internal/modules/git/repo.go       recording one repo
internal/modules/git/module.go     Module: Scan, Prepare, Finish
internal/modules/git/verify.go     Module: Verify
internal/preflight/preflight.go    Apple Silicon, macOS 26+, FileVault, machine id
internal/report/report.go          text and JSON rendering, scrubbing
internal/state/state.go            state.json: last verify result
internal/app/app.go                orchestration of scan, prepare, verify, cleanup
```

---

### Task 1: Go module, redaction, and the command runner

**Files:**
- Create: `go.mod`, `.gitignore`
- Create: `internal/redact/redact.go`, `internal/redact/redact_test.go`
- Create: `internal/runner/runner.go`, `internal/runner/exec.go`, `internal/runner/fake.go`, `internal/runner/logged.go`, `internal/runner/runner_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `redact.Mask`, `redact.Args(args []string) []string`, `redact.Text(s string) string`
  - `runner.Command{Name, Args, Dir, Env, Stdin}`, `runner.Output{Stdout, Stderr []byte; Code int}`, `runner.ExitError{Command string; Code int; Stderr string}`
  - `runner.Runner` interface `Run(ctx, Command) (Output, error)`
  - `runner.Display(Command) string`, `runner.ExitCode(error) (int, bool)`
  - `runner.Exec{}`
  - `runner.Fake{Responses map[string]Response; Fallback Runner}` with `Calls() []Command`; `runner.Response{Stdout, Stderr string; Code int; Err error}`
  - `runner.Logged{Inner Runner; W io.Writer}`

- [ ] **Step 1: Create the module files**

`go.mod`:
```
module github.com/momentmaker/mtm

go 1.26
```

`.gitignore`:
```
/mtm
/dist/
*.test
*.out
```

- [ ] **Step 2: Write the failing redaction tests**

`internal/redact/redact_test.go`:
```go
package redact

import (
	"slices"
	"strings"
	"testing"
)

func TestArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"secret flag then value", []string{"--password", "hunter2", "db"}, []string{"--password", Mask, "db"}},
		{"secret flag with equals", []string{"--token=abc123"}, []string{"--token=" + Mask}},
		{"secret key=value pair", []string{"API_KEY=abc", "PATH=/bin"}, []string{"API_KEY=" + Mask, "PATH=/bin"}},
		{"ordinary flags untouched", []string{"-C", "/tmp/repo", "--format=%(refname) %(objectname)"}, []string{"-C", "/tmp/repo", "--format=%(refname) %(objectname)"}},
		{"credentials in a URL", []string{"clone", "https://me:s3cret@example.com/r.git"}, []string{"clone", "https://" + Mask + "@example.com/r.git"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Args(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("Args(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestArgsDoesNotModifyInput(t *testing.T) {
	in := []string{"--password", "hunter2"}
	Args(in)
	if in[1] != "hunter2" {
		t.Errorf("input changed to %q", in)
	}
}

func TestTextMasksKnownTokens(t *testing.T) {
	in := "auth failed for ghp_" + strings.Repeat("a", 36) + " and sk-ant-" + strings.Repeat("b", 30)
	got := Text(in)
	if strings.Contains(got, "ghp_") || strings.Contains(got, "sk-ant-") {
		t.Errorf("Text(%q) = %q, still contains a token", in, got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/redact/...`
Expected: FAIL with `undefined: Args`, `undefined: Mask`, `undefined: Text`.

- [ ] **Step 4: Implement redaction**

`internal/redact/redact.go`:
```go
// Package redact removes secret values from text mtm writes to logs and reports.
package redact

import (
	"regexp"
	"strings"
)

const Mask = "[REDACTED]"

var secretNameParts = []string{
	"password", "passwd", "passphrase", "secret", "token",
	"apikey", "api_key", "api-key", "credential", "private_key", "auth",
}

var (
	urlCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^\s/@]+@`)
	knownTokens    = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_\-]{20,}|AKIA[0-9A-Z]{16}|xox[abpr]-[A-Za-z0-9\-]{10,})`)
)

// Text masks credentials embedded in URLs and well-known token formats.
func Text(s string) string {
	s = urlCredentials.ReplaceAllString(s, "${1}"+Mask+"@")
	return knownTokens.ReplaceAllString(s, Mask)
}

// Args returns a copy of command arguments with secret values masked: the
// value after a secret-looking flag, the value in --secret=value and
// SECRET=value, and anything Text masks.
func Args(args []string) []string {
	out := make([]string, len(args))
	maskNext := false
	for i, arg := range args {
		switch {
		case maskNext:
			out[i] = Mask
			maskNext = false
		case strings.HasPrefix(arg, "-"):
			name, _, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			switch {
			case !isSecretName(name):
				out[i] = Text(arg)
			case hasValue:
				out[i] = arg[:strings.IndexByte(arg, '=')+1] + Mask
			default:
				out[i] = arg
				maskNext = true
			}
		default:
			if key, _, ok := strings.Cut(arg, "="); ok && isSecretName(key) {
				out[i] = key + "=" + Mask
			} else {
				out[i] = Text(arg)
			}
		}
	}
	return out
}

func isSecretName(name string) bool {
	lower := strings.ToLower(name)
	for _, part := range secretNameParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/redact/...`
Expected: PASS.

- [ ] **Step 6: Write the failing runner tests**

`internal/runner/runner_test.go`:
```go
package runner

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
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
```

- [ ] **Step 7: Run the tests to verify they fail**

Run: `go test ./internal/runner/...`
Expected: FAIL with `undefined: Exec`, `undefined: Command`, and similar.

- [ ] **Step 8: Implement the runner**

`internal/runner/runner.go`:
```go
// Package runner is the only place mtm executes system commands, so tests
// can replace every command with a fake.
package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/momentmaker/mtm/internal/redact"
)

// Command is one system command. Env entries are appended to the current
// environment. Stdin, when set, is written to the command's standard input.
type Command struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin string
}

// String is the unredacted command line. Fake uses it as the lookup key.
func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Display is the command line with secrets masked, safe for logs and errors.
func Display(c Command) string {
	return strings.Join(append([]string{c.Name}, redact.Args(c.Args)...), " ")
}

type Output struct {
	Stdout []byte
	Stderr []byte
	Code   int
}

// ExitError reports a command that ran but exited non-zero.
type ExitError struct {
	Command string
	Code    int
	Stderr  string
}

func (e *ExitError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("%s: exit status %d", e.Command, e.Code)
	}
	return fmt.Sprintf("%s: exit status %d: %s", e.Command, e.Code, e.Stderr)
}

// ExitCode returns the exit status carried by err when err is an *ExitError.
func ExitCode(err error) (int, bool) {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code, true
	}
	return 0, false
}

type Runner interface {
	Run(ctx context.Context, cmd Command) (Output, error)
}

const maxStderr = 2000

func newExitError(cmd Command, code int, stderr []byte) *ExitError {
	s := redact.Text(strings.TrimSpace(string(stderr)))
	if len(s) > maxStderr {
		s = s[:maxStderr] + "…"
	}
	return &ExitError{Command: Display(cmd), Code: code, Stderr: s}
}
```

`internal/runner/exec.go`:
```go
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Exec runs commands on the real system. Standard input is empty unless
// Command.Stdin is set, so no command can stop to prompt the user.
type Exec struct{}

func (Exec) Run(ctx context.Context, cmd Command) (Output, error) {
	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	c.Dir = cmd.Dir
	if len(cmd.Env) > 0 {
		c.Env = append(os.Environ(), cmd.Env...)
	}
	if cmd.Stdin != "" {
		c.Stdin = strings.NewReader(cmd.Stdin)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	out := Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return out, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		out.Code = exitErr.ExitCode()
		return out, newExitError(cmd, out.Code, out.Stderr)
	}
	return out, fmt.Errorf("%s: %w", Display(cmd), err)
}
```

`internal/runner/fake.go`:
```go
package runner

import (
	"context"
	"fmt"
	"sync"
)

// Response is a canned result for one command line.
type Response struct {
	Stdout string
	Stderr string
	Code   int
	Err    error
}

// Fake answers commands from Responses, keyed by Command.String(). Commands
// without a response go to Fallback when it is set, and fail otherwise.
type Fake struct {
	Responses map[string]Response
	Fallback  Runner

	mu    sync.Mutex
	calls []Command
}

func (f *Fake) Run(ctx context.Context, cmd Command) (Output, error) {
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	resp, ok := f.Responses[cmd.String()]
	f.mu.Unlock()
	if !ok {
		if f.Fallback != nil {
			return f.Fallback.Run(ctx, cmd)
		}
		return Output{}, fmt.Errorf("fake runner: unexpected command: %s", cmd.String())
	}
	out := Output{Stdout: []byte(resp.Stdout), Stderr: []byte(resp.Stderr), Code: resp.Code}
	switch {
	case resp.Err != nil:
		return out, resp.Err
	case resp.Code != 0:
		return out, newExitError(cmd, resp.Code, out.Stderr)
	}
	return out, nil
}

// Calls returns the commands run so far, in order.
func (f *Fake) Calls() []Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Command(nil), f.calls...)
}
```

`internal/runner/logged.go`:
```go
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/momentmaker/mtm/internal/redact"
)

// Logged writes every command it runs, and how it ended, to W with secrets masked.
type Logged struct {
	Inner Runner
	W     io.Writer

	mu sync.Mutex
}

func (l *Logged) Run(ctx context.Context, cmd Command) (Output, error) {
	out, err := l.Inner.Run(ctx, cmd)
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.W, "$ %s\n", Display(cmd))
	if cmd.Dir != "" {
		fmt.Fprintf(l.W, "  in %s\n", cmd.Dir)
	}
	var ee *ExitError
	switch {
	case err == nil:
		fmt.Fprintln(l.W, "  ok")
	case errors.As(err, &ee):
		fmt.Fprintf(l.W, "  exit %d: %s\n", ee.Code, ee.Stderr)
	default:
		fmt.Fprintf(l.W, "  error: %s\n", redact.Text(err.Error()))
	}
	return out, err
}
```

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./internal/runner/... ./internal/redact/...`
Expected: PASS.

- [ ] **Step 10: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add go.mod .gitignore internal/redact internal/runner
git commit -m "Add command runner with redacted logging

Every system command goes through one fakeable interface, and anything
logged has secrets masked, so tests never touch the real machine and logs
are safe to share.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: The private stage folder (`~/.mtm`)

**Files:**
- Create: `internal/stage/stage.go`, `internal/stage/stage_test.go`

**Interfaces:**
- Consumes: `runner.Runner`, `runner.Command` (Task 1).
- Produces:
  - `stage.DirName = ".mtm"`, `stage.ErrNotFound`
  - `stage.Stage{Root string}`
  - `stage.Open(home string) (*Stage, error)`: creates the folder if absent, then checks it.
  - `stage.Existing(home string) (*Stage, error)`: returns `ErrNotFound` if absent.
  - `(*Stage).Path(elem ...string) string`, `(*Stage).MkdirAll(name string) error`, `(*Stage).WriteFile(name string, data []byte) error`, `(*Stage).Create(name string) (*File, error)`, `(*Stage).ExcludeFromBackups(ctx, runner.Runner) error`, `(*Stage).Remove() error`
  - `stage.File`, which embeds `*os.File`, with `Commit() error` and `Abort()`

- [ ] **Step 1: Write the failing tests**

`internal/stage/stage_test.go`:
```go
package stage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/runner"
)

func mustOpen(t *testing.T) *Stage {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestOpenCreatesPrivateFolder(t *testing.T) {
	home := t.TempDir()
	st, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if st.Root != filepath.Join(home, DirName) {
		t.Errorf("Root = %q, want %q", st.Root, filepath.Join(home, DirName))
	}
	info, err := os.Stat(st.Root)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("mode = %04o, want 0700", perm)
	}
}

func TestOpenRefusesLoosePermissions(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, DirName)
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Open(home)
	if err == nil || !strings.Contains(err.Error(), "chmod 700") {
		t.Errorf("err = %v, want a refusal that suggests chmod 700", err)
	}
}

func TestOpenRefusesSymlink(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(home, DirName)); err != nil {
		t.Fatal(err)
	}
	_, err := Open(home)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("err = %v, want a refusal mentioning symlink", err)
	}
}

func TestExistingReportsMissingStage(t *testing.T) {
	_, err := Existing(t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestWriteFileIsPrivateAndAtomic(t *testing.T) {
	st := mustOpen(t)
	if err := st.WriteFile("logs/a.txt", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(st.Path("logs", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %04o, want 0600", perm)
	}
	dirInfo, err := os.Stat(st.Path("logs"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %04o, want 0700", perm)
	}
	entries, _ := os.ReadDir(st.Path("logs"))
	if len(entries) != 1 {
		t.Errorf("logs/ has %d entries, want 1 (no temp files left)", len(entries))
	}
}

func TestAbortLeavesNothing(t *testing.T) {
	st := mustOpen(t)
	f, err := st.Create("x.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("partial"))
	f.Abort()
	entries, _ := os.ReadDir(st.Root)
	if len(entries) != 0 {
		t.Errorf("stage has %d entries after Abort, want 0", len(entries))
	}
}

func TestExcludeFromBackupsRunsTmutil(t *testing.T) {
	st := mustOpen(t)
	fake := &runner.Fake{Responses: map[string]runner.Response{"/usr/bin/tmutil addexclusion " + st.Root: {}}}
	if err := st.ExcludeFromBackups(context.Background(), fake); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls()) != 1 {
		t.Errorf("calls = %d, want 1", len(fake.Calls()))
	}
}

func TestRemoveDeletesStage(t *testing.T) {
	st := mustOpen(t)
	st.WriteFile("manifest.json", []byte("{}"))
	if err := st.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st.Root); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat after Remove: %v, want not exist", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/stage/...`
Expected: FAIL with `undefined: Open`, `undefined: DirName`, and similar.

- [ ] **Step 3: Implement the stage folder**

`internal/stage/stage.go`:
```go
// Package stage manages ~/.mtm, the private folder holding mtm's record,
// exports, state, and logs.
package stage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/momentmaker/mtm/internal/runner"
)

const DirName = ".mtm"

var ErrNotFound = errors.New("no ~/.mtm folder")

type Stage struct {
	Root string
}

// Open returns the stage folder under home, creating it with mode 0700
// when it does not exist.
func Open(home string) (*Stage, error) {
	root := filepath.Join(home, DirName)
	if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("create %s: %w", root, err)
	}
	if err := checkSafe(root); err != nil {
		return nil, err
	}
	return &Stage{Root: root}, nil
}

// Existing returns the stage folder under home only if it already exists.
func Existing(home string) (*Stage, error) {
	root := filepath.Join(home, DirName)
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w; run mtm prepare on the old Mac first", root, ErrNotFound)
	}
	if err := checkSafe(root); err != nil {
		return nil, err
	}
	return &Stage{Root: root}, nil
}

func checkSafe(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", root, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to use it. Remove it and re-run", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a folder; refusing to use it", root)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect %s: no owner information", root)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by uid %d, not you (uid %d). If Migration Assistant gave your account a new uid, run: sudo chown -R $(id -un) %s", root, st.Uid, os.Getuid(), root)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%s has permissions %04o but must be private. Fix with: chmod 700 %s", root, perm, root)
	}
	return nil
}

func (s *Stage) Path(elem ...string) string {
	return filepath.Join(append([]string{s.Root}, elem...)...)
}

func (s *Stage) MkdirAll(name string) error {
	if err := os.MkdirAll(s.Path(name), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", s.Path(name), err)
	}
	return nil
}

// File is a private (0600) file that appears at its final path only after Commit.
type File struct {
	*os.File
	final string
	done  bool
}

func (s *Stage) Create(name string) (*File, error) {
	final := s.Path(name)
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(final)+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", final, err)
	}
	return &File{File: f, final: final}, nil
}

func (f *File) Commit() error {
	if err := f.File.Sync(); err != nil {
		f.Abort()
		return fmt.Errorf("sync %s: %w", f.final, err)
	}
	if err := f.File.Close(); err != nil {
		os.Remove(f.Name())
		f.done = true
		return fmt.Errorf("close %s: %w", f.final, err)
	}
	if err := os.Rename(f.Name(), f.final); err != nil {
		os.Remove(f.Name())
		f.done = true
		return fmt.Errorf("save %s: %w", f.final, err)
	}
	f.done = true
	return nil
}

func (f *File) Abort() {
	if f.done {
		return
	}
	f.File.Close()
	os.Remove(f.Name())
	f.done = true
}

func (s *Stage) WriteFile(name string, data []byte) error {
	f, err := s.Create(name)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Abort()
		return fmt.Errorf("write %s: %w", f.final, err)
	}
	return f.Commit()
}

// ExcludeFromBackups keeps database dumps and the record out of Time Machine.
func (s *Stage) ExcludeFromBackups(ctx context.Context, r runner.Runner) error {
	if _, err := r.Run(ctx, runner.Command{Name: "/usr/bin/tmutil", Args: []string{"addexclusion", s.Root}}); err != nil {
		return fmt.Errorf("exclude %s from Time Machine: %w", s.Root, err)
	}
	return nil
}

func (s *Stage) Remove() error {
	if err := os.RemoveAll(s.Root); err != nil {
		return fmt.Errorf("delete %s: %w", s.Root, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/stage/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/stage
git commit -m "Add private ~/.mtm stage folder

The record and later exports hold sensitive data, so the folder is created
0700, refused when it is a symlink, foreign-owned, or world-readable,
written atomically with 0600 files, and excluded from Time Machine.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Home-relative path patterns

**Files:**
- Create: `internal/pathset/pathset.go`, `internal/pathset/pathset_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `pathset.Set`, `pathset.New(patterns ...string) (Set, error)`, `pathset.MustNew(patterns ...string) Set`, `(Set).Match(rel string) bool`. Pattern forms:
  - `dir/`: the folder and everything under it. May contain globs, such as `Library/Containers/*/Data/Library/Caches/`.
  - `a/*.log`: matched against the whole path.
  - `.DS_Store` (no slash): matched against the base name at any depth.
  - A leading `~/` is ignored.

- [ ] **Step 1: Write the failing tests**

`internal/pathset/pathset_test.go`:
```go
package pathset

import "testing"

func TestMatch(t *testing.T) {
	set := MustNew(
		"Library/Caches/",
		"Library/Containers/*/Data/Library/Caches/",
		"Library/Saved Application State/",
		".DS_Store",
		".zcompdump*",
		"~/Library/Application Support/Foo/*.log",
	)
	tests := []struct {
		rel  string
		want bool
	}{
		{"Library/Caches", true},
		{"Library/Caches/com.apple.Safari/Cache.db", true},
		{"Library/CachesBackup/x", false},
		{"Library", false},
		{"Library/Containers/com.docker.docker/Data/Library/Caches/x", true},
		{"Library/Containers/com.docker.docker/Data/vms", false},
		{"Library/Saved Application State/com.app.savedState/data.data", true},
		{"GitHub/app/.DS_Store", true},
		{".DS_Store", true},
		{".zcompdump-host-5.9", true},
		{"Library/Application Support/Foo/run.log", true},
		{"Library/Application Support/Foo/sub/run.log", false},
		{"GitHub/app/main.go", false},
	}
	for _, tt := range tests {
		if got := set.Match(tt.rel); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}

func TestNewRejectsBadPattern(t *testing.T) {
	if _, err := New("["); err == nil {
		t.Error("New(\"[\") succeeded, want an error")
	}
}

func TestEmptySetMatchesNothing(t *testing.T) {
	if MustNew().Match("anything") {
		t.Error("empty set matched")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/pathset/...`
Expected: FAIL with `undefined: MustNew`.

- [ ] **Step 3: Implement path patterns**

`internal/pathset/pathset.go`:
```go
// Package pathset matches home-relative, slash-separated paths against
// simple patterns.
//
// Pattern forms:
//
//	"Library/Caches/"   a folder and everything under it (globs allowed)
//	"Library/*/x.log"   matched against the whole path
//	".DS_Store"         no slash: matched against the base name at any depth
//
// A leading "~/" is ignored.
package pathset

import (
	"fmt"
	"path"
	"strings"
)

type Set struct {
	dirs  []string
	whole []string
	base  []string
}

func New(patterns ...string) (Set, error) {
	var s Set
	for _, p := range patterns {
		p = strings.TrimPrefix(p, "~/")
		if p == "" {
			continue
		}
		if _, err := path.Match(p, ""); err != nil {
			return Set{}, fmt.Errorf("pattern %q: %w", p, err)
		}
		switch {
		case strings.HasSuffix(p, "/"):
			s.dirs = append(s.dirs, strings.TrimSuffix(p, "/"))
		case strings.Contains(p, "/"):
			s.whole = append(s.whole, p)
		default:
			s.base = append(s.base, p)
		}
	}
	return s, nil
}

func MustNew(patterns ...string) Set {
	s, err := New(patterns...)
	if err != nil {
		panic(err)
	}
	return s
}

func (s Set) Match(rel string) bool {
	for _, d := range s.dirs {
		if matchDir(d, rel) {
			return true
		}
	}
	for _, w := range s.whole {
		if ok, _ := path.Match(w, rel); ok {
			return true
		}
	}
	base := path.Base(rel)
	for _, b := range s.base {
		if ok, _ := path.Match(b, base); ok {
			return true
		}
	}
	return false
}

// matchDir reports whether rel is the folder pattern or inside it, by
// matching the pattern against rel's leading path components.
func matchDir(pattern, rel string) bool {
	n := strings.Count(pattern, "/") + 1
	parts := strings.SplitN(rel, "/", n+1)
	if len(parts) < n {
		return false
	}
	ok, _ := path.Match(pattern, strings.Join(parts[:n], "/"))
	return ok
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/pathset/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/pathset
git commit -m "Add home-relative path patterns

Exclusions, volatile paths, git skip lists, and (in Stage 4) catalog
entries all need the same small pattern language, so it lives in one
tested place.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Manifest and file record

**Files:**
- Create: `internal/manifest/manifest.go`, `internal/manifest/record.go`, `internal/manifest/manifest_test.go`, `internal/manifest/record_test.go`

**Interfaces:**
- Consumes: `stage.Stage` with `Path`, `WriteFile` (Task 2).
- Produces:
  - Constants `manifest.Schema = 1`, `manifest.FileName = "manifest.json"`, `manifest.RecordName = "files.jsonl.gz"`. Errors `manifest.ErrNewerSchema`, `manifest.ErrNoModule`.
  - `manifest.Manifest{Schema int; MtmVersion, MacOSVersion, MacOSBuild, Hostname, MachineID string; PreparedAt time.Time; Modules map[string]ModuleRecord}`, `manifest.ModuleRecord{PreparedAt time.Time; Inventory json.RawMessage}`.
  - `manifest.New(mtmVersion string) *Manifest`, `manifest.Load(*stage.Stage) (*Manifest, error)` (wraps `fs.ErrNotExist` when absent), `manifest.Save(*stage.Stage, *Manifest) error`.
  - `(*Manifest).SetModule(name string, at time.Time, inventory any) error`, `(*Manifest).Module(name string, inventory any) (time.Time, error)`.
  - `manifest.Entry{Path, Type string; Size, MTime int64; Mode uint32; Hash, Target, Area string}`.
  - Type constants `TypeFile "f"`, `TypeDir "d"`, `TypeLink "l"`, `TypeCloud "cloud"`, `TypeUnreadable "unreadable"`. Area constants `AreaCritical "critical"`, `AreaAppState "appstate"`.
  - `manifest.NewRecordWriter(io.Writer) *RecordWriter` with `Write(Entry) error` and `Close() error`.
  - `manifest.NewRecordReader(io.Reader) (*RecordReader, error)` with `Next() (Entry, error)` (`io.EOF` at end).

- [ ] **Step 1: Write the failing tests**

`internal/manifest/manifest_test.go`:
```go
package manifest

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/stage"
)

func openStage(t *testing.T) *stage.Stage {
	t.Helper()
	st, err := stage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSaveLoadRoundTrip(t *testing.T) {
	st := openStage(t)
	at := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	m := New("1.0.0")
	m.MacOSVersion, m.MacOSBuild, m.MachineID = "26.7.1", "25G241", "sha256:ab"
	if err := m.SetModule("files", at, map[string]int{"entries": 3}); err != nil {
		t.Fatal(err)
	}
	if err := Save(st, m); err != nil {
		t.Fatal(err)
	}
	got, err := Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != Schema || got.MtmVersion != "1.0.0" || got.MacOSBuild != "25G241" || got.MachineID != "sha256:ab" {
		t.Errorf("loaded %+v", got)
	}
	var inv map[string]int
	preparedAt, err := got.Module("files", &inv)
	if err != nil {
		t.Fatal(err)
	}
	if !preparedAt.Equal(at) || inv["entries"] != 3 || !got.PreparedAt.Equal(at) {
		t.Errorf("module prepared %v inv %v manifest prepared %v", preparedAt, inv, got.PreparedAt)
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	st := openStage(t)
	st.WriteFile(FileName, []byte(`{"schema": 99}`))
	if _, err := Load(st); !errors.Is(err, ErrNewerSchema) {
		t.Errorf("err = %v, want ErrNewerSchema", err)
	}
}

func TestLoadMissingIsNotExist(t *testing.T) {
	if _, err := Load(openStage(t)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestModuleMissing(t *testing.T) {
	var v any
	if _, err := New("x").Module("git", &v); !errors.Is(err, ErrNoModule) {
		t.Errorf("err = %v, want ErrNoModule", err)
	}
}

func TestSetModuleKeepsLatestPreparedAt(t *testing.T) {
	m := New("x")
	later := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	m.SetModule("git", later, struct{}{})
	m.SetModule("files", later.Add(-time.Hour), struct{}{})
	if !m.PreparedAt.Equal(later) {
		t.Errorf("PreparedAt = %v, want %v", m.PreparedAt, later)
	}
}
```

`internal/manifest/record_test.go`:
```go
package manifest

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"
)

func TestRecordRoundTrip(t *testing.T) {
	want := []Entry{
		{Path: "Docs/résumé.pdf", Type: TypeFile, Size: 10, MTime: 1793500000123456789, Mode: 0o644, Area: AreaCritical},
		{Path: "odd\nname", Type: TypeFile, Size: 0, Mode: 0, Hash: "sha256:00", Area: AreaCritical},
		{Path: "with space/link", Type: TypeLink, Target: "../x", Area: AreaCritical},
		{Path: "Library/Preferences", Type: TypeDir, Mode: 0o700, Area: AreaAppState},
	}
	var buf bytes.Buffer
	w := NewRecordWriter(&buf)
	for _, e := range want {
		if err := w.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewRecordReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var got []Entry
	for {
		e, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, e)
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestRecordReaderRejectsGarbage(t *testing.T) {
	if _, err := NewRecordReader(bytes.NewReader([]byte("not gzip"))); err == nil {
		t.Error("NewRecordReader accepted non-gzip input")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/manifest/...`
Expected: FAIL with `undefined: New`, `undefined: Entry`, and similar.

- [ ] **Step 3: Implement the manifest**

`internal/manifest/manifest.go`:
```go
// Package manifest defines manifest.json and the file record, the two
// files prepare writes and verify reads.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/momentmaker/mtm/internal/stage"
)

const (
	Schema     = 1
	FileName   = "manifest.json"
	RecordName = "files.jsonl.gz"
)

var (
	ErrNewerSchema = errors.New("the record was written by a newer mtm; upgrade mtm on this Mac")
	ErrNoModule    = errors.New("module not in manifest")
)

type Manifest struct {
	Schema       int                     `json:"schema"`
	MtmVersion   string                  `json:"mtm_version"`
	MacOSVersion string                  `json:"macos_version"`
	MacOSBuild   string                  `json:"macos_build"`
	Hostname     string                  `json:"hostname"`
	MachineID    string                  `json:"machine_id"`
	PreparedAt   time.Time               `json:"prepared_at"`
	Modules      map[string]ModuleRecord `json:"modules"`
}

type ModuleRecord struct {
	PreparedAt time.Time       `json:"prepared_at"`
	Inventory  json.RawMessage `json:"inventory"`
}

func New(mtmVersion string) *Manifest {
	return &Manifest{Schema: Schema, MtmVersion: mtmVersion, Modules: map[string]ModuleRecord{}}
}

func Load(st *stage.Stage) (*Manifest, error) {
	path := st.Path(FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Schema > Schema {
		return nil, fmt.Errorf("%w (record schema %d, this mtm reads up to %d)", ErrNewerSchema, m.Schema, Schema)
	}
	if m.Modules == nil {
		m.Modules = map[string]ModuleRecord{}
	}
	return &m, nil
}

func Save(st *stage.Stage, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	return st.WriteFile(FileName, data)
}

func (m *Manifest) SetModule(name string, at time.Time, inventory any) error {
	raw, err := json.Marshal(inventory)
	if err != nil {
		return fmt.Errorf("encode %s inventory: %w", name, err)
	}
	m.Modules[name] = ModuleRecord{PreparedAt: at, Inventory: raw}
	if at.After(m.PreparedAt) {
		m.PreparedAt = at
	}
	return nil
}

func (m *Manifest) Module(name string, inventory any) (time.Time, error) {
	rec, ok := m.Modules[name]
	if !ok {
		return time.Time{}, fmt.Errorf("%s: %w", name, ErrNoModule)
	}
	if err := json.Unmarshal(rec.Inventory, inventory); err != nil {
		return time.Time{}, fmt.Errorf("decode %s inventory: %w", name, err)
	}
	return rec.PreparedAt, nil
}
```

`internal/manifest/record.go`:
```go
package manifest

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	TypeFile       = "f"
	TypeDir        = "d"
	TypeLink       = "l"
	TypeCloud      = "cloud"
	TypeUnreadable = "unreadable"

	AreaCritical = "critical"
	AreaAppState = "appstate"
)

// Entry is one line of the file record. Path is relative to the home
// folder and uses forward slashes. MTime is in Unix nanoseconds.
type Entry struct {
	Path   string `json:"p"`
	Type   string `json:"t"`
	Size   int64  `json:"s,omitempty"`
	MTime  int64  `json:"m,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`
	Hash   string `json:"h,omitempty"`
	Target string `json:"target,omitempty"`
	Area   string `json:"area"`
}

type RecordWriter struct {
	gz  *gzip.Writer
	enc *json.Encoder
}

func NewRecordWriter(w io.Writer) *RecordWriter {
	gz := gzip.NewWriter(w)
	return &RecordWriter{gz: gz, enc: json.NewEncoder(gz)}
}

func (w *RecordWriter) Write(e Entry) error {
	if err := w.enc.Encode(e); err != nil {
		return fmt.Errorf("write record entry %q: %w", e.Path, err)
	}
	return nil
}

// Close flushes the record. It does not close the underlying writer.
func (w *RecordWriter) Close() error {
	if err := w.gz.Close(); err != nil {
		return fmt.Errorf("finish file record: %w", err)
	}
	return nil
}

type RecordReader struct {
	dec *json.Decoder
}

func NewRecordReader(r io.Reader) (*RecordReader, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("open file record: %w", err)
	}
	return &RecordReader{dec: json.NewDecoder(gz)}, nil
}

// Next returns the next entry, or io.EOF after the last one.
func (r *RecordReader) Next() (Entry, error) {
	var e Entry
	if err := r.dec.Decode(&e); err != nil {
		if errors.Is(err, io.EOF) {
			return Entry{}, io.EOF
		}
		return Entry{}, fmt.Errorf("read file record: %w", err)
	}
	return e, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/manifest/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/manifest
git commit -m "Add manifest and streaming file record

The manifest carries per-module inventories with their prepare times, and
the file record streams through gzip so millions of home-folder entries
never sit in memory.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Module contract and verdict

**Files:**
- Create: `internal/module/module.go`, `internal/module/module_test.go`
- Create: `internal/verdict/verdict.go`, `internal/verdict/verdict_test.go`

**Interfaces:**
- Consumes: `manifest.Manifest`, `manifest.ErrNoModule` (Task 4); `stage.Stage` (Task 2); `runner.Runner` (Task 1).
- Produces:
  - `module.Machine` with `OldMac "old"` and `NewMac "new"`.
  - `module.Env{Ctx context.Context; Run runner.Runner; Home string; Machine Machine; Deep bool; Manifest *manifest.Manifest}`.
  - `module.Severity`: `SeverityInfo`, `SeverityWarn`, `SeverityBlocker`. `module.Handler`: `ByMigrationAssistant`, `ByMtm`, `Manual`.
  - `module.Finding{Module, ID string; Severity; Handler; Title, Detail, Fix string}`.
  - `module.Status`: `StatusPass`, `StatusFail`, `StatusWarn`, `StatusInfo`.
  - `module.Check{Module, ID string; Status; Critical bool; Title, Detail string; Paths []string}`.
  - `module.Result{Inventory any; Done []string; Problems []Finding}`.
  - `module.Module` interface: `Name() string`, `Scan(Env) ([]Finding, error)`, `Prepare(Env, *stage.Stage) (Result, error)`, `Finish(Env, *stage.Stage) (Result, error)`, `Verify(Env, *stage.Stage) ([]Check, error)`.
  - `module.ErrNotPrepared`, `module.LoadInventory(env Env, name string, v any) (time.Time, error)`, `module.HasBlocker([]Finding) bool`.
  - `verdict.Verdict{Safe bool; Blocking []module.Check; Pending []string}`, `verdict.Decide(checks []module.Check, pending []string) Verdict`.
- Later stages extend the contract and leave existing fields alone. Stage 3 adds export artifacts with fingerprints to `Result`. Stage 4 adds checklist items to `Result` and the catalog to `Env` (spec 7.3). Logging is handled by the `runner.Logged` decorator, not an `Env` field.

- [ ] **Step 1: Write the failing tests**

`internal/module/module_test.go`:
```go
package module

import (
	"errors"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
)

func TestLoadInventoryNeedsManifest(t *testing.T) {
	var v map[string]int
	if _, err := LoadInventory(Env{}, "files", &v); !errors.Is(err, ErrNotPrepared) {
		t.Errorf("err = %v, want ErrNotPrepared", err)
	}
}

func TestLoadInventoryMissingModule(t *testing.T) {
	var v map[string]int
	if _, err := LoadInventory(Env{Manifest: manifest.New("t")}, "files", &v); !errors.Is(err, ErrNotPrepared) {
		t.Errorf("err = %v, want ErrNotPrepared", err)
	}
}

func TestLoadInventoryDecodes(t *testing.T) {
	m := manifest.New("t")
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	if err := m.SetModule("files", at, map[string]int{"entries": 7}); err != nil {
		t.Fatal(err)
	}
	var v map[string]int
	got, err := LoadInventory(Env{Manifest: m}, "files", &v)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(at) || v["entries"] != 7 {
		t.Errorf("got %v %v", got, v)
	}
}

func TestHasBlocker(t *testing.T) {
	if HasBlocker([]Finding{{Severity: SeverityWarn}, {Severity: SeverityInfo}}) {
		t.Error("warn and info reported as blocker")
	}
	if !HasBlocker([]Finding{{Severity: SeverityWarn}, {Severity: SeverityBlocker}}) {
		t.Error("blocker not detected")
	}
}
```

`internal/verdict/verdict_test.go`:
```go
package verdict

import (
	"testing"

	"github.com/momentmaker/mtm/internal/module"
)

func TestDecide(t *testing.T) {
	pass := module.Check{ID: "a", Status: module.StatusPass, Critical: true}
	warn := module.Check{ID: "b", Status: module.StatusWarn}
	info := module.Check{ID: "c", Status: module.StatusInfo}
	fail := module.Check{ID: "d", Status: module.StatusFail}
	criticalWarn := module.Check{ID: "e", Status: module.StatusWarn, Critical: true}
	criticalInfo := module.Check{ID: "f", Status: module.StatusInfo, Critical: true}

	tests := []struct {
		name     string
		checks   []module.Check
		pending  []string
		safe     bool
		blocking int
	}{
		{"all pass", []module.Check{pass}, nil, true, 0},
		{"non-critical warn and info allowed", []module.Check{pass, warn, info}, nil, true, 0},
		{"any fail blocks", []module.Check{pass, fail}, nil, false, 1},
		{"critical warn blocks", []module.Check{criticalWarn}, nil, false, 1},
		{"critical info blocks", []module.Check{criticalInfo}, nil, false, 1},
		{"open checklist item blocks", []module.Check{pass}, []string{"Grant Accessibility to Raycast"}, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Decide(tt.checks, tt.pending)
			if v.Safe != tt.safe || len(v.Blocking) != tt.blocking {
				t.Errorf("Decide = safe %v blocking %d, want safe %v blocking %d", v.Safe, len(v.Blocking), tt.safe, tt.blocking)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/module/... ./internal/verdict/...`
Expected: FAIL with `undefined: LoadInventory`, `undefined: Decide`, and similar.

- [ ] **Step 3: Implement the contract and the verdict**

`internal/module/module.go`:
```go
// Package module defines the contract every mtm module implements.
package module

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
)

type Machine string

const (
	OldMac Machine = "old"
	NewMac Machine = "new"
)

// Env is what a module gets to work with. Manifest is set on the new Mac
// and nil during scan and prepare.
type Env struct {
	Ctx      context.Context
	Run      runner.Runner
	Home     string
	Machine  Machine
	Deep     bool
	Manifest *manifest.Manifest
}

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarn    Severity = "warn"
	SeverityBlocker Severity = "blocker"
)

type Handler string

const (
	ByMigrationAssistant Handler = "migration-assistant"
	ByMtm                Handler = "mtm"
	Manual               Handler = "manual"
)

type Finding struct {
	Module   string   `json:"module"`
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Handler  Handler  `json:"handler"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Fix      string   `json:"fix,omitempty"`
}

type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusWarn Status = "warn"
	StatusInfo Status = "info"
)

type Check struct {
	Module   string   `json:"module"`
	ID       string   `json:"id"`
	Status   Status   `json:"status"`
	Critical bool     `json:"critical"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Paths    []string `json:"paths,omitempty"`
}

// Result is what Prepare and Finish return. Inventory is stored in the
// manifest under the module's name. A Problems entry with SeverityBlocker
// makes the run NOT READY.
type Result struct {
	Inventory any
	Done      []string
	Problems  []Finding
}

type Module interface {
	Name() string
	Scan(env Env) ([]Finding, error)
	Prepare(env Env, st *stage.Stage) (Result, error)
	Finish(env Env, st *stage.Stage) (Result, error)
	Verify(env Env, st *stage.Stage) ([]Check, error)
}

var ErrNotPrepared = errors.New("not recorded by mtm prepare on the old Mac")

// LoadInventory decodes the named module's inventory from env.Manifest and
// returns the time that module was prepared.
func LoadInventory(env Env, name string, v any) (time.Time, error) {
	if env.Manifest == nil {
		return time.Time{}, fmt.Errorf("%s: %w", name, ErrNotPrepared)
	}
	at, err := env.Manifest.Module(name, v)
	if errors.Is(err, manifest.ErrNoModule) {
		return time.Time{}, fmt.Errorf("%s: %w", name, ErrNotPrepared)
	}
	return at, err
}

func HasBlocker(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == SeverityBlocker {
			return true
		}
	}
	return false
}
```

`internal/verdict/verdict.go`:
```go
// Package verdict decides whether the old Mac is safe to wipe.
package verdict

import "github.com/momentmaker/mtm/internal/module"

type Verdict struct {
	Safe     bool           `json:"safe"`
	Blocking []module.Check `json:"blocking,omitempty"`
	Pending  []string       `json:"pending_checklist,omitempty"`
}

// Decide applies spec section 8.5: no failed check, every critical check
// passed, and no checklist item left open.
func Decide(checks []module.Check, pending []string) Verdict {
	v := Verdict{Pending: pending}
	for _, c := range checks {
		if c.Status == module.StatusFail || (c.Critical && c.Status != module.StatusPass) {
			v.Blocking = append(v.Blocking, c)
		}
	}
	v.Safe = len(v.Blocking) == 0 && len(pending) == 0
	return v
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/module/... ./internal/verdict/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/module internal/verdict
git commit -m "Add module contract and SAFE TO WIPE verdict

All modules share one scan/prepare/finish/verify contract so the
orchestrator and reports stay generic, and the wipe decision lives in one
small, table-tested function.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Files policy and per-file classification

**Files:**
- Create: `internal/modules/files/policy.go`, `internal/modules/files/classify.go`
- Create: `internal/modules/files/policy_test.go`, `internal/modules/files/classify_test.go`

**Interfaces:**
- Consumes: `pathset` (Task 3); `manifest.Entry` and its constants (Task 4); `module.Status` (Task 5).
- Produces (package `files`, used by Tasks 7–8):
  - Package vars `excluded`, `volatile`, `fingerprinted` (`pathset.Set`); `area(rel string) string`; `shouldFingerprint(rel string, deep bool) bool`.
  - `Outcome` with `Match`, `ChangedAfterPrepare`, `Corrupt`, `MissingVolatile`, `MissingCritical`, `MissingAppState`, `CloudOnly`, `NotChecked`.
  - `Observed{Exists, Unreadable, Dataless bool; Type string; Size, MTime int64; Target, Hash string}`.
  - `Classify(rec manifest.Entry, obs Observed, preparedAt int64, isVolatile func(string) bool) Outcome`.
  - `statusOf(Outcome) module.Status`.

- [ ] **Step 1: Write the failing tests**

`internal/modules/files/policy_test.go`:
```go
package files

import (
	"testing"

	"github.com/momentmaker/mtm/internal/manifest"
)

func TestArea(t *testing.T) {
	tests := map[string]string{
		"Library":                     manifest.AreaAppState,
		"Library/Preferences/x.plist": manifest.AreaAppState,
		"GitHub/app/main.go":          manifest.AreaCritical,
		"LibraryNotes/todo.md":        manifest.AreaCritical,
		".zshrc":                      manifest.AreaCritical,
	}
	for rel, want := range tests {
		if got := area(rel); got != want {
			t.Errorf("area(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestShouldFingerprint(t *testing.T) {
	tests := []struct {
		rel  string
		deep bool
		want bool
	}{
		{".zshrc", false, true},
		{".ssh/id_ed25519", false, true},
		{".gnupg/pubring.kbx", false, true},
		{".config/git/config", false, true},
		{"GitHub/app/main.go", false, false},
		{"GitHub/app/.env", false, false},
		{"GitHub/app/main.go", true, true},
	}
	for _, tt := range tests {
		if got := shouldFingerprint(tt.rel, tt.deep); got != tt.want {
			t.Errorf("shouldFingerprint(%q, %v) = %v, want %v", tt.rel, tt.deep, got, tt.want)
		}
	}
}

func TestExcludedAndVolatile(t *testing.T) {
	for _, rel := range []string{
		".mtm/manifest.json",
		".Trash/old.txt",
		"Library/Caches/com.apple.Safari",
		"Library/Logs/DiagnosticReports/x.ips",
		"Library/Containers/com.apple.mail/Data/Library/Caches/x",
		"Library/Containers/com.docker.docker/Data/vms/0/data/Docker.raw",
	} {
		if !excluded.Match(rel) {
			t.Errorf("excluded.Match(%q) = false, want true", rel)
		}
	}
	if excluded.Match("Library/Preferences/com.apple.dock.plist") {
		t.Error("preferences excluded")
	}
	for _, rel := range []string{"GitHub/.DS_Store", ".zcompdump-host-5.9", "Library/Saved Application State/x/data.data"} {
		if !volatile.Match(rel) {
			t.Errorf("volatile.Match(%q) = false, want true", rel)
		}
	}
}
```

`internal/modules/files/classify_test.go`:
```go
package files

import (
	"testing"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
)

func withHash(o Observed, h string) Observed {
	o.Hash = h
	return o
}

func TestClassify(t *testing.T) {
	const prepared = int64(1_000)
	notVolatile := func(string) bool { return false }
	isVolatile := func(string) bool { return true }

	file := manifest.Entry{Path: "GitHub/a.go", Type: manifest.TypeFile, Size: 10, MTime: 900, Area: manifest.AreaCritical}
	hashed := file
	hashed.Hash = "sha256:aa"
	appFile := manifest.Entry{Path: "Library/Preferences/p.plist", Type: manifest.TypeFile, Size: 5, MTime: 900, Area: manifest.AreaAppState}
	link := manifest.Entry{Path: "link", Type: manifest.TypeLink, Target: "GitHub", MTime: 900, Area: manifest.AreaCritical}
	dir := manifest.Entry{Path: "GitHub", Type: manifest.TypeDir, Area: manifest.AreaCritical}
	same := Observed{Exists: true, Type: manifest.TypeFile, Size: 10, MTime: 900}

	tests := []struct {
		name     string
		rec      manifest.Entry
		obs      Observed
		volatile func(string) bool
		want     Outcome
	}{
		{"identical", file, same, notVolatile, Match},
		{"identical with fingerprint", hashed, withHash(same, "sha256:aa"), notVolatile, Match},
		{"modified after prepare", file, Observed{Exists: true, Type: manifest.TypeFile, Size: 99, MTime: 1_500}, notVolatile, ChangedAfterPrepare},
		{"size differs, old time", file, Observed{Exists: true, Type: manifest.TypeFile, Size: 11, MTime: 900}, notVolatile, Corrupt},
		{"fingerprint differs, old time", hashed, withHash(same, "sha256:bb"), notVolatile, Corrupt},
		{"missing critical", file, Observed{}, notVolatile, MissingCritical},
		{"missing app state", appFile, Observed{}, notVolatile, MissingAppState},
		{"missing volatile", file, Observed{}, isVolatile, MissingVolatile},
		{"recorded cloud-only", manifest.Entry{Path: "x", Type: manifest.TypeCloud, Area: manifest.AreaCritical}, Observed{}, notVolatile, CloudOnly},
		{"recorded unreadable", manifest.Entry{Path: "x", Type: manifest.TypeUnreadable, Area: manifest.AreaCritical}, Observed{}, notVolatile, NotChecked},
		{"unreadable now", file, Observed{Exists: true, Unreadable: true}, notVolatile, NotChecked},
		{"evicted to the cloud", file, Observed{Exists: true, Dataless: true, Type: manifest.TypeFile}, notVolatile, CloudOnly},
		{"symlink target changed", link, Observed{Exists: true, Type: manifest.TypeLink, Target: "Elsewhere", MTime: 900}, notVolatile, Corrupt},
		{"file became a folder", file, Observed{Exists: true, Type: manifest.TypeDir, MTime: 900}, notVolatile, Corrupt},
		{"folder with new entries", dir, Observed{Exists: true, Type: manifest.TypeDir, MTime: 1_500}, notVolatile, Match},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.rec, tt.obs, prepared, tt.volatile); got != tt.want {
				t.Errorf("Classify = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatusOf(t *testing.T) {
	tests := map[Outcome]module.Status{
		Match:               module.StatusPass,
		ChangedAfterPrepare: module.StatusInfo,
		MissingVolatile:     module.StatusInfo,
		CloudOnly:           module.StatusInfo,
		NotChecked:          module.StatusInfo,
		MissingAppState:     module.StatusWarn,
		MissingCritical:     module.StatusFail,
		Corrupt:             module.StatusFail,
	}
	for o, want := range tests {
		if got := statusOf(o); got != want {
			t.Errorf("statusOf(%v) = %q, want %q", o, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/files/...`
Expected: FAIL with `undefined: area`, `undefined: Classify`, and similar.

- [ ] **Step 3: Implement the policy and the classification**

`internal/modules/files/policy.go`:
```go
package files

import (
	"strings"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/pathset"
)

// excluded paths are never recorded: caches, the trash, logs, mtm's own
// folder, and the Docker Desktop VM, which the docker module checks.
var excluded = pathset.MustNew(
	".mtm/",
	".Trash/",
	"Library/Caches/",
	"Library/Logs/",
	"Library/Containers/*/Data/Library/Caches/",
	"Library/Group Containers/*/Library/Caches/",
	"Library/Containers/com.docker.docker/Data/vms/",
)

// volatile paths come and go on their own, so a missing one is not an error.
var volatile = pathset.MustNew(
	".DS_Store",
	".zcompdump*",
	"Library/Saved Application State/",
)

var fingerprinted = pathset.MustNew(".ssh/", ".gnupg/", ".config/")

func area(rel string) string {
	if rel == "Library" || strings.HasPrefix(rel, "Library/") {
		return manifest.AreaAppState
	}
	return manifest.AreaCritical
}

// shouldFingerprint reports whether a regular file gets a content
// fingerprint: top-level dotfiles and everything in fingerprinted folders,
// or every file when deep is set.
func shouldFingerprint(rel string, deep bool) bool {
	if deep {
		return true
	}
	isTopLevelDotfile := !strings.Contains(rel, "/") && strings.HasPrefix(rel, ".")
	return isTopLevelDotfile || fingerprinted.Match(rel)
}
```

`internal/modules/files/classify.go`:
```go
package files

import (
	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
)

type Outcome int

const (
	Match Outcome = iota
	ChangedAfterPrepare
	Corrupt
	MissingVolatile
	MissingCritical
	MissingAppState
	CloudOnly
	NotChecked
)

// Observed is what verify finds at a recorded path on the new Mac. Hash is
// filled only when the record has a fingerprint and the file was not
// modified after prepare.
type Observed struct {
	Exists     bool
	Unreadable bool
	Dataless   bool
	Type       string
	Size       int64
	MTime      int64
	Target     string
	Hash       string
}

// Classify applies spec section 8.4 to one recorded entry. A file modified
// after prepare is a normal change, not damage. Folders are checked for
// presence only, because their times change whenever an entry is added.
func Classify(rec manifest.Entry, obs Observed, preparedAt int64, isVolatile func(string) bool) Outcome {
	switch {
	case rec.Type == manifest.TypeCloud:
		return CloudOnly
	case rec.Type == manifest.TypeUnreadable:
		return NotChecked
	case !obs.Exists && isVolatile(rec.Path):
		return MissingVolatile
	case !obs.Exists && rec.Area == manifest.AreaCritical:
		return MissingCritical
	case !obs.Exists:
		return MissingAppState
	case obs.Unreadable:
		return NotChecked
	case obs.Dataless:
		return CloudOnly
	case rec.Type != manifest.TypeDir && obs.MTime > preparedAt:
		return ChangedAfterPrepare
	case obs.Type != rec.Type:
		return Corrupt
	case rec.Type == manifest.TypeFile && (obs.Size != rec.Size || (rec.Hash != "" && obs.Hash != rec.Hash)):
		return Corrupt
	case rec.Type == manifest.TypeLink && obs.Target != rec.Target:
		return Corrupt
	}
	return Match
}

func statusOf(o Outcome) module.Status {
	switch o {
	case Match:
		return module.StatusPass
	case MissingAppState:
		return module.StatusWarn
	case MissingCritical, Corrupt:
		return module.StatusFail
	}
	return module.StatusInfo
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/modules/files/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/modules/files
git commit -m "Add file policy and per-file verify rules

Separating normal post-prepare changes from same-time corruption is the
heart of a trustworthy SAFE TO WIPE, so the rules are a pure function
with a table test for every case in spec 8.4.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Walking and recording the home folder

**Files:**
- Create: `internal/fsinfo/fsinfo.go`, `internal/fsinfo/fsinfo_test.go`
- Create: `internal/testutil/testutil.go`
- Create: `internal/modules/files/walk.go`, `internal/modules/files/record.go`, `internal/modules/files/record_test.go`

**Interfaces:**
- Consumes: `excluded`, `area`, `shouldFingerprint` (Task 6); `manifest.RecordWriter`, `manifest.Entry` (Task 4); `pathset.Set` (Task 3).
- Produces:
  - `fsinfo.IsDataless(fs.FileInfo) bool`; `fsinfo.HashFile(path string) (string, error)`, which returns `"sha256:<hex>"`.
  - `testutil.WriteFile(t, path, content string)`: creates parent folders, mode 0644.
  - Package `files`:
    - `Stats{Entries, Fingerprinted, Cloud int; Unreadable []string}`, the files inventory;
    - `writeRecord(ctx, home string, w *manifest.RecordWriter, deep bool) (Stats, error)`;
    - `walkHome(ctx, home string, skip pathset.Set, emit func(item) error) error`;
    - `item{rel, abs string; info fs.FileInfo; target string; cloud, unreadable bool}`.

- [ ] **Step 1: Write the failing tests**

`internal/fsinfo/fsinfo_test.go`:
```go
package fsinfo

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type fakeInfo struct{ sys any }

func (fakeInfo) Name() string       { return "x" }
func (fakeInfo) Size() int64        { return 0 }
func (fakeInfo) Mode() fs.FileMode  { return 0 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any         { return f.sys }

func TestIsDataless(t *testing.T) {
	if !IsDataless(fakeInfo{sys: &syscall.Stat_t{Flags: sfDataless}}) {
		t.Error("dataless flag not detected")
	}
	if IsDataless(fakeInfo{sys: &syscall.Stat_t{}}) {
		t.Error("regular file reported dataless")
	}
	if IsDataless(fakeInfo{}) {
		t.Error("missing stat reported dataless")
	}
}

func TestHashFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	os.WriteFile(path, []byte("hello\n"), 0o600)
	got, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("hello\n"))); got != want {
		t.Errorf("HashFile = %q, want %q", got, want)
	}
}
```

`internal/modules/files/record_test.go`:
```go
package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/testutil"
)

func recordHome(t *testing.T, home string, deep bool) (map[string]manifest.Entry, Stats) {
	t.Helper()
	var buf bytes.Buffer
	w := manifest.NewRecordWriter(&buf)
	stats, err := writeRecord(context.Background(), home, w, deep)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := manifest.NewRecordReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]manifest.Entry{}
	for {
		e, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		entries[e.Path] = e
	}
	return entries, stats
}

func sha(content string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
}

func TestRecordFingerprintsImportantFilesOnly(t *testing.T) {
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	testutil.WriteFile(t, filepath.Join(home, ".ssh/id_ed25519"), "key\n")
	testutil.WriteFile(t, filepath.Join(home, "GitHub/app/main.go"), "package main\n")

	entries, stats := recordHome(t, home, false)

	if got := entries[".zshrc"].Hash; got != sha("export A=1\n") {
		t.Errorf(".zshrc hash = %q", got)
	}
	if got := entries[".ssh/id_ed25519"].Hash; got != sha("key\n") {
		t.Errorf("ssh key hash = %q", got)
	}
	main := entries["GitHub/app/main.go"]
	if main.Hash != "" || main.Type != manifest.TypeFile || main.Size != 13 || main.Area != manifest.AreaCritical {
		t.Errorf("main.go entry = %+v", main)
	}
	if entries["GitHub"].Type != manifest.TypeDir {
		t.Errorf("GitHub entry = %+v, want a folder", entries["GitHub"])
	}
	if stats.Fingerprinted != 2 {
		t.Errorf("Fingerprinted = %d, want 2", stats.Fingerprinted)
	}
}

func TestRecordDeepFingerprintsEverything(t *testing.T) {
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, "GitHub/app/main.go"), "package main\n")
	entries, _ := recordHome(t, home, true)
	if got := entries["GitHub/app/main.go"].Hash; got != sha("package main\n") {
		t.Errorf("deep hash = %q", got)
	}
}

func TestRecordSkipsExcludedPaths(t *testing.T) {
	home := t.TempDir()
	for _, rel := range []string{
		"Library/Caches/big",
		".mtm/manifest.json",
		".Trash/old.txt",
		"Library/Containers/com.docker.docker/Data/vms/0/data/Docker.raw",
		"Library/Preferences/p.plist",
	} {
		testutil.WriteFile(t, filepath.Join(home, rel), "x")
	}
	entries, _ := recordHome(t, home, false)
	for rel := range entries {
		if excluded.Match(rel) {
			t.Errorf("excluded path recorded: %q", rel)
		}
	}
	if got := entries["Library/Preferences/p.plist"]; got.Area != manifest.AreaAppState {
		t.Errorf("preferences entry = %+v, want app state", got)
	}
}

func TestRecordKeepsUnusualNames(t *testing.T) {
	home := t.TempDir()
	names := []string{"Docs/résumé.pdf", "Docs/with space.txt", "Docs/odd\nname.txt", "Docs/-rf"}
	for _, rel := range names {
		testutil.WriteFile(t, filepath.Join(home, rel), "x")
	}
	entries, _ := recordHome(t, home, false)
	for _, rel := range names {
		if entries[rel].Type != manifest.TypeFile {
			t.Errorf("%q not recorded as a file: %+v", rel, entries[rel])
		}
	}
}

func TestRecordNeverFollowsSymlinks(t *testing.T) {
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, "GitHub/app/main.go"), "package main\n")
	links := map[string]string{"applink": "GitHub/app", "loop": "loop", "dangling": "nowhere", "outside": "/etc"}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(home, name)); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := recordHome(t, home, false)
	for name, target := range links {
		if e := entries[name]; e.Type != manifest.TypeLink || e.Target != target {
			t.Errorf("%s = %+v, want link to %q", name, e, target)
		}
	}
	for rel := range entries {
		if strings.HasPrefix(rel, "applink/") {
			t.Errorf("walk followed a symlink: %q", rel)
		}
	}
}

func TestRecordMarksUnreadableFolders(t *testing.T) {
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, "Private/secret.txt"), "x")
	testutil.WriteFile(t, filepath.Join(home, "Public/readme.txt"), "y")
	private := filepath.Join(home, "Private")
	if err := os.Chmod(private, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(private, 0o755) })

	entries, stats := recordHome(t, home, false)

	if got := entries["Private"].Type; got != manifest.TypeUnreadable {
		t.Errorf("Private type = %q, want unreadable", got)
	}
	if _, ok := entries["Private/secret.txt"]; ok {
		t.Error("recorded a file inside an unreadable folder")
	}
	if _, ok := entries["Public/readme.txt"]; !ok {
		t.Error("walk stopped at the unreadable folder")
	}
	if !slices.Equal(stats.Unreadable, []string{"Private"}) {
		t.Errorf("Unreadable = %q, want [Private]", stats.Unreadable)
	}
}

func TestRecordSkipsPipes(t *testing.T) {
	home := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(home, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, _ := recordHome(t, home, true)
	if _, ok := entries["pipe"]; ok {
		t.Error("recorded a named pipe")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/fsinfo/... ./internal/modules/files/...`
Expected: FAIL. `fsinfo` and `testutil` don't exist yet, and `writeRecord` is undefined.

- [ ] **Step 3: Implement fsinfo, the test helper, the walk, and the record**

`internal/fsinfo/fsinfo.go`:
```go
// Package fsinfo holds file-system helpers shared by modules.
package fsinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// sfDataless is SF_DATALESS from <sys/stat.h>: the item's content lives in
// the cloud, and reading it would start a download.
const sfDataless = 0x40000000

func IsDataless(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Flags&sfDataless != 0
}

// HashFile returns "sha256:" followed by the hex SHA-256 of the file's content.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
```

`internal/testutil/testutil.go`:
```go
// Package testutil holds helpers shared by tests.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// WriteFile creates path with content, making parent folders as needed.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

`internal/modules/files/walk.go`:
```go
package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/pathset"
)

type item struct {
	rel        string
	abs        string
	info       fs.FileInfo
	target     string
	cloud      bool
	unreadable bool
}

// walkHome visits everything under home in sorted order without following
// symlinks. It skips paths in skip and non-regular files such as sockets.
// It never opens cloud-only items, and it reports folders it cannot read as
// unreadable instead of failing.
func walkHome(ctx context.Context, home string, skip pathset.Set, emit func(item) error) error {
	entries, err := os.ReadDir(home)
	if err != nil {
		return fmt.Errorf("read home folder %s: %w", home, err)
	}
	return visitEntries(ctx, home, "", entries, skip, emit)
}

func visitEntries(ctx context.Context, home, rel string, entries []fs.DirEntry, skip pathset.Set, emit func(item) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, e := range entries {
		childRel := path.Join(rel, e.Name())
		if skip.Match(childRel) {
			continue
		}
		if err := visit(ctx, home, childRel, skip, emit); err != nil {
			return err
		}
	}
	return nil
}

func visit(ctx context.Context, home, rel string, skip pathset.Set, emit func(item) error) error {
	abs := filepath.Join(home, filepath.FromSlash(rel))
	info, err := os.Lstat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case errors.Is(err, fs.ErrPermission):
		return emit(item{rel: rel, abs: abs, unreadable: true})
	case err != nil:
		return fmt.Errorf("inspect %s: %w", abs, err)
	}
	it := item{rel: rel, abs: abs, info: info}
	switch mode := info.Mode(); {
	case fsinfo.IsDataless(info):
		it.cloud = true
		return emit(it)
	case mode&fs.ModeSymlink != 0:
		if it.target, err = os.Readlink(abs); err != nil {
			return fmt.Errorf("read link %s: %w", abs, err)
		}
		return emit(it)
	case mode.IsDir():
		entries, err := os.ReadDir(abs)
		switch {
		case errors.Is(err, fs.ErrPermission):
			return emit(item{rel: rel, abs: abs, unreadable: true})
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return fmt.Errorf("read %s: %w", abs, err)
		}
		if err := emit(it); err != nil {
			return err
		}
		return visitEntries(ctx, home, rel, entries, skip, emit)
	case mode.IsRegular():
		return emit(it)
	}
	return nil
}
```

`internal/modules/files/record.go`:
```go
package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"sync"

	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/manifest"
)

// Stats summarizes a file record. It is the files module's inventory.
type Stats struct {
	Entries       int      `json:"entries"`
	Fingerprinted int      `json:"fingerprinted"`
	Cloud         int      `json:"cloud"`
	Unreadable    []string `json:"unreadable,omitempty"`
}

func (s *Stats) add(e manifest.Entry) {
	s.Entries++
	switch {
	case e.Hash != "":
		s.Fingerprinted++
	case e.Type == manifest.TypeCloud:
		s.Cloud++
	case e.Type == manifest.TypeUnreadable:
		s.Unreadable = append(s.Unreadable, e.Path)
	}
}

type pending struct {
	entry manifest.Entry
	abs   string
	done  chan error
}

// writeRecord walks home and writes the file record to w. Files are
// fingerprinted in parallel while the record keeps walk order, and memory
// stays bounded by the channel sizes.
func writeRecord(ctx context.Context, home string, w *manifest.RecordWriter, deep bool) (Stats, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workers := runtime.NumCPU()
	jobs := make(chan *pending, workers*4)
	ordered := make(chan *pending, workers*64)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				hash, err := fsinfo.HashFile(p.abs)
				p.entry.Hash = hash
				p.done <- err
			}
		}()
	}

	walkErr := make(chan error, 1)
	go func() {
		defer close(ordered)
		defer close(jobs)
		walkErr <- walkHome(ctx, home, excluded, func(it item) error {
			p := &pending{entry: toEntry(it)}
			if p.entry.Type == manifest.TypeFile && shouldFingerprint(it.rel, deep) {
				p.abs = it.abs
				p.done = make(chan error, 1)
				select {
				case jobs <- p:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			select {
			case ordered <- p:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	var stats Stats
	var writeErr error
	for p := range ordered {
		if p.done != nil {
			if err := <-p.done; err != nil {
				switch {
				case errors.Is(err, fs.ErrNotExist):
					continue
				case errors.Is(err, fs.ErrPermission):
					p.entry = manifest.Entry{Path: p.entry.Path, Type: manifest.TypeUnreadable, Area: p.entry.Area}
				case writeErr == nil:
					writeErr = fmt.Errorf("fingerprint %s: %w", p.abs, err)
					cancel()
					continue
				default:
					continue
				}
			}
		}
		if writeErr != nil {
			continue
		}
		if err := w.Write(p.entry); err != nil {
			writeErr = err
			cancel()
			continue
		}
		stats.add(p.entry)
	}
	wg.Wait()
	if writeErr != nil {
		return stats, writeErr
	}
	if err := <-walkErr; err != nil {
		return stats, err
	}
	return stats, nil
}

func toEntry(it item) manifest.Entry {
	e := manifest.Entry{Path: it.rel, Area: area(it.rel)}
	switch {
	case it.unreadable:
		e.Type = manifest.TypeUnreadable
	case it.cloud:
		e.Type = manifest.TypeCloud
		e.Size = it.info.Size()
		e.MTime = it.info.ModTime().UnixNano()
	case it.info.Mode()&fs.ModeSymlink != 0:
		e.Type = manifest.TypeLink
		e.Target = it.target
		e.MTime = it.info.ModTime().UnixNano()
	case it.info.IsDir():
		e.Type = manifest.TypeDir
		e.Mode = uint32(it.info.Mode().Perm())
	default:
		e.Type = manifest.TypeFile
		e.Size = it.info.Size()
		e.MTime = it.info.ModTime().UnixNano()
		e.Mode = uint32(it.info.Mode().Perm())
	}
	return e
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/fsinfo/... ./internal/modules/files/...`
Expected: PASS, with no race reports.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/fsinfo internal/testutil internal/modules/files
git commit -m "Record the home folder with parallel fingerprints

The walk never follows symlinks, never opens cloud-only files (which
would start downloads), and records unreadable folders instead of
failing, so prepare works on a real Mac without Full Disk Access.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Files module (prepare, verify, scan)

**Files:**
- Create: `internal/modules/files/module.go`, `internal/modules/files/verify.go`, `internal/modules/files/scan.go`, `internal/modules/files/module_test.go`
- Modify: `internal/testutil/testutil.go` (add `Copy`)

**Interfaces:**
- Consumes: `writeRecord`, `Stats`, `Classify`, `statusOf`, `volatile` (Tasks 6–7); `stage.Stage` (Task 2); `manifest` (Task 4); `module` (Task 5); `fsinfo` (Task 7).
- Produces:
  - `files.Name = "files"`, `files.HomeGroup = "home folder files"`, `files.New() Module`. `files.Module` implements `module.Module`.
  - Check IDs are `"files:" + group`. A group is the top-level folder, the first two levels inside `Library`, or `HomeGroup` for files sitting directly in the home folder.
  - `testutil.Copy(t, src, dst string)` copies with `ditto`, which keeps times, modes, and extended attributes.

- [ ] **Step 1: Add the copy helper to testutil**

Add to `internal/testutil/testutil.go`. Merge `os/exec` into the existing import block.
```go
// Copy copies src to dst with ditto, which keeps modification times,
// permissions, and extended attributes, like Migration Assistant.
func Copy(t *testing.T, src, dst string) {
	t.Helper()
	if out, err := exec.Command("/usr/bin/ditto", src, dst).CombinedOutput(); err != nil {
		t.Fatalf("ditto %s %s: %v\n%s", src, dst, err, out)
	}
}
```

- [ ] **Step 2: Write the failing tests**

`internal/modules/files/module_test.go`:
```go
package files

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
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

func seedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for rel, content := range map[string]string{
		".zshrc":                      "export A=1\n",
		".ssh/id_ed25519":             "key\n",
		"GitHub/app/main.go":          "package main\n",
		"GitHub/app/.DS_Store":        "ds\n",
		"Documents/notes.md":          "notes\n",
		"Library/Preferences/p.plist": "plist\n",
	} {
		testutil.WriteFile(t, filepath.Join(home, rel), content)
	}
	return home
}

func prepareHome(t *testing.T, home string) *manifest.Manifest {
	t.Helper()
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	preparedAt := time.Now()
	res, err := New().Prepare(module.Env{Ctx: context.Background(), Home: home}, st)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.New("test")
	if err := m.SetModule(Name, preparedAt, res.Inventory); err != nil {
		t.Fatal(err)
	}
	return m
}

func verifyHome(t *testing.T, home string, m *manifest.Manifest) map[string]module.Check {
	t.Helper()
	st, err := stage.Existing(home)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := New().Verify(module.Env{Ctx: context.Background(), Home: home, Manifest: m}, st)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]module.Check{}
	for _, c := range checks {
		byTitle[c.Title] = c
	}
	return byTitle
}

func assertAllPass(t *testing.T, checks map[string]module.Check) {
	t.Helper()
	for title, c := range checks {
		if c.Status != module.StatusPass {
			t.Errorf("%s: %s (%s) %v", title, c.Status, c.Detail, c.Paths)
		}
	}
}

func TestPrepareThenVerifyPasses(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	checks := verifyHome(t, home, m)
	assertAllPass(t, checks)
	for _, group := range []string{HomeGroup, ".ssh", "GitHub", "Documents", "Library/Preferences"} {
		if _, ok := checks[group]; !ok {
			t.Errorf("no check for group %q", group)
		}
	}
}

func TestVerifyFailsOnMissingCriticalFile(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	os.Remove(filepath.Join(home, ".ssh/id_ed25519"))
	c := verifyHome(t, home, m)[".ssh"]
	if c.Status != module.StatusFail || !c.Critical || !slices.Contains(c.Paths, ".ssh/id_ed25519") {
		t.Errorf(".ssh check = %+v, want critical fail listing the key", c)
	}
}

func TestVerifyFailsOnCorruptionWithOriginalTime(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	path := filepath.Join(home, ".zshrc")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("export B=2\n"), 0o644)
	os.Chtimes(path, info.ModTime(), info.ModTime())
	if c := verifyHome(t, home, m)[HomeGroup]; c.Status != module.StatusFail {
		t.Errorf("home group = %+v, want fail", c)
	}
}

func TestVerifyNotesChangesAfterPrepare(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	path := filepath.Join(home, "GitHub/app/main.go")
	os.WriteFile(path, []byte("package main // edited\n"), 0o644)
	later := time.Now().Add(time.Minute)
	os.Chtimes(path, later, later)
	c := verifyHome(t, home, m)["GitHub"]
	if c.Status != module.StatusPass || !strings.Contains(c.Detail, "1 changed after prepare") {
		t.Errorf("GitHub check = %+v, want pass noting the change", c)
	}
}

func TestVerifyWarnsOnMissingAppState(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	os.Remove(filepath.Join(home, "Library/Preferences/p.plist"))
	c := verifyHome(t, home, m)["Library/Preferences"]
	if c.Status != module.StatusWarn || c.Critical {
		t.Errorf("preferences check = %+v, want non-critical warn", c)
	}
}

func TestVerifyIgnoresMissingVolatile(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	os.Remove(filepath.Join(home, "GitHub/app/.DS_Store"))
	c := verifyHome(t, home, m)["GitHub"]
	if c.Status != module.StatusPass || !strings.Contains(c.Detail, "1 missing (expected)") {
		t.Errorf("GitHub check = %+v, want pass noting expected missing file", c)
	}
}

func TestVerifyReportsUnreadableAsNotChecked(t *testing.T) {
	home := seedHome(t)
	testutil.WriteFile(t, filepath.Join(home, "Private/secret.txt"), "x")
	private := filepath.Join(home, "Private")
	os.Chmod(private, 0)
	t.Cleanup(func() { os.Chmod(private, 0o755) })
	m := prepareHome(t, home)
	c := verifyHome(t, home, m)["Private"]
	if c.Status != module.StatusPass || !strings.Contains(c.Detail, "not checked") {
		t.Errorf("Private check = %+v, want pass noting not checked", c)
	}
}

func TestVerifyWorksAfterCopyToDifferentHome(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	newHome := filepath.Join(t.TempDir(), "newuser")
	testutil.Copy(t, home, newHome)
	assertAllPass(t, verifyHome(t, newHome, m))
}

func TestScanReportsMissingFullDiskAccess(t *testing.T) {
	home := t.TempDir()
	safari := filepath.Join(home, "Library/Safari")
	os.MkdirAll(safari, 0o755)
	os.Chmod(safari, 0)
	t.Cleanup(func() { os.Chmod(safari, 0o755) })
	findings, err := New().Scan(module.Env{Ctx: context.Background(), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(findings, func(f module.Finding) bool { return f.ID == "full-disk-access" }) {
		t.Errorf("findings = %+v, want full-disk-access warning", findings)
	}
}

func TestScanWithReadableHomeHasNoAccessWarning(t *testing.T) {
	findings, err := New().Scan(module.Env{Ctx: context.Background(), Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].ID != "home" {
		t.Errorf("findings = %+v, want only the home info finding", findings)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/modules/files/...`
Expected: FAIL with `undefined: New`, `undefined: HomeGroup`, `undefined: Name`.

- [ ] **Step 4: Implement the module**

`internal/modules/files/module.go`:
```go
// Package files records the home folder on the old Mac and checks the copy
// on the new Mac.
package files

import (
	"fmt"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
)

const Name = "files"

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return Name }

func (Module) Prepare(env module.Env, st *stage.Stage) (module.Result, error) {
	f, err := st.Create(manifest.RecordName)
	if err != nil {
		return module.Result{}, err
	}
	rw := manifest.NewRecordWriter(f)
	stats, err := writeRecord(env.Ctx, env.Home, rw, env.Deep)
	if err == nil {
		err = rw.Close()
	}
	if err != nil {
		f.Abort()
		return module.Result{}, err
	}
	if err := f.Commit(); err != nil {
		return module.Result{}, err
	}
	done := []string{fmt.Sprintf("recorded %d items, %d fingerprinted", stats.Entries, stats.Fingerprinted)}
	if stats.Cloud > 0 {
		done = append(done, fmt.Sprintf("left %d cloud-only items untouched", stats.Cloud))
	}
	if n := len(stats.Unreadable); n > 0 {
		done = append(done, fmt.Sprintf("could not read %d folders; they will not be checked", n))
	}
	return module.Result{Inventory: stats, Done: done}, nil
}

// Finish has nothing to restore: Migration Assistant copies the files.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}
```

`internal/modules/files/verify.go`:
```go
package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
)

const (
	HomeGroup        = "home folder files"
	maxPathsPerGroup = 200
)

func (Module) Verify(env module.Env, st *stage.Stage) ([]module.Check, error) {
	var stats Stats
	preparedAt, err := module.LoadInventory(env, Name, &stats)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(st.Path(manifest.RecordName))
	if err != nil {
		return nil, fmt.Errorf("open file record: %w", err)
	}
	defer f.Close()
	rr, err := manifest.NewRecordReader(f)
	if err != nil {
		return nil, err
	}
	prepared := preparedAt.UnixNano()
	t := newTally()
	for {
		if err := env.Ctx.Err(); err != nil {
			return nil, err
		}
		rec, err := rr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		obs, err := observe(env.Home, rec, prepared)
		if err != nil {
			return nil, err
		}
		t.add(rec, Classify(rec, obs, prepared, volatile.Match))
	}
	return t.checks(), nil
}

func observe(home string, rec manifest.Entry, preparedAt int64) (Observed, error) {
	if rec.Type == manifest.TypeCloud || rec.Type == manifest.TypeUnreadable {
		return Observed{}, nil
	}
	abs := filepath.Join(home, filepath.FromSlash(rec.Path))
	info, err := os.Lstat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return Observed{}, nil
	case errors.Is(err, fs.ErrPermission):
		return Observed{Exists: true, Unreadable: true}, nil
	case err != nil:
		return Observed{}, fmt.Errorf("inspect %s: %w", abs, err)
	}
	obs := Observed{Exists: true, Size: info.Size(), MTime: info.ModTime().UnixNano(), Dataless: fsinfo.IsDataless(info)}
	switch mode := info.Mode(); {
	case mode&fs.ModeSymlink != 0:
		obs.Type = manifest.TypeLink
		if obs.Target, err = os.Readlink(abs); err != nil {
			return Observed{}, fmt.Errorf("read link %s: %w", abs, err)
		}
	case mode.IsDir():
		obs.Type = manifest.TypeDir
	default:
		obs.Type = manifest.TypeFile
	}
	needsHash := rec.Hash != "" && obs.Type == manifest.TypeFile && !obs.Dataless && obs.MTime <= preparedAt && obs.Size == rec.Size
	if !needsHash {
		return obs, nil
	}
	obs.Hash, err = fsinfo.HashFile(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Observed{}, nil
	case errors.Is(err, fs.ErrPermission):
		return Observed{Exists: true, Unreadable: true}, nil
	case err != nil:
		return Observed{}, fmt.Errorf("fingerprint %s: %w", abs, err)
	}
	return obs, nil
}

type group struct {
	name     string
	critical bool
	counts   map[Outcome]int
	paths    []string
	extra    int
}

type tally struct {
	groups map[string]*group
	order  []string
}

func newTally() *tally { return &tally{groups: map[string]*group{}} }

func (t *tally) add(rec manifest.Entry, o Outcome) {
	name := groupOf(rec.Path, rec.Type == manifest.TypeDir || rec.Type == manifest.TypeUnreadable)
	g := t.groups[name]
	if g == nil {
		g = &group{name: name, critical: rec.Area == manifest.AreaCritical, counts: map[Outcome]int{}}
		t.groups[name] = g
		t.order = append(t.order, name)
	}
	g.counts[o]++
	if s := statusOf(o); s == module.StatusFail || s == module.StatusWarn {
		if len(g.paths) < maxPathsPerGroup {
			g.paths = append(g.paths, rec.Path)
		} else {
			g.extra++
		}
	}
}

func (t *tally) checks() []module.Check {
	out := make([]module.Check, 0, len(t.order))
	for _, name := range t.order {
		g := t.groups[name]
		out = append(out, module.Check{
			Module:   Name,
			ID:       Name + ":" + name,
			Status:   g.status(),
			Critical: g.critical,
			Title:    name,
			Detail:   g.detail(),
			Paths:    g.paths,
		})
	}
	return out
}

// status follows spec 8.4: fail beats warn, and info outcomes leave a
// group at pass.
func (g *group) status() module.Status {
	status := module.StatusPass
	for o, n := range g.counts {
		if n == 0 {
			continue
		}
		switch statusOf(o) {
		case module.StatusFail:
			return module.StatusFail
		case module.StatusWarn:
			status = module.StatusWarn
		}
	}
	return status
}

var outcomeLabels = []struct {
	outcome Outcome
	label   string
}{
	{Match, "match"},
	{ChangedAfterPrepare, "changed after prepare"},
	{MissingVolatile, "missing (expected)"},
	{CloudOnly, "cloud-only"},
	{NotChecked, "not checked (unreadable)"},
	{MissingAppState, "missing"},
	{MissingCritical, "missing"},
	{Corrupt, "corrupted"},
}

func (g *group) detail() string {
	var parts []string
	for _, l := range outcomeLabels {
		if n := g.counts[l.outcome]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, l.label))
		}
	}
	if g.extra > 0 {
		parts = append(parts, fmt.Sprintf("%d more paths not listed", g.extra))
	}
	return strings.Join(parts, ", ")
}

// groupOf names the check a path belongs to: its top-level folder, the
// first two levels inside Library, or HomeGroup for files directly in the
// home folder.
func groupOf(rel string, isDir bool) string {
	parts := strings.Split(rel, "/")
	if !isDir {
		parts = parts[:len(parts)-1]
	}
	switch {
	case len(parts) == 0:
		return HomeGroup
	case parts[0] == "Library" && len(parts) >= 2:
		return "Library/" + parts[1]
	}
	return parts[0]
}
```

`internal/modules/files/scan.go`:
```go
package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/momentmaker/mtm/internal/module"
)

// protectedDirs can only be read with Full Disk Access, so they show
// whether the terminal has it.
var protectedDirs = []string{"Library/Mail", "Library/Messages", "Library/Safari", "Library/Containers/com.apple.Safari"}

func (Module) Scan(env module.Env) ([]module.Finding, error) {
	findings := []module.Finding{{
		Module:   Name,
		ID:       "home",
		Severity: module.SeverityInfo,
		Handler:  module.ByMigrationAssistant,
		Title:    "Home folder: files, dotfiles, and projects",
		Detail:   "Migration Assistant copies these. mtm records them in prepare and checks them in verify.",
	}}
	var blocked []string
	for _, rel := range protectedDirs {
		if _, err := os.ReadDir(filepath.Join(env.Home, filepath.FromSlash(rel))); errors.Is(err, fs.ErrPermission) {
			blocked = append(blocked, "~/"+rel)
		}
	}
	if len(blocked) > 0 {
		findings = append(findings, module.Finding{
			Module:   Name,
			ID:       "full-disk-access",
			Severity: module.SeverityWarn,
			Handler:  module.Manual,
			Title:    "Your terminal has no Full Disk Access, so some folders can't be checked",
			Detail:   "Unreadable: " + strings.Join(blocked, ", "),
			Fix:      "Optional: System Settings > Privacy & Security > Full Disk Access, add your terminal app, then re-run. mtm cleanup reminds you to remove it afterward.",
		})
	}
	return findings, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -race ./internal/modules/files/...`
Expected: PASS.

- [ ] **Step 6: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/modules/files internal/testutil
git commit -m "Add files module: record, verify, and scan

Verify streams the record, groups results by folder so a real home folder
produces a readable report, and is proven against a ditto copy at a
different home path, the same shape as a Migration Assistant transfer.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Parsing git output

**Files:**
- Create: `internal/modules/git/parse.go`, `internal/modules/git/parse_test.go`

**Interfaces:**
- Consumes: `pathset` (Task 3).
- Produces (package `git`):
  - `parseRefs([]byte) map[string]string`, which skips `refs/remotes/` and `refs/stash`;
  - `parseLines([]byte) []string`;
  - `parseCount([]byte) (int, error)`;
  - `parseStatus([]byte) (records, untracked, ignored []string)` for NUL-separated porcelain v2 output;
  - `parseMissing([]byte) []string`;
  - `importantIgnored(ignored []string) []string`;
  - `plural(n int, one, many string) string`.

- [ ] **Step 1: Write the failing tests**

`internal/modules/git/parse_test.go`:
```go
package git

import (
	"maps"
	"slices"
	"testing"
)

func TestParseRefs(t *testing.T) {
	out := []byte("refs/heads/main aaa\nrefs/heads/feature bbb\nrefs/remotes/origin/main ccc\nrefs/stash ddd\nrefs/tags/v1 eee\n")
	want := map[string]string{"refs/heads/main": "aaa", "refs/heads/feature": "bbb", "refs/tags/v1": "eee"}
	if got := parseRefs(out); !maps.Equal(got, want) {
		t.Errorf("parseRefs = %v, want %v", got, want)
	}
	if got := parseRefs(nil); len(got) != 0 {
		t.Errorf("parseRefs(nil) = %v, want empty", got)
	}
}

func TestParseCount(t *testing.T) {
	if n, err := parseCount([]byte("3\n")); err != nil || n != 3 {
		t.Errorf("parseCount = %d, %v", n, err)
	}
	if _, err := parseCount([]byte("x")); err == nil {
		t.Error("parseCount accepted garbage")
	}
}

func TestParseStatus(t *testing.T) {
	out := []byte("# branch.oid abc\x00# branch.head main\x00" +
		"1 .M N... 100644 100644 100644 h1 h2 src/app.go\x00" +
		"2 R. N... 100644 100644 100644 h1 h2 R100 new name.txt\x00old name.txt\x00" +
		"? notes\nwith newline.txt\x00? -rf.txt\x00! .env\x00! build/\x00")
	records, untracked, ignored := parseStatus(out)
	wantRecords := []string{
		"# branch.oid abc",
		"# branch.head main",
		"1 .M N... 100644 100644 100644 h1 h2 src/app.go",
		"2 R. N... 100644 100644 100644 h1 h2 R100 new name.txt\x00old name.txt",
		"? notes\nwith newline.txt",
		"? -rf.txt",
	}
	if !slices.Equal(records, wantRecords) {
		t.Errorf("records = %q\nwant %q", records, wantRecords)
	}
	if want := []string{"notes\nwith newline.txt", "-rf.txt"}; !slices.Equal(untracked, want) {
		t.Errorf("untracked = %q, want %q", untracked, want)
	}
	if want := []string{".env", "build/"}; !slices.Equal(ignored, want) {
		t.Errorf("ignored = %q, want %q", ignored, want)
	}
}

func TestParseMissing(t *testing.T) {
	out := []byte("aaa commit 230\nbbb missing\nccc tag 140\n")
	if got := parseMissing(out); !slices.Equal(got, []string{"bbb"}) {
		t.Errorf("parseMissing = %q, want [bbb]", got)
	}
}

func TestImportantIgnored(t *testing.T) {
	in := []string{".env", "config/.env.local", "build/", "certs/dev.pem", "id.key", "out.bin", "app.local.json"}
	want := []string{".env", "config/.env.local", "certs/dev.pem", "id.key", "app.local.json"}
	if got := importantIgnored(in); !slices.Equal(got, want) {
		t.Errorf("importantIgnored = %q, want %q", got, want)
	}
}

func TestPlural(t *testing.T) {
	if got := plural(1, "stash", "stashes"); got != "1 stash" {
		t.Errorf("plural(1) = %q", got)
	}
	if got := plural(3, "stash", "stashes"); got != "3 stashes" {
		t.Errorf("plural(3) = %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/git/...`
Expected: FAIL with `undefined: parseRefs`, and similar.

- [ ] **Step 3: Implement the parsers**

`internal/modules/git/parse.go`:
```go
package git

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/momentmaker/mtm/internal/pathset"
)

// important matches ignored files worth fingerprinting: secrets and local
// configuration that exist nowhere but this Mac.
var important = pathset.MustNew(".env*", "*.pem", "*.key", "*.p12", "*.local.*")

// parseRefs reads `git for-each-ref --format=%(refname) %(objectname)`.
// Remote-tracking refs are skipped because their commits live on the
// remote. The stash is read separately with its full list.
func parseRefs(out []byte) map[string]string {
	refs := map[string]string{}
	for _, line := range parseLines(out) {
		name, sha, ok := strings.Cut(line, " ")
		if !ok || strings.HasPrefix(name, "refs/remotes/") || name == "refs/stash" {
			continue
		}
		refs[name] = sha
	}
	return refs
}

func parseLines(out []byte) []string {
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func parseCount(out []byte) (int, error) {
	s := strings.TrimSpace(string(out))
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("parse commit count %q: %w", s, err)
	}
	return n, nil
}

// parseStatus splits `git status --porcelain=v2 -z` output into records
// for comparison, untracked paths, and ignored paths. A rename record keeps
// its original path, which -z sends as the next field.
func parseStatus(out []byte) (records, untracked, ignored []string) {
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		switch f[0] {
		case '?':
			untracked = append(untracked, strings.TrimPrefix(f, "? "))
			records = append(records, f)
		case '!':
			ignored = append(ignored, strings.TrimPrefix(f, "! "))
		case '2':
			if i+1 < len(fields) {
				f += "\x00" + fields[i+1]
				i++
			}
			records = append(records, f)
		default:
			records = append(records, f)
		}
	}
	return records, untracked, ignored
}

// parseMissing returns the object names `git cat-file --batch-check` reported missing.
func parseMissing(out []byte) []string {
	var missing []string
	for _, line := range parseLines(out) {
		if name, ok := strings.CutSuffix(line, " missing"); ok {
			missing = append(missing, name)
		}
	}
	return missing
}

func importantIgnored(ignored []string) []string {
	var out []string
	for _, p := range ignored {
		if !strings.HasSuffix(p, "/") && important.Match(p) {
			out = append(out, p)
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/modules/git/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/modules/git
git commit -m "Parse git output for the git module

Status is read NUL-separated so file names with newlines or leading
dashes parse exactly, and remote-tracking refs are dropped because their
commits already live on the remote.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Git module (discover, scan, prepare, verify)

**Files:**
- Create: `internal/modules/git/discover.go`, `internal/modules/git/repo.go`, `internal/modules/git/module.go`, `internal/modules/git/verify.go`, `internal/modules/git/module_test.go`
- Modify: `internal/testutil/testutil.go` (add `IsolateGit`, `Git`)

**Interfaces:**
- Consumes: the parsers and `plural` (Task 9); `fsinfo` (Task 7); `pathset` (Task 3); `module`, `stage`, `manifest`, `runner` (Tasks 1–5).
- Produces:
  - `git.Name = "git"` and `git.New() Module` (`MaxDepth` 6). `git.Module` implements `module.Module`.
  - `git.Inventory{Repos []Repo}`.
  - `git.Repo{Path, Head, HeadCommit string; Refs map[string]string; Stash, Status []string; Unpushed int; Untracked, Important []string; Fingerprints map[string]string}`.
  - Check IDs are `"git:" + repo path`. Check titles are `"~/" + repo path`.
  - `testutil.IsolateGit(t)` and `testutil.Git(t, dir string, args ...string) string`.

- [ ] **Step 1: Add the git helpers to testutil**

Add to `internal/testutil/testutil.go`. Merge `strings` into the imports.
```go
// IsolateGit makes git ignore the developer's own configuration (signing,
// hooks, templates) and gives commits a fixed identity.
func IsolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "mtm test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "mtm test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

// Git runs git in dir and returns its trimmed output, failing the test on error.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
```

- [ ] **Step 2: Write the failing tests**

`internal/modules/git/module_test.go`:
```go
package git

import (
	"context"
	"maps"
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

func testEnv(home string, m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: runner.Exec{}, Home: home, Manifest: m}
}

// seedRepo makes ~/GitHub/app with two branches, an annotated tag, a
// stash, an untracked file, an ignored .env, and an ignored build folder.
func seedRepo(t *testing.T, home string) string {
	t.Helper()
	testutil.IsolateGit(t)
	repo := filepath.Join(home, "GitHub", "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "init", "-q", "-b", "main")
	testutil.WriteFile(t, filepath.Join(repo, "README.md"), "hello\n")
	testutil.WriteFile(t, filepath.Join(repo, ".gitignore"), ".env\nbuild/\n")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-q", "-m", "first")
	testutil.Git(t, repo, "tag", "-a", "v1", "-m", "v1")
	testutil.Git(t, repo, "switch", "-q", "-c", "feature")
	testutil.WriteFile(t, filepath.Join(repo, "feature.txt"), "f\n")
	testutil.Git(t, repo, "add", "feature.txt")
	testutil.Git(t, repo, "commit", "-q", "-m", "feature")
	testutil.Git(t, repo, "switch", "-q", "main")
	testutil.WriteFile(t, filepath.Join(repo, "README.md"), "hello, stash\n")
	testutil.Git(t, repo, "stash", "push", "-q", "-m", "wip")
	testutil.WriteFile(t, filepath.Join(repo, "notes.txt"), "todo\n")
	testutil.WriteFile(t, filepath.Join(repo, ".env"), "SECRET=1\n")
	testutil.WriteFile(t, filepath.Join(repo, "build/out.bin"), "bin\n")
	return repo
}

func prepare(t *testing.T, home string) *manifest.Manifest {
	t.Helper()
	preparedAt := time.Now()
	res, err := New().Prepare(testEnv(home, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) > 0 {
		t.Fatalf("prepare problems: %+v", res.Problems)
	}
	m := manifest.New("test")
	if err := m.SetModule(Name, preparedAt, res.Inventory); err != nil {
		t.Fatal(err)
	}
	return m
}

func verify(t *testing.T, home string, m *manifest.Manifest) map[string]module.Check {
	t.Helper()
	checks, err := New().Verify(testEnv(home, m), nil)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]module.Check{}
	for _, c := range checks {
		byTitle[c.Title] = c
	}
	return byTitle
}

func copyHome(t *testing.T, home string) string {
	t.Helper()
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, home, newHome)
	return newHome
}

func TestDiscoverFindsReposAndSkipsNoise(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{
		"GitHub/app/.git",
		"GitHub/app/node_modules/pkg/.git",
		"Library/Application Support/x/.git",
		".cargo/registry/index/x/.git",
		"deep/1/2/3/4/5/.git",
		"deep/1/2/3/4/5/6/.git",
	} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.Symlink(filepath.Join(home, "GitHub/app"), filepath.Join(home, "applink"))
	got, err := discover(context.Background(), home, 6)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"GitHub/app", "deep/1/2/3/4/5"}; !slices.Equal(got, want) {
		t.Errorf("discover = %q, want %q", got, want)
	}
}

func TestScanWarnsAboutLocalOnlyWork(t *testing.T) {
	home := t.TempDir()
	seedRepo(t, home)
	findings, err := New().Scan(testEnv(home, nil))
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(findings, func(f module.Finding) bool { return f.ID == "local-only:GitHub/app" })
	if i < 0 {
		t.Fatalf("findings = %+v, want local-only warning for GitHub/app", findings)
	}
	for _, want := range []string{"2 commits not on any remote", "1 stash", "1 untracked file", "1 ignored file"} {
		if !strings.Contains(findings[i].Detail, want) {
			t.Errorf("detail %q missing %q", findings[i].Detail, want)
		}
	}
}

func TestPrepareRecordsRepoState(t *testing.T) {
	home := t.TempDir()
	seedRepo(t, home)
	res, err := New().Prepare(testEnv(home, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	if len(inv.Repos) != 1 {
		t.Fatalf("repos = %d, want 1", len(inv.Repos))
	}
	r := inv.Repos[0]
	if r.Head != "refs/heads/main" || len(r.Stash) != 1 {
		t.Errorf("head %q stash %d", r.Head, len(r.Stash))
	}
	for _, ref := range []string{"refs/heads/main", "refs/heads/feature", "refs/tags/v1"} {
		if r.Refs[ref] == "" {
			t.Errorf("ref %s not recorded", ref)
		}
	}
	keys := slices.Sorted(maps.Keys(r.Fingerprints))
	if want := []string{".env", "notes.txt"}; !slices.Equal(keys, want) {
		t.Errorf("fingerprinted = %q, want %q", keys, want)
	}
}

func TestVerifyPassesOnIdenticalCopy(t *testing.T) {
	home := t.TempDir()
	seedRepo(t, home)
	m := prepare(t, home)
	c := verify(t, copyHome(t, home), m)["~/GitHub/app"]
	if c.Status != module.StatusPass || !c.Critical {
		t.Errorf("check = %+v, want critical pass", c)
	}
}

func TestVerifyAllowsWorkAfterPrepare(t *testing.T) {
	home := t.TempDir()
	seedRepo(t, home)
	m := prepare(t, home)
	newHome := copyHome(t, home)
	repo := filepath.Join(newHome, "GitHub", "app")
	testutil.WriteFile(t, filepath.Join(repo, "new.txt"), "new\n")
	testutil.Git(t, repo, "add", "new.txt")
	testutil.Git(t, repo, "commit", "-q", "-m", "on the new Mac")
	envPath := filepath.Join(repo, ".env")
	testutil.WriteFile(t, envPath, "SECRET=2\n")
	later := time.Now().Add(time.Minute)
	os.Chtimes(envPath, later, later)

	c := verify(t, newHome, m)["~/GitHub/app"]
	if c.Status != module.StatusPass {
		t.Fatalf("check = %+v, want pass", c)
	}
	for _, want := range []string{"refs moved since prepare: refs/heads/main", "changed since prepare: .env"} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("detail %q missing %q", c.Detail, want)
		}
	}
}

func TestVerifyFailsWhenImportantFileCorrupted(t *testing.T) {
	home := t.TempDir()
	seedRepo(t, home)
	m := prepare(t, home)
	newHome := copyHome(t, home)
	envPath := filepath.Join(newHome, "GitHub", "app", ".env")
	testutil.WriteFile(t, envPath, "SECRET=9\n")
	earlier := time.Now().Add(-time.Hour)
	os.Chtimes(envPath, earlier, earlier)
	c := verify(t, newHome, m)["~/GitHub/app"]
	if c.Status != module.StatusFail || !strings.Contains(c.Detail, "content differs: .env") {
		t.Errorf("check = %+v, want fail naming .env", c)
	}
}

func TestVerifyFailsWhenCommitMissing(t *testing.T) {
	home := t.TempDir()
	seedRepo(t, home)
	m := prepare(t, home)
	newHome := copyHome(t, home)
	repo := filepath.Join(newHome, "GitHub", "app")
	sha := testutil.Git(t, repo, "rev-parse", "feature")
	if err := os.Remove(filepath.Join(repo, ".git", "objects", sha[:2], sha[2:])); err != nil {
		t.Fatal(err)
	}
	if c := verify(t, newHome, m)["~/GitHub/app"]; c.Status != module.StatusFail {
		t.Errorf("check = %+v, want fail", c)
	}
}

func TestVerifyFailsWhenRepoMissing(t *testing.T) {
	home := t.TempDir()
	seedRepo(t, home)
	m := prepare(t, home)
	newHome := copyHome(t, home)
	os.RemoveAll(filepath.Join(newHome, "GitHub", "app"))
	c := verify(t, newHome, m)["~/GitHub/app"]
	if c.Status != module.StatusFail || c.Detail != "repository is missing" {
		t.Errorf("check = %+v, want fail: repository is missing", c)
	}
}

func TestUnusualRepoStates(t *testing.T) {
	home := t.TempDir()
	repo := seedRepo(t, home)
	testutil.WriteFile(t, filepath.Join(repo, "odd\nname.txt"), "x\n")
	testutil.WriteFile(t, filepath.Join(repo, "-rf.txt"), "y\n")

	empty := filepath.Join(home, "GitHub", "empty")
	os.MkdirAll(empty, 0o755)
	testutil.Git(t, empty, "init", "-q", "-b", "main")

	detached := filepath.Join(home, "GitHub", "detached")
	os.MkdirAll(detached, 0o755)
	testutil.Git(t, detached, "init", "-q", "-b", "main")
	testutil.WriteFile(t, filepath.Join(detached, "a.txt"), "a\n")
	testutil.Git(t, detached, "add", "a.txt")
	testutil.Git(t, detached, "commit", "-q", "-m", "a")
	testutil.Git(t, detached, "switch", "-q", "--detach", "HEAD")

	testutil.Git(t, repo, "worktree", "add", "-q", filepath.Join(home, "GitHub", "app-wt"), "feature")

	m := prepare(t, home)
	checks := verify(t, home, m)
	for _, title := range []string{"~/GitHub/app", "~/GitHub/empty", "~/GitHub/detached", "~/GitHub/app-wt"} {
		c, ok := checks[title]
		if !ok {
			t.Errorf("no check for %s", title)
			continue
		}
		if c.Status != module.StatusPass {
			t.Errorf("%s: %s (%s)", title, c.Status, c.Detail)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/modules/git/...`
Expected: FAIL with `undefined: New`, `undefined: discover`, `undefined: Inventory`.

- [ ] **Step 4: Implement the git module**

`internal/modules/git/discover.go`:
```go
package git

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/pathset"
)

// skipDirs holds folders that never contain the user's own repos: app
// state, caches, and package-manager checkouts.
var skipDirs = pathset.MustNew(
	"Library/", ".Trash/", ".mtm/", ".cache/", ".npm/", ".rustup/",
	".cargo/registry/", ".cargo/git/", "go/pkg/",
	"node_modules", ".git",
)

// discover returns home-relative paths of git repos, in sorted walk order,
// down to maxDepth folders below home. Symlinks are not followed.
func discover(ctx context.Context, home string, maxDepth int) ([]string, error) {
	var repos []string
	var visit func(rel string, depth int) error
	visit = func(rel string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		abs := filepath.Join(home, filepath.FromSlash(rel))
		entries, err := os.ReadDir(abs)
		switch {
		case errors.Is(err, fs.ErrPermission), errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return fmt.Errorf("read %s: %w", abs, err)
		}
		if slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == ".git" }) {
			repos = append(repos, cmp.Or(rel, "."))
		}
		if depth == maxDepth {
			return nil
		}
		for _, e := range entries {
			child := path.Join(rel, e.Name())
			if !e.IsDir() || skipDirs.Match(child) {
				continue
			}
			info, err := e.Info()
			if err != nil || fsinfo.IsDataless(info) {
				continue
			}
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit("", 0); err != nil {
		return nil, err
	}
	return repos, nil
}
```

`internal/modules/git/repo.go`:
```go
package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
)

// Repo is what prepare records about one repository. Fingerprints maps
// repo-relative paths of untracked and important ignored files to their
// content fingerprints.
type Repo struct {
	Path         string            `json:"path"`
	Head         string            `json:"head,omitempty"`
	HeadCommit   string            `json:"head_commit,omitempty"`
	Refs         map[string]string `json:"refs,omitempty"`
	Stash        []string          `json:"stash,omitempty"`
	Status       []string          `json:"status,omitempty"`
	Unpushed     int               `json:"unpushed"`
	Untracked    []string          `json:"untracked,omitempty"`
	Important    []string          `json:"important,omitempty"`
	Fingerprints map[string]string `json:"fingerprints,omitempty"`
}

type gitCLI struct {
	ctx context.Context
	run runner.Runner
	dir string
}

func (g gitCLI) command(args ...string) runner.Command {
	base := []string{"-C", g.dir, "--no-optional-locks", "-c", "core.fsmonitor=false"}
	return runner.Command{Name: "git", Args: append(base, args...)}
}

func (g gitCLI) output(args ...string) ([]byte, error) {
	out, err := g.run.Run(g.ctx, g.command(args...))
	return out.Stdout, err
}

// optional runs a git command whose exit status 1 means "none".
func (g gitCLI) optional(args ...string) (string, error) {
	out, err := g.output(args...)
	if code, ok := runner.ExitCode(err); ok && code == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (g gitCLI) missingObjects(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	cmd := g.command("cat-file", "--batch-check")
	cmd.Stdin = strings.Join(names, "\n") + "\n"
	out, err := g.run.Run(g.ctx, cmd)
	if err != nil {
		return nil, err
	}
	return parseMissing(out.Stdout), nil
}

func collect(env module.Env, rel string, withFingerprints bool) (Repo, error) {
	abs := filepath.Join(env.Home, filepath.FromSlash(rel))
	g := gitCLI{ctx: env.Ctx, run: env.Run, dir: abs}
	r := Repo{Path: rel}
	var err error
	if r.Head, err = g.optional("symbolic-ref", "-q", "HEAD"); err != nil {
		return r, err
	}
	if r.HeadCommit, err = g.optional("rev-parse", "-q", "--verify", "HEAD"); err != nil {
		return r, err
	}
	refs, err := g.output("for-each-ref", "--format=%(refname) %(objectname)")
	if err != nil {
		return r, err
	}
	r.Refs = parseRefs(refs)
	stash, err := g.output("stash", "list", "--format=%H")
	if err != nil {
		return r, err
	}
	r.Stash = parseLines(stash)
	if r.HeadCommit != "" || len(r.Refs) > 0 {
		count, err := g.output("rev-list", "--count", "--branches", "--not", "--remotes")
		if err != nil {
			return r, err
		}
		if r.Unpushed, err = parseCount(count); err != nil {
			return r, err
		}
	}
	status, err := g.output("status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all", "--ignored=matching")
	if err != nil {
		return r, err
	}
	var ignored []string
	r.Status, r.Untracked, ignored = parseStatus(status)
	r.Important = importantIgnored(ignored)
	if withFingerprints {
		paths := append(append([]string(nil), r.Untracked...), r.Important...)
		if r.Fingerprints, err = fingerprint(abs, paths); err != nil {
			return r, err
		}
	}
	return r, nil
}

func fingerprint(repoAbs string, rels []string) (map[string]string, error) {
	out := map[string]string{}
	for _, rel := range rels {
		abs := filepath.Join(repoAbs, filepath.FromSlash(rel))
		info, err := os.Lstat(abs)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", abs, err)
		}
		if !info.Mode().IsRegular() || fsinfo.IsDataless(info) {
			continue
		}
		hash, err := fsinfo.HashFile(abs)
		if err != nil {
			return nil, fmt.Errorf("fingerprint %s: %w", abs, err)
		}
		out[rel] = hash
	}
	return out, nil
}

// commits returns every object the old Mac's repo pointed at: HEAD, local
// refs, and stash entries.
func (r Repo) commits() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(r.HeadCommit)
	for _, name := range slices.Sorted(maps.Keys(r.Refs)) {
		add(r.Refs[name])
	}
	for _, s := range r.Stash {
		add(s)
	}
	return out
}

func (r Repo) risks() []string {
	var out []string
	if r.Unpushed > 0 {
		out = append(out, plural(r.Unpushed, "commit", "commits")+" not on any remote")
	}
	if n := len(r.Stash); n > 0 {
		out = append(out, plural(n, "stash", "stashes"))
	}
	if n := len(r.Untracked); n > 0 {
		out = append(out, plural(n, "untracked file", "untracked files"))
	}
	if n := len(r.Important); n > 0 {
		out = append(out, plural(n, "ignored file that looks like a secret or local config", "ignored files that look like secrets or local config"))
	}
	return out
}

func displayPath(rel string) string {
	if rel == "." {
		return "~"
	}
	return "~/" + rel
}
```

`internal/modules/git/module.go`:
```go
// Package git records repositories on the old Mac and checks that every
// recorded commit and untracked file arrived on the new Mac.
package git

import (
	"strings"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
)

const (
	Name            = "git"
	defaultMaxDepth = 6
)

type Inventory struct {
	Repos []Repo `json:"repos"`
}

type Module struct {
	MaxDepth int
}

func New() Module { return Module{MaxDepth: defaultMaxDepth} }

func (Module) Name() string { return Name }

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	repos, err := discover(env.Ctx, env.Home, m.MaxDepth)
	if err != nil {
		return nil, err
	}
	findings := []module.Finding{{
		Module:   Name,
		ID:       "repos",
		Severity: module.SeverityInfo,
		Handler:  module.ByMigrationAssistant,
		Title:    plural(len(repos), "git repository", "git repositories") + " found",
		Detail:   "Migration Assistant copies them. mtm checks that every recorded commit and untracked file arrived.",
	}}
	for _, rel := range repos {
		if err := env.Ctx.Err(); err != nil {
			return nil, err
		}
		r, err := collect(env, rel, false)
		if err != nil {
			findings = append(findings, unreadableRepo(rel, err))
			continue
		}
		if risks := r.risks(); len(risks) > 0 {
			findings = append(findings, module.Finding{
				Module:   Name,
				ID:       "local-only:" + rel,
				Severity: module.SeverityWarn,
				Handler:  module.ByMigrationAssistant,
				Title:    displayPath(rel) + ": work that exists only on this Mac",
				Detail:   strings.Join(risks, ", "),
				Fix:      "Push it if you can. Either way, don't wipe this Mac until mtm verify says SAFE TO WIPE.",
			})
		}
	}
	return findings, nil
}

func (m Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	repos, err := discover(env.Ctx, env.Home, m.MaxDepth)
	if err != nil {
		return module.Result{}, err
	}
	var inv Inventory
	var res module.Result
	for _, rel := range repos {
		if err := env.Ctx.Err(); err != nil {
			return module.Result{}, err
		}
		r, err := collect(env, rel, true)
		if err != nil {
			res.Problems = append(res.Problems, unreadableRepo(rel, err))
			continue
		}
		inv.Repos = append(inv.Repos, r)
	}
	res.Inventory = inv
	res.Done = []string{"recorded " + plural(len(inv.Repos), "repository", "repositories")}
	return res, nil
}

// Finish has nothing to restore: Migration Assistant copies the repos.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}

func unreadableRepo(rel string, err error) module.Finding {
	return module.Finding{
		Module:   Name,
		ID:       "unreadable:" + rel,
		Severity: module.SeverityWarn,
		Handler:  module.Manual,
		Title:    displayPath(rel) + ": git could not read this repository, so mtm won't check it",
		Detail:   err.Error(),
		Fix:      "Run git status in it to see the problem, fix it, then re-run.",
	}
}
```

`internal/modules/git/verify.go`:
```go
package git

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
)

func (Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var inv Inventory
	preparedAt, err := module.LoadInventory(env, Name, &inv)
	if err != nil {
		return nil, err
	}
	checks := make([]module.Check, 0, len(inv.Repos))
	for _, old := range inv.Repos {
		if err := env.Ctx.Err(); err != nil {
			return nil, err
		}
		checks = append(checks, verifyRepo(env, old, preparedAt.UnixNano()))
	}
	return checks, nil
}

// verifyRepo fails only on lost data: a missing repo, missing recorded
// commits, or untracked and important files that are missing or changed
// without a newer time. Work done after prepare is noted, not failed.
func verifyRepo(env module.Env, old Repo, preparedAt int64) module.Check {
	check := module.Check{Module: Name, ID: Name + ":" + old.Path, Status: module.StatusPass, Critical: true, Title: displayPath(old.Path)}
	abs := filepath.Join(env.Home, filepath.FromSlash(old.Path))
	if _, err := os.Lstat(filepath.Join(abs, ".git")); err != nil {
		check.Status = module.StatusFail
		check.Detail = "repository is missing"
		return check
	}
	cur, err := collect(env, old.Path, false)
	if err != nil {
		check.Status = module.StatusFail
		check.Detail = "git could not read the repository: " + err.Error()
		return check
	}
	g := gitCLI{ctx: env.Ctx, run: env.Run, dir: abs}
	var problems, notes []string
	switch missing, err := g.missingObjects(old.commits()); {
	case err != nil:
		problems = append(problems, "could not check recorded commits: "+err.Error())
	case len(missing) > 0:
		problems = append(problems, plural(len(missing), "recorded commit is", "recorded commits are")+" missing")
	}
	p, n := compareFingerprints(abs, old.Fingerprints, preparedAt)
	problems = append(problems, p...)
	notes = append(notes, n...)
	if moved := movedRefs(old.Refs, cur.Refs); len(moved) > 0 {
		notes = append(notes, "refs moved since prepare: "+strings.Join(moved, ", "))
	}
	if !slices.Equal(old.Status, cur.Status) {
		notes = append(notes, "working tree status changed since prepare")
	}
	if env.Deep {
		if _, err := g.output("fsck", "--connectivity-only", "--no-progress"); err != nil {
			problems = append(problems, "git fsck failed: "+err.Error())
		}
	}
	if len(problems) > 0 {
		check.Status = module.StatusFail
	}
	check.Detail = strings.Join(append(problems, notes...), "; ")
	return check
}

func compareFingerprints(repoAbs string, recorded map[string]string, preparedAt int64) (problems, notes []string) {
	for _, rel := range slices.Sorted(maps.Keys(recorded)) {
		abs := filepath.Join(repoAbs, filepath.FromSlash(rel))
		info, err := os.Lstat(abs)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			problems = append(problems, "missing: "+rel)
			continue
		case err != nil:
			problems = append(problems, "unreadable: "+rel)
			continue
		case info.ModTime().UnixNano() > preparedAt:
			notes = append(notes, "changed since prepare: "+rel)
			continue
		}
		got, err := fsinfo.HashFile(abs)
		switch {
		case err != nil:
			problems = append(problems, "unreadable: "+rel)
		case got != recorded[rel]:
			problems = append(problems, "content differs: "+rel)
		}
	}
	return problems, notes
}

func movedRefs(old, cur map[string]string) []string {
	var moved []string
	for _, name := range slices.Sorted(maps.Keys(old)) {
		if cur[name] != old[name] {
			moved = append(moved, name)
		}
	}
	return moved
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/modules/git/...`
Expected: PASS.

- [ ] **Step 6: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/modules/git internal/testutil
git commit -m "Add git module: record repos, verify commits arrived

Verify fails only on lost data (missing repo, missing recorded commits,
missing or silently changed untracked and secret files), so fetching or
committing on the new Mac never produces a false NOT SAFE.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Preflight checks

**Files:**
- Create: `internal/preflight/preflight.go`, `internal/preflight/preflight_test.go`
- Modify: `internal/testutil/testutil.go` (add `MacResponses`)

**Interfaces:**
- Consumes: `runner` (Task 1); `module.Finding` (Task 5).
- Produces:
  - `preflight.Module = "preflight"`, `preflight.MinMacOSMajor = 26`.
  - `preflight.Check(ctx, runner.Runner) []module.Finding`: blockers with IDs `apple-silicon`, `macos-version`, `filevault`.
  - `preflight.Info{MacOSVersion, MacOSBuild, MachineID string}`; `preflight.Gather(ctx, runner.Runner) (Info, error)`. `MachineID` is `"sha256:<hex>"` of the hardware UUID.
  - `testutil.MacResponses(uuid string) map[string]runner.Response`: canned output for a supported Mac with FileVault on.

- [ ] **Step 1: Add the fake Mac responses to testutil**

Add to `internal/testutil/testutil.go`. Merge the runner import.
```go
// MacResponses are fake answers from a supported Mac (Apple Silicon,
// macOS 26.7.1, FileVault on) whose hardware UUID is uuid.
func MacResponses(uuid string) map[string]runner.Response {
	return map[string]runner.Response{
		"/usr/sbin/sysctl -n hw.optional.arm64":          {Stdout: "1\n"},
		"/usr/bin/sw_vers -productVersion":               {Stdout: "26.7.1\n"},
		"/usr/bin/sw_vers -buildVersion":                 {Stdout: "25G241\n"},
		"/usr/bin/fdesetup status":                       {Stdout: "FileVault is On.\n"},
		"/usr/sbin/ioreg -rd1 -c IOPlatformExpertDevice": {Stdout: `    "IOPlatformUUID" = "` + uuid + `"` + "\n"},
	}
}
```
Import line to add: `"github.com/momentmaker/mtm/internal/runner"`.

- [ ] **Step 2: Write the failing tests**

`internal/preflight/preflight_test.go`:
```go
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
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/preflight/...`
Expected: FAIL with `undefined: Check`, `undefined: Gather`.

- [ ] **Step 4: Implement preflight**

`internal/preflight/preflight.go`:
```go
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/preflight/... ./internal/testutil/...`
Expected: PASS.

- [ ] **Step 6: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/preflight internal/testutil
git commit -m "Add preflight gate and hashed machine id

FileVault is a hard requirement because the record and exports hold
sensitive data. The machine id lets mtm refuse to verify on, or overwrite
a record from, the wrong Mac, without storing the raw hardware UUID.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Report rendering

**Files:**
- Create: `internal/report/report.go`, `internal/report/report_test.go`

**Interfaces:**
- Consumes: `module.Finding`, `module.Check` (Task 5); `verdict.Verdict` (Task 5); `redact.Text` (Task 1).
- Produces:
  - `report.Findings(w io.Writer, findings []module.Finding)`
  - `report.Prepare(w io.Writer, ready bool, done []string, problems []module.Finding)`
  - `report.Verify(w io.Writer, checks []module.Check, v verdict.Verdict, details bool)`
  - `report.JSON(w io.Writer, v any) error`
  - Every string passes through scrubbing: `sha256:<hex>` becomes `[fingerprint]`, and `redact.Text` is applied.

- [ ] **Step 1: Write the failing tests**

`internal/report/report_test.go`:
```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/report/...`
Expected: FAIL with `undefined: Verify`, and similar.

- [ ] **Step 3: Implement the report**

`internal/report/report.go`:
```go
// Package report renders mtm's results for people and for scripts. Every
// string passes through clean, so fingerprints and credentials never reach
// the terminal or a pasted bug report.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/redact"
	"github.com/momentmaker/mtm/internal/verdict"
)

const shownPaths = 5

var fingerprintPattern = regexp.MustCompile(`sha256:[0-9a-f]+`)

func clean(s string) string {
	return redact.Text(fingerprintPattern.ReplaceAllString(s, "[fingerprint]"))
}

func cleanPath(p string) string {
	p = clean(p)
	if strings.ContainsAny(p, "\n\r\t") {
		return strconv.Quote(p)
	}
	return p
}

func severitySymbol(s module.Severity) string {
	switch s {
	case module.SeverityBlocker:
		return "✗"
	case module.SeverityWarn:
		return "!"
	}
	return "·"
}

func statusSymbol(s module.Status) string {
	switch s {
	case module.StatusPass:
		return "✓"
	case module.StatusFail:
		return "✗"
	case module.StatusWarn:
		return "!"
	}
	return "·"
}

// Findings prints scan results: blockers first, then grouped by who
// handles each item.
func Findings(w io.Writer, findings []module.Finding) {
	sections := []struct {
		title string
		keep  func(module.Finding) bool
	}{
		{"Blockers: fix these before mtm prepare", func(f module.Finding) bool { return f.Severity == module.SeverityBlocker }},
		{"Migration Assistant handles", func(f module.Finding) bool {
			return f.Severity != module.SeverityBlocker && f.Handler == module.ByMigrationAssistant
		}},
		{"mtm handles", func(f module.Finding) bool { return f.Severity != module.SeverityBlocker && f.Handler == module.ByMtm }},
		{"You'll do these by hand, with a guide", func(f module.Finding) bool {
			return f.Severity != module.SeverityBlocker && f.Handler == module.Manual
		}},
	}
	for _, s := range sections {
		var picked []module.Finding
		for _, f := range findings {
			if s.keep(f) {
				picked = append(picked, f)
			}
		}
		if len(picked) == 0 {
			continue
		}
		fmt.Fprintln(w, s.title)
		for _, f := range picked {
			writeFinding(w, f)
		}
		fmt.Fprintln(w)
	}
}

func writeFinding(w io.Writer, f module.Finding) {
	fmt.Fprintf(w, "  %s %s\n", severitySymbol(f.Severity), clean(f.Title))
	if f.Detail != "" {
		fmt.Fprintf(w, "      %s\n", clean(f.Detail))
	}
	if f.Fix != "" {
		fmt.Fprintf(w, "      → %s\n", clean(f.Fix))
	}
}

const readyText = `READY FOR MIGRATION ASSISTANT

Next:
  1. Don't change anything on this Mac from now on. Changes made after prepare aren't checked.
  2. Turn on the new Mac. During its first-boot setup, choose to transfer from a Mac,
     not from a Time Machine backup. A Thunderbolt cable between the Macs is fastest.
  3. On this Mac, open Migration Assistant and choose "To another Mac".
  4. Select every category.
  5. When it finishes, open Terminal on the new Mac and run: mtm verify
`

func Prepare(w io.Writer, ready bool, done []string, problems []module.Finding) {
	if len(done) > 0 {
		fmt.Fprintln(w, "Done")
		for _, d := range done {
			fmt.Fprintf(w, "  ✓ %s\n", clean(d))
		}
		fmt.Fprintln(w)
	}
	if len(problems) > 0 {
		fmt.Fprintln(w, "Problems")
		for _, p := range problems {
			writeFinding(w, p)
		}
		fmt.Fprintln(w)
	}
	if !ready {
		fmt.Fprintln(w, "NOT READY FOR MIGRATION ASSISTANT")
		fmt.Fprintln(w, "Fix the problems marked ✗ and run mtm prepare again.")
		return
	}
	fmt.Fprint(w, readyText)
}

func Verify(w io.Writer, checks []module.Check, v verdict.Verdict, details bool) {
	lastModule := ""
	for _, c := range checks {
		if c.Module != lastModule {
			if lastModule != "" {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, c.Module)
			lastModule = c.Module
		}
		writeCheck(w, c, details)
	}
	fmt.Fprintln(w)
	if v.Safe {
		fmt.Fprintln(w, "SAFE TO WIPE")
		fmt.Fprintln(w, "Everything mtm recorded arrived. You can erase the old Mac, then run mtm cleanup here.")
		return
	}
	fmt.Fprintln(w, "NOT SAFE TO WIPE")
	for _, c := range v.Blocking {
		reason := c.Detail
		if reason == "" {
			reason = string(c.Status)
		}
		fmt.Fprintf(w, "  ✗ %s %s: %s\n", c.Module, clean(c.Title), clean(reason))
	}
	for _, item := range v.Pending {
		fmt.Fprintf(w, "  ✗ checklist: %s\n", clean(item))
	}
}

func writeCheck(w io.Writer, c module.Check, details bool) {
	line := fmt.Sprintf("  %s %s", statusSymbol(c.Status), clean(c.Title))
	if c.Detail != "" {
		line += "  (" + clean(c.Detail) + ")"
	}
	fmt.Fprintln(w, line)
	paths := c.Paths
	if !details && len(paths) > shownPaths {
		paths = paths[:shownPaths]
	}
	for _, p := range paths {
		fmt.Fprintf(w, "      %s\n", cleanPath(p))
	}
	if hidden := len(c.Paths) - len(paths); hidden > 0 {
		fmt.Fprintf(w, "      … %d more (use --details)\n", hidden)
	}
}

func JSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	_, err = fmt.Fprintln(w, clean(string(data)))
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/report/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/report
git commit -m "Add report rendering with fingerprint scrubbing

Reports are meant to be pasted into public issues, so every string is
scrubbed of fingerprints and credentials at the last step, regardless of
what a module put in it.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: State and orchestration (`internal/app`)

**Files:**
- Create: `internal/state/state.go`, `internal/state/state_test.go`
- Create: `internal/app/app.go`, `internal/app/app_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–12.
- Produces:
  - `state.FileName = "state.json"`, `state.VerifyResult{At time.Time; Safe bool}`, `state.State{LastVerify *VerifyResult}`, `state.Load(*stage.Stage) (State, error)` (zero value if absent), `state.Save(*stage.Stage, State) error`.
  - `app.App{Runner runner.Runner; Home, Version string; Now func() time.Time; Modules []module.Module}`.
  - `(*App).Scan(ctx) ScanReport`, `(*App).Prepare(ctx, PrepareOptions) (PrepareReport, error)`, `(*App).Verify(ctx, VerifyOptions) (VerifyReport, error)`, `(*App).Cleanup(force bool) (removed bool, err error)`.
  - `app.ScanReport{Findings}`, `app.PrepareOptions{Only []string; Deep bool}`, `app.PrepareReport{Ready bool; Done []string; Problems []module.Finding}`, `app.VerifyOptions{Only []string; Deep bool}`, `app.VerifyReport{Checks []module.Check; Verdict verdict.Verdict}`.
  - `app.ErrOtherMacsRecord`, `app.ErrNotVerified`.

- [ ] **Step 1: Write the failing tests**

`internal/state/state_test.go`:
```go
package state

import (
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/stage"
)

func TestLoadMissingIsZero(t *testing.T) {
	st, _ := stage.Open(t.TempDir())
	s, err := Load(st)
	if err != nil || s.LastVerify != nil {
		t.Errorf("Load = %+v, %v; want zero state", s, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	st, _ := stage.Open(t.TempDir())
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if err := Save(st, State{LastVerify: &VerifyResult{At: at, Safe: true}}); err != nil {
		t.Fatal(err)
	}
	s, err := Load(st)
	if err != nil || s.LastVerify == nil || !s.LastVerify.Safe || !s.LastVerify.At.Equal(at) {
		t.Errorf("Load = %+v, %v", s, err)
	}
}
```

`internal/app/app_test.go`:
```go
package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/modules/files"
	"github.com/momentmaker/mtm/internal/modules/git"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

const (
	oldUUID = "OLD-MAC-UUID"
	newUUID = "NEW-MAC-UUID"
)

var ctx = context.Background()

func fakeMac(home, uuid string) *runner.Fake {
	responses := testutil.MacResponses(uuid)
	responses["/usr/bin/tmutil addexclusion "+filepath.Join(home, stage.DirName)] = runner.Response{}
	return &runner.Fake{Responses: responses, Fallback: runner.Exec{}}
}

func newTestApp(home string, r runner.Runner) *App {
	return &App{Runner: r, Home: home, Version: "test", Now: time.Now, Modules: []module.Module{files.New(), git.New()}}
}

func seedHome(t *testing.T) string {
	t.Helper()
	testutil.IsolateGit(t)
	home := t.TempDir()
	testutil.WriteFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	testutil.WriteFile(t, filepath.Join(home, ".ssh/id_ed25519"), "key\n")
	testutil.WriteFile(t, filepath.Join(home, "Documents/notes.md"), "notes\n")
	testutil.WriteFile(t, filepath.Join(home, "Library/Preferences/p.plist"), "plist\n")
	repo := filepath.Join(home, "GitHub", "app")
	os.MkdirAll(repo, 0o755)
	testutil.Git(t, repo, "init", "-q", "-b", "main")
	testutil.WriteFile(t, filepath.Join(repo, ".gitignore"), ".env\n")
	testutil.WriteFile(t, filepath.Join(repo, "main.go"), "package main\n")
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "commit", "-q", "-m", "first")
	testutil.WriteFile(t, filepath.Join(repo, ".env"), "SECRET=1\n")
	return home
}

func prepareOld(t *testing.T, home string) {
	t.Helper()
	rep, err := newTestApp(home, fakeMac(home, oldUUID)).Prepare(ctx, PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Ready {
		t.Fatalf("not ready: %+v", rep.Problems)
	}
}

func migrate(t *testing.T, oldHome string) string {
	t.Helper()
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, oldHome, newHome)
	return newHome
}

func verifyOn(t *testing.T, home, uuid string, opts VerifyOptions) VerifyReport {
	t.Helper()
	rep, err := newTestApp(home, fakeMac(home, uuid)).Verify(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func blockingIDs(rep VerifyReport) []string {
	var ids []string
	for _, c := range rep.Verdict.Blocking {
		ids = append(ids, c.ID)
	}
	return ids
}

// keepTime rewrites path with same-length content and restores its old
// modification time: corruption that a timestamp check alone would miss.
func keepTime(t *testing.T, path, content string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(path, info.ModTime(), info.ModTime())
}

func TestSimulatedMigrationIsSafe(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	rep := verifyOn(t, migrate(t, old), newUUID, VerifyOptions{})
	if !rep.Verdict.Safe {
		t.Fatalf("not safe; blocking: %v", blockingIDs(rep))
	}
}

func TestVerifyCatchesFaults(t *testing.T) {
	tests := []struct {
		name   string
		damage func(t *testing.T, home string)
		want   string
	}{
		{"missing ssh key", func(t *testing.T, home string) {
			os.Remove(filepath.Join(home, ".ssh/id_ed25519"))
		}, "files:.ssh"},
		{"dotfile corrupted with original time", func(t *testing.T, home string) {
			keepTime(t, filepath.Join(home, ".zshrc"), "export B=2\n")
		}, "files:" + files.HomeGroup},
		{"repo missing", func(t *testing.T, home string) {
			os.RemoveAll(filepath.Join(home, "GitHub", "app"))
		}, "git:GitHub/app"},
		{"ignored secret corrupted with original time", func(t *testing.T, home string) {
			keepTime(t, filepath.Join(home, "GitHub", "app", ".env"), "SECRET=2\n")
		}, "git:GitHub/app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := seedHome(t)
			prepareOld(t, old)
			newHome := migrate(t, old)
			tt.damage(t, newHome)
			rep := verifyOn(t, newHome, newUUID, VerifyOptions{})
			if rep.Verdict.Safe {
				t.Fatal("verdict is SAFE TO WIPE, want NOT SAFE")
			}
			if !slices.Contains(blockingIDs(rep), tt.want) {
				t.Errorf("blocking = %v, want it to include %q", blockingIDs(rep), tt.want)
			}
		})
	}
}

func TestChangesAfterPrepareStaySafe(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	notes := filepath.Join(newHome, "Documents/notes.md")
	os.WriteFile(notes, []byte("edited on the new Mac\n"), 0o644)
	later := time.Now().Add(time.Minute)
	os.Chtimes(notes, later, later)
	if rep := verifyOn(t, newHome, newUUID, VerifyOptions{}); !rep.Verdict.Safe {
		t.Errorf("not safe after a normal edit; blocking: %v", blockingIDs(rep))
	}
}

func TestVerifyOnSameMacIsNotSafe(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	rep := verifyOn(t, old, oldUUID, VerifyOptions{})
	if rep.Verdict.Safe || !slices.Contains(blockingIDs(rep), "same-mac") {
		t.Errorf("blocking = %v, want same-mac", blockingIDs(rep))
	}
}

func TestVerifyWithoutRecord(t *testing.T) {
	home := t.TempDir()
	_, err := newTestApp(home, fakeMac(home, newUUID)).Verify(ctx, VerifyOptions{})
	if !errors.Is(err, stage.ErrNotFound) {
		t.Errorf("err = %v, want stage.ErrNotFound", err)
	}
}

func TestPrepareRefusesWithoutFileVault(t *testing.T) {
	home := seedHome(t)
	fake := fakeMac(home, oldUUID)
	fake.Responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
	rep, err := newTestApp(home, fake).Prepare(ctx, PrepareOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ready {
		t.Error("ready without FileVault")
	}
	if _, err := os.Stat(filepath.Join(home, stage.DirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("~/.mtm created without FileVault: %v", err)
	}
}

func TestPrepareOnlyKeepsOtherModules(t *testing.T) {
	home := seedHome(t)
	prepareOld(t, home)
	rep, err := newTestApp(home, fakeMac(home, oldUUID)).Prepare(ctx, PrepareOptions{Only: []string{"git"}})
	if err != nil || !rep.Ready {
		t.Fatalf("prepare --only git: %+v, %v", rep, err)
	}
	st, _ := stage.Existing(home)
	m, err := manifest.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Modules[files.Name]; !ok {
		t.Error("files module dropped from the manifest")
	}
}

func TestPrepareRejectsUnknownModule(t *testing.T) {
	home := seedHome(t)
	_, err := newTestApp(home, fakeMac(home, oldUUID)).Prepare(ctx, PrepareOptions{Only: []string{"bogus"}})
	if err == nil || !strings.Contains(err.Error(), `unknown module "bogus"`) {
		t.Errorf("err = %v, want unknown module", err)
	}
}

func TestPrepareRefusesToOverwriteAnotherMacsRecord(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	_, err := newTestApp(newHome, fakeMac(newHome, newUUID)).Prepare(ctx, PrepareOptions{})
	if !errors.Is(err, ErrOtherMacsRecord) || !strings.Contains(err.Error(), "mtm verify") {
		t.Errorf("err = %v, want ErrOtherMacsRecord pointing to mtm verify", err)
	}
}

func TestPrepareWritesRedactedLog(t *testing.T) {
	home := seedHome(t)
	prepareOld(t, home)
	logs, err := os.ReadDir(filepath.Join(home, stage.DirName, "logs"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("logs = %v, %v; want one log file", logs, err)
	}
	data, _ := os.ReadFile(filepath.Join(home, stage.DirName, "logs", logs[0].Name()))
	if !strings.Contains(string(data), "$ /usr/sbin/ioreg") || !strings.Contains(string(data), "$ git -C") {
		t.Errorf("log missing expected commands:\n%s", data)
	}
	if strings.Contains(string(data), "SECRET=1") {
		t.Error("log contains file contents")
	}
}

func TestScanIsReadOnlyAndCombinesFindings(t *testing.T) {
	home := seedHome(t)
	fake := fakeMac(home, oldUUID)
	fake.Responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
	rep := newTestApp(home, fake).Scan(ctx)
	var ids []string
	for _, f := range rep.Findings {
		ids = append(ids, f.ID)
	}
	for _, want := range []string{"filevault", "home", "repos"} {
		if !slices.Contains(ids, want) {
			t.Errorf("finding ids %v missing %q", ids, want)
		}
	}
	if _, err := os.Stat(filepath.Join(home, stage.DirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("scan created ~/.mtm")
	}
}

func TestCleanupRules(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	a := newTestApp(newHome, fakeMac(newHome, newUUID))

	if _, err := a.Cleanup(false); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("cleanup before verify: %v, want ErrNotVerified", err)
	}
	verifyOn(t, newHome, newUUID, VerifyOptions{Only: []string{"git"}})
	if _, err := a.Cleanup(false); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("cleanup after verify --only: %v, want ErrNotVerified", err)
	}
	if rep := verifyOn(t, newHome, newUUID, VerifyOptions{}); !rep.Verdict.Safe {
		t.Fatalf("full verify not safe: %v", blockingIDs(rep))
	}
	removed, err := a.Cleanup(false)
	if err != nil || !removed {
		t.Fatalf("cleanup after SAFE: %v, %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(newHome, stage.DirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("~/.mtm still exists")
	}
	if removed, err := a.Cleanup(false); err != nil || removed {
		t.Errorf("second cleanup = %v, %v; want nothing to remove", removed, err)
	}
}

func TestCleanupForce(t *testing.T) {
	home := seedHome(t)
	prepareOld(t, home)
	removed, err := newTestApp(home, fakeMac(home, oldUUID)).Cleanup(true)
	if err != nil || !removed {
		t.Errorf("forced cleanup = %v, %v", removed, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/state/... ./internal/app/...`
Expected: FAIL with `undefined: Load`, `undefined: App`, and similar.

- [ ] **Step 3: Implement state**

`internal/state/state.go`:
```go
// Package state keeps mtm's progress on the new Mac in ~/.mtm/state.json.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/momentmaker/mtm/internal/stage"
)

const FileName = "state.json"

type VerifyResult struct {
	At   time.Time `json:"at"`
	Safe bool      `json:"safe"`
}

type State struct {
	LastVerify *VerifyResult `json:"last_verify,omitempty"`
}

func Load(st *stage.Stage) (State, error) {
	path := st.Path(FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

func Save(st *stage.Stage, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return st.WriteFile(FileName, data)
}
```

- [ ] **Step 4: Implement the orchestrator**

`internal/app/app.go`:
```go
// Package app runs mtm's commands: it applies the preflight gate, manages
// ~/.mtm, calls each module, and decides the verdict.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/preflight"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
	"github.com/momentmaker/mtm/internal/verdict"
)

var (
	ErrOtherMacsRecord = errors.New("~/.mtm holds the record from another Mac, and prepare would overwrite it")
	ErrNotVerified     = errors.New("mtm verify has not reported SAFE TO WIPE on this Mac; run mtm verify first, or pass --force to delete ~/.mtm anyway")
)

type App struct {
	Runner  runner.Runner
	Home    string
	Version string
	Now     func() time.Time
	Modules []module.Module
}

type ScanReport struct {
	Findings []module.Finding `json:"findings"`
}

type PrepareOptions struct {
	Only []string
	Deep bool
}

type PrepareReport struct {
	Ready    bool             `json:"ready"`
	Done     []string         `json:"done"`
	Problems []module.Finding `json:"problems"`
}

type VerifyOptions struct {
	Only []string
	Deep bool
}

type VerifyReport struct {
	Checks  []module.Check  `json:"checks"`
	Verdict verdict.Verdict `json:"verdict"`
}

// Scan reports what mtm, Migration Assistant, and the user will each
// handle. It changes nothing on disk.
func (a *App) Scan(ctx context.Context) ScanReport {
	findings := preflight.Check(ctx, a.Runner)
	env := a.env(ctx, module.OldMac, false, nil, a.Runner)
	for _, m := range a.Modules {
		found, err := m.Scan(env)
		if err != nil {
			findings = append(findings, moduleFailure(m.Name(), "scan", err))
			continue
		}
		findings = append(findings, found...)
	}
	return ScanReport{Findings: findings}
}

func (a *App) Prepare(ctx context.Context, opts PrepareOptions) (PrepareReport, error) {
	mods, err := a.selectModules(opts.Only)
	if err != nil {
		return PrepareReport{}, err
	}
	rep := PrepareReport{Problems: preflight.Check(ctx, a.Runner)}
	if module.HasBlocker(rep.Problems) {
		return rep, nil
	}
	st, err := stage.Open(a.Home)
	if err != nil {
		return rep, err
	}
	logFile, run, err := a.openLog(st, "prepare")
	if err != nil {
		return rep, err
	}
	defer logFile.Close()
	if err := st.ExcludeFromBackups(ctx, run); err != nil {
		rep.Problems = append(rep.Problems, module.Finding{
			Module: "stage", ID: "time-machine", Severity: module.SeverityBlocker, Handler: module.ByMtm,
			Title:  "Could not exclude ~/.mtm from Time Machine",
			Detail: err.Error(),
			Fix:    "Run: tmutil addexclusion ~/.mtm, then run mtm prepare again.",
		})
		return rep, nil
	}
	info, err := preflight.Gather(ctx, run)
	if err != nil {
		return rep, err
	}
	m, err := manifest.Load(st)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		m = manifest.New(a.Version)
	case err != nil:
		return rep, err
	case m.MachineID != "" && m.MachineID != info.MachineID:
		return rep, fmt.Errorf("%w (recorded %s). If this is the new Mac, run mtm verify instead", ErrOtherMacsRecord, m.PreparedAt.Local().Format(time.RFC1123))
	}
	m.MtmVersion = a.Version
	m.MacOSVersion, m.MacOSBuild, m.MachineID = info.MacOSVersion, info.MacOSBuild, info.MachineID
	m.Hostname, _ = os.Hostname()

	startedAt := a.Now()
	env := a.env(ctx, module.OldMac, opts.Deep, nil, run)
	for _, mod := range mods {
		res, err := mod.Prepare(env, st)
		if err != nil {
			rep.Problems = append(rep.Problems, moduleFailure(mod.Name(), "prepare", err))
			continue
		}
		if err := m.SetModule(mod.Name(), startedAt, res.Inventory); err != nil {
			return rep, err
		}
		for _, d := range res.Done {
			rep.Done = append(rep.Done, mod.Name()+": "+d)
		}
		rep.Problems = append(rep.Problems, res.Problems...)
	}
	if err := manifest.Save(st, m); err != nil {
		return rep, err
	}
	rep.Ready = !module.HasBlocker(rep.Problems)
	return rep, nil
}

func (a *App) Verify(ctx context.Context, opts VerifyOptions) (VerifyReport, error) {
	mods, err := a.selectModules(opts.Only)
	if err != nil {
		return VerifyReport{}, err
	}
	st, err := stage.Existing(a.Home)
	if err != nil {
		return VerifyReport{}, err
	}
	logFile, run, err := a.openLog(st, "verify")
	if err != nil {
		return VerifyReport{}, err
	}
	defer logFile.Close()
	m, err := manifest.Load(st)
	if errors.Is(err, fs.ErrNotExist) {
		return VerifyReport{}, fmt.Errorf("~/.mtm has no record from the old Mac; run mtm prepare there before Migration Assistant: %w", err)
	}
	if err != nil {
		return VerifyReport{}, err
	}

	var checks []module.Check
	for _, f := range preflight.Check(ctx, run) {
		checks = append(checks, module.Check{Module: f.Module, ID: f.ID, Status: module.StatusFail, Critical: true, Title: f.Title, Detail: f.Fix})
	}
	info, err := preflight.Gather(ctx, run)
	if err != nil {
		return VerifyReport{}, err
	}
	if info.MachineID == m.MachineID {
		checks = append(checks, module.Check{
			Module: preflight.Module, ID: "same-mac", Status: module.StatusFail, Critical: true,
			Title:  "This is the Mac that ran prepare",
			Detail: "run mtm verify on the new Mac after Migration Assistant finishes",
		})
	}
	if m.MtmVersion != a.Version {
		checks = append(checks, module.Check{
			Module: preflight.Module, ID: "version", Status: module.StatusWarn,
			Title:  "Different mtm version",
			Detail: fmt.Sprintf("prepare ran mtm %s; this is mtm %s", m.MtmVersion, a.Version),
		})
	}

	env := a.env(ctx, module.NewMac, opts.Deep, m, run)
	for _, mod := range mods {
		if _, ok := m.Modules[mod.Name()]; !ok {
			checks = append(checks, module.Check{
				Module: mod.Name(), ID: mod.Name() + ":not-prepared", Status: module.StatusWarn,
				Title:  mod.Name() + " was not recorded by prepare",
				Detail: "run mtm prepare on the old Mac to include it",
			})
			continue
		}
		cs, err := mod.Verify(env, st)
		if err != nil {
			checks = append(checks, module.Check{
				Module: mod.Name(), ID: mod.Name() + ":error", Status: module.StatusFail, Critical: true,
				Title: mod.Name() + ": verify failed", Detail: err.Error(),
			})
			continue
		}
		checks = append(checks, cs...)
	}

	v := verdict.Decide(checks, nil)
	s, err := state.Load(st)
	if err != nil {
		return VerifyReport{}, err
	}
	s.LastVerify = &state.VerifyResult{At: a.Now(), Safe: v.Safe && len(opts.Only) == 0}
	if err := state.Save(st, s); err != nil {
		return VerifyReport{}, err
	}
	return VerifyReport{Checks: checks, Verdict: v}, nil
}

// Cleanup deletes ~/.mtm. Without force, it requires a SAFE TO WIPE result
// from a full verify on this Mac.
func (a *App) Cleanup(force bool) (bool, error) {
	st, err := stage.Existing(a.Home)
	if errors.Is(err, stage.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !force {
		s, err := state.Load(st)
		if err != nil {
			return false, err
		}
		if s.LastVerify == nil || !s.LastVerify.Safe {
			return false, ErrNotVerified
		}
	}
	if err := st.Remove(); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) env(ctx context.Context, machine module.Machine, deep bool, m *manifest.Manifest, run runner.Runner) module.Env {
	return module.Env{Ctx: ctx, Run: run, Home: a.Home, Machine: machine, Deep: deep, Manifest: m}
}

func (a *App) selectModules(only []string) ([]module.Module, error) {
	if len(only) == 0 {
		return a.Modules, nil
	}
	byName := map[string]module.Module{}
	var names []string
	for _, m := range a.Modules {
		byName[m.Name()] = m
		names = append(names, m.Name())
	}
	var out []module.Module
	for _, name := range only {
		m, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown module %q (known: %s)", name, strings.Join(names, ", "))
		}
		out = append(out, m)
	}
	return out, nil
}

func (a *App) openLog(st *stage.Stage, command string) (*os.File, runner.Runner, error) {
	if err := st.MkdirAll("logs"); err != nil {
		return nil, nil, err
	}
	path := st.Path("logs", a.Now().UTC().Format("20060102T150405Z")+"-"+command+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log %s: %w", path, err)
	}
	return f, &runner.Logged{Inner: a.Runner, W: f}, nil
}

func moduleFailure(name, step string, err error) module.Finding {
	return module.Finding{
		Module: name, ID: name + ":error", Severity: module.SeverityBlocker, Handler: module.ByMtm,
		Title:  fmt.Sprintf("%s: %s failed", name, step),
		Detail: err.Error(),
		Fix:    "Fix the cause above and run the command again. Details are in ~/.mtm/logs.",
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/state/... ./internal/app/...`
Expected: PASS.

- [ ] **Step 6: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/state internal/app
git commit -m "Orchestrate scan, prepare, verify, and cleanup

A simulated migration (ditto copy) proves the full flow ends in SAFE TO
WIPE and that each injected fault blocks it. Guards stop prepare from
overwriting the old Mac's record and stop cleanup before a full SAFE
verify.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: CLI and Stage 1 wrap-up

**Files:**
- Create: `cmd/mtm/main.go`, `cmd/mtm/main_test.go`
- Modify: `IMPLEMENTATION_PLAN.md` (Stage 1 status)

**Interfaces:**
- Consumes: `app.App` and its methods and reports (Task 13); `report` (Task 12); `files.New`, `git.New` (Tasks 8, 10); `runner.Exec` (Task 1).
- Produces:
  - Binary `mtm` with `scan [--json]`, `prepare [--only a,b] [--deep]`, `verify [--only a,b] [--deep] [--details] [--json]`, `cleanup [--force]`, `version`, `help`.
  - `run(ctx, args []string, stdout, stderr io.Writer, newApp func(home string) *app.App) int`.

- [ ] **Step 1: Write the failing tests**

`cmd/mtm/main_test.go`:
```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/testutil"
)

var ctx = context.Background()

func noApp(t *testing.T) func(string) *app.App {
	return func(string) *app.App {
		t.Fatal("app should not be built for this command")
		return nil
	}
}

func fileVaultOffApp(home string) *app.App {
	responses := testutil.MacResponses("AAAA")
	responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
	return &app.App{Runner: &runner.Fake{Responses: responses}, Home: home, Version: "test", Now: time.Now}
}

func TestVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"version"}, &out, &errOut, noApp(t)); code != exitOK || out.String() != "mtm dev\n" {
		t.Errorf("code %d out %q", code, out.String())
	}
}

func TestNoArgsShowsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(ctx, nil, &out, &errOut, noApp(t)); code != exitFailed || !strings.Contains(errOut.String(), "Usage:") {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"bogus"}, &out, &errOut, noApp(t)); code != exitFailed || !strings.Contains(errOut.String(), `unknown command "bogus"`) {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestBadFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"scan", "--bogus"}, &out, &errOut, fileVaultOffApp); code != exitFailed {
		t.Errorf("code %d, want %d", code, exitFailed)
	}
}

func TestScanExitsOneOnBlockers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"scan"}, &out, &errOut, fileVaultOffApp)
	if code != exitProblems || !strings.Contains(out.String(), "FileVault is off") {
		t.Errorf("code %d out:\n%s", code, out.String())
	}
}

func TestScanJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	run(ctx, []string{"scan", "--json"}, &out, &errOut, fileVaultOffApp)
	if !json.Valid(out.Bytes()) {
		t.Errorf("not valid JSON:\n%s", out.String())
	}
}

func TestPrepareNotReadyExitsOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"prepare"}, &out, &errOut, fileVaultOffApp)
	if code != exitProblems || !strings.Contains(out.String(), "NOT READY") {
		t.Errorf("code %d out:\n%s", code, out.String())
	}
}

func TestVerifyWithoutRecordFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"verify"}, &out, &errOut, fileVaultOffApp)
	if code != exitFailed || !strings.Contains(errOut.String(), "run mtm prepare") {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestCleanupWithNothingToClean(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"cleanup"}, &out, &errOut, fileVaultOffApp)
	if code != exitOK || !strings.Contains(out.String(), "Nothing to clean up") {
		t.Errorf("code %d out %q", code, out.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/mtm/...`
Expected: FAIL with `undefined: run`, `undefined: exitOK`.

- [ ] **Step 3: Implement the CLI**

`cmd/mtm/main.go`:
```go
// Command mtm moves a developer's Mac to a new Mac, alongside Migration Assistant.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/modules/files"
	"github.com/momentmaker/mtm/internal/modules/git"
	"github.com/momentmaker/mtm/internal/report"
	"github.com/momentmaker/mtm/internal/runner"
)

var version = "dev"

const (
	exitOK       = 0
	exitProblems = 1
	exitFailed   = 2
)

const usage = `mtm moves a developer's Mac to a new Mac, alongside Migration Assistant.

Usage:
  mtm scan [--json]                         check this Mac before migrating (changes nothing)
  mtm prepare [--only a,b] [--deep]         record this Mac before Migration Assistant
  mtm verify [--only a,b] [--deep] [--details] [--json]
                                            check the new Mac against the record
  mtm cleanup [--force]                     delete ~/.mtm after SAFE TO WIPE
  mtm version
`

type command func(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int

var commands = map[string]command{
	"scan":    cmdScan,
	"prepare": cmdPrepare,
	"verify":  cmdVerify,
	"cleanup": cmdCleanup,
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, defaultApp)
	stop()
	os.Exit(code)
}

func defaultApp(home string) *app.App {
	return &app.App{
		Runner:  runner.Exec{},
		Home:    home,
		Version: version,
		Now:     time.Now,
		Modules: []module.Module{files.New(), git.New()},
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, newApp func(home string) *app.App) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitFailed
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintln(stdout, "mtm", version)
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "mtm: unknown command %q\n\n%s", args[0], usage)
		return exitFailed
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "mtm:", err)
		return exitFailed
	}
	return cmd(ctx, newApp(home), args[1:], stdout, stderr)
}

func cmdScan(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	rep := a.Scan(ctx)
	if *asJSON {
		if err := report.JSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, "mtm scan:", err)
			return exitFailed
		}
	} else {
		report.Findings(stdout, rep.Findings)
	}
	if module.HasBlocker(rep.Findings) {
		return exitProblems
	}
	return exitOK
}

func cmdPrepare(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	only := flags.String("only", "", "comma-separated modules to prepare")
	deep := flags.Bool("deep", false, "fingerprint every file (slow)")
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	rep, err := a.Prepare(ctx, app.PrepareOptions{Only: splitList(*only), Deep: *deep})
	if err != nil {
		fmt.Fprintln(stderr, "mtm prepare:", err)
		return exitFailed
	}
	report.Prepare(stdout, rep.Ready, rep.Done, rep.Problems)
	if !rep.Ready {
		return exitProblems
	}
	return exitOK
}

func cmdVerify(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	only := flags.String("only", "", "comma-separated modules to verify")
	deep := flags.Bool("deep", false, "also run git fsck on every repo")
	details := flags.Bool("details", false, "list every path")
	asJSON := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	rep, err := a.Verify(ctx, app.VerifyOptions{Only: splitList(*only), Deep: *deep})
	if err != nil {
		fmt.Fprintln(stderr, "mtm verify:", err)
		return exitFailed
	}
	if *asJSON {
		if err := report.JSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, "mtm verify:", err)
			return exitFailed
		}
	} else {
		report.Verify(stdout, rep.Checks, rep.Verdict, *details)
	}
	if !rep.Verdict.Safe {
		return exitProblems
	}
	return exitOK
}

func cmdCleanup(_ context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	force := flags.Bool("force", false, "delete ~/.mtm even without SAFE TO WIPE")
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	removed, err := a.Cleanup(*force)
	switch {
	case errors.Is(err, app.ErrNotVerified):
		fmt.Fprintln(stderr, "mtm cleanup:", err)
		return exitProblems
	case err != nil:
		fmt.Fprintln(stderr, "mtm cleanup:", err)
		return exitFailed
	case !removed:
		fmt.Fprintln(stdout, "Nothing to clean up: ~/.mtm does not exist.")
		return exitOK
	}
	fmt.Fprintln(stdout, "Deleted ~/.mtm.")
	fmt.Fprintln(stdout, "If you gave your terminal Full Disk Access for mtm, remove it now:")
	fmt.Fprintln(stdout, "System Settings > Privacy & Security > Full Disk Access.")
	return exitOK
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/mtm/...`
Expected: PASS.

- [ ] **Step 5: Run the whole suite and a manual smoke test**

Run:
```bash
gofmt -l . && go vet ./... && go test -race ./...
go build -o mtm ./cmd/mtm && ./mtm version && ./mtm scan; echo "exit $?"
```
Expected:
- `gofmt -l` prints nothing, and vet and all tests pass.
- `./mtm version` prints `mtm dev`.
- `./mtm scan` shows the real preflight result for this Mac, the home folder finding, and one warning per repo with local-only work. It exits `0`, or `1` if there is a blocker.
- `ls ~/.mtm` afterward shows it was not created (scan is read-only).

- [ ] **Step 6: Mark Stage 1 complete and commit**

In `IMPLEMENTATION_PLAN.md`, change the Stage 1 line `**Status**: Not Started` to `**Status**: Complete`.

```bash
gofmt -w . && go vet ./... && go test ./...
git add cmd/mtm IMPLEMENTATION_PLAN.md
git commit -m "Add mtm CLI and complete Stage 1

scan, prepare, verify, and cleanup are usable end to end for home-folder
files and git repos, with exit codes scripts can rely on.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
