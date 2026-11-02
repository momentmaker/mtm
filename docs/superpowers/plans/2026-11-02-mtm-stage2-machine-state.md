# mtm Stage 2: Machine State and `finish` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record and restore the machine state outside the home folder that a developer relies on: login shell, edited `/etc` files, installed apps and Rosetta, Homebrew packages and services, toolchains on the login shell's `PATH`, and launch agents and login items. Add `mtm finish` (new Mac) and `mtm prepare --undo` (old Mac).

**Architecture:**
- Four new modules implement the existing `module.Module` contract: `system`, `homebrew`, `toolchains`, and `loginitems`.
- A new optional `module.Undoer` interface lets `prepare --undo` call only the modules that change the old Mac. Today that is Homebrew services.
- `app.Finish` and `app.Undo` reuse the Stage 1 orchestration: the preflight gate, `~/.mtm`, the redacted log, and the machine-id guard.
- During `prepare`, `Env.Manifest` now holds this Mac's previous record, so a re-run can carry forward what the first run changed.
- System facts come from Apple's command-line tools, called by absolute path, through the fakeable runner.

**Tech Stack:**
- Go 1.26, standard library only.
- macOS tools: `dscl`, `plutil`, `lipo`, `launchctl`, `osascript`, and `sandbox-exec` (test only).
- Homebrew (`brew services`, `brew bundle`).

**Spec:** `docs/superpowers/specs/2026-11-02-mtm-design.md`. Stage 2 covers:
- sections 5 (rows `finish` and `prepare --undo`) and 7.3 (`Env.Manifest` during prepare, `Undo`);
- 9.3 `homebrew`, 9.4 `toolchains`, 9.7 `loginitems`, and 9.11 `system`;
- 10 (undo removes records; `finish` refuses on the wrong Mac and without FileVault);
- 11 (the no-network sandbox test).

These sections were updated while planning this stage. The stage list and the known gaps from Stage 1 are in `IMPLEMENTATION_PLAN.md`.

## Global Constraints

- Module path `github.com/momentmaker/mtm`, `go 1.26`, and no third-party dependencies.
- `mtm` makes no network connections and never reads secret contents (spec 6.2). Reports and logs never contain fingerprints, file contents, or environment values (spec 6.3).
- Apple tools are called by absolute path: `/usr/bin/dscl`, `/usr/bin/plutil`, `/usr/bin/lipo`, `/bin/launchctl`, `/usr/bin/osascript`, plus Stage 1's `/usr/bin/fdesetup`, `/usr/bin/sw_vers`, `/usr/sbin/sysctl`, `/usr/sbin/ioreg`, `/usr/bin/tmutil`.
- Homebrew rules:
  - Every `brew` command runs with `HOMEBREW_NO_AUTO_UPDATE=1`, `HOMEBREW_NO_ANALYTICS=1`, `HOMEBREW_NO_ENV_HINTS=1`, and `HOMEBREW_NO_INSTALL_CLEANUP=1`.
  - `brew bundle dump` always passes `--file=- --formula --cask --tap --mas`.
- `mtm` never runs `sudo`. Steps that need root are printed for the user.
- Every path in a printed shell command goes through `macos.ShellQuote`.
- System Events (`osascript`) is queried only in `loginitems` Prepare. Never in Scan, Verify, or tests that use the real runner.
- No test may stop, start, or load anything on the developer's Mac. Every runner-driven test uses `runner.Fake`. The `integration`-tagged no-network test skips when user Homebrew services are running.
- Modules never print. All output goes through `internal/report`. Comments explain why. No commented-out code.
- Exit codes:
  - `verify`: `0` safe, `1` not safe, `2` mtm failed.
  - Other commands: `0` success, `1` completed with problems or refused, `2` mtm failed.
- Before every commit: `gofmt -l .` prints nothing, `go vet ./...` passes, and `go test ./...` passes.
- Commit messages explain why and end with a `Co-Authored-By:` trailer naming the model that wrote the commit, as your harness instructs.

## Review Focus

These are the five inputs most likely to bite a real user that the main tests don't exercise. Each has a pinned test in the task that owns the code.

1. **A login shell whose startup files print noise or exit non-zero** (this author's `.zprofile` prints errors). The PATH is still read from the marker line. A shell that prints no PATH is a clear error. Tests: Task 6 `TestCollectToleratesNoisyShell`, `TestCollectFailsWithoutPATH`.
2. **Re-running `prepare` after the first run stopped services.** The services stay recorded as stopped, so `finish` still restarts them. Test: Task 5 `TestPrepareAgainKeepsServicesStoppedEarlier`.
3. **Running `finish` on the old Mac, or `prepare --undo` on the new Mac.** Both are refused. After undo, `verify` no longer trusts the undone record. Tests: Task 2 `TestFinishRefusesOnTheOldMac`, `TestUndoRefusesOnTheNewMac`, `TestUndoRestoresAndForgetsTheRecord`.
4. **Home folders and file names with spaces or apostrophes inside printed commands.** They are shell-quoted. Tests: Task 1 `TestShellQuote`, Task 4 `TestFinishPrintsRestoreCommands` (home "my home"), Task 7 `TestFinishBootstrapFailureIsByHand`.
5. **Odd app bundles.** These include a missing `Info.plist` key, a script as the executable, a vendor subfolder, an unreadable vendor folder, and a symlinked app. Each is listed or skipped without failing the inventory. Test: Task 3 `TestListHandlesOddBundles`.

## File Structure

```
internal/words/words.go                 Count: "1 thing" / "3 things"
internal/macos/macos.go                 LoginShell, PlistValue, Archs, ShellQuote, RosettaPath
internal/fsinfo/fsinfo.go               (modify) add HashBytes
internal/modules/git/*.go               (modify) use words.Count instead of the local plural
internal/module/module.go               (modify) Undoer; Env.Manifest during prepare
internal/app/app.go                     (modify) prepare passes the previous record; Finish, Undo, StepReport, loadRecord
internal/report/report.go               (modify) Finish, Undo, shared writeDone/writeProblems; ready text names mtm finish
cmd/mtm/main.go                         (modify) finish command, prepare --undo, module wiring
internal/apps/apps.go                   app inventory: List, Compare, App.IntelOnly
internal/modules/system/system.go       shell, /etc copies, apps, Rosetta: Scan, Prepare, Finish
internal/modules/system/verify.go       system Verify checks
internal/modules/homebrew/homebrew.go   services, Brewfile, Undo/Finish/Verify
internal/modules/toolchains/toolchains.go  PATH from the login shell, tool versions
internal/modules/loginitems/loginitems.go  launch agents and login items
cmd/mtm/nonetwork_integration_test.go   sandbox-exec no-network test (build tag integration)
```

---

### Task 1: Shared helpers (`words`, `macos`, `fsinfo.HashBytes`)

**Files:**
- Create: `internal/words/words.go`, `internal/words/words_test.go`
- Create: `internal/macos/macos.go`, `internal/macos/macos_test.go`
- Modify: `internal/fsinfo/fsinfo.go`, `internal/fsinfo/fsinfo_test.go`
- Modify: `internal/modules/git/parse.go`, `parse_test.go`, `module.go`, `repo.go`, `verify.go`

**Interfaces:**
- Consumes: `runner.Runner`, `runner.Command`, `runner.ExitCode` (Stage 1).
- Produces:
  - `words.Count(n int, one, many string) string`
  - `macos.LoginShell(ctx, runner.Runner, user string) (string, error)`
  - `macos.PlistValue(ctx, runner.Runner, plist, key string) (string, error)`, which returns `""` when the key is absent
  - `macos.Archs(ctx, runner.Runner, exe string) []string`
  - `macos.ShellQuote(s string) string`
  - `macos.RosettaPath`
  - `fsinfo.HashBytes(data []byte) string`, in the same `sha256:<hex>` format as `HashFile`

- [ ] **Step 1: Write the failing tests**

`internal/words/words_test.go`:
```go
package words

import "testing"

func TestCount(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{{0, "0 stashes"}, {1, "1 stash"}, {3, "3 stashes"}}
	for _, tt := range tests {
		if got := Count(tt.n, "stash", "stashes"); got != tt.want {
			t.Errorf("Count(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
```

`internal/macos/macos_test.go`:
```go
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
		"/Users/dev/.mtm":            "'/Users/dev/.mtm'",
		"/Users/my home/.mtm":        "'/Users/my home/.mtm'",
		"/Users/o'brien/.mtm/x":      `'/Users/o'\''brien/.mtm/x'`,
	}
	for in, want := range tests {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}
```

Add to `internal/fsinfo/fsinfo_test.go`:
```go
func TestHashBytesMatchesHashFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := HashBytes([]byte("127.0.0.1 localhost\n")); got != want {
		t.Errorf("HashBytes = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/words/... ./internal/macos/... ./internal/fsinfo/...`
Expected: FAIL with `undefined: Count`, `undefined: LoginShell`, `undefined: HashBytes`.

- [ ] **Step 3: Implement the helpers**

`internal/words/words.go`:
```go
// Package words formats counts for messages.
package words

import "fmt"

// Count returns "1 one" for a single item and "n many" otherwise.
func Count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
```

`internal/macos/macos.go`:
```go
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
```

Add to `internal/fsinfo/fsinfo.go`. `crypto/sha256` and `encoding/hex` are already imported.
```go
// HashBytes fingerprints content already in memory, in HashFile's format.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Switch the git module to `words.Count`**

Run:
```bash
perl -pi -e 's/\bplural\(/words.Count(/g' internal/modules/git/module.go internal/modules/git/repo.go internal/modules/git/verify.go
```
Then:
- Delete `func plural` from `internal/modules/git/parse.go`, and `TestPlural` from `internal/modules/git/parse_test.go`.
- Add `"github.com/momentmaker/mtm/internal/words"` to the import block of every git file that now calls `words.Count`.
- Keep `fmt` in `parse.go`, because `parseCount` still uses it.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/words/... ./internal/macos/... ./internal/fsinfo/... ./internal/modules/git/...`
Expected: PASS.

- [ ] **Step 6: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/words internal/macos internal/fsinfo internal/modules/git
git commit -m "Add shared macOS and wording helpers

The Stage 2 modules all read the login shell or plists, print sudo
commands with paths in them, and count things in messages. One tested
copy of each beats a private helper per module.

Co-Authored-By: <your model> <noreply@anthropic.com>"
```

---

### Task 2: `mtm finish` and `mtm prepare --undo`

**Files:**
- Modify: `internal/module/module.go`
- Modify: `internal/app/app.go`, `internal/app/app_test.go`
- Modify: `internal/report/report.go`, `internal/report/report_test.go`
- Modify: `cmd/mtm/main.go`, `cmd/mtm/main_test.go`

**Interfaces:**
- Consumes: Stage 1's `app.App`, `preflight.Check/Gather`, `stage.Existing`, `manifest.Load/Save`, `module.LoadInventory`, `moduleFailure`, `openLog`. Also the test helpers `fakeMac`, `migrate`, `blockingIDs`, `oldUUID`, and `newUUID` in `app_test.go`.
- Produces:
  - `module.Undoer` interface: `Undo(env Env, st *stage.Stage) (Result, error)`.
  - During `Prepare`, `Env.Manifest` is this Mac's previous record: the loaded manifest, or a new empty one.
  - `app.StepReport{Done []string; Problems []module.Finding}`.
  - `(*App).Finish(ctx) (StepReport, error)` and `(*App).Undo(ctx) (StepReport, error)`.
  - `app.ErrFinishOnOldMac` and `app.ErrUndoOnNewMac`.
  - `report.Finish(w, done, problems)` and `report.Undo(w, done, problems)`.
  - CLI: `mtm finish` and `mtm prepare --undo`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/app/app_test.go`:
```go
// stepModule records which steps ran, to test the orchestration of
// prepare, finish, and undo without touching the machine.
type stepModule struct {
	name  string
	calls *[]string
}

func (m stepModule) Name() string                               { return m.name }
func (stepModule) Scan(module.Env) ([]module.Finding, error) { return nil, nil }

func (m stepModule) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	call := "prepare"
	if _, ok := env.Manifest.Modules[m.name]; ok {
		call = "prepare-again"
	}
	*m.calls = append(*m.calls, call)
	return module.Result{Inventory: map[string]bool{"ok": true}}, nil
}

func (m stepModule) Finish(module.Env, *stage.Stage) (module.Result, error) {
	*m.calls = append(*m.calls, "finish")
	return module.Result{
		Done:     []string{"started things"},
		Problems: []module.Finding{{Module: m.name, ID: "by-hand", Severity: module.SeverityWarn, Handler: module.Manual, Title: "Do a thing"}},
	}, nil
}

func (m stepModule) Verify(module.Env, *stage.Stage) ([]module.Check, error) {
	return []module.Check{{Module: m.name, ID: m.name + ":ok", Status: module.StatusPass, Title: "ok"}}, nil
}

func (m stepModule) Undo(module.Env, *stage.Stage) (module.Result, error) {
	*m.calls = append(*m.calls, "undo")
	return module.Result{Done: []string{"restarted things"}}, nil
}

// finishOnly has no Undo, like the files and git modules.
type finishOnly struct{ name string }

func (m finishOnly) Name() string                                        { return m.name }
func (finishOnly) Scan(module.Env) ([]module.Finding, error)          { return nil, nil }
func (finishOnly) Prepare(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{Inventory: true}, nil
}
func (finishOnly) Finish(module.Env, *stage.Stage) (module.Result, error) { return module.Result{}, nil }
func (m finishOnly) Verify(module.Env, *stage.Stage) ([]module.Check, error) {
	return []module.Check{{Module: m.name, ID: m.name + ":ok", Status: module.StatusPass, Title: "ok"}}, nil
}

func stepApp(home, uuid string, mods ...module.Module) *App {
	return &App{Runner: fakeMac(home, uuid), Home: home, Version: "test", Now: time.Now, Modules: mods}
}

func mustPrepare(t *testing.T, a *App) {
	t.Helper()
	rep, err := a.Prepare(ctx, PrepareOptions{})
	if err != nil || !rep.Ready {
		t.Fatalf("prepare = %+v, %v", rep, err)
	}
}

func TestPrepareShowsModulesTheirPreviousRecord(t *testing.T) {
	home := t.TempDir()
	var calls []string
	a := stepApp(home, oldUUID, stepModule{name: "svc", calls: &calls})
	mustPrepare(t, a)
	mustPrepare(t, a)
	if want := []string{"prepare", "prepare-again"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %q, want %q", calls, want)
	}
}

func TestFinishRunsRecordedModulesOnTheNewMac(t *testing.T) {
	old := t.TempDir()
	var calls []string
	step := stepModule{name: "svc", calls: &calls}
	mustPrepare(t, stepApp(old, oldUUID, step))
	newHome := migrate(t, old)
	unrecorded := stepModule{name: "extra", calls: &calls}
	rep, err := stepApp(newHome, newUUID, step, unrecorded).Finish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Done, []string{"svc: started things"}) || len(rep.Problems) != 1 {
		t.Errorf("report = %+v", rep)
	}
	if want := []string{"prepare", "finish"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %q, want %q (an unrecorded module must not run)", calls, want)
	}
}

func TestFinishRefusesOnTheOldMac(t *testing.T) {
	home := t.TempDir()
	var calls []string
	a := stepApp(home, oldUUID, stepModule{name: "svc", calls: &calls})
	mustPrepare(t, a)
	if _, err := a.Finish(ctx); !errors.Is(err, ErrFinishOnOldMac) {
		t.Errorf("err = %v, want ErrFinishOnOldMac", err)
	}
	if slices.Contains(calls, "finish") {
		t.Error("finish ran on the old Mac")
	}
}

func TestFinishNeedsFileVault(t *testing.T) {
	old := t.TempDir()
	var calls []string
	step := stepModule{name: "svc", calls: &calls}
	mustPrepare(t, stepApp(old, oldUUID, step))
	newHome := migrate(t, old)
	a := stepApp(newHome, newUUID, step)
	a.Runner.(*runner.Fake).Responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
	rep, err := a.Finish(ctx)
	if err != nil || !module.HasBlocker(rep.Problems) {
		t.Errorf("Finish = %+v, %v; want a FileVault blocker", rep, err)
	}
	if slices.Contains(calls, "finish") {
		t.Error("finish ran without FileVault")
	}
}

func TestUndoRestoresAndForgetsTheRecord(t *testing.T) {
	old := t.TempDir()
	var calls []string
	step := stepModule{name: "svc", calls: &calls}
	keep := finishOnly{name: "plain"}
	a := stepApp(old, oldUUID, step, keep)
	mustPrepare(t, a)
	rep, err := a.Undo(ctx)
	if err != nil || !slices.Equal(rep.Done, []string{"svc: restarted things"}) {
		t.Fatalf("Undo = %+v, %v", rep, err)
	}
	st, err := stage.Existing(old)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Modules["svc"]; ok {
		t.Error("undone module still has a record")
	}
	if _, ok := m.Modules["plain"]; !ok {
		t.Error("a module without Undo lost its record")
	}
	vrep, err := stepApp(migrate(t, old), newUUID, step, keep).Verify(ctx, VerifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(blockingIDs(vrep), "svc:not-prepared") {
		t.Errorf("blocking = %v, want svc:not-prepared after undo", blockingIDs(vrep))
	}
}

func TestUndoRefusesOnTheNewMac(t *testing.T) {
	old := t.TempDir()
	var calls []string
	step := stepModule{name: "svc", calls: &calls}
	mustPrepare(t, stepApp(old, oldUUID, step))
	newHome := migrate(t, old)
	if _, err := stepApp(newHome, newUUID, step).Undo(ctx); !errors.Is(err, ErrUndoOnNewMac) {
		t.Errorf("err = %v, want ErrUndoOnNewMac", err)
	}
	if slices.Contains(calls, "undo") {
		t.Error("undo ran on the new Mac")
	}
}

func TestUndoWithoutRecord(t *testing.T) {
	home := t.TempDir()
	var calls []string
	if _, err := stepApp(home, oldUUID, stepModule{name: "svc", calls: &calls}).Undo(ctx); !errors.Is(err, stage.ErrNotFound) {
		t.Errorf("err = %v, want stage.ErrNotFound", err)
	}
}
```

Add to `internal/report/report_test.go`:
```go
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
```

Add to `cmd/mtm/main_test.go`:
```go
func fileVaultOnApp(home string) *app.App {
	return &app.App{Runner: &runner.Fake{Responses: testutil.MacResponses("AAAA")}, Home: home, Version: "test", Now: time.Now}
}

func TestFinishWithoutRecordFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"finish"}, &out, &errOut, fileVaultOnApp)
	if code != exitFailed || !strings.Contains(errOut.String(), "run mtm prepare") {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestPrepareUndoRejectsOtherFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"prepare", "--undo", "--only", "git"}, &out, &errOut, fileVaultOnApp)
	if code != exitFailed || !strings.Contains(errOut.String(), "can't be combined") {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}

func TestPrepareUndoWithoutRecordFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"prepare", "--undo"}, &out, &errOut, fileVaultOnApp)
	if code != exitFailed || !strings.Contains(errOut.String(), "run mtm prepare") {
		t.Errorf("code %d stderr %q", code, errOut.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/... ./internal/report/... ./cmd/mtm/...`
Expected: FAIL with `undefined: ErrFinishOnOldMac`, `a.Finish undefined`, `undefined: Finish`, and similar. `TestPrepareShowsModulesTheirPreviousRecord` panics on a nil manifest until Step 3.

- [ ] **Step 3: Implement the module contract change**

In `internal/module/module.go`, replace the `Env` doc comment:
```go
// Env is what a module gets to work with. Manifest is the record: on the
// new Mac, the one verify and finish check against; during prepare, this
// Mac's previous record (empty on the first run); nil during scan.
```
Add after the `Module` interface:
```go
// Undoer is implemented by modules whose prepare changes the old Mac, so
// prepare --undo can put it back.
type Undoer interface {
	Undo(env Env, st *stage.Stage) (Result, error)
}
```

- [ ] **Step 4: Implement Finish and Undo in `internal/app/app.go`**

1. In `Prepare`, change `env := a.env(ctx, module.OldMac, opts.Deep, nil, run)` to `env := a.env(ctx, module.OldMac, opts.Deep, m, run)`.
2. In `Verify`, replace the `manifest.Load` block (from `m, err := manifest.Load(st)` through its two `if` checks) with:
   ```go
   	m, err := loadRecord(st)
   	if err != nil {
   		return VerifyReport{}, err
   	}
   ```
3. Add to the `var (...)` error block:
   ```go
   	ErrFinishOnOldMac = errors.New("this is the Mac that ran prepare; run mtm finish on the new Mac after Migration Assistant")
   	ErrUndoOnNewMac   = errors.New("~/.mtm holds the record from another Mac; prepare --undo only works on the Mac that ran prepare")
   ```
4. Add after `VerifyReport`:
   ```go
   // StepReport is what finish or prepare --undo did, and what is left to do
   // by hand.
   type StepReport struct {
   	Done     []string         `json:"done"`
   	Problems []module.Finding `json:"problems"`
   }

   func (r *StepReport) add(name string, res module.Result) {
   	for _, d := range res.Done {
   		r.Done = append(r.Done, name+": "+d)
   	}
   	r.Problems = append(r.Problems, res.Problems...)
   }
   ```
5. Add after `Verify`:
   ```go
   // Finish restores, on the new Mac, what prepare set aside on the old one.
   // It refuses on the old Mac, where it would restart what prepare stopped.
   func (a *App) Finish(ctx context.Context) (StepReport, error) {
   	if problems := preflight.Check(ctx, a.Runner); module.HasBlocker(problems) {
   		return StepReport{Problems: problems}, nil
   	}
   	st, err := stage.Existing(a.Home)
   	if err != nil {
   		return StepReport{}, err
   	}
   	logFile, run, err := a.openLog(st, "finish")
   	if err != nil {
   		return StepReport{}, err
   	}
   	defer logFile.Close()
   	m, err := loadRecord(st)
   	if err != nil {
   		return StepReport{}, err
   	}
   	info, err := preflight.Gather(ctx, run)
   	if err != nil {
   		return StepReport{}, err
   	}
   	if info.MachineID == m.MachineID {
   		return StepReport{}, ErrFinishOnOldMac
   	}
   	env := a.env(ctx, module.NewMac, false, m, run)
   	var rep StepReport
   	for _, mod := range a.Modules {
   		if _, ok := m.Modules[mod.Name()]; !ok {
   			continue
   		}
   		res, err := mod.Finish(env, st)
   		if err != nil {
   			rep.Problems = append(rep.Problems, moduleFailure(mod.Name(), "finish", err))
   			continue
   		}
   		rep.add(mod.Name(), res)
   	}
   	return rep, nil
   }

   // Undo puts the old Mac back in use when the migration is called off.
   // Undone modules lose their record: it describes a state that no longer
   // holds, so verify must not trust it.
   func (a *App) Undo(ctx context.Context) (StepReport, error) {
   	st, err := stage.Existing(a.Home)
   	if err != nil {
   		return StepReport{}, err
   	}
   	logFile, run, err := a.openLog(st, "undo")
   	if err != nil {
   		return StepReport{}, err
   	}
   	defer logFile.Close()
   	m, err := loadRecord(st)
   	if err != nil {
   		return StepReport{}, err
   	}
   	info, err := preflight.Gather(ctx, run)
   	if err != nil {
   		return StepReport{}, err
   	}
   	if info.MachineID != m.MachineID {
   		return StepReport{}, ErrUndoOnNewMac
   	}
   	env := a.env(ctx, module.OldMac, false, m, run)
   	var rep StepReport
   	for _, mod := range a.Modules {
   		u, ok := mod.(module.Undoer)
   		if !ok {
   			continue
   		}
   		if _, recorded := m.Modules[mod.Name()]; !recorded {
   			continue
   		}
   		res, err := u.Undo(env, st)
   		if err != nil {
   			rep.Problems = append(rep.Problems, moduleFailure(mod.Name(), "undo", err))
   			continue
   		}
   		delete(m.Modules, mod.Name())
   		rep.add(mod.Name(), res)
   	}
   	if err := manifest.Save(st, m); err != nil {
   		return rep, err
   	}
   	return rep, nil
   }

   // loadRecord reads the manifest prepare wrote, explaining a missing one.
   func loadRecord(st *stage.Stage) (*manifest.Manifest, error) {
   	m, err := manifest.Load(st)
   	if errors.Is(err, fs.ErrNotExist) {
   		return nil, fmt.Errorf("~/.mtm has no record from the old Mac; run mtm prepare there before Migration Assistant: %w", err)
   	}
   	return m, err
   }
   ```

- [ ] **Step 5: Implement the report changes in `internal/report/report.go`**

1. In `readyText`, change the last step to:
   ```
     5. When it finishes, open Terminal on the new Mac and run: mtm finish, then mtm verify
   ```
2. Replace `Prepare` with the version below and add the helpers:
   ```go
   func writeDone(w io.Writer, done []string) {
   	if len(done) == 0 {
   		return
   	}
   	fmt.Fprintln(w, "Done")
   	for _, d := range done {
   		fmt.Fprintf(w, "  ✓ %s\n", clean(d))
   	}
   	fmt.Fprintln(w)
   }

   func writeProblems(w io.Writer, title string, problems []module.Finding) {
   	if len(problems) == 0 {
   		return
   	}
   	fmt.Fprintln(w, title)
   	for _, p := range problems {
   		writeFinding(w, p)
   	}
   	fmt.Fprintln(w)
   }

   func Prepare(w io.Writer, ready bool, done []string, problems []module.Finding) {
   	writeDone(w, done)
   	writeProblems(w, "Problems", problems)
   	if !ready {
   		fmt.Fprintln(w, "NOT READY FOR MIGRATION ASSISTANT")
   		fmt.Fprintln(w, "Fix the problems marked ✗ and run mtm prepare again.")
   		return
   	}
   	fmt.Fprint(w, readyText)
   }

   // Finish prints what finish did and what is left to do by hand.
   func Finish(w io.Writer, done []string, problems []module.Finding) {
   	writeDone(w, done)
   	if module.HasBlocker(problems) {
   		writeProblems(w, "Problems", problems)
   		fmt.Fprintln(w, "Fix the problems marked ✗ and run mtm finish again.")
   		return
   	}
   	writeProblems(w, "Do these by hand", problems)
   	fmt.Fprintln(w, "Next: run mtm verify")
   }

   // Undo prints what prepare --undo put back.
   func Undo(w io.Writer, done []string, problems []module.Finding) {
   	writeDone(w, done)
   	writeProblems(w, "Problems", problems)
   	fmt.Fprintln(w, "This Mac is back in use. Run mtm prepare again before Migration Assistant.")
   }
   ```

- [ ] **Step 6: Implement the CLI changes in `cmd/mtm/main.go`**

1. Replace `usage` with:
   ```go
   const usage = `mtm moves a developer's Mac to a new Mac, alongside Migration Assistant.

   Usage:
     mtm scan [--json]                         check this Mac before migrating (changes nothing)
     mtm prepare [--only a,b] [--deep]         record this Mac before Migration Assistant
     mtm prepare --undo                        put this Mac back in use if you call off the migration
     mtm finish                                on the new Mac: restore what prepare set aside
     mtm verify [--only a,b] [--deep] [--details] [--json]
                                               check the new Mac against the record
     mtm cleanup [--force]                     delete ~/.mtm after SAFE TO WIPE
     mtm version
   `
   ```
2. Add `"finish": cmdFinish,` to `commands`.
3. In `cmdPrepare`, after the `deep` flag, add `undo := flags.Bool("undo", false, "put this Mac back in use: restart what prepare stopped")`. After `flags.Parse` succeeds, add:
   ```go
   	if *undo {
   		if *only != "" || *deep {
   			fmt.Fprintln(stderr, "mtm prepare: --undo can't be combined with --only or --deep")
   			return exitFailed
   		}
   		return runUndo(ctx, a, stdout, stderr)
   	}
   ```
4. Add:
   ```go
   func runUndo(ctx context.Context, a *app.App, stdout, stderr io.Writer) int {
   	rep, err := a.Undo(ctx)
   	switch {
   	case errors.Is(err, app.ErrUndoOnNewMac):
   		fmt.Fprintln(stderr, "mtm prepare --undo:", err)
   		return exitProblems
   	case err != nil:
   		fmt.Fprintln(stderr, "mtm prepare --undo:", err)
   		return exitFailed
   	}
   	report.Undo(stdout, rep.Done, rep.Problems)
   	if len(rep.Problems) > 0 {
   		return exitProblems
   	}
   	return exitOK
   }

   func cmdFinish(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
   	flags := flag.NewFlagSet("finish", flag.ContinueOnError)
   	flags.SetOutput(stderr)
   	if err := flags.Parse(args); err != nil {
   		return exitFailed
   	}
   	rep, err := a.Finish(ctx)
   	switch {
   	case errors.Is(err, app.ErrFinishOnOldMac):
   		fmt.Fprintln(stderr, "mtm finish:", err)
   		return exitProblems
   	case err != nil:
   		fmt.Fprintln(stderr, "mtm finish:", err)
   		return exitFailed
   	}
   	report.Finish(stdout, rep.Done, rep.Problems)
   	if len(rep.Problems) > 0 {
   		return exitProblems
   	}
   	return exitOK
   }
   ```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/app/... ./internal/report/... ./cmd/mtm/...`
Expected: PASS. The Stage 1 tests in those packages still pass.

- [ ] **Step 8: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/module internal/app internal/report cmd/mtm
git commit -m "Add mtm finish and prepare --undo

finish restores on the new Mac what prepare set aside, and refuses on
the old Mac where it would restart stopped services. --undo puts the
old Mac back and drops the undone records so verify can't trust them.
prepare now shows modules their previous record so re-runs can carry
state forward.

Co-Authored-By: <your model> <noreply@anthropic.com>"
```

---

### Task 3: App inventory (`internal/apps`)

**Files:**
- Create: `internal/apps/apps.go`, `internal/apps/apps_test.go`

**Interfaces:**
- Consumes: `macos.PlistValue`, `macos.Archs` (Task 1); `runner.Runner`, `runner.Fake` (Stage 1); `testutil.WriteFile` (Stage 1).
- Produces:
  - `apps.App{Path, BundleID, Version string; Archs []string}` with `(App).IntelOnly() bool`.
  - `apps.Root{Dir, Display string}`.
  - `apps.List(ctx, runner.Runner, roots []Root) ([]App, error)`.
  - `apps.Compare(a, b string) (int, bool)`.

- [ ] **Step 1: Write the failing tests**

`internal/apps/apps_test.go`:
```go
package apps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/momentmaker/mtm/internal/runner"
)

var ctx = context.Background()

type fakeApps struct {
	root string
	fake *runner.Fake
}

func newFakeApps(t *testing.T) fakeApps {
	return fakeApps{root: t.TempDir(), fake: &runner.Fake{Responses: map[string]runner.Response{}}}
}

// add creates an app bundle folder and the plutil and lipo answers for it.
// An empty value makes plutil report the key as absent.
func (f fakeApps) add(t *testing.T, rel, id, version, exe, archs string) {
	t.Helper()
	dir := filepath.Join(f.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Join(dir, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(dir, "Contents", "Info.plist")
	for key, val := range map[string]string{"CFBundleIdentifier": id, "CFBundleShortVersionString": version, "CFBundleExecutable": exe} {
		resp := runner.Response{Stdout: val + "\n"}
		if val == "" {
			resp = runner.Response{Code: 1, Stderr: "Could not extract value"}
		}
		f.fake.Responses["/usr/bin/plutil -extract "+key+" raw -o - "+plist] = resp
	}
	if exe != "" {
		resp := runner.Response{Stdout: archs + "\n"}
		if archs == "" {
			resp = runner.Response{Code: 1, Stderr: "not a Mach-O file"}
		}
		f.fake.Responses["/usr/bin/lipo -archs "+filepath.Join(dir, "Contents", "MacOS", exe)] = resp
	}
}

func TestListFindsAppsAndSkipsApple(t *testing.T) {
	f := newFakeApps(t)
	f.add(t, "Ghostty.app", "com.mitchellh.ghostty", "1.4.0", "ghostty", "x86_64 arm64")
	f.add(t, "Utilities/Tool.app", "com.example.tool", "2.0", "tool", "x86_64")
	f.add(t, "Safari.app", "com.apple.Safari", "26.0", "Safari", "arm64")
	got, err := List(ctx, f.fake, []Root{{Dir: f.root, Display: "/Applications"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("apps = %+v, want Ghostty and Tool", got)
	}
	if got[0].Path != "/Applications/Ghostty.app" || got[0].Version != "1.4.0" || got[0].IntelOnly() {
		t.Errorf("Ghostty = %+v", got[0])
	}
	if got[1].Path != "/Applications/Utilities/Tool.app" || !got[1].IntelOnly() {
		t.Errorf("Tool = %+v", got[1])
	}
}

func TestListHandlesOddBundles(t *testing.T) {
	f := newFakeApps(t)
	f.add(t, "NoVersion.app", "com.example.noversion", "", "nv", "arm64")
	f.add(t, "Script.app", "com.example.script", "1.0", "run.sh", "")
	f.add(t, "NoID.app", "", "1.0", "x", "arm64")
	f.add(t, "Vendor/Inner.app", "com.vendor.inner", "3.1", "inner", "arm64")
	locked := filepath.Join(f.root, "Locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(locked, 0)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	if err := os.Symlink(filepath.Join(f.root, "NoVersion.app"), filepath.Join(f.root, "Linked.app")); err != nil {
		t.Fatal(err)
	}
	got, err := List(ctx, f.fake, []Root{{Dir: f.root, Display: "/Applications"}})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]App{}
	for _, a := range got {
		byPath[a.Path] = a
	}
	if len(got) != 3 {
		t.Errorf("apps = %+v, want NoVersion, Script, Vendor/Inner", got)
	}
	if a := byPath["/Applications/NoVersion.app"]; a.BundleID != "com.example.noversion" || a.Version != "" {
		t.Errorf("NoVersion = %+v", a)
	}
	if a := byPath["/Applications/Script.app"]; a.Archs != nil || a.IntelOnly() {
		t.Errorf("Script = %+v, want no archs and not Intel-only", a)
	}
	if _, ok := byPath["/Applications/Vendor/Inner.app"]; !ok {
		t.Error("app in a vendor folder not listed")
	}
}

func TestListMissingRootIsEmpty(t *testing.T) {
	got, err := List(ctx, &runner.Fake{}, []Root{{Dir: filepath.Join(t.TempDir(), "nope"), Display: "~/Applications"}})
	if err != nil || len(got) != 0 {
		t.Errorf("List = %+v, %v", got, err)
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"1.10", "1.9", 1, true},
		{"1.9", "1.10", -1, true},
		{"2.0", "2", 0, true},
		{"1.4.0 (build 7)", "1.4.0 (build 12)", -1, true},
		{"26.0.1", "026.0.1", 0, true},
		{"beta", "1.0", 0, false},
		{"", "1.0", 0, false},
	}
	for _, tt := range tests {
		got, ok := Compare(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/apps/...`
Expected: FAIL with `undefined: List`, `undefined: Root`, `undefined: Compare`.

- [ ] **Step 3: Implement the inventory**

`internal/apps/apps.go`:
```go
// Package apps lists the applications installed on a Mac.
package apps

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/runner"
)

type App struct {
	Path     string   `json:"path"`
	BundleID string   `json:"bundle_id"`
	Version  string   `json:"version,omitempty"`
	Archs    []string `json:"archs,omitempty"`
}

// IntelOnly reports an app that needs Rosetta on Apple Silicon.
func (a App) IntelOnly() bool {
	return len(a.Archs) > 0 && !slices.Contains(a.Archs, "arm64")
}

// Root is a folder of applications. Display replaces Dir in recorded paths,
// so ~/Applications compares equal across home folders.
type Root struct {
	Dir     string
	Display string
}

// List finds .app bundles in each root and one level of subfolders. It
// skips symlinks and Apple's own apps, which come with macOS.
func List(ctx context.Context, r runner.Runner, roots []Root) ([]App, error) {
	var out []App
	for _, root := range roots {
		bundles, err := bundlesIn(root.Dir)
		if err != nil {
			return nil, err
		}
		for _, rel := range bundles {
			app, ok, err := inspect(ctx, r, root, rel)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, app)
			}
		}
	}
	return out, nil
}

func bundlesIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var rels []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".app") {
			rels = append(rels, e.Name())
			continue
		}
		sub, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			// A vendor folder we can't read holds no app we could inspect.
			continue
		}
		for _, s := range sub {
			if s.IsDir() && strings.HasSuffix(s.Name(), ".app") {
				rels = append(rels, e.Name()+"/"+s.Name())
			}
		}
	}
	return rels, nil
}

func inspect(ctx context.Context, r runner.Runner, root Root, rel string) (App, bool, error) {
	dir := filepath.Join(root.Dir, filepath.FromSlash(rel))
	plist := filepath.Join(dir, "Contents", "Info.plist")
	id, err := macos.PlistValue(ctx, r, plist, "CFBundleIdentifier")
	if err != nil {
		return App{}, false, err
	}
	if id == "" || strings.HasPrefix(id, "com.apple.") {
		return App{}, false, nil
	}
	version, err := macos.PlistValue(ctx, r, plist, "CFBundleShortVersionString")
	if err != nil {
		return App{}, false, err
	}
	exe, err := macos.PlistValue(ctx, r, plist, "CFBundleExecutable")
	if err != nil {
		return App{}, false, err
	}
	app := App{Path: root.Display + "/" + rel, BundleID: id, Version: version}
	if exe != "" {
		app.Archs = macos.Archs(ctx, r, filepath.Join(dir, "Contents", "MacOS", exe))
	}
	return app, true, nil
}

var digits = regexp.MustCompile(`\d+`)

// Compare orders version strings by their numeric parts, so "1.10" is newer
// than "1.9". ok is false when either has no digits; then only equality of
// the raw strings means anything.
func Compare(a, b string) (int, bool) {
	pa, pb := digits.FindAllString(a, -1), digits.FindAllString(b, -1)
	if len(pa) == 0 || len(pb) == 0 {
		return 0, false
	}
	for i := range max(len(pa), len(pb)) {
		x, y := part(pa, i), part(pb, i)
		if len(x) != len(y) {
			return cmp.Compare(len(x), len(y)), true
		}
		if c := strings.Compare(x, y); c != 0 {
			return c, true
		}
	}
	return 0, true
}

func part(parts []string, i int) string {
	if i < len(parts) {
		return strings.TrimLeft(parts[i], "0")
	}
	return ""
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/apps/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/apps
git commit -m "Add the app inventory

Apps in /Applications and ~/Applications (one folder deep) are recorded
by bundle id, version, and architectures, so verify can tell a missing
or downgraded app and finish can tell when Rosetta is needed.

Co-Authored-By: <your model> <noreply@anthropic.com>"
```

---

### Task 4: `system` module

**Files:**
- Create: `internal/modules/system/system.go`, `internal/modules/system/verify.go`, `internal/modules/system/system_test.go`

**Interfaces:**
- Consumes: `apps.List`, `apps.App`, `apps.Root`, `apps.Compare` (Task 3); `macos.LoginShell`, `macos.ShellQuote`, `macos.RosettaPath` (Task 1); `fsinfo.HashBytes` (Task 1); `words.Count` (Task 1); `module.*`, `stage.Stage`, `manifest.New`, `runner.Fake`, `testutil.WriteFile` (Stage 1).
- Produces:
  - `system.Name = "system"`, `system.New(user string) Module`, `system.DefaultAppRoots(home) []apps.Root`.
  - `system.Module{User, EtcDir string; AppRoots func(string) []apps.Root; RosettaPath string}`.
  - `system.Inventory`.
  - Check IDs: `system:shell`, `system:etc`, `system:apps`, and `system:rosetta` (only when Intel-only apps exist).
  - Copies of `/etc` files at `exports/etc/<path>` in `~/.mtm`.

- [ ] **Step 1: Write the failing tests**

`internal/modules/system/system_test.go`:
```go
package system

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/apps"
	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

const user = "dev"

type fixture struct {
	home, etc, apps, shell, rosetta string
	fake                            *runner.Fake
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	f := fixture{
		home:    filepath.Join(root, "my home"),
		etc:     filepath.Join(root, "etc"),
		apps:    filepath.Join(root, "Applications"),
		shell:   filepath.Join(root, "bin", "fish"),
		rosetta: filepath.Join(root, "rosetta"),
		fake:    &runner.Fake{Responses: map[string]runner.Response{}},
	}
	testutil.WriteFile(t, f.shell, "#!/bin/sh\n")
	if err := os.Chmod(f.shell, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(f.etc, "hosts"), "127.0.0.1 localhost\n")
	testutil.WriteFile(t, filepath.Join(f.etc, "shells"), "# shells\n/bin/zsh\n"+f.shell+"\n")
	testutil.WriteFile(t, filepath.Join(f.etc, "paths.d", "MacGPG2"), "/usr/local/MacGPG2/bin\n")
	if err := os.MkdirAll(f.home, 0o755); err != nil {
		t.Fatal(err)
	}
	f.setShell(f.shell)
	f.addApp(t, "Ghostty.app", "com.mitchellh.ghostty", "1.4.0", "ghostty", "x86_64 arm64")
	f.addApp(t, "sioyek.app", "info.sioyek.sioyek", "2.0.0", "sioyek", "x86_64")
	return f
}

func (f fixture) setShell(shell string) {
	f.fake.Responses["/usr/bin/dscl . -read /Users/"+user+" UserShell"] = runner.Response{Stdout: "UserShell: " + shell + "\n"}
}

func (f fixture) addApp(t *testing.T, rel, id, version, exe, archs string) {
	t.Helper()
	dir := filepath.Join(f.apps, rel)
	if err := os.MkdirAll(filepath.Join(dir, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(dir, "Contents", "Info.plist")
	for key, val := range map[string]string{"CFBundleIdentifier": id, "CFBundleShortVersionString": version, "CFBundleExecutable": exe} {
		f.fake.Responses["/usr/bin/plutil -extract "+key+" raw -o - "+plist] = runner.Response{Stdout: val + "\n"}
	}
	f.fake.Responses["/usr/bin/lipo -archs "+filepath.Join(dir, "Contents", "MacOS", exe)] = runner.Response{Stdout: archs + "\n"}
}

func (f fixture) module() Module {
	return Module{
		User:        user,
		EtcDir:      f.etc,
		AppRoots:    func(string) []apps.Root { return []apps.Root{{Dir: f.apps, Display: "/Applications"}} },
		RosettaPath: f.rosetta,
	}
}

func (f fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func (f fixture) prepare(t *testing.T) (*manifest.Manifest, *stage.Stage, module.Result) {
	t.Helper()
	st, err := stage.Open(f.home)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.New("test")
	res, err := f.module().Prepare(f.env(m), st)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetModule(Name, time.Now(), res.Inventory); err != nil {
		t.Fatal(err)
	}
	return m, st, res
}

func (f fixture) verify(t *testing.T, m *manifest.Manifest, st *stage.Stage) map[string]module.Check {
	t.Helper()
	checks, err := f.module().Verify(f.env(m), st)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]module.Check{}
	for _, c := range checks {
		byID[c.ID] = c
	}
	return byID
}

func TestPrepareRecordsShellEtcAndApps(t *testing.T) {
	f := newFixture(t)
	_, st, res := f.prepare(t)
	inv := res.Inventory.(Inventory)
	if inv.Shell != f.shell || !inv.ShellListed || len(inv.Apps) != 2 {
		t.Errorf("inventory = %+v", inv)
	}
	keys := slices.Sorted(maps.Keys(inv.Etc))
	if want := []string{"hosts", "paths.d/MacGPG2", "shells"}; !slices.Equal(keys, want) {
		t.Errorf("etc files = %q, want %q", keys, want)
	}
	copied, err := os.ReadFile(st.Path("exports", "etc", "hosts"))
	if err != nil || string(copied) != "127.0.0.1 localhost\n" {
		t.Errorf("copied hosts = %q, %v", copied, err)
	}
	if !slices.ContainsFunc(res.Problems, func(p module.Finding) bool {
		return p.ID == "intel-only" && strings.Contains(p.Detail, "sioyek")
	}) {
		t.Errorf("problems = %+v, want an Intel-only warning naming sioyek", res.Problems)
	}
}

func TestVerifyPassesWhenNothingChanged(t *testing.T) {
	f := newFixture(t)
	testutil.WriteFile(t, f.rosetta, "")
	m, st, _ := f.prepare(t)
	for id, c := range f.verify(t, m, st) {
		if c.Status != module.StatusPass {
			t.Errorf("%s: %s (%s)", id, c.Status, c.Detail)
		}
	}
}

func TestVerifyReportsChanges(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, f fixture)
		id     string
		want   module.Status
	}{
		{"edited /etc/hosts", func(t *testing.T, f fixture) {
			testutil.WriteFile(t, filepath.Join(f.etc, "hosts"), "10.0.0.1 nas\n")
		}, "system:etc", module.StatusWarn},
		{"app missing", func(t *testing.T, f fixture) {
			os.RemoveAll(filepath.Join(f.apps, "sioyek.app"))
		}, "system:apps", module.StatusFail},
		{"older app", func(t *testing.T, f fixture) {
			f.addApp(t, "Ghostty.app", "com.mitchellh.ghostty", "1.3.9", "ghostty", "x86_64 arm64")
		}, "system:apps", module.StatusWarn},
		{"shell changed", func(t *testing.T, f fixture) { f.setShell("/bin/zsh") }, "system:shell", module.StatusWarn},
		{"shell missing", func(t *testing.T, f fixture) { os.Remove(f.shell) }, "system:shell", module.StatusFail},
		{"no Rosetta", func(t *testing.T, f fixture) { os.Remove(f.rosetta) }, "system:rosetta", module.StatusWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			testutil.WriteFile(t, f.rosetta, "")
			m, st, _ := f.prepare(t)
			tt.change(t, f)
			if c := f.verify(t, m, st)[tt.id]; c.Status != tt.want {
				t.Errorf("%s = %s (%s), want %s", tt.id, c.Status, c.Detail, tt.want)
			}
		})
	}
}

func TestFinishPrintsRestoreCommands(t *testing.T) {
	f := newFixture(t)
	m, st, _ := f.prepare(t)
	testutil.WriteFile(t, filepath.Join(f.etc, "hosts"), "10.0.0.1 nas\n")
	f.setShell("/bin/zsh")
	res, err := f.module().Finish(f.env(m), st)
	if err != nil {
		t.Fatal(err)
	}
	fixes := map[string]string{}
	for _, p := range res.Problems {
		fixes[p.ID] = p.Fix
	}
	if want := "sudo cp '" + st.Path("exports", "etc", "hosts") + "' /etc/hosts"; !strings.Contains(fixes["etc:hosts"], want) {
		t.Errorf("hosts fix = %q, want it to contain %q", fixes["etc:hosts"], want)
	}
	if !strings.Contains(fixes["rosetta"], "softwareupdate --install-rosetta") {
		t.Errorf("rosetta fix = %q", fixes["rosetta"])
	}
	if want := "chsh -s '" + f.shell + "'"; !strings.Contains(fixes["shell"], want) {
		t.Errorf("shell fix = %q, want %q", fixes["shell"], want)
	}
}

func TestFinishWithNothingToDo(t *testing.T) {
	f := newFixture(t)
	testutil.WriteFile(t, f.rosetta, "")
	m, st, _ := f.prepare(t)
	res, err := f.module().Finish(f.env(m), st)
	if err != nil || len(res.Problems) != 0 || len(res.Done) != 1 {
		t.Errorf("Finish = %+v, %v", res, err)
	}
}

func TestScanSummarizes(t *testing.T) {
	f := newFixture(t)
	findings, err := f.module().Scan(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, fd := range findings {
		ids = append(ids, fd.ID)
	}
	if want := []string{"apps", "shell", "intel-only"}; !slices.Equal(ids, want) {
		t.Errorf("finding ids = %q, want %q", ids, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/system/...`
Expected: FAIL with `undefined: Module`, `undefined: Inventory`, and similar.

- [ ] **Step 3: Implement the module**

`internal/modules/system/system.go`:
```go
// Package system records the parts of the Mac outside the home folder that
// a developer relies on: the login shell, edited /etc files, and installed
// apps, including whether they need Rosetta.
package system

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/apps"
	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const (
	Name      = "system"
	etcExport = "exports/etc"
)

// etcFiles are the /etc files developers commonly edit. Every file in
// /etc/paths.d is added when recording.
var etcFiles = []string{"hosts", "shells", "pam.d/sudo_local"}

type Module struct {
	User        string
	EtcDir      string
	AppRoots    func(home string) []apps.Root
	RosettaPath string
}

func New(user string) Module {
	return Module{User: user, EtcDir: "/etc", AppRoots: DefaultAppRoots, RosettaPath: macos.RosettaPath}
}

func DefaultAppRoots(home string) []apps.Root {
	return []apps.Root{
		{Dir: "/Applications", Display: "/Applications"},
		{Dir: filepath.Join(home, "Applications"), Display: "~/Applications"},
	}
}

func (Module) Name() string { return Name }

// Inventory maps /etc files, relative to /etc, to their fingerprints.
type Inventory struct {
	Shell       string            `json:"shell"`
	ShellListed bool              `json:"shell_listed"`
	Etc         map[string]string `json:"etc"`
	Apps        []apps.App        `json:"apps"`
}

type snapshot struct {
	Inventory
	etcData map[string][]byte
}

func (m Module) snapshot(env module.Env) (snapshot, error) {
	shell, err := macos.LoginShell(env.Ctx, env.Run, m.User)
	if err != nil {
		return snapshot{}, err
	}
	listed, err := m.shellListed(shell)
	if err != nil {
		return snapshot{}, err
	}
	etc, data, err := m.readEtc()
	if err != nil {
		return snapshot{}, err
	}
	list, err := apps.List(env.Ctx, env.Run, m.AppRoots(env.Home))
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{Inventory: Inventory{Shell: shell, ShellListed: listed, Etc: etc, Apps: list}, etcData: data}, nil
}

func (m Module) shellListed(shell string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(m.EtcDir, "shells"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read /etc/shells: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == shell {
			return true, nil
		}
	}
	return false, nil
}

func (m Module) readEtc() (map[string]string, map[string][]byte, error) {
	rels := slices.Clone(etcFiles)
	entries, err := os.ReadDir(filepath.Join(m.EtcDir, "paths.d"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("read /etc/paths.d: %w", err)
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			rels = append(rels, "paths.d/"+e.Name())
		}
	}
	hashes, data := map[string]string{}, map[string][]byte{}
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(m.EtcDir, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read /etc/%s: %w", rel, err)
		}
		hashes[rel], data[rel] = fsinfo.HashBytes(b), b
	}
	return hashes, data, nil
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	snap, err := m.snapshot(env)
	if err != nil {
		return nil, err
	}
	findings := []module.Finding{
		{
			Module: Name, ID: "apps", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
			Title:  words.Count(len(snap.Apps), "app", "apps") + " in /Applications and ~/Applications",
			Detail: "Migration Assistant copies them. mtm records each one and checks it arrived.",
		},
		{
			Module: Name, ID: "shell", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
			Title: "Login shell: " + snap.Shell,
		},
	}
	if f, ok := intelOnly(snap.Apps); ok {
		findings = append(findings, f)
	}
	return findings, nil
}

func (m Module) Prepare(env module.Env, st *stage.Stage) (module.Result, error) {
	snap, err := m.snapshot(env)
	if err != nil {
		return module.Result{}, err
	}
	for rel, b := range snap.etcData {
		if err := st.WriteFile(filepath.Join(etcExport, filepath.FromSlash(rel)), b); err != nil {
			return module.Result{}, err
		}
	}
	res := module.Result{
		Inventory: snap.Inventory,
		Done: []string{fmt.Sprintf("recorded login shell %s, %s, and %s", snap.Shell,
			words.Count(len(snap.Etc), "/etc file", "/etc files"), words.Count(len(snap.Apps), "app", "apps"))},
	}
	if f, ok := intelOnly(snap.Apps); ok {
		res.Problems = append(res.Problems, f)
	}
	return res, nil
}

func (m Module) Finish(env module.Env, st *stage.Stage) (module.Result, error) {
	var old Inventory
	if _, err := module.LoadInventory(env, Name, &old); err != nil {
		return module.Result{}, err
	}
	now, err := m.snapshot(env)
	if err != nil {
		return module.Result{}, err
	}
	var res module.Result
	for _, rel := range slices.Sorted(maps.Keys(old.Etc)) {
		if now.Etc[rel] == old.Etc[rel] {
			continue
		}
		res.Problems = append(res.Problems, byHand("etc:"+rel, "/etc/"+rel+" differs from the old Mac",
			"Review the old copy, then restore it with: sudo cp "+macos.ShellQuote(st.Path(etcExport, filepath.FromSlash(rel)))+" /etc/"+rel))
	}
	if hasIntelOnly(old.Apps) && !m.rosettaInstalled() {
		res.Problems = append(res.Problems, byHand("rosetta", "Intel-only apps need Rosetta", "Run: sudo softwareupdate --install-rosetta --agree-to-license"))
	}
	if old.Shell != "" && now.Shell != old.Shell {
		listed, err := m.shellListed(old.Shell)
		if err != nil {
			return module.Result{}, err
		}
		fix := "Run: chsh -s " + macos.ShellQuote(old.Shell)
		if !listed {
			fix = "Run: echo " + macos.ShellQuote(old.Shell) + " | sudo tee -a /etc/shells, then: chsh -s " + macos.ShellQuote(old.Shell)
		}
		res.Problems = append(res.Problems, byHand("shell", "Your login shell is "+now.Shell+", not "+old.Shell, fix))
	}
	if len(res.Problems) == 0 {
		res.Done = []string{"login shell, /etc files, and Rosetta are as they were"}
	}
	return res, nil
}

func intelOnly(list []apps.App) (module.Finding, bool) {
	var names []string
	for _, a := range list {
		if a.IntelOnly() {
			names = append(names, appName(a))
		}
	}
	if len(names) == 0 {
		return module.Finding{}, false
	}
	return module.Finding{
		Module: Name, ID: "intel-only", Severity: module.SeverityWarn, Handler: module.Manual,
		Title:  words.Count(len(names), "app is Intel-only and needs", "apps are Intel-only and need") + " Rosetta on the new Mac",
		Detail: strings.Join(names, ", "),
		Fix:    "mtm finish prints the Rosetta install command. Rosetta is limited after macOS 27, so look for Apple Silicon versions.",
	}, true
}

func hasIntelOnly(list []apps.App) bool { return slices.ContainsFunc(list, apps.App.IntelOnly) }

func appName(a apps.App) string { return strings.TrimSuffix(path.Base(a.Path), ".app") }

func (m Module) rosettaInstalled() bool {
	_, err := os.Stat(m.RosettaPath)
	return err == nil
}

func byHand(id, title, fix string) module.Finding {
	return module.Finding{Module: Name, ID: id, Severity: module.SeverityWarn, Handler: module.Manual, Title: title, Fix: fix}
}
```

`internal/modules/system/verify.go`:
```go
package system

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/apps"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

func (m Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var old Inventory
	if _, err := module.LoadInventory(env, Name, &old); err != nil {
		return nil, err
	}
	now, err := m.snapshot(env)
	if err != nil {
		return nil, err
	}
	checks := []module.Check{shellCheck(old, now.Inventory), etcCheck(old, now.Inventory), appsCheck(old, now.Inventory)}
	if c, ok := m.rosettaCheck(old); ok {
		checks = append(checks, c)
	}
	return checks, nil
}

func check(id, title string) module.Check {
	return module.Check{Module: Name, ID: Name + ":" + id, Status: module.StatusPass, Title: title}
}

func shellCheck(old, now Inventory) module.Check {
	c := check("shell", "login shell")
	c.Detail = now.Shell
	switch {
	case old.Shell == "":
	case !executable(old.Shell):
		c.Status, c.Detail = module.StatusFail, old.Shell+" is missing; reinstall it, or you can't open a terminal with your shell"
	case now.Shell != old.Shell:
		c.Status, c.Detail = module.StatusWarn, fmt.Sprintf("now %s (was %s); mtm finish prints the command to switch back", now.Shell, old.Shell)
	case !now.ShellListed:
		c.Status, c.Detail = module.StatusWarn, now.Shell+" is not listed in /etc/shells; mtm finish prints the fix"
	}
	return c
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func etcCheck(old, now Inventory) module.Check {
	c := check("etc", "/etc files")
	var changed []string
	for _, rel := range slices.Sorted(maps.Keys(old.Etc)) {
		if now.Etc[rel] != old.Etc[rel] {
			changed = append(changed, "/etc/"+rel)
		}
	}
	c.Detail = words.Count(len(old.Etc), "file matches", "files match")
	if len(changed) > 0 {
		c.Status, c.Paths = module.StatusWarn, changed
		c.Detail = words.Count(len(changed), "file differs", "files differ") + " from the old Mac; mtm finish prints the commands to restore them"
	}
	return c
}

// appsCheck fails on a missing app: a discontinued or paid app may be
// impossible to get again once the old Mac is erased.
func appsCheck(old, now Inventory) module.Check {
	c := check("apps", "apps")
	byID := map[string]apps.App{}
	for _, a := range now.Apps {
		byID[a.BundleID] = a
	}
	var missing, older []string
	for _, a := range old.Apps {
		cur, ok := byID[a.BundleID]
		if !ok {
			missing = append(missing, a.Path)
			continue
		}
		if order, ok := apps.Compare(cur.Version, a.Version); ok && order < 0 {
			older = append(older, fmt.Sprintf("%s (%s, was %s)", appName(a), cur.Version, a.Version))
		}
	}
	c.Detail = words.Count(len(old.Apps)-len(missing)-len(older), "app matches", "apps match")
	if len(older) > 0 {
		c.Status = module.StatusWarn
		c.Detail += "; older: " + strings.Join(older, ", ")
	}
	if len(missing) > 0 {
		c.Status, c.Paths = module.StatusFail, missing
		c.Detail += "; missing: " + words.Count(len(missing), "app", "apps") + " (copy or reinstall them before you erase the old Mac)"
	}
	return c
}

func (m Module) rosettaCheck(old Inventory) (module.Check, bool) {
	if !hasIntelOnly(old.Apps) {
		return module.Check{}, false
	}
	c := check("rosetta", "Rosetta")
	c.Detail = "installed"
	if !m.rosettaInstalled() {
		c.Status, c.Detail = module.StatusWarn, "not installed, so Intel-only apps won't open; mtm finish prints the install command"
	}
	return c, true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/modules/system/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/modules/system
git commit -m "Add the system module

Records the login shell, edited /etc files (with copies so finish can
print an exact restore command), and every app with its architectures.
verify fails on a missing app, since it may be impossible to get again
once the old Mac is erased.

Co-Authored-By: <your model> <noreply@anthropic.com>"
```

---

### Task 5: `homebrew` module

**Files:**
- Create: `internal/modules/homebrew/homebrew.go`, `internal/modules/homebrew/homebrew_test.go`

**Interfaces:**
- Consumes: `module.Undoer`, and `Env.Manifest` holding the previous record during prepare (Task 2); `macos.ShellQuote`, `words.Count` (Task 1); `module.*`, `stage.Stage`, `manifest.New`, `runner.Fake`, `runner.ExitCode`, `testutil.WriteFile` (Stage 1).
- Produces:
  - `homebrew.Name = "homebrew"`, `homebrew.BrewfileName = "Brewfile"`, `homebrew.New(user) Module`.
  - `homebrew.Module{Brew, User string}`, which implements `module.Module` and `module.Undoer`.
  - `homebrew.Inventory{Installed bool; Services []Service; Stopped, Root []string; Brewfile bool}`.
  - Check IDs: `homebrew:installed`, `homebrew:packages`, `homebrew:services`.

- [ ] **Step 1: Write the failing tests**

`internal/modules/homebrew/homebrew_test.go`:
```go
package homebrew

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
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

// listJSON is brew services list --json as captured on macOS 26, with
// postgresql@16 running as the user and unbound running as root.
const listJSON = `[
  {"name": "atuin", "status": "none", "user": null, "file": "/opt/homebrew/opt/atuin/sh.brew.atuin.plist", "exit_code": null},
  {"name": "postgresql@16", "status": "started", "user": "dev", "file": "/Users/dev/Library/LaunchAgents/homebrew.mxcl.postgresql@16.plist", "exit_code": 0},
  {"name": "unbound", "status": "started", "user": "root", "file": "/Library/LaunchDaemons/homebrew.mxcl.unbound.plist", "exit_code": 0}
]`

const allStoppedJSON = `[
  {"name": "atuin", "status": "none", "user": null},
  {"name": "postgresql@16", "status": "none", "user": null},
  {"name": "unbound", "status": "none", "user": null}
]`

type fixture struct {
	home string
	mod  Module
	fake *runner.Fake
	st   *stage.Stage
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	brew := filepath.Join(root, "homebrew", "bin", "brew")
	testutil.WriteFile(t, brew, "")
	home := filepath.Join(root, "home")
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{home: home, mod: Module{Brew: brew, User: "dev"}, fake: &runner.Fake{Responses: map[string]runner.Response{}}, st: st}
	f.respond("services list --json", runner.Response{Stdout: listJSON})
	f.respond("bundle dump --file=- --formula --cask --tap --mas", runner.Response{Stdout: "tap \"homebrew/core\"\nbrew \"git\"\n"})
	f.respond("services stop postgresql@16", runner.Response{})
	f.respond("services start postgresql@16", runner.Response{})
	return f
}

func (f fixture) respond(args string, r runner.Response) {
	f.fake.Responses[f.mod.Brew+" "+args] = r
}

func (f fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func (f fixture) ran(args string) bool {
	return slices.ContainsFunc(f.fake.Calls(), func(c runner.Command) bool { return c.String() == f.mod.Brew+" "+args })
}

func record(t *testing.T, inv Inventory) *manifest.Manifest {
	t.Helper()
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), inv); err != nil {
		t.Fatal(err)
	}
	return m
}

func ids(findings []module.Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.ID)
	}
	return out
}

func TestScanReportsServices(t *testing.T) {
	f := newFixture(t)
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"homebrew", "services", "root-services"}; !slices.Equal(ids(findings), want) {
		t.Fatalf("ids = %q, want %q", ids(findings), want)
	}
	if !strings.Contains(findings[1].Title, "postgresql@16") || !strings.Contains(findings[2].Fix, "sudo brew services stop unbound") {
		t.Errorf("findings = %+v", findings)
	}
}

func TestScanWithoutHomebrew(t *testing.T) {
	f := newFixture(t)
	f.mod.Brew = filepath.Join(t.TempDir(), "nope")
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil || len(findings) != 1 || findings[0].ID != "not-installed" {
		t.Errorf("Scan = %+v, %v", findings, err)
	}
}

func TestPrepareStopsUserServicesAndSavesBrewfile(t *testing.T) {
	f := newFixture(t)
	res, err := f.mod.Prepare(f.env(manifest.New("test")), f.st)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	if !slices.Equal(inv.Stopped, []string{"postgresql@16"}) || !slices.Equal(inv.Root, []string{"unbound"}) || !inv.Brewfile {
		t.Errorf("inventory = %+v", inv)
	}
	if !f.ran("services stop postgresql@16") {
		t.Error("postgresql@16 was not stopped")
	}
	if f.ran("services stop unbound") {
		t.Error("mtm tried to stop a root service itself")
	}
	brewfile, err := os.ReadFile(f.st.Path(BrewfileName))
	if err != nil || !strings.Contains(string(brewfile), `brew "git"`) {
		t.Errorf("Brewfile = %q, %v", brewfile, err)
	}
	if !slices.Contains(ids(res.Problems), "root-services") || module.HasBlocker(res.Problems) {
		t.Errorf("problems = %+v, want only the root-services warning", res.Problems)
	}
}

func TestPrepareStopFailureBlocks(t *testing.T) {
	f := newFixture(t)
	f.respond("services stop postgresql@16", runner.Response{Code: 1, Stderr: "Error: Failure while executing"})
	res, err := f.mod.Prepare(f.env(manifest.New("test")), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if !module.HasBlocker(res.Problems) || slices.Contains(res.Inventory.(Inventory).Stopped, "postgresql@16") {
		t.Errorf("Prepare = %+v, want a blocker and postgresql@16 not recorded as stopped", res)
	}
}

func TestPrepareBrewfileFailureWarns(t *testing.T) {
	f := newFixture(t)
	f.respond("bundle dump --file=- --formula --cask --tap --mas", runner.Response{Code: 1, Stderr: "Error: boom"})
	res, err := f.mod.Prepare(f.env(manifest.New("test")), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if res.Inventory.(Inventory).Brewfile || !slices.Contains(ids(res.Problems), "brewfile") || module.HasBlocker(res.Problems) {
		t.Errorf("Prepare = %+v", res)
	}
}

func TestPrepareAgainKeepsServicesStoppedEarlier(t *testing.T) {
	f := newFixture(t)
	f.respond("services list --json", runner.Response{Stdout: allStoppedJSON})
	previous := record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}, Root: []string{"unbound"}})
	res, err := f.mod.Prepare(f.env(previous), f.st)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	if !slices.Equal(inv.Stopped, []string{"postgresql@16"}) || !slices.Equal(inv.Root, []string{"unbound"}) {
		t.Errorf("inventory = %+v, want the earlier stops carried forward", inv)
	}
	if f.ran("services stop postgresql@16") {
		t.Error("stopped an already stopped service")
	}
}

func TestUndoAndFinishStartServices(t *testing.T) {
	for _, step := range []string{"undo", "finish"} {
		t.Run(step, func(t *testing.T) {
			f := newFixture(t)
			m := record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}, Root: []string{"unbound"}})
			var res module.Result
			var err error
			if step == "undo" {
				res, err = f.mod.Undo(f.env(m), f.st)
			} else {
				res, err = f.mod.Finish(f.env(m), f.st)
			}
			if err != nil {
				t.Fatal(err)
			}
			if !f.ran("services start postgresql@16") || !slices.Equal(res.Done, []string{"started postgresql@16"}) {
				t.Errorf("result = %+v", res)
			}
			if len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Fix, "sudo brew services start unbound") {
				t.Errorf("problems = %+v", res.Problems)
			}
		})
	}
}

func TestFinishStartFailureIsByHand(t *testing.T) {
	f := newFixture(t)
	f.respond("services start postgresql@16", runner.Response{Code: 1, Stderr: "Error: boom"})
	res, err := f.mod.Finish(f.env(record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}})), f.st)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Fix, "brew services start postgresql@16") {
		t.Errorf("problems = %+v", res.Problems)
	}
}

func TestVerifyPackagesAndServices(t *testing.T) {
	f := newFixture(t)
	f.st.WriteFile(BrewfileName, []byte("brew \"wget\"\n"))
	check := "bundle check --file=" + f.st.Path(BrewfileName) + " --no-upgrade --verbose"
	f.respond(check, runner.Response{Code: 1, Stdout: "brew bundle can't satisfy your Brewfile's dependencies.\n→ Formula wget needs to be installed.\n"})
	f.respond("services list --json", runner.Response{Stdout: allStoppedJSON})
	m := record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}, Brewfile: true})
	checks, err := f.mod.Verify(f.env(m), f.st)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]module.Check{}
	for _, c := range checks {
		byID[c.ID] = c
	}
	if c := byID["homebrew:packages"]; c.Status != module.StatusWarn || !slices.Equal(c.Paths, []string{"Formula wget"}) || !strings.Contains(c.Detail, "brew bundle install") {
		t.Errorf("packages = %+v", c)
	}
	if c := byID["homebrew:services"]; c.Status != module.StatusWarn || !slices.Equal(c.Paths, []string{"postgresql@16"}) {
		t.Errorf("services = %+v", c)
	}
}

func TestVerifyPassesWhenAllIsWell(t *testing.T) {
	f := newFixture(t)
	f.st.WriteFile(BrewfileName, []byte("brew \"git\"\n"))
	f.respond("bundle check --file="+f.st.Path(BrewfileName)+" --no-upgrade --verbose", runner.Response{Stdout: "The Brewfile's dependencies are satisfied.\n"})
	checks, err := f.mod.Verify(f.env(record(t, Inventory{Installed: true, Stopped: []string{"postgresql@16"}, Brewfile: true})), f.st)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.Status != module.StatusPass {
			t.Errorf("%s = %s (%s)", c.ID, c.Status, c.Detail)
		}
	}
}

func TestVerifyHomebrewMissing(t *testing.T) {
	f := newFixture(t)
	f.mod.Brew = filepath.Join(t.TempDir(), "nope")
	checks, err := f.mod.Verify(f.env(record(t, Inventory{Installed: true})), f.st)
	if err != nil || len(checks) != 1 || checks[0].ID != "homebrew:installed" || checks[0].Status != module.StatusFail {
		t.Errorf("Verify = %+v, %v", checks, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/homebrew/...`
Expected: FAIL with `undefined: Module`, `undefined: Inventory`, and similar.

- [ ] **Step 3: Implement the module**

`internal/modules/homebrew/homebrew.go`:
```go
// Package homebrew stops running Homebrew services so their data copies
// whole, starts them again afterwards, and checks every package arrived.
package homebrew

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const (
	Name         = "homebrew"
	BrewfileName = "Brewfile"
)

// brewEnv keeps Homebrew from updating itself, sending analytics, or
// cleaning up, so it makes no network calls and changes nothing on mtm's
// behalf.
var brewEnv = []string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ANALYTICS=1", "HOMEBREW_NO_ENV_HINTS=1", "HOMEBREW_NO_INSTALL_CLEANUP=1"}

type Module struct {
	Brew string
	User string
}

func New(user string) Module { return Module{Brew: "/opt/homebrew/bin/brew", User: user} }

func (Module) Name() string { return Name }

type Service struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	User   string `json:"user,omitempty"`
}

// Inventory records which services prepare stopped (Stopped) and which ran
// as root and must be stopped and started by hand (Root).
type Inventory struct {
	Installed bool      `json:"installed"`
	Services  []Service `json:"services,omitempty"`
	Stopped   []string  `json:"stopped,omitempty"`
	Root      []string  `json:"root,omitempty"`
	Brewfile  bool      `json:"brewfile"`
}

func (m Module) installed() bool {
	_, err := os.Stat(m.Brew)
	return err == nil
}

func (m Module) brew(env module.Env, args ...string) (runner.Output, error) {
	return env.Run.Run(env.Ctx, runner.Command{Name: m.Brew, Args: args, Env: brewEnv})
}

func (m Module) services(env module.Env) ([]Service, error) {
	out, err := m.brew(env, "services", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("list Homebrew services: %w", err)
	}
	return parseServices(out.Stdout)
}

func parseServices(out []byte) ([]Service, error) {
	var raw []struct {
		Name   string  `json:"name"`
		Status string  `json:"status"`
		User   *string `json:"user"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse brew services list: %w", err)
	}
	services := make([]Service, 0, len(raw))
	for _, r := range raw {
		s := Service{Name: r.Name, Status: r.Status}
		if r.User != nil {
			s.User = *r.User
		}
		services = append(services, s)
	}
	return services, nil
}

func started(services []Service, name string) bool {
	return slices.ContainsFunc(services, func(s Service) bool { return s.Name == name && s.Status == "started" })
}

// running splits started services into those this user can stop and those
// that need sudo.
func (m Module) running(services []Service) (mine, others []string) {
	for _, s := range services {
		switch {
		case s.Status != "started":
		case s.User == m.User:
			mine = append(mine, s.Name)
		default:
			others = append(others, s.Name)
		}
	}
	return mine, others
}

// stillStopped keeps names an earlier prepare recorded that are still not
// running, so finish restarts them even after prepare runs again.
func stillStopped(earlier []string, services []Service) []string {
	var out []string
	for _, name := range earlier {
		if !started(services, name) {
			out = append(out, name)
		}
	}
	return out
}

func sudo(verb string, names []string) string {
	cmds := make([]string, len(names))
	for i, n := range names {
		cmds[i] = "sudo brew services " + verb + " " + n
	}
	return strings.Join(cmds, "; ")
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	if !m.installed() {
		return []module.Finding{{Module: Name, ID: "not-installed", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant, Title: "Homebrew is not installed"}}, nil
	}
	services, err := m.services(env)
	if err != nil {
		return nil, err
	}
	findings := []module.Finding{{
		Module: Name, ID: "homebrew", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
		Title:  "Homebrew in " + filepath.Dir(filepath.Dir(m.Brew)),
		Detail: "Migration Assistant copies it. mtm saves a Brewfile and checks every package arrived.",
	}}
	mine, others := m.running(services)
	if len(mine) > 0 {
		findings = append(findings, module.Finding{
			Module: Name, ID: "services", Severity: module.SeverityInfo, Handler: module.ByMtm,
			Title:  words.Count(len(mine), "running service", "running services") + ": " + strings.Join(mine, ", "),
			Detail: "prepare stops them so their data is copied whole; finish starts them on the new Mac.",
		})
	}
	if len(others) > 0 {
		findings = append(findings, module.Finding{
			Module: Name, ID: "root-services", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: words.Count(len(others), "service runs", "services run") + " as root: " + strings.Join(others, ", "),
			Fix:   "Before Migration Assistant, run: " + sudo("stop", others),
		})
	}
	return findings, nil
}

func (m Module) Prepare(env module.Env, st *stage.Stage) (module.Result, error) {
	if !m.installed() {
		return module.Result{Inventory: Inventory{}, Done: []string{"Homebrew is not installed"}}, nil
	}
	var previous Inventory
	if _, err := module.LoadInventory(env, Name, &previous); err != nil && !errors.Is(err, module.ErrNotPrepared) {
		return module.Result{}, err
	}
	services, err := m.services(env)
	if err != nil {
		return module.Result{}, err
	}
	inv := Inventory{Installed: true, Services: services}
	var res module.Result
	if err := m.saveBrewfile(env, st); err != nil {
		res.Problems = append(res.Problems, module.Finding{
			Module: Name, ID: "brewfile", Severity: module.SeverityWarn, Handler: module.ByMtm,
			Title: "Could not save a Brewfile", Detail: err.Error(),
			Fix:   "verify can't list missing packages, but Migration Assistant still copies Homebrew.",
		})
	} else {
		inv.Brewfile = true
		res.Done = append(res.Done, "saved a Brewfile")
	}
	mine, others := m.running(services)
	inv.Stopped = stillStopped(previous.Stopped, services)
	for _, name := range mine {
		if _, err := m.brew(env, "services", "stop", name); err != nil {
			res.Problems = append(res.Problems, module.Finding{
				Module: Name, ID: "stop:" + name, Severity: module.SeverityBlocker, Handler: module.Manual,
				Title: "Could not stop " + name, Detail: err.Error(),
				Fix:   "Run: brew services stop " + name + ", then run mtm prepare again. A running service's data could be copied mid-write.",
			})
			continue
		}
		inv.Stopped = append(inv.Stopped, name)
	}
	if len(inv.Stopped) > 0 {
		res.Done = append(res.Done, "stopped "+strings.Join(inv.Stopped, ", "))
	}
	inv.Root = append(stillStopped(previous.Root, services), others...)
	if len(others) > 0 {
		res.Problems = append(res.Problems, module.Finding{
			Module: Name, ID: "root-services", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: words.Count(len(others), "service runs", "services run") + " as root and must be stopped by hand",
			Fix:   "Run: " + sudo("stop", others),
		})
	}
	res.Inventory = inv
	return res, nil
}

// saveBrewfile passes the type flags so brew bundle doesn't ask cargo, go,
// npm, or VS Code for their packages; cargo's rustup reaches the network.
func (m Module) saveBrewfile(env module.Env, st *stage.Stage) error {
	out, err := m.brew(env, "bundle", "dump", "--file=-", "--formula", "--cask", "--tap", "--mas")
	if err != nil {
		return err
	}
	return st.WriteFile(BrewfileName, out.Stdout)
}

func (m Module) Undo(env module.Env, _ *stage.Stage) (module.Result, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return module.Result{}, err
	}
	return m.restart(env, inv), nil
}

func (m Module) Finish(env module.Env, st *stage.Stage) (module.Result, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return module.Result{}, err
	}
	if !inv.Installed {
		return module.Result{}, nil
	}
	if !m.installed() {
		return module.Result{Problems: []module.Finding{{
			Module: Name, ID: "missing", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Homebrew is missing on this Mac",
			Fix:   "Migration Assistant should have copied it. Copy it from the old Mac before you erase it, or reinstall Homebrew and run: brew bundle install --file " + macos.ShellQuote(st.Path(BrewfileName)),
		}}}, nil
	}
	return m.restart(env, inv), nil
}

func (m Module) restart(env module.Env, inv Inventory) module.Result {
	var res module.Result
	var ok []string
	for _, name := range inv.Stopped {
		if _, err := m.brew(env, "services", "start", name); err != nil {
			res.Problems = append(res.Problems, module.Finding{
				Module: Name, ID: "start:" + name, Severity: module.SeverityWarn, Handler: module.Manual,
				Title: "Could not start " + name, Detail: err.Error(), Fix: "Run: brew services start " + name,
			})
			continue
		}
		ok = append(ok, name)
	}
	if len(ok) > 0 {
		res.Done = append(res.Done, "started "+strings.Join(ok, ", "))
	}
	if len(inv.Root) > 0 {
		res.Problems = append(res.Problems, module.Finding{
			Module: Name, ID: "root-services", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Start the services that ran as root", Fix: "Run: " + sudo("start", inv.Root),
		})
	}
	return res
}

func (m Module) Verify(env module.Env, st *stage.Stage) ([]module.Check, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	if !inv.Installed {
		return nil, nil
	}
	if !m.installed() {
		return []module.Check{{Module: Name, ID: Name + ":installed", Status: module.StatusFail, Title: "Homebrew",
			Detail: m.Brew + " is missing; Migration Assistant should have copied Homebrew"}}, nil
	}
	var checks []module.Check
	if inv.Brewfile {
		checks = append(checks, m.packagesCheck(env, st))
	}
	services, err := m.services(env)
	if err != nil {
		return nil, err
	}
	return append(checks, servicesCheck(inv, services)), nil
}

func (m Module) packagesCheck(env module.Env, st *stage.Stage) module.Check {
	c := module.Check{Module: Name, ID: Name + ":packages", Status: module.StatusPass, Title: "packages", Detail: "every Brewfile package is installed"}
	brewfile := st.Path(BrewfileName)
	out, err := m.brew(env, "bundle", "check", "--file="+brewfile, "--no-upgrade", "--verbose")
	if err == nil {
		return c
	}
	missing := parseMissing(append(out.Stdout, out.Stderr...))
	if code, ok := runner.ExitCode(err); !ok || code != 1 || len(missing) == 0 {
		c.Status, c.Detail = module.StatusWarn, "could not check packages: "+err.Error()
		return c
	}
	c.Status, c.Paths = module.StatusWarn, missing
	c.Detail = words.Count(len(missing), "package is", "packages are") + " missing; run: brew bundle install --file " + macos.ShellQuote(brewfile)
	return c
}

// parseMissing reads brew bundle check --verbose lines such as
// "→ Formula wget needs to be installed."
func parseMissing(out []byte) []string {
	var missing []string
	for _, line := range strings.Split(string(out), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "→ ")
		if !ok {
			continue
		}
		if name, _, ok := strings.Cut(rest, " needs to be"); ok {
			missing = append(missing, name)
		}
	}
	return missing
}

func servicesCheck(inv Inventory, services []Service) module.Check {
	c := module.Check{Module: Name, ID: Name + ":services", Status: module.StatusPass, Title: "services"}
	want := append(slices.Clone(inv.Stopped), inv.Root...)
	var down []string
	for _, name := range want {
		if !started(services, name) {
			down = append(down, name)
		}
	}
	c.Detail = words.Count(len(want)-len(down), "service is", "services are") + " running again"
	if len(down) > 0 {
		c.Status, c.Paths = module.StatusWarn, down
		c.Detail = "not running: " + strings.Join(down, ", ") + "; run mtm finish"
	}
	return c
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/modules/homebrew/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/modules/homebrew
git commit -m "Add the homebrew module

prepare stops the user's running services so their data copies whole,
saves a Brewfile without asking cargo or npm (which reach the network),
and remembers what it stopped across re-runs. finish and --undo start
them again; root services are printed as sudo commands.

Co-Authored-By: <your model> <noreply@anthropic.com>"
```

---

### Task 6: `toolchains` module

**Files:**
- Create: `internal/modules/toolchains/toolchains.go`, `internal/modules/toolchains/toolchains_test.go`

**Interfaces:**
- Consumes: `macos.LoginShell`, `words.Count` (Task 1); `module.*`, `stage.Stage`, `manifest.New`, `runner.Fake`, `testutil.WriteFile` (Stage 1).
- Produces:
  - `toolchains.Name = "toolchains"`, `toolchains.New(user) Module`.
  - `toolchains.Inventory{Shell string; Tools []Tool}` and `toolchains.Tool{Name, Path, Version string}`.
  - Check IDs: `toolchains:<tool>`, and `toolchains:shell` when the PATH can't be read.

- [ ] **Step 1: Write the failing tests**

`internal/modules/toolchains/toolchains_test.go`:
```go
package toolchains

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
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/testutil"
)

type fixture struct {
	home, bin1, bin2 string
	fake             *runner.Fake
}

// newFixture puts node in two PATH folders (the first must win) and go in
// the second, under a home folder.
func newFixture(t *testing.T, shell string) fixture {
	t.Helper()
	home := t.TempDir()
	f := fixture{home: home, bin1: filepath.Join(home, ".asdf", "shims"), bin2: filepath.Join(home, "bin"), fake: &runner.Fake{Responses: map[string]runner.Response{}}}
	for _, exe := range []string{filepath.Join(f.bin1, "node"), filepath.Join(f.bin2, "node"), filepath.Join(f.bin2, "go")} {
		testutil.WriteFile(t, exe, "#!/bin/sh\n")
		if err := os.Chmod(exe, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	testutil.WriteFile(t, filepath.Join(f.bin2, "java"), "not executable")
	f.fake.Responses["/usr/bin/dscl . -read /Users/dev UserShell"] = runner.Response{Stdout: "UserShell: " + shell + "\n"}
	// No system folders in this PATH: macOS ships git, python3, and java
	// stubs in /usr/bin, which would make the expected tool list depend on
	// the test machine.
	f.fake.Responses[pathCommand(shell).String()] = runner.Response{Stdout: pathMarker + f.bin1 + ":" + f.bin2 + "\n"}
	f.fake.Responses[filepath.Join(f.bin1, "node")+" --version"] = runner.Response{Stdout: "v22.1.0\n"}
	f.fake.Responses[filepath.Join(f.bin2, "go")+" version"] = runner.Response{Stdout: "go version go1.26.1 darwin/arm64\n"}
	return f
}

func (f fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func TestPathCommandPerShell(t *testing.T) {
	tests := map[string][]string{
		"/opt/homebrew/bin/fish": {"-l", "-c"},
		"/bin/zsh":               {"-l", "-i", "-c"},
		"/bin/bash":              {"-l", "-i", "-c"},
		"/bin/tcsh":              {"-l", "-c"},
	}
	for shell, flags := range tests {
		cmd := pathCommand(shell)
		if cmd.Name != shell || !slices.Equal(cmd.Args[:len(cmd.Args)-1], flags) || !strings.Contains(cmd.Args[len(cmd.Args)-1], pathMarker) {
			t.Errorf("pathCommand(%s) = %+v", shell, cmd)
		}
	}
	if fish := pathCommand("/opt/homebrew/bin/fish"); !strings.Contains(fish.Args[2], "string join : $PATH") {
		t.Errorf("fish command = %q", fish.Args[2])
	}
}

func TestParsePath(t *testing.T) {
	out := []byte("Last login: today\n" + pathMarker + "/old\nnoise\n" + pathMarker + "/a:/b\n")
	if p, ok := parsePath(out); !ok || p != "/a:/b" {
		t.Errorf("parsePath = %q, %v", p, ok)
	}
	if _, ok := parsePath([]byte("no marker here\n")); ok {
		t.Error("parsePath found a PATH without the marker")
	}
}

func TestCollectFindsToolsInPathOrder(t *testing.T) {
	f := newFixture(t, "/opt/homebrew/bin/fish")
	inv, err := Module{User: "dev"}.collect(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []Tool{
		{Name: "node", Path: "~/.asdf/shims/node", Version: "v22.1.0"},
		{Name: "go", Path: "~/bin/go", Version: "go version go1.26.1 darwin/arm64"},
	}
	if inv.Shell != "/opt/homebrew/bin/fish" || !slices.Equal(inv.Tools, want) {
		t.Errorf("inventory = %+v, want tools %+v", inv, want)
	}
}

func TestCollectToleratesNoisyShell(t *testing.T) {
	f := newFixture(t, "/bin/zsh")
	f.fake.Responses[pathCommand("/bin/zsh").String()] = runner.Response{
		Stdout: "/Users/dev/.zprofile:3: no such file\n" + pathMarker + f.bin1 + ":" + f.bin2 + "\n",
		Code:   1,
	}
	inv, err := Module{User: "dev"}.collect(f.env(nil))
	if err != nil || len(inv.Tools) != 2 {
		t.Errorf("collect = %+v, %v; want both tools despite the noisy, failing shell", inv, err)
	}
}

func TestCollectFailsWithoutPATH(t *testing.T) {
	f := newFixture(t, "/bin/zsh")
	f.fake.Responses[pathCommand("/bin/zsh").String()] = runner.Response{Stdout: "zsh: broken\n", Code: 1}
	if _, err := (Module{User: "dev"}).collect(f.env(nil)); err == nil || !strings.Contains(err.Error(), "read PATH") {
		t.Errorf("err = %v, want a read PATH error", err)
	}
}

func TestVersionFallsBackToStderr(t *testing.T) {
	f := newFixture(t, "/opt/homebrew/bin/fish")
	java := filepath.Join(f.bin2, "java")
	os.Chmod(java, 0o755)
	f.fake.Responses[java+" -version"] = runner.Response{Stderr: "openjdk version \"21.0.2\" 2024-01-16\nOpenJDK Runtime\n"}
	inv, err := Module{User: "dev"}.collect(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(inv.Tools, Tool{Name: "java", Path: "~/bin/java", Version: `openjdk version "21.0.2" 2024-01-16`}) {
		t.Errorf("tools = %+v", inv.Tools)
	}
}

func TestVerify(t *testing.T) {
	old := Inventory{Shell: "/opt/homebrew/bin/fish", Tools: []Tool{
		{Name: "node", Path: "~/.asdf/shims/node", Version: "v22.1.0"},
		{Name: "go", Path: "~/bin/go", Version: "go version go1.26.1 darwin/arm64"},
		{Name: "rustc", Path: "~/.cargo/bin/rustc", Version: "rustc 1.91.0"},
	}}
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), old); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, "/opt/homebrew/bin/fish")
	f.fake.Responses[filepath.Join(f.bin2, "go")+" version"] = runner.Response{Stdout: "go version go1.25.0 darwin/arm64\n"}
	checks, err := Module{User: "dev"}.Verify(f.env(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]module.Status{}
	for _, c := range checks {
		status[c.ID] = c.Status
	}
	want := map[string]module.Status{"toolchains:node": module.StatusPass, "toolchains:go": module.StatusFail, "toolchains:rustc": module.StatusFail}
	for id, s := range want {
		if status[id] != s {
			t.Errorf("%s = %s, want %s", id, status[id], s)
		}
	}
}

func TestVerifyShellFailure(t *testing.T) {
	m := manifest.New("test")
	m.SetModule(Name, time.Now(), Inventory{Shell: "/bin/zsh", Tools: []Tool{{Name: "node", Path: "~/bin/node", Version: "v22"}}})
	f := newFixture(t, "/bin/zsh")
	f.fake.Responses[pathCommand("/bin/zsh").String()] = runner.Response{Stdout: "nothing\n"}
	checks, err := Module{User: "dev"}.Verify(f.env(m), nil)
	if err != nil || len(checks) != 1 || checks[0].ID != "toolchains:shell" || checks[0].Status != module.StatusFail {
		t.Errorf("Verify = %+v, %v", checks, err)
	}
}

func TestScanListsTools(t *testing.T) {
	f := newFixture(t, "/opt/homebrew/bin/fish")
	findings, err := Module{User: "dev"}.Scan(f.env(nil))
	if err != nil || len(findings) != 1 || !strings.Contains(findings[0].Detail, "node v22.1.0") {
		t.Errorf("Scan = %+v, %v", findings, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/toolchains/...`
Expected: FAIL with `undefined: pathCommand`, `undefined: Module`, and similar.

- [ ] **Step 3: Implement the module**

`internal/modules/toolchains/toolchains.go`:
```go
// Package toolchains records which developer tools the login shell finds,
// and at which versions, so verify can tell the new Mac's terminal behaves
// the same.
package toolchains

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const (
	Name           = "toolchains"
	pathMarker     = "__MTM_PATH__="
	shellTimeout   = 20 * time.Second
	versionTimeout = 10 * time.Second
)

type tool struct {
	name string
	args []string
}

var known = []tool{
	{"brew", []string{"--version"}},
	{"git", []string{"--version"}},
	{"node", []string{"--version"}},
	{"npm", []string{"--version"}},
	{"pnpm", []string{"--version"}},
	{"bun", []string{"--version"}},
	{"deno", []string{"--version"}},
	{"python3", []string{"--version"}},
	{"uv", []string{"--version"}},
	{"go", []string{"version"}},
	{"rustc", []string{"--version"}},
	{"cargo", []string{"--version"}},
	{"ruby", []string{"--version"}},
	{"java", []string{"-version"}},
	{"asdf", []string{"--version"}},
	{"mise", []string{"--version"}},
}

type Module struct {
	User string
}

func New(user string) Module { return Module{User: user} }

func (Module) Name() string { return Name }

// Tool paths inside the home folder are recorded as "~/…", so they compare
// across usernames.
type Tool struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

type Inventory struct {
	Shell string `json:"shell"`
	Tools []Tool `json:"tools,omitempty"`
}

func (m Module) collect(env module.Env) (Inventory, error) {
	shell, err := macos.LoginShell(env.Ctx, env.Run, m.User)
	if err != nil {
		return Inventory{}, err
	}
	path, err := loginPath(env, shell)
	if err != nil {
		return Inventory{}, err
	}
	inv := Inventory{Shell: shell}
	for _, t := range known {
		abs := lookPath(t.name, path)
		if abs == "" {
			continue
		}
		inv.Tools = append(inv.Tools, Tool{Name: t.name, Path: homeRelative(env.Home, abs), Version: version(env, abs, t.args, path)})
	}
	return inv, nil
}

func pathCommand(shell string) runner.Command {
	posix := `printf '` + pathMarker + `%s\n' "$PATH"`
	switch filepath.Base(shell) {
	case "fish":
		return runner.Command{Name: shell, Args: []string{"-l", "-c", `printf '` + pathMarker + `%s\n' (string join : $PATH)`}}
	case "zsh", "bash":
		// Many developers set PATH in .zshrc or .bashrc, which only
		// interactive shells read.
		return runner.Command{Name: shell, Args: []string{"-l", "-i", "-c", posix}}
	}
	return runner.Command{Name: shell, Args: []string{"-l", "-c", posix}}
}

func loginPath(env module.Env, shell string) (string, error) {
	ctx, cancel := context.WithTimeout(env.Ctx, shellTimeout)
	defer cancel()
	cmd := pathCommand(shell)
	cmd.Dir = env.Home
	out, err := env.Run.Run(ctx, cmd)
	// Startup files may print noise, or make the shell exit non-zero after
	// it printed the PATH; the marker line is what counts.
	if p, ok := parsePath(out.Stdout); ok {
		return p, nil
	}
	if err != nil {
		return "", fmt.Errorf("read PATH from login shell %s: %w", shell, err)
	}
	return "", fmt.Errorf("read PATH from login shell %s: it printed no PATH", shell)
}

func parsePath(out []byte) (string, bool) {
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if p, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), pathMarker); ok {
			return p, true
		}
	}
	return "", false
}

func lookPath(name, path string) string {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// version runs the tool with the login shell's PATH, so shims such as
// asdf's find their real tools. A tool that fails to report reads
// "unknown", which verify compares like any other version.
func version(env module.Env, abs string, args []string, path string) string {
	ctx, cancel := context.WithTimeout(env.Ctx, versionTimeout)
	defer cancel()
	out, err := env.Run.Run(ctx, runner.Command{Name: abs, Args: args, Dir: env.Home, Env: []string{"PATH=" + path}})
	if err != nil {
		return "unknown"
	}
	if line := firstLine(out.Stdout); line != "" {
		return line
	}
	if line := firstLine(out.Stderr); line != "" {
		return line
	}
	return "unknown"
}

func firstLine(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func homeRelative(home, p string) string {
	if p == home || strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func summary(tools []Tool) string {
	parts := make([]string, len(tools))
	for i, t := range tools {
		parts[i] = t.Name + " " + t.Version
	}
	return strings.Join(parts, "; ")
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	inv, err := m.collect(env)
	if err != nil {
		return []module.Finding{{
			Module: Name, ID: "shell", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Could not read PATH from your login shell", Detail: err.Error(),
			Fix:   "Check that a new terminal window starts cleanly, then run mtm scan again.",
		}}, nil
	}
	return []module.Finding{{
		Module: Name, ID: "tools", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
		Title:  words.Count(len(inv.Tools), "toolchain", "toolchains") + " found through " + inv.Shell,
		Detail: summary(inv.Tools),
	}}, nil
}

func (m Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	inv, err := m.collect(env)
	if err != nil {
		return module.Result{}, err
	}
	return module.Result{Inventory: inv, Done: []string{"recorded " + words.Count(len(inv.Tools), "tool", "tools") + " found through " + inv.Shell}}, nil
}

// Finish has nothing to restore: the tools live in folders Migration
// Assistant copies.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}

func (m Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var old Inventory
	if _, err := module.LoadInventory(env, Name, &old); err != nil {
		return nil, err
	}
	cur, err := m.collect(env)
	if err != nil {
		return []module.Check{{Module: Name, ID: Name + ":shell", Status: module.StatusFail, Title: "login shell", Detail: err.Error()}}, nil
	}
	now := map[string]Tool{}
	for _, t := range cur.Tools {
		now[t.Name] = t
	}
	var checks []module.Check
	for _, t := range old.Tools {
		c := module.Check{Module: Name, ID: Name + ":" + t.Name, Status: module.StatusPass, Title: t.Name, Detail: t.Version + " at " + t.Path}
		n, ok := now[t.Name]
		switch {
		case !ok:
			c.Status, c.Detail = module.StatusFail, fmt.Sprintf("missing (was %s at %s)", t.Version, t.Path)
		case n.Path != t.Path || n.Version != t.Version:
			c.Status, c.Detail = module.StatusFail, fmt.Sprintf("now %s at %s (was %s at %s)", n.Version, n.Path, t.Version, t.Path)
		}
		checks = append(checks, c)
	}
	return checks, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/modules/toolchains/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/modules/toolchains
git commit -m "Add the toolchains module

Reads PATH once from the user's real login shell (fish included, and
zsh/bash interactively, since PATH often lives in .zshrc), tolerating
noisy startup files via a marker line, then records each tool's path
and version for verify to compare.

Co-Authored-By: <your model> <noreply@anthropic.com>"
```

---

### Task 7: `loginitems` module

**Files:**
- Create: `internal/modules/loginitems/loginitems.go`, `internal/modules/loginitems/loginitems_test.go`

**Interfaces:**
- Consumes: `macos.PlistValue`, `macos.ShellQuote`, `words.Count` (Task 1); `module.*`, `stage.Stage`, `manifest.New`, `runner.Fake`, `testutil.WriteFile` (Stage 1).
- Produces:
  - `loginitems.Name = "loginitems"`, `loginitems.New(uid int) Module`, `loginitems.Module{UID int}`.
  - `loginitems.Inventory{Agents []Agent; LoginItems []string; LoginItemsListed bool}` and `loginitems.Agent{File, Label string; Loaded bool}`.
  - Check IDs: `loginitems:agents`, and `loginitems:login-items` (info).

- [ ] **Step 1: Write the failing tests**

`internal/modules/loginitems/loginitems_test.go`:
```go
package loginitems

import (
	"context"
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

const uid = 501

// launchctlList is `launchctl list` output: com.a is loaded, com.b is not.
const launchctlList = "PID\tStatus\tLabel\n-\t0\tcom.a\n123\t0\tcom.other\n"

type fixture struct {
	home string
	fake *runner.Fake
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{home: filepath.Join(t.TempDir(), "my home"), fake: &runner.Fake{Responses: map[string]runner.Response{
		"/bin/launchctl list": {Stdout: launchctlList},
	}}}
	dir := filepath.Join(f.home, "Library", "LaunchAgents")
	for file, label := range map[string]string{"a.plist": "com.a", "b.plist": "com.b"} {
		testutil.WriteFile(t, filepath.Join(dir, file), "<plist/>")
		f.fake.Responses["/usr/bin/plutil -extract Label raw -o - "+filepath.Join(dir, file)] = runner.Response{Stdout: label + "\n"}
	}
	testutil.WriteFile(t, filepath.Join(dir, "c.plist.disabled"), "<plist/>")
	testutil.WriteFile(t, filepath.Join(dir, "notes.txt"), "x")
	return f
}

func (f fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func record(t *testing.T, inv Inventory) *manifest.Manifest {
	t.Helper()
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), inv); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPrepareRecordsAgentsAndLoginItems(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses["/usr/bin/osascript -e "+loginItemsScript] = runner.Response{Stdout: "Raycast, 1Password\n"}
	res, err := New(uid).Prepare(f.env(manifest.New("test")), nil)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	want := []Agent{{File: "a.plist", Label: "com.a", Loaded: true}, {File: "b.plist", Label: "com.b", Loaded: false}}
	if !slices.Equal(inv.Agents, want) || !inv.LoginItemsListed || !slices.Equal(inv.LoginItems, []string{"Raycast", "1Password"}) {
		t.Errorf("inventory = %+v", inv)
	}
}

func TestPrepareLoginItemsDenied(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses["/usr/bin/osascript -e "+loginItemsScript] = runner.Response{Code: 1, Stderr: "Not authorized to send Apple events to System Events. (-1743)"}
	res, err := New(uid).Prepare(f.env(manifest.New("test")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Inventory.(Inventory).LoginItemsListed || len(res.Problems) != 1 || module.HasBlocker(res.Problems) {
		t.Errorf("Prepare = %+v, want one non-blocking warning", res)
	}
}

func TestScanDoesNotAskSystemEvents(t *testing.T) {
	f := newFixture(t)
	findings, err := New(uid).Scan(f.env(nil))
	if err != nil || len(findings) != 2 || !strings.Contains(findings[0].Title, "2 launch agents") {
		t.Errorf("Scan = %+v, %v", findings, err)
	}
	for _, c := range f.fake.Calls() {
		if c.Name == "/usr/bin/osascript" {
			t.Error("scan asked System Events")
		}
	}
}

func TestFinishLoadsAgentsAndListsLoginItems(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses["/bin/launchctl list"] = runner.Response{Stdout: "PID\tStatus\tLabel\n"}
	plist := filepath.Join(f.home, "Library", "LaunchAgents", "a.plist")
	f.fake.Responses["/bin/launchctl bootstrap gui/501 "+plist] = runner.Response{}
	m := record(t, Inventory{Agents: []Agent{{File: "a.plist", Label: "com.a", Loaded: true}, {File: "b.plist", Label: "com.b"}}, LoginItems: []string{"Raycast"}, LoginItemsListed: true})
	res, err := New(uid).Finish(f.env(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Done, []string{"loaded com.a"}) {
		t.Errorf("done = %q", res.Done)
	}
	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Detail, "Raycast") {
		t.Errorf("problems = %+v", res.Problems)
	}
}

func TestFinishBootstrapFailureIsByHand(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses["/bin/launchctl list"] = runner.Response{Stdout: "PID\tStatus\tLabel\n"}
	plist := filepath.Join(f.home, "Library", "LaunchAgents", "a.plist")
	f.fake.Responses["/bin/launchctl bootstrap gui/501 "+plist] = runner.Response{Code: 5, Stderr: "Bootstrap failed: 5: Input/output error"}
	m := record(t, Inventory{Agents: []Agent{{File: "a.plist", Label: "com.a", Loaded: true}}, LoginItemsListed: true})
	res, err := New(uid).Finish(f.env(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Fix, "launchctl bootstrap gui/501 '"+plist+"'") {
		t.Errorf("problems = %+v", res.Problems)
	}
}

func TestFinishWhenLoginItemsWereNotListed(t *testing.T) {
	f := newFixture(t)
	res, err := New(uid).Finish(f.env(record(t, Inventory{})), nil)
	if err != nil || len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Detail, "couldn't list them") {
		t.Errorf("Finish = %+v, %v", res, err)
	}
}

func TestVerifyAgents(t *testing.T) {
	f := newFixture(t)
	m := record(t, Inventory{Agents: []Agent{{File: "a.plist", Label: "com.a", Loaded: true}, {File: "b.plist", Label: "com.b", Loaded: true}}, LoginItems: []string{"Raycast"}, LoginItemsListed: true})
	checks, err := New(uid).Verify(f.env(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 || checks[0].Status != module.StatusWarn || !slices.Equal(checks[0].Paths, []string{"com.b"}) {
		t.Errorf("agents check = %+v", checks)
	}
	if checks[1].ID != "loginitems:login-items" || checks[1].Status != module.StatusInfo {
		t.Errorf("login items check = %+v", checks[1])
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/loginitems/...`
Expected: FAIL with `undefined: New`, `undefined: Inventory`, and similar.

- [ ] **Step 3: Implement the module**

`internal/modules/loginitems/loginitems.go`:
```go
// Package loginitems records launch agents and login items, loads the
// agents again on the new Mac, and lists the login items to add back.
package loginitems

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const (
	Name             = "loginitems"
	loginItemsScript = `tell application "System Events" to get the name of every login item`
)

type Module struct {
	UID int
}

func New(uid int) Module { return Module{UID: uid} }

func (Module) Name() string { return Name }

type Agent struct {
	File   string `json:"file"`
	Label  string `json:"label"`
	Loaded bool   `json:"loaded"`
}

type Inventory struct {
	Agents           []Agent  `json:"agents,omitempty"`
	LoginItems       []string `json:"login_items,omitempty"`
	LoginItemsListed bool     `json:"login_items_listed"`
}

func agentsDir(home string) string { return filepath.Join(home, "Library", "LaunchAgents") }

func agents(env module.Env) ([]Agent, error) {
	loaded, err := loadedLabels(env)
	if err != nil {
		return nil, err
	}
	dir := agentsDir(env.Home)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []Agent
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".plist") {
			continue
		}
		label, err := macos.PlistValue(env.Ctx, env.Run, filepath.Join(dir, e.Name()), "Label")
		if err != nil {
			return nil, err
		}
		if label == "" {
			label = strings.TrimSuffix(e.Name(), ".plist")
		}
		out = append(out, Agent{File: e.Name(), Label: label, Loaded: loaded[label]})
	}
	return out, nil
}

func loadedLabels(env module.Env) (map[string]bool, error) {
	out, err := env.Run.Run(env.Ctx, runner.Command{Name: "/bin/launchctl", Args: []string{"list"}})
	if err != nil {
		return nil, fmt.Errorf("list loaded launch agents: %w", err)
	}
	labels := map[string]bool{}
	for i, line := range strings.Split(string(out.Stdout), "\n") {
		fields := strings.Split(line, "\t")
		if i == 0 || len(fields) != 3 {
			continue
		}
		labels[fields[2]] = true
	}
	return labels, nil
}

func loginItems(env module.Env) ([]string, error) {
	out, err := env.Run.Run(env.Ctx, runner.Command{Name: "/usr/bin/osascript", Args: []string{"-e", loginItemsScript}})
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(string(out.Stdout))
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, ", "), nil
}

func countLoaded(list []Agent) int {
	n := 0
	for _, a := range list {
		if a.Loaded {
			n++
		}
	}
	return n
}

// Scan doesn't ask System Events, so it never brings up a permission
// prompt; it explains the one prepare will.
func (Module) Scan(env module.Env) ([]module.Finding, error) {
	list, err := agents(env)
	if err != nil {
		return nil, err
	}
	return []module.Finding{
		{
			Module: Name, ID: "agents", Severity: module.SeverityInfo, Handler: module.ByMtm,
			Title:  fmt.Sprintf("%s in ~/Library/LaunchAgents (%d loaded)", words.Count(len(list), "launch agent", "launch agents"), countLoaded(list)),
			Detail: "finish loads them again on the new Mac.",
		},
		{
			Module: Name, ID: "login-items", Severity: module.SeverityInfo, Handler: module.Manual,
			Title:  "Login items",
			Detail: "prepare asks System Events for your login items, so macOS may ask to let your terminal control System Events. finish lists them so you can add them back.",
		},
	}, nil
}

func (Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	list, err := agents(env)
	if err != nil {
		return module.Result{}, err
	}
	inv := Inventory{Agents: list}
	var res module.Result
	items, err := loginItems(env)
	if err != nil {
		res.Problems = append(res.Problems, module.Finding{
			Module: Name, ID: "login-items", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Could not list your login items", Detail: err.Error(),
			Fix:   "Allow your terminal to control System Events when macOS asks and run mtm prepare again, or note them in System Settings > General > Login Items.",
		})
	} else {
		inv.LoginItems, inv.LoginItemsListed = items, true
	}
	res.Inventory = inv
	res.Done = []string{fmt.Sprintf("recorded %s (%d loaded) and %s", words.Count(len(list), "launch agent", "launch agents"),
		countLoaded(list), words.Count(len(inv.LoginItems), "login item", "login items"))}
	return res, nil
}

func (m Module) Finish(env module.Env, _ *stage.Stage) (module.Result, error) {
	var old Inventory
	if _, err := module.LoadInventory(env, Name, &old); err != nil {
		return module.Result{}, err
	}
	loaded, err := loadedLabels(env)
	if err != nil {
		return module.Result{}, err
	}
	domain := "gui/" + strconv.Itoa(m.UID)
	var res module.Result
	var ok []string
	for _, a := range old.Agents {
		if !a.Loaded || loaded[a.Label] {
			continue
		}
		plist := filepath.Join(agentsDir(env.Home), a.File)
		if _, err := env.Run.Run(env.Ctx, runner.Command{Name: "/bin/launchctl", Args: []string{"bootstrap", domain, plist}}); err != nil {
			res.Problems = append(res.Problems, module.Finding{
				Module: Name, ID: "agent:" + a.Label, Severity: module.SeverityWarn, Handler: module.Manual,
				Title: "Could not load " + a.Label, Detail: err.Error(),
				Fix:   "Allow it in System Settings > General > Login Items, or run: launchctl bootstrap " + domain + " " + macos.ShellQuote(plist),
			})
			continue
		}
		ok = append(ok, a.Label)
	}
	if len(ok) > 0 {
		res.Done = append(res.Done, "loaded "+strings.Join(ok, ", "))
	}
	switch {
	case !old.LoginItemsListed:
		res.Problems = append(res.Problems, addBack("mtm couldn't list them on the old Mac; check the list there before you erase it."))
	case len(old.LoginItems) > 0:
		res.Problems = append(res.Problems, addBack("Add: "+strings.Join(old.LoginItems, ", ")))
	}
	return res, nil
}

func addBack(detail string) module.Finding {
	return module.Finding{
		Module: Name, ID: "login-items", Severity: module.SeverityWarn, Handler: module.Manual,
		Title: "Add your login items back", Detail: detail,
		Fix:   "System Settings > General > Login Items > Open at Login",
	}
}

func (Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var old Inventory
	if _, err := module.LoadInventory(env, Name, &old); err != nil {
		return nil, err
	}
	loaded, err := loadedLabels(env)
	if err != nil {
		return nil, err
	}
	c := module.Check{Module: Name, ID: Name + ":agents", Status: module.StatusPass, Title: "launch agents"}
	var down []string
	want := 0
	for _, a := range old.Agents {
		if !a.Loaded {
			continue
		}
		want++
		if !loaded[a.Label] {
			down = append(down, a.Label)
		}
	}
	c.Detail = words.Count(want-len(down), "agent is", "agents are") + " loaded"
	if len(down) > 0 {
		c.Status, c.Paths = module.StatusWarn, down
		c.Detail = "not loaded: " + strings.Join(down, ", ") + "; run mtm finish"
	}
	checks := []module.Check{c}
	if len(old.LoginItems) > 0 {
		checks = append(checks, module.Check{
			Module: Name, ID: Name + ":login-items", Status: module.StatusInfo, Title: "login items",
			Detail: "add back by hand: " + strings.Join(old.LoginItems, ", "),
		})
	}
	return checks, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/modules/loginitems/...`
Expected: PASS.

- [ ] **Step 5: Format, vet, and commit**

```bash
gofmt -w . && go vet ./... && go test ./...
git add internal/modules/loginitems
git commit -m "Add the loginitems module

Migration Assistant copies launch agent files but not the login items
list or background approvals. finish loads the agents that were running
and lists the login items to add back; only prepare asks System Events,
so scan never triggers a permission prompt.

Co-Authored-By: <your model> <noreply@anthropic.com>"
```

---

### Task 8: Wire the modules, the no-network test, and the Stage 2 wrap-up

**Files:**
- Modify: `cmd/mtm/main.go`, `cmd/mtm/main_test.go`
- Create: `cmd/mtm/nonetwork_integration_test.go`
- Modify: `IMPLEMENTATION_PLAN.md`

**Interfaces:**
- Consumes: `system.New`, `homebrew.New`, `toolchains.New`, `loginitems.New` (Tasks 4–7); `app.App`; `testutil.WriteFile`.
- Produces:
  - `defaultApp` registers modules in this order: `files`, `git`, `system`, `homebrew`, `toolchains`, `loginitems`.
  - `currentUser() string`.
  - Integration test `TestNoNetwork`, behind the `integration` build tag.

- [ ] **Step 1: Write the failing test**

Add to `cmd/mtm/main_test.go`:
```go
func TestDefaultAppModules(t *testing.T) {
	var names []string
	for _, m := range defaultApp(t.TempDir()).Modules {
		names = append(names, m.Name())
	}
	if want := []string{"files", "git", "system", "homebrew", "toolchains", "loginitems"}; !slices.Equal(names, want) {
		t.Errorf("modules = %q, want %q", names, want)
	}
}
```
Add `"slices"` to that file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/mtm/ -run TestDefaultAppModules`
Expected: FAIL. The modules list is `[files git]`.

- [ ] **Step 3: Wire the modules**

In `cmd/mtm/main.go`:
- Add the imports `"os/user"` and the four module packages: `.../internal/modules/homebrew`, `.../internal/modules/loginitems`, `.../internal/modules/system`, `.../internal/modules/toolchains`.
- Replace `defaultApp` with:
```go
func defaultApp(home string) *app.App {
	name := currentUser()
	return &app.App{
		Runner:  runner.Exec{},
		Home:    home,
		Version: version,
		Now:     time.Now,
		Modules: []module.Module{
			files.New(), git.New(), system.New(name), homebrew.New(name), toolchains.New(name), loginitems.New(os.Getuid()),
		},
	}
}

// currentUser is the account name Directory Services knows the user by.
func currentUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}
```

- [ ] **Step 4: Add the no-network integration test**

`cmd/mtm/nonetwork_integration_test.go`:
```go
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

func requireIntegrationHost(t *testing.T) {
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
}

// TestNoNetwork runs mtm under a sandbox that denies network access, with
// a temporary home folder, so every git, brew, and shell command mtm runs
// must work offline. loginitems is left out because it asks System Events,
// which would show a permission prompt.
func TestNoNetwork(t *testing.T) {
	requireIntegrationHost(t)
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
		cmd.Env = append(os.Environ(), "HOME="+home)
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
```

- [ ] **Step 5: Run the tests**

Run:
```bash
gofmt -l . && go vet ./... && go vet -tags integration ./cmd/mtm/ && go test -race ./...
go test -tags integration -run TestNoNetwork -v ./cmd/mtm/
```
Expected:
- `gofmt -l .` prints nothing.
- Both vet runs are clean.
- All tests pass.
- `TestNoNetwork` passes, or skips with its stated reason (record which in the report).

- [ ] **Step 6: Smoke test on this Mac (read-only)**

Run:
```bash
go build -o mtm ./cmd/mtm && ./mtm scan; echo "exit $?"; ls ~/.mtm 2>&1 | head -1; rm -f ./mtm
```
Expected:
- `scan` lists the `system` findings (app count, login shell, Intel-only apps), the `homebrew` findings (Homebrew in `/opt/homebrew`, plus running or root services if any), the `toolchains` finding (tools found through the login shell, with versions), and the `loginitems` findings.
- `~/.mtm` does not exist.
- Do NOT run `prepare`, `finish`, `verify`, or `cleanup` against the real home folder.
- Summarize the output in the report: section headings, counts, exit code. Don't paste personal paths beyond a couple of examples.

- [ ] **Step 7: Mark Stage 2 complete and commit**

In `IMPLEMENTATION_PLAN.md`, change Stage 2's `**Status**: Not Started` to `**Status**: Complete`.

```bash
gofmt -w . && go vet ./... && go test ./...
git add cmd/mtm IMPLEMENTATION_PLAN.md
git commit -m "Wire the Stage 2 modules and prove they stay offline

The no-network test runs scan, prepare, undo, and verify under a
sandbox that denies outbound connections, catching any brew, git, or
shell command that would reach the network on mtm's behalf.

Co-Authored-By: <your model> <noreply@anthropic.com>"
```
