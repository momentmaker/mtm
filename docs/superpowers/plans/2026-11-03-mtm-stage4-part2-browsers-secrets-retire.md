# mtm Stage 4, Part 2: Browsers, Secrets, and Retire Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Check that browser profiles and SSH and GPG identities arrive on the new Mac, list the browser and security steps no tool can do, and guide the user through retiring the old Mac with `mtm retire`.

**Architecture:**
- **The `browsers` module** records each browser's profiles and extensions, and whether each Chromium browser's Safe Storage key is in the keychain. It looks for the key's presence only. `verify` fails a missing profile or key.
- **The `secrets` module** records SSH and GPG identities and checks them as critical. It never reads key material. It turns what can't move into checklist steps: Secure Enclave keys, password managers and their SSH agents, iCloud Keychain, and Touch ID.
- **`mtm retire`** runs on the old Mac. It lists files changed since `prepare`, found by walking the home folder alongside the record. It then walks through retirement steps kept in `~/.mtm/retire.json`: licenses to release, Secure Enclave keys to remove from services, iCloud sign-out, the device list, and the erase.
- **New interfaces:**
  - `module.Retirer`, for modules with something for `retire`;
  - `app.Steps`, which lets `mtm checklist`'s code work on either list.
- **The catalog** gains an app's `kind`, its SSH agent's sockets, and license deactivation.
- **Part 1's follow-ups** come first: N1, N4, and N5 from its final re-review.

**Tech Stack:**
- Go 1.26 with the one dependency the spec allows, `github.com/BurntSushi/toml` v1.6.0.
- macOS tools, by absolute path: `/usr/bin/security` (presence checks only) and `/usr/bin/bioutil` (`-c` only).
- gpg from Homebrew or GPG Suite, when a GnuPG folder exists.

**Spec:** `docs/superpowers/specs/2026-11-02-mtm-design.md`. Part 2 covers:
- §5, the `mtm retire` row;
- §6.5, retiring the old Mac;
- §7.3, `Retirer`, and §7.4, the new catalog fields;
- §8.1, `retire.json`, and §8.5, the critical browser checks;
- §9.1, `retire` in `files`; §9.9, `browsers`; §9.10, `secrets`; §9.11, deactivation in `system`;
- §11, the no-network test runs `retire`.

The tasks update each section as they go.

**How this plan was checked:**
- **Each task's code ran before the plan was written.** Every task was written and run in a scratch copy of the repository as its own commit.
- **The gate is clean at every task.** At each one, `gofmt -l .`, `go vet ./...`, `go vet -tags integration ./...`, and `go test ./...` passed.
- **The red step is real.** Each task's "verify they fail" output was captured by putting only that task's tests on the previous task's code.
- **`TestNoNetwork` passed** with `browsers`, `secrets`, and `mtm retire` included.
- **The fixtures follow this author's Mac.**
  - Chrome and Brave `Local State`, the `Extensions` folders, and the Safe Storage lookups were looked at on it, values unprinted. Exit 0 means found and 44 means missing.
  - So were the `~/.ssh` layout, an `IdentityAgent` line for 1Password, `bioutil -c` output, and gpg's colon fields.
  - Firefox isn't installed there, so its fixtures follow its file formats.

**Diffs:** a modified file's change is a unified diff against the file as the previous task left it. Apply it with `git apply`, or by hand. A new file is given in full.

## Global Constraints

- **Module and dependencies:**
  - module path `github.com/momentmaker/mtm`, `go 1.26`;
  - the only third-party dependency is `github.com/BurntSushi/toml` v1.6.0 (spec 6.6).
- **Network and secrets (spec 6.2):** `mtm` makes no network connections and never reads secret contents. In this part:
  - the keychain is asked only `security find-generic-password -s <service>`, never with `-w` or `-g`;
  - private SSH keys are only looked for in a folder listing, never opened;
  - gpg runs only `--list-secret-keys --with-colons`, and only when a GnuPG folder exists, since it would create one;
  - from Chromium's `Local State`, only profile folders and display names are recorded.
- **What reports and logs hold (spec 6.3):** never fingerprints, file contents, or environment values.
  - SSH and GPG fingerprints stay in `manifest.json`. Reports name SSH keys by file, and GPG keys by algorithm and date.
  - `SSH_AUTH_SOCK` is matched against the catalog, never printed or recorded. The record keeps only the matching catalog id.
- **Apple tools by absolute path:** this part adds `/usr/bin/security` and `/usr/bin/bioutil`. bioutil runs only as `bioutil -c`, which counts fingerprints. Its other flags change them (`-p` purges them all).
- **Checklist steps:**
  - A step's id is stable, `<module>:<key>`.
  - Retirement steps live in `~/.mtm/retire.json`, through `state.SaveFile`. They never go in `state.json`, so none can count toward the new Mac's verdict.
- **The developer's Mac is never changed or read by tests:**
  - **Fakes:** unit tests use `runner.Fake`, with no `Fallback` wherever a command could reach the real keychain, bioutil, or gpg.
  - **Fixtures:** keys, browser folders, and GnuPG folders live in a fixture home folder in `t.TempDir()`. No test reads the real `~/.ssh`, `~/.gnupg`, browser data, or keychain.
  - **The no-network test** empties `GNUPGHOME`, `SSH_AUTH_SOCK`, and `SSH_SK_PROVIDER`, so `secrets` sees only its temporary home folder.
  - **Commands:** never run `mtm prepare`, `finish`, `verify`, `cleanup`, `checklist`, or `retire` against the real home folder.
- **Code style:** modules never print; all output goes through `internal/report`. Comments explain why, never restate code. No commented-out code.
- **Exit codes:**
  - `verify`: `0` safe, `1` not safe, `2` mtm failed.
  - `retire`: `0` when nothing needs attention; `1` when a file outside `~/Library` changed after `prepare`, a module failed, or it isn't the Mac that ran `prepare`; `2` when mtm failed or for a usage error.
  - Other commands: `0` success, `1` completed with problems or refused, `2` mtm failed or a usage error.
- **Before every commit:** `gofmt -l .` prints nothing, and `go vet ./...`, `go vet -tags integration ./...`, and `go test ./...` pass.
- **Commit messages** explain why and end with a `Co-Authored-By:` trailer naming the model that wrote the commit, as your harness instructs.

## Review Focus

These are the five conditions most likely to bite a real user. Each has a pinned test in the task that owns the code.

1. **A Chromium browser whose Safe Storage key didn't arrive.** Its passwords and cookies came across but can't be decrypted. verify fails it as critical, and says how to copy the key from the old Mac.
   - Task 3: `TestVerifyFailsWhenTheSafeStorageKeyIsMissing`.
2. **An SSH agent set in `~/.ssh/config`, or reached through a link.** This is the author's own setup: 1Password through `IdentityAgent`. The agent is found, and finish lists turning it on.
   - Task 4: `TestPrepareRecordsIdentitiesAndTheAgent`, `TestAgentIsFoundThroughALink`, and `TestStepsForPasswordManagersAgentsAndTouchID`.
3. **Private keys mtm can't open, and a key replaced under the same name.** prepare never opens a private key. verify fails a replaced key without printing its fingerprint.
   - Task 4: `TestPrepareReadsNoPrivateKey` and `TestVerifyFailsMissingOrReplacedSSHKeys`.
4. **Changes `retire` must not miss or invent:**
   - a file copied onto the old Mac after `prepare` with an old time, which retire still lists;
   - a deleted file ahead of others in the record, past which retire keeps its place.
   - Task 6: `TestRetireListsANewFileWithAnOldTime` and `TestRetireKeepsItsPlacePastADeletedFile`.
5. **A retirement step closed on the old Mac.** It never reaches the new Mac's checklist or verdict.
   - Task 5: `TestRetirementStepsLiveApartFromTheChecklist`.
   - Task 7: `TestRetireKeepsWhatTheUserDidAndStaysOutOfTheChecklist`.

## File Structure

```
internal/app/app.go                          (modify) finish reads state.json again before merging steps; resolve closes every open step
internal/catalog/catalog.go                  (modify) kind, ssh_agent, license.deactivate and deactivate_note
catalog/1password.toml, bitwarden.toml, secretive.toml
                                             password managers and a Secure Enclave agent, with their SSH agent sockets
catalog/cleanshot-x.toml, malwarebytes.toml  (modify) deactivate on the old Mac
catalog/README.md                            (modify) the new fields
internal/modules/browsers/browsers.go        Scan, Prepare, Steps, Verify; Safe Storage presence
internal/modules/browsers/profiles.go        Chromium Local State and Extensions, Firefox profiles.ini and extensions.json
internal/modules/secrets/secrets.go          Scan, Prepare, Steps, Verify, Retire; Touch ID
internal/modules/secrets/ssh.go              ~/.ssh keys, ssh_config agents, matching agents to the catalog
internal/modules/secrets/gpg.go              gpg secret key listing
internal/state/state.go                      (modify) retire.json; LoadFile and SaveFile
internal/app/checklist.go                    (modify) Steps: one checklist file, for the new Mac or for retiring the old one
internal/report/checklist.go                 (modify) the command a step's hint names
cmd/mtm/checklist.go                         (modify) the checklist commands work on either list
internal/module/module.go                    (modify) Retirement and Retirer
internal/modules/files/retire.go             changes since prepare, walking alongside the record
internal/modules/system/system.go            (modify) Retire: licenses to deactivate
internal/app/retire.go                       App.Retire and retire's own steps
internal/report/retire.go                    the changes report
cmd/mtm/retire.go                            mtm retire
cmd/mtm/main.go                              (modify) browsers and secrets wiring; the retire command and usage
cmd/mtm/nonetwork_integration_test.go        (modify) prepare browsers and secrets offline; run retire
```

---

### Task 1: Part 1's follow-ups

Part 1's final re-review left two findings to fix before Part 2 (N1 and N4), plus a third that fits here (N5):
- **N1:** finish read `state.json` before its module loop and saved it after. A step the user closed with `mtm checklist` while a Docker or Postgres restore ran was overwritten, so it reopened.
- **N4:** the spec still said only `finish` creates checklist steps, in §9.7, §9.8, and §9.11, and so did the empty checklist's message. Since Part 1's Stepper, `verify` does too.
- **N5:** `resolve` closed only steps with status `pending`. `Entry.Open` also counts an empty or unknown status, or a skip without a reason, so a passing check left those open.

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`
- Modify: `docs/superpowers/specs/2026-11-02-mtm-design.md`
- Modify: `internal/app/app.go`
- Modify: `internal/report/checklist.go`
- Test: `internal/app/checklist_test.go` (modify), `internal/report/checklist_test.go` (modify)

**Interfaces:**
- Consumes: `state.Load`, `mergeSteps`, `resolve`, and `checklist.Entry.Open` (Part 1).
- Produces: no new names. `App.Finish` now loads `state.json` twice: before the modules run, only to check it, and again just before merging steps.

- [ ] **Step 1: Write the failing tests**

`internal/app/checklist_test.go`:
```diff
--- a/internal/app/checklist_test.go
+++ b/internal/app/checklist_test.go
@@ -266,3 +266,37 @@ func TestVerifyOnTheOldMacLeavesNoStepsToCarryOver(t *testing.T) {
 		t.Errorf("new Mac's pending = %q, want %q", rep.Verdict.Pending, want)
 	}
 }
+
+// answersMidFinish is stepsModule with a finish slow enough for the user to
+// close a step with mtm checklist in another terminal meanwhile.
+type answersMidFinish struct {
+	stepsModule
+	answer func()
+}
+
+func (m answersMidFinish) Finish(module.Env, *stage.Stage) (module.Result, error) {
+	m.answer()
+	return module.Result{}, nil
+}
+
+func TestFinishKeepsAnAnswerGivenWhileItRuns(t *testing.T) {
+	newHome := finishedNewMac(t, stepsModule{})
+	mod := answersMidFinish{answer: func() { skip(t, newHome, licenseStep.ID, "sold the license", time.Now()) }}
+	if _, err := stepApp(newHome, newUUID, mod).Finish(ctx, FinishOptions{}); err != nil {
+		t.Fatal(err)
+	}
+	if e, _ := loadState(t, newHome).Checklist.Find(licenseStep.ID); e.Status != checklist.Skipped {
+		t.Errorf("license step = %+v, want the skip given during finish", e)
+	}
+}
+
+func TestResolveClosesEveryKindOfOpenStep(t *testing.T) {
+	l := checklist.List{Entries: []checklist.Entry{
+		{Item: grantStep, Status: ""},
+		{Item: licenseStep, Status: checklist.Skipped},
+	}}
+	resolve(&l, []module.Check{{ID: "steps:all", Status: module.StatusPass, Resolves: []string{grantStep.ID, licenseStep.ID}}}, time.Now())
+	if open := l.Pending(); len(open) != 0 {
+		t.Errorf("open steps = %+v, want none", open)
+	}
+}
```

`internal/report/checklist_test.go`:
```diff
--- a/internal/report/checklist_test.go
+++ b/internal/report/checklist_test.go
@@ -43,7 +43,7 @@ func TestChecklistAllShowsSkippedStepsWithTheirReason(t *testing.T) {
 func TestChecklistEmptyAndFinished(t *testing.T) {
 	var out bytes.Buffer
 	Checklist(&out, checklist.List{}, false)
-	if !strings.Contains(out.String(), "mtm finish fills it") {
+	if !strings.Contains(out.String(), "mtm finish and mtm verify fill it") {
 		t.Errorf("empty output:\n%s", out.String())
 	}
 	l := sampleList()
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ ./internal/report/ -run 'TestFinishKeepsAnAnswerGivenWhileItRuns|TestResolveClosesEveryKindOfOpenStep|TestChecklistEmptyAndFinished'`
Expected: FAIL. The skip given during finish is lost, the hand-edited steps stay open, and the empty message is the old one:
```text
--- FAIL: TestFinishKeepsAnAnswerGivenWhileItRuns (0.03s)
    checklist_test.go:289: license step = {Item:{ID:steps:license Module:steps Title:Reactivate Dash Detail: Where: Link:} Status:pending Reason: At:0001-01-01 
--- FAIL: TestResolveClosesEveryKindOfOpenStep (0.00s)
    checklist_test.go:300: open steps = [{Item:{ID:steps:grant Module:steps Title:Give Raycast Accessibility access Detail: Where: Link:} Status: Reason: At:000
FAIL
FAIL	github.com/momentmaker/mtm/internal/app	0.437s
--- FAIL: TestChecklistEmptyAndFinished (0.00s)
    checklist_test.go:47: empty output:
```

- [ ] **Step 3: Implement**

`internal/app/app.go`:
```diff
--- a/internal/app/app.go
+++ b/internal/app/app.go
@@ -341,8 +341,7 @@ func (a *App) Finish(ctx context.Context, opts FinishOptions) (StepReport, error
 	}
 	// Read before any module acts, so a damaged state.json stops finish
 	// before it restarts or restores anything.
-	s, err := state.Load(st)
-	if err != nil {
+	if _, err := state.Load(st); err != nil {
 		return StepReport{}, err
 	}
 	env := a.env(ctx, module.NewMac, false, m, run, exportsFor(st, m))
@@ -359,6 +358,12 @@ func (a *App) Finish(ctx context.Context, opts FinishOptions) (StepReport, error
 		}
 		rep.add(mod.Name(), res)
 	}
+	// Read again: a restore can take minutes, and a step the user closed
+	// meanwhile with mtm checklist must stay closed.
+	s, err := state.Load(st)
+	if err != nil {
+		return rep, err
+	}
 	added, failed := mergeSteps(env, mods, m, &s.Checklist)
 	for _, f := range failed {
 		rep.Problems = append(rep.Problems, moduleFailure(f.module, "list checklist steps", f.err))
@@ -405,7 +410,7 @@ func mergeSteps(env module.Env, mods []module.Module, m *manifest.Manifest, l *c
 	return added, failed
 }
 
-// resolve marks done the pending checklist steps a passing check proved
+// resolve marks done the open checklist steps a passing check proved
 // done. A step the user already closed keeps what they recorded, such as a
 // skip's reason.
 func resolve(l *checklist.List, checks []module.Check, at time.Time) {
@@ -414,7 +419,7 @@ func resolve(l *checklist.List, checks []module.Check, at time.Time) {
 			continue
 		}
 		for _, id := range c.Resolves {
-			if e, ok := l.Find(id); ok && e.Status == checklist.Pending {
+			if e, ok := l.Find(id); ok && e.Open() {
 				l.Done(id, at)
 			}
 		}
```

`internal/report/checklist.go`:
```diff
--- a/internal/report/checklist.go
+++ b/internal/report/checklist.go
@@ -24,7 +24,7 @@ func stepSymbol(e checklist.Entry) string {
 func Checklist(w io.Writer, l checklist.List, all bool) {
 	open := len(l.Pending())
 	if len(l.Entries) == 0 {
-		fmt.Fprintln(w, "The checklist is empty. mtm finish fills it on the new Mac.")
+		fmt.Fprintln(w, "The checklist is empty. mtm finish and mtm verify fill it on the new Mac.")
 		return
 	}
 	if open == 0 && !all {
```

- [ ] **Step 4: Update the spec and the plan file**

The spec now says where steps come from in each module's section. `IMPLEMENTATION_PLAN.md` gets this plan's path and marks Part 2 In Progress.

`docs/superpowers/specs/2026-11-02-mtm-design.md`:
```diff
--- a/docs/superpowers/specs/2026-11-02-mtm-design.md
+++ b/docs/superpowers/specs/2026-11-02-mtm-design.md
@@ -174,7 +174,7 @@ type Module interface {
 - Modules whose `prepare` changes the old Mac also implement `Undo(env, st) (Result, error)`, which `prepare --undo` calls. After a successful undo, that module's record is removed, so `verify` reports it not prepared until `prepare` runs again.
 - `Finding` has: module, id, severity (`info`, `warn`, `blocker`), handler (`migration-assistant`, `mtm`, `manual`), title, detail, fix.
 - `Result` has: what was done, artifacts written (each with its fingerprint), and problems.
-- Modules that leave steps for the user on the new Mac also implement `Steps(env) ([]checklist.Item, error)` (`Stepper`), which works from the record alone. On the new Mac, `finish` and `verify` both merge every recorded module's steps into `state.json`, so `verify` lists them even when `finish` didn't run, or ran with `--only`. A step already there keeps its status and gets the new text, so either command can run again. A `finish` that adds a step clears the last verify result, so `cleanup` waits for a new SAFE TO WIPE.
+- Modules that leave steps for the user on the new Mac also implement `Steps(env) ([]checklist.Item, error)` (`Stepper`), which works from the record alone. On the new Mac, `verify` merges every recorded module's steps into `state.json`, and `finish` those of the modules it ran, so `verify` lists them even when `finish` didn't run, or ran with `--only`. A step already there keeps its status and gets the new text, so either command can run again. A `finish` that adds a step clears the last verify result, so `cleanup` waits for a new SAFE TO WIPE.
 - A `Check` can name the checklist items it proves done (`Resolves`). When it passes, `verify` marks them done, as the `permissions` module does for each grant it finds.
 - `Check` has: module, id, status (`pass`, `fail`, `warn`, `info`), critical flag, title, detail.
 - Modules never print. All output goes through `report`.
@@ -396,7 +396,8 @@ Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0`
 ### 9.7 `loginitems`
 - Records each launch agent in `~/Library/LaunchAgents` (`*.plist` only), with its `Label` and whether it is loaded (`launchctl list`).
 - `prepare`, not `scan`, records login items through System Events (`osascript`). That triggers a one-time Automation permission prompt, which `scan` explains beforehand. If access is denied, `prepare` warns and asks the user to note their login items by hand.
-- `finish` bootstraps the launch agents that were loaded and aren't now (`launchctl bootstrap gui/<uid> <plist>`). A failure becomes a by-hand step pointing at Login Items approvals. `finish` also adds a checklist step for each login item to add again, with a link to Login Items. If `prepare` couldn't list them, the step is to note them on the old Mac first.
+- `finish` bootstraps the launch agents that were loaded and aren't now (`launchctl bootstrap gui/<uid> <plist>`). A failure becomes a by-hand step pointing at Login Items approvals.
+- **Checklist steps** (section 7.3): one for each login item to add again, with a link to Login Items. If `prepare` couldn't list them, the step is to note them on the old Mac first.
 - `verify` warns when an agent that was loaded isn't now, and lists the login items to add again (info).
 
 ### 9.8 `permissions`
@@ -407,7 +408,7 @@ Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0`
   - Any other sqlite3 error is a failure.
 - **`scan`:** with Full Disk Access, an info finding with the number of grants. Without it, a warn: `mtm` will use its catalog instead. The fix is to give the terminal Full Disk Access.
 - **`prepare`:** records the grants (source `tcc`). Without Full Disk Access it records source `catalog` and warns the same way.
-- **`finish`:** creates one checklist item per expected grant, with the pane's name and its deep link for this Mac's macOS.
+- **Checklist steps** (section 7.3): one per expected grant, with the pane's name and its deep link for this Mac's macOS.
   - The expected grants are the recorded ones. With a `catalog` record, they are what the catalog lists for the apps in the `system` module's record.
   - Grants for services System Settings doesn't show, which macOS uses internally, get no item.
   - Item ids are `permissions:<client>:<pane>`, plus `:<target>` for Automation.
@@ -440,8 +441,8 @@ Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0`
   - fingerprints of `/etc/hosts`, `/etc/shells`, `/etc/paths.d/*`, `/etc/pam.d/sudo_local` (Touch ID for sudo). Copies go into `~/.mtm/exports/etc/` so `finish` can print an exact restore command. These files hold configuration, not secrets;
   - app inventory of `/Applications` and `~/Applications`, including one level of subfolders, skipping Apple's own `com.apple.*` apps and symlinks. Each app records its bundle id, version (`plutil -extract … raw`), and architectures (`lipo -archs`). Home apps are recorded as `~/Applications/…`.
 - **`scan` and `prepare`:** warn about Intel-only apps. They need Rosetta on the new Mac, and Rosetta is limited after macOS 27.
+- **Checklist steps** (section 7.3): from the catalog, for the recorded apps: one to reactivate each license marked `reactivate`, and one per note.
 - **`finish`:**
-  - Adds checklist items from the catalog for the recorded apps: one to reactivate each license marked `reactivate`, and one per note.
   - Prints `sudo softwareupdate --install-rosetta --agree-to-license` if Rosetta is needed and missing.
   - For each `/etc` file that differs, prints `sudo install -m 0644 -o root -g wheel <copy> /etc/<file>`. `install` rather than `cp`, because the private copy is 0600 and `cp` would give a new `/etc` file that mode.
   - When the login shell changed, prints one line to paste: `echo <shell> | sudo tee -a /etc/shells >/dev/null && chsh -s <shell>`, without the `tee` part if the shell is already listed.
```

`IMPLEMENTATION_PLAN.md`:
```diff
--- a/IMPLEMENTATION_PLAN.md
+++ b/IMPLEMENTATION_PLAN.md
@@ -140,7 +140,8 @@ Found in the final review.
 
 **Success Criteria**: `retire` lists a file changed on the old Mac after `prepare`. SSH and GPG identities are critical checks.
 **Tests**: module unit tests with fixtures; a `retire` integration test on a temp home folder.
-**Status**: Not Started
+**Plan**: `docs/superpowers/plans/2026-11-03-mtm-stage4-part2-browsers-secrets-retire.md`
+**Status**: In Progress
 
 ## Stage 5: Ship
 **Goal**: GitHub Actions CI on Apple Silicon (`go test`, `go vet`, `gofmt`, `govulncheck`, catalog validation); end-to-end tests in Tart VMs (cloned VM as a perfect migration, injected faults); release builds with checksums and provenance attestations; a Homebrew tap; README and contributor guide (including how to add a catalog app).
```

- [ ] **Step 5: Run the tests to verify they pass**

Run:
```bash
gofmt -l .
go vet ./...
go vet -tags integration ./...
go test ./...
```
Expected: `gofmt -l .` prints nothing, and the rest pass.

- [ ] **Step 6: Commit**

End the message with your `Co-Authored-By:` trailer (Global Constraints).

```bash
git add internal/app/checklist_test.go internal/report/checklist_test.go internal/app/app.go internal/report/checklist.go docs/superpowers/specs/2026-11-02-mtm-design.md IMPLEMENTATION_PLAN.md
git commit -m "Keep checklist answers given while finish runs

finish read state.json before its modules ran and saved it after, so a
step closed with mtm checklist during a long restore reopened. It now
reads it again just before merging steps. resolve also closes every
open step, not only those marked pending, and the spec and the empty
checklist's message say verify adds steps too."
```

---

### Task 2: Catalog fields for password managers, SSH agents, and retiring licenses

**Files:**
- Create: `catalog/1password.toml`
- Create: `catalog/bitwarden.toml`
- Create: `catalog/secretive.toml`
- Modify: `catalog/README.md`
- Modify: `catalog/cleanshot-x.toml`
- Modify: `catalog/malwarebytes.toml`
- Modify: `docs/superpowers/specs/2026-11-02-mtm-design.md`
- Modify: `internal/catalog/catalog.go`
- Test: `internal/catalog/catalog_test.go` (modify)

**Interfaces:**
- Consumes: `catalog.Parse`, `catalog.Load`, and `App.validate` (Part 1).
- Produces:
  - `catalog.KindPasswordManager = "password-manager"` and `catalog.KindSecureEnclaveAgent = "secure-enclave-agent"`.
  - `catalog.App.Kind string` (toml `kind`), and `catalog.App.SSHAgent []string` (toml `ssh_agent`): the sockets the app's SSH agent listens on, as `~/` paths.
  - `catalog.License.Deactivate bool` (toml `deactivate`) and `catalog.License.DeactivateNote string` (toml `deactivate_note`).
  - Three new catalog files: `1password` and `bitwarden` (kind `password-manager`) and `secretive` (kind `secure-enclave-agent`). The catalog then has 26 files.
  - `deactivate = true` for `cleanshot-x` and `malwarebytes`, whose vendors count Macs.

Validation rejects:
- a `kind` other than the two constants;
- an `ssh_agent` path that doesn't start with `~/`;
- a `deactivate_note` without `deactivate = true`.

Top-level keys must come before the first `[table]` in a file, as in TOML generally. The new top-level keys `kind` and `ssh_agent` go next to `notes`.

- [ ] **Step 1: Write the failing tests**

`internal/catalog/catalog_test.go`:
```diff
--- a/internal/catalog/catalog_test.go
+++ b/internal/catalog/catalog_test.go
@@ -64,6 +64,8 @@ client = "com.raycast.helper"
 [license]
 reactivate = true
 note = "Enter the key."
+deactivate = true
+deactivate_note = "Deactivate it in Settings."
 
 [state]
 volatile = ["~/Library/Application Support/Raycast/logs/"]
@@ -81,6 +83,25 @@ volatile = ["~/Library/Application Support/Raycast/logs/"]
 	if len(a.Notes) != 1 || len(a.State.Volatile) != 1 {
 		t.Errorf("notes %q, volatile %q", a.Notes, a.State.Volatile)
 	}
+	if !a.License.Deactivate || a.License.DeactivateNote != "Deactivate it in Settings." {
+		t.Errorf("license = %+v", a.License)
+	}
+}
+
+func TestParseReadsAPasswordManager(t *testing.T) {
+	data := `id = "1password"
+name = "1Password"
+bundle_id = "com.1password.1password"
+kind = "password-manager"
+ssh_agent = ["~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"]
+`
+	a, err := Parse("1password.toml", []byte(data))
+	if err != nil {
+		t.Fatal(err)
+	}
+	if a.Kind != KindPasswordManager || len(a.SSHAgent) != 1 {
+		t.Errorf("app = %+v", a)
+	}
 }
 
 func TestParseRejectsMistakes(t *testing.T) {
@@ -98,6 +119,9 @@ func TestParseRejectsMistakes(t *testing.T) {
 		{"volatile outside home", "raycast.toml", raycast + "\n[state]\nvolatile = [\"/tmp/x\"]\n", "must start with ~/"},
 		{"broken pattern", "raycast.toml", raycast + "\n[state]\nvolatile = [\"~/[x\"]\n", "volatile path"},
 		{"not TOML", "raycast.toml", "id = ", "catalog file raycast.toml"},
+		{"unknown kind", "raycast.toml", raycastHead + "kind = \"launcher\"\n", "kind \"launcher\""},
+		{"agent outside home", "raycast.toml", raycastHead + "ssh_agent = [\"/tmp/agent.sock\"]\n", "must start with ~/"},
+		{"note for no deactivation", "raycast.toml", raycast + "\n[license]\ndeactivate_note = \"x\"\n", "needs license.deactivate"},
 	}
 	for _, tt := range tests {
 		_, err := Parse(tt.file, []byte(tt.data))
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/catalog/`
Expected: FAIL, because the build fails:
```text
FAIL	github.com/momentmaker/mtm/internal/catalog [build failed]
FAIL
# github.com/momentmaker/mtm/internal/catalog [github.com/momentmaker/mtm/internal/catalog.test]
internal/catalog/catalog_test.go:86:16: a.License.Deactivate undefined (type License has no field or method Deactivate)
internal/catalog/catalog_test.go:86:40: a.License.DeactivateNote undefined (type License has no field or method DeactivateNote)
internal/catalog/catalog_test.go:102:7: a.Kind undefined (type App has no field or method Kind)
internal/catalog/catalog_test.go:102:15: undefined: KindPasswordManager
```

- [ ] **Step 3: Implement the fields and their checks**

`internal/catalog/catalog.go`:
```diff
--- a/internal/catalog/catalog.go
+++ b/internal/catalog/catalog.go
@@ -1,6 +1,8 @@
 // Package catalog reads mtm's app definitions: the privacy permissions an
-// app needs, whether its license needs reactivating, notes for the
-// checklist, and paths that come and go on their own.
+// app needs, whether its license needs reactivating on the new Mac or
+// deactivating on the old one, notes for the checklist, paths that come and
+// go on their own, and what the secrets module needs to know about
+// password managers and SSH agents.
 package catalog
 
 import (
@@ -18,10 +20,21 @@ import (
 	"github.com/momentmaker/mtm/internal/pathset"
 )
 
+// Kinds of app the secrets module looks for.
+const (
+	KindPasswordManager    = "password-manager"
+	KindSecureEnclaveAgent = "secure-enclave-agent"
+)
+
+// App is one catalog file. Kind is empty or one of the Kind constants.
+// SSHAgent lists the sockets, as ~/ paths, where the app's SSH agent
+// listens, so the secrets module can tell which agent SSH uses.
 type App struct {
 	ID          string       `toml:"id"`
 	Name        string       `toml:"name"`
 	BundleID    string       `toml:"bundle_id"`
+	Kind        string       `toml:"kind"`
+	SSHAgent    []string     `toml:"ssh_agent"`
 	Permissions []Permission `toml:"permissions"`
 	License     License      `toml:"license"`
 	Notes       []string     `toml:"notes"`
@@ -43,9 +56,15 @@ func (p Permission) ClientFor(a App) string {
 	return a.BundleID
 }
 
+// License says whether the app's license must be entered again on the new
+// Mac (Reactivate, with Note), and whether it must be released on the old
+// Mac before it is erased, because the vendor counts Macs (Deactivate,
+// with DeactivateNote).
 type License struct {
-	Reactivate bool   `toml:"reactivate"`
-	Note       string `toml:"note"`
+	Reactivate     bool   `toml:"reactivate"`
+	Note           string `toml:"note"`
+	Deactivate     bool   `toml:"deactivate"`
+	DeactivateNote string `toml:"deactivate_note"`
 }
 
 // State holds volatile: home-folder paths, as pathset patterns, that the
@@ -117,6 +136,15 @@ func (a App) validate(file string) error {
 		return fmt.Errorf("name is missing")
 	case !bundlePattern.MatchString(a.BundleID):
 		return fmt.Errorf("bundle_id %q is not a reverse-DNS id", a.BundleID)
+	case a.Kind != "" && a.Kind != KindPasswordManager && a.Kind != KindSecureEnclaveAgent:
+		return fmt.Errorf("kind %q is not %q or %q", a.Kind, KindPasswordManager, KindSecureEnclaveAgent)
+	case a.License.DeactivateNote != "" && !a.License.Deactivate:
+		return fmt.Errorf("license.deactivate_note needs license.deactivate = true")
+	}
+	for _, s := range a.SSHAgent {
+		if !strings.HasPrefix(s, "~/") {
+			return fmt.Errorf("ssh_agent socket %q must start with ~/", s)
+		}
 	}
 	for _, p := range a.Permissions {
 		if _, ok := ServiceNamed(p.Service); !ok {
```

- [ ] **Step 4: Add the catalog files and document the fields**

The socket paths are the vendors' documented ones. 1Password's comes from this author's Mac. Bitwarden's are from its help pages, one for the download from its site and one for the App Store build, which is sandboxed. Secretive's is its agent's container. `TestReadmeExampleIsAValidFile` still parses the README's example, so the new keys appear there commented out or with a value that is valid.

`catalog/1password.toml`:
```toml
id = "1password"
name = "1Password"
bundle_id = "com.1password.1password"
kind = "password-manager"
ssh_agent = ["~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"]
```

`catalog/bitwarden.toml`:
```toml
id = "bitwarden"
name = "Bitwarden"
bundle_id = "com.bitwarden.desktop"
kind = "password-manager"
ssh_agent = [
  "~/.bitwarden-ssh-agent.sock",
  "~/Library/Containers/com.bitwarden.desktop/Data/.bitwarden-ssh-agent.sock",
]
```

`catalog/secretive.toml`:
```toml
id = "secretive"
name = "Secretive"
bundle_id = "com.maxgoedjen.Secretive.Host"
kind = "secure-enclave-agent"
ssh_agent = ["~/Library/Containers/com.maxgoedjen.Secretive.SecretAgent/Data/socket.ssh"]
```

`catalog/cleanshot-x.toml`:
```diff
--- a/catalog/cleanshot-x.toml
+++ b/catalog/cleanshot-x.toml
@@ -11,3 +11,5 @@ service = "Accessibility"
 [license]
 reactivate = true
 note = "Activate CleanShot X with your license key; it counts Macs, so remove the old one from your account."
+deactivate = true
+deactivate_note = "CleanShot X counts Macs: deactivate it in its settings, or remove this Mac from your CleanShot account."
```

`catalog/malwarebytes.toml`:
```diff
--- a/catalog/malwarebytes.toml
+++ b/catalog/malwarebytes.toml
@@ -8,3 +8,5 @@ service = "Full Disk Access"
 [license]
 reactivate = true
 note = "Malwarebytes Premium counts devices: sign in on the new Mac, and remove the old Mac from your account."
+deactivate = true
+deactivate_note = "Malwarebytes Premium counts devices: remove this Mac from your Malwarebytes account."
```

`catalog/README.md`:
```diff
--- a/catalog/README.md
+++ b/catalog/README.md
@@ -7,6 +7,8 @@ id = "raycast"                 # lowercase words joined by hyphens; must match t
 name = "Raycast"               # as the app names itself
 bundle_id = "com.raycast.macos"
 notes = ["Sign in to your account again if it asks."]   # extra checklist steps
+# kind = "password-manager"    # or "secure-enclave-agent"; only for those kinds of app
+# ssh_agent = ["~/.example/agent.sock"]   # where the app's SSH agent listens, if it has one
 
 [[permissions]]                # privacy permissions the app needs to work
 service = "Accessibility"      # a name from internal/catalog/services.go
@@ -15,6 +17,8 @@ service = "Accessibility"      # a name from internal/catalog/services.go
 [license]
 reactivate = true              # the license must be entered again on a new Mac
 note = "Enter your license again if it asks."
+deactivate = false             # the license must be released on the old Mac before it's erased
+# deactivate_note = "Deactivate it in Settings > License."
 
 [state]
 volatile = ["~/Library/Application Support/Raycast/logs/"]   # paths the app deletes on its own
@@ -23,7 +27,12 @@ volatile = ["~/Library/Application Support/Raycast/logs/"]   # paths the app del
 mtm turns these into checklist steps on the new Mac:
 - one per permission, unless it can read the old Mac's privacy permissions directly;
 - one for a license to reactivate;
-- one per note.
+- one per note;
+- for a password manager, one to sign in to it, and one to turn its SSH agent on again if SSH used it.
+
+On the old Mac, `mtm retire` adds a step for each license to deactivate.
+
+A `secure-enclave-agent` app keeps SSH keys in the Mac's Secure Enclave, which no migration can move, so mtm asks for new keys on the new Mac.
 
 Volatile paths that are missing after Migration Assistant are reported as info, not warnings.
 
```

- [ ] **Step 5: Update the spec**

`docs/superpowers/specs/2026-11-02-mtm-design.md`:
```diff
--- a/docs/superpowers/specs/2026-11-02-mtm-design.md
+++ b/docs/superpowers/specs/2026-11-02-mtm-design.md
@@ -200,10 +200,11 @@ volatile = ["~/Library/Application Support/com.raycast.macos/*.log"]
 - Fields:
   - identity: `id` (lowercase words joined by hyphens, matching the file name), `name`, and `bundle_id`;
   - required privacy permissions: `[[permissions]]`, each with `service` and an optional `client`, for when macOS records the grant for a helper rather than the app;
-  - license reactivation: `[license]`, with `reactivate` and a `note`. Deactivate steps for `retire` come with `retire`.
+  - licenses: `[license]`, with `reactivate` and a `note` for the new Mac, and `deactivate` and a `deactivate_note` for apps that count Macs, which `retire` turns into a step on the old Mac;
+  - for the `secrets` module, `kind` (`password-manager` or `secure-enclave-agent`) and `ssh_agent`, the `~/` paths of the sockets the app's SSH agent listens on;
   - per-app checklist `notes`;
   - `[state]` `volatile` paths (`~/` patterns of files the app deletes on its own). The `files` module treats a missing one as info.
-- The tests reject a file with an unknown key, an unknown service, a malformed id or bundle id, or a bundle id already in another file.
+- The tests reject a file with an unknown key, an unknown service or kind, a malformed id or bundle id, a bundle id already in another file, an `ssh_agent` or `volatile` path outside `~/`, or a `deactivate_note` without `deactivate`.
 - Privacy panes live in one table in `internal/catalog`, with each pane's name, its privacy-database service id, and its System Settings deep link per macOS major version. They are not repeated in each app file. A macOS major without a checked link gets the pane's name and no link.
 - Initial data is seeded from the `tape` project's definitions (BSD-2-Clause, credited). mackup's GPL-3 database is not copied.
 - Adding an app means adding a file. The contributor guide documents the format, and CI validates every file.
```

- [ ] **Step 6: Run the tests to verify they pass**

Run:
```bash
gofmt -l .
go vet ./...
go vet -tags integration ./...
go test ./...
```
Expected: `gofmt -l .` prints nothing, and the rest pass.

- [ ] **Step 7: Commit**

End the message with your `Co-Authored-By:` trailer (Global Constraints).

```bash
git add internal/catalog/catalog_test.go internal/catalog/catalog.go catalog/1password.toml catalog/bitwarden.toml catalog/secretive.toml catalog/cleanshot-x.toml catalog/malwarebytes.toml catalog/README.md docs/superpowers/specs/2026-11-02-mtm-design.md
git commit -m "Teach the catalog about password managers, SSH agents, and deactivation

The secrets module needs to know which apps are password managers and
where their SSH agents listen, and which agent keeps keys in the Secure
Enclave. retire needs to know which licenses count Macs, so they can be
released before the old Mac is erased. Adds 1Password, Bitwarden, and
Secretive."
```

---

### Task 3: The `browsers` module

**Files:**
- Create: `internal/modules/browsers/browsers.go`
- Create: `internal/modules/browsers/profiles.go`
- Modify: `cmd/mtm/main.go`
- Modify: `cmd/mtm/nonetwork_integration_test.go`
- Modify: `docs/superpowers/specs/2026-11-02-mtm-design.md`
- Test: `internal/modules/browsers/browsers_test.go`, `cmd/mtm/main_test.go` (modify)

**Interfaces:**
- Consumes:
  - `module.Module`, `module.Stepper`, `module.LoadInventory`, `runner.ExitCode`, `words.Count`, and `checklist.Item` (earlier stages);
  - `testutil.WriteFile` and `testutil.Copy` in tests.
- Produces:
  - `browsers.Name = "browsers"`, `browsers.New() Module`, and `Module`, which implements `module.Module` and `module.Stepper`.
  - `browsers.Inventory{Browsers []Browser; Safari string}`, `Browser{ID string; Profiles []Profile; SafeStorage bool}`, and `Profile{Dir, Name string; Extensions []string}`.
  - The constants `SafariReadable`, `SafariUnreadable`, and `SafariAbsent`.
  - Check ids `browsers:<id>:profiles` and `browsers:<id>:safe-storage`, both critical, plus `browsers:<id>:extensions` and `browsers:safari`.
  - Step ids `browsers:google:<id>` (Chromium only) and `browsers:sites:<id>`, where `<id>` is `chrome`, `brave`, `arc`, or `firefox`.

**Why it works this way:**
- **Keychain:** the Safe Storage item is looked up with `security find-generic-password -s "<Browser> Safe Storage"`. Without `-w` or `-g`, that prints only the item's attributes (spec 6.2). Exit 44 means no such item; any other failure is an error.
- **Which browsers are recorded:** only those with profiles, so a leftover data folder from a deleted browser costs no keychain lookup. This author's Mac has one, from Arc.
- **What is read:** `Local State` gives each profile's folder and display name; its account fields are never recorded. A profile key that isn't a single folder name is ignored, and so is a Firefox relative path that climbs out with `..`, so an edited file can't send mtm reading elsewhere.
- **A profile list mtm can't read** is a warning, not a module failure, so it can't hold up the migration.
- **verify:** a missing profile or Safe Storage key fails, because the old Mac has the only other copy. A missing extension only warns, because the extension store has it. This is spec §9.9 as updated in Step 5, and §8.5's list of critical checks grows to match.

- [ ] **Step 1: Write the failing tests**

The fixtures are a temporary home folder with:
- Chrome's `Local State`, with two real profiles, a profile listed without a folder, and a key that tries to leave Chrome's folder; the folders those entries point at exist;
- extension folders, including Chrome's `Temp` working folder;
- Firefox's `profiles.ini` and `extensions.json`;
- an Arc folder that never ran.

The fake runner has no fallback, so no test can reach the real keychain.

`internal/modules/browsers/browsers_test.go`:
```go
package browsers

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/testutil"
)

const (
	chromeDir  = "Library/Application Support/Google/Chrome"
	firefoxDir = "Library/Application Support/Firefox"
	extA       = "aapbdbdomjkkjkaonfhkkikfgjllcleb"
	extB       = "nngceckbapebfimnlniiiahkandclblb"
)

// localState is Chrome's Local State with two profiles, one listed
// without a folder, and a key that tries to leave Chrome's folder. The
// account fields are there because real files have them; mtm must not
// record them.
const localState = `{"profile":{"info_cache":{
"Default":{"name":"Person 1","user_name":"dev@example.com","gaia_id":"123"},
"Profile 1":{"name":"Work"},
"Profile 9":{"name":"Deleted"},
"../escape":{"name":"Elsewhere"}}}}`

const profilesINI = `[Install4F96D1932A9F858E]
Default=Profiles/abcd.default-release

[Profile0]
Name=default-release
IsRelative=1
Path=Profiles/abcd.default-release

[Profile1]
Name=escape
IsRelative=1
Path=../../Documents

[General]
Version=2
`

const extensionsJSON = `{"addons":[
{"id":"uBlock0@raymondhill.net","type":"extension","location":"app-profile"},
{"id":"formautofill@mozilla.org","type":"extension","location":"app-builtin"},
{"id":"en-US@dictionaries","type":"dictionary","location":"app-profile"}]}`

// seedHome is an old Mac's home folder with Chrome and Firefox profiles,
// and the leftovers of an Arc that never ran. The folders the escaping
// profile entries point at exist, so only mtm's own check keeps them out.
func seedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	chrome := filepath.Join(home, chromeDir)
	testutil.WriteFile(t, filepath.Join(chrome, "Local State"), localState)
	testutil.WriteFile(t, filepath.Join(chrome, "Default", "Extensions", extA, "1.0", "manifest.json"), "{}")
	testutil.WriteFile(t, filepath.Join(chrome, "Default", "Extensions", "Temp", "x"), "")
	testutil.WriteFile(t, filepath.Join(chrome, "Profile 1", "Extensions", extB, "2.0", "manifest.json"), "{}")
	testutil.WriteFile(t, filepath.Join(home, "Library/Application Support/Google/escape/Extensions", extB, "manifest.json"), "{}")
	testutil.WriteFile(t, filepath.Join(home, "Library/Application Support/Arc/User Data/Crashpad/settings.dat"), "")
	firefox := filepath.Join(home, firefoxDir)
	testutil.WriteFile(t, filepath.Join(firefox, "profiles.ini"), profilesINI)
	testutil.WriteFile(t, filepath.Join(firefox, "Profiles", "abcd.default-release", "extensions.json"), extensionsJSON)
	testutil.WriteFile(t, filepath.Join(home, "Library", "Documents", "extensions.json"), extensionsJSON)
	return home
}

const chromeKeychain = "/usr/bin/security find-generic-password -s Chrome Safe Storage"

// keychain answers the Safe Storage lookup: found, or exit 44, not found.
func keychain(found bool) *runner.Fake {
	resp := runner.Response{Stdout: `keychain: "/Users/dev/Library/Keychains/login.keychain-db"`}
	if !found {
		resp = runner.Response{Code: 44, Stderr: "security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain."}
	}
	return &runner.Fake{Responses: map[string]runner.Response{chromeKeychain: resp}}
}

func env(home string, r runner.Runner, m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: r, Home: home, Manifest: m}
}

var wantInventory = Inventory{
	Browsers: []Browser{
		{ID: "chrome", SafeStorage: true, Profiles: []Profile{
			{Dir: "Default", Name: "Person 1", Extensions: []string{extA}},
			{Dir: "Profile 1", Name: "Work", Extensions: []string{extB}},
		}},
		{ID: "firefox", Profiles: []Profile{
			{Dir: "Profiles/abcd.default-release", Name: "default-release", Extensions: []string{"uBlock0@raymondhill.net"}},
		}},
	},
	Safari: SafariAbsent,
}

func prepared(t *testing.T, home string) (Inventory, *manifest.Manifest) {
	t.Helper()
	res, err := New().Prepare(env(home, keychain(true), nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), res.Inventory); err != nil {
		t.Fatal(err)
	}
	return res.Inventory.(Inventory), m
}

func TestPrepareRecordsProfilesExtensionsAndSafeStorage(t *testing.T) {
	inv, _ := prepared(t, seedHome(t))
	if !reflect.DeepEqual(inv, wantInventory) {
		t.Errorf("inventory = %+v\nwant        %+v", inv, wantInventory)
	}
}

func TestPrepareAsksTheKeychainOnlyAboutBrowsersWithProfiles(t *testing.T) {
	fake := keychain(true)
	if _, err := New().Prepare(env(seedHome(t), fake, nil), nil); err != nil {
		t.Fatal(err)
	}
	if calls := fake.Calls(); len(calls) != 1 || calls[0].String() != chromeKeychain {
		t.Errorf("commands = %v, want only Chrome's Safe Storage lookup", calls)
	}
}

func TestPrepareWarnsAboutAProfileListItCannotRead(t *testing.T) {
	home := seedHome(t)
	testutil.WriteFile(t, filepath.Join(home, chromeDir, "Local State"), "{")
	res, err := New().Prepare(env(home, keychain(true), nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || res.Problems[0].ID != "chrome:unreadable" || res.Problems[0].Severity != module.SeverityWarn {
		t.Errorf("problems = %+v, want a warning for Chrome", res.Problems)
	}
	if inv := res.Inventory.(Inventory); len(inv.Browsers) != 1 || inv.Browsers[0].ID != "firefox" {
		t.Errorf("browsers = %+v, want Firefox still recorded", inv.Browsers)
	}
}

func TestPrepareFailsWhenTheKeychainCannotBeAsked(t *testing.T) {
	fake := &runner.Fake{Responses: map[string]runner.Response{chromeKeychain: {Code: 51, Stderr: "security: interaction not allowed"}}}
	if _, err := New().Prepare(env(seedHome(t), fake, nil), nil); err == nil || !strings.Contains(err.Error(), "Chrome Safe Storage") {
		t.Errorf("err = %v, want the failed lookup", err)
	}
}

func TestSafariWithoutFullDiskAccess(t *testing.T) {
	home := t.TempDir()
	safariDir := filepath.Join(home, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Mode 000 stands in for macOS's privacy protection, which refuses
	// the same way without Full Disk Access.
	if err := os.Chmod(safariDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(safariDir, 0o755) })
	findings, err := New().Scan(env(home, keychain(true), nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].ID != "safari" || !strings.Contains(findings[0].Title, "Full Disk Access") {
		t.Errorf("findings = %+v, want Safari's note", findings)
	}
}

func TestScanSummarizesEachBrowser(t *testing.T) {
	findings, err := New().Scan(env(seedHome(t), keychain(true), nil))
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, f := range findings {
		titles = append(titles, f.Title)
	}
	if want := []string{"Chrome: 2 profiles, 2 extensions", "Firefox: 1 profile, 1 extension"}; !slices.Equal(titles, want) {
		t.Errorf("titles = %q, want %q", titles, want)
	}
}

func checkByID(checks []module.Check, id string) (module.Check, bool) {
	i := slices.IndexFunc(checks, func(c module.Check) bool { return c.ID == id })
	if i < 0 {
		return module.Check{}, false
	}
	return checks[i], true
}

func TestVerifyPassesWhenEverythingArrived(t *testing.T) {
	old := seedHome(t)
	_, m := prepared(t, old)
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, old, newHome)
	checks, err := New().Verify(env(newHome, keychain(true), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range checks {
		if c.Status != module.StatusPass {
			t.Errorf("%s = %s (%s), want pass", c.ID, c.Status, c.Detail)
		}
		ids = append(ids, c.ID)
	}
	want := []string{"browsers:chrome:profiles", "browsers:chrome:extensions", "browsers:chrome:safe-storage", "browsers:firefox:profiles", "browsers:firefox:extensions"}
	if !slices.Equal(ids, want) {
		t.Errorf("checks = %q, want %q", ids, want)
	}
}

func TestVerifyFailsAMissingProfile(t *testing.T) {
	old := seedHome(t)
	_, m := prepared(t, old)
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, old, newHome)
	if err := os.RemoveAll(filepath.Join(newHome, chromeDir, "Profile 1")); err != nil {
		t.Fatal(err)
	}
	checks, err := New().Verify(env(newHome, keychain(true), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := checkByID(checks, "browsers:chrome:profiles")
	if c.Status != module.StatusFail || !c.Critical || !slices.Equal(c.Paths, []string{"Work (Profile 1)"}) {
		t.Errorf("profiles check = %+v, want a critical fail naming Work", c)
	}
}

func TestVerifyWarnsAboutAMissingExtension(t *testing.T) {
	old := seedHome(t)
	_, m := prepared(t, old)
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, old, newHome)
	if err := os.RemoveAll(filepath.Join(newHome, chromeDir, "Default", "Extensions", extA)); err != nil {
		t.Fatal(err)
	}
	checks, err := New().Verify(env(newHome, keychain(true), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := checkByID(checks, "browsers:chrome:extensions")
	if c.Status != module.StatusWarn || c.Critical || !slices.Equal(c.Paths, []string{"Person 1 (Default): " + extA}) {
		t.Errorf("extensions check = %+v, want a warning naming the extension", c)
	}
}

func TestVerifyFailsWhenTheSafeStorageKeyIsMissing(t *testing.T) {
	old := seedHome(t)
	_, m := prepared(t, old)
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, old, newHome)
	checks, err := New().Verify(env(newHome, keychain(false), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := checkByID(checks, "browsers:chrome:safe-storage")
	if c.Status != module.StatusFail || !c.Critical || !strings.Contains(c.Detail, "Keychain Access") {
		t.Errorf("Safe Storage check = %+v, want a critical fail with the fix", c)
	}
}

func TestStepsAskForGoogleInChromiumAndSitesInEveryBrowser(t *testing.T) {
	_, m := prepared(t, seedHome(t))
	items, err := New().Steps(env(t.TempDir(), keychain(true), m))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if want := []string{"browsers:google:chrome", "browsers:sites:chrome", "browsers:sites:firefox"}; !slices.Equal(ids, want) {
		t.Errorf("steps = %q, want %q", ids, want)
	}
	if !strings.Contains(items[0].Detail, "Person 1 (Default), Work (Profile 1)") {
		t.Errorf("Google step detail = %q, want the profile names", items[0].Detail)
	}
}

func TestVerifyNotesSafari(t *testing.T) {
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), Inventory{Safari: SafariUnreadable}); err != nil {
		t.Fatal(err)
	}
	checks, err := New().Verify(env(t.TempDir(), keychain(true), m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].ID != "browsers:safari" || checks[0].Status != module.StatusInfo {
		t.Errorf("checks = %+v, want Safari's info", checks)
	}
}
```

`cmd/mtm/main_test.go`:
```diff
--- a/cmd/mtm/main_test.go
+++ b/cmd/mtm/main_test.go
@@ -279,7 +279,7 @@ func TestDefaultAppModules(t *testing.T) {
 	for _, m := range defaultApp(t.TempDir()).Modules {
 		names = append(names, m.Name())
 	}
-	if want := []string{"files", "git", "system", "databases", "docker", "homebrew", "toolchains", "loginitems", "permissions"}; !slices.Equal(names, want) {
+	if want := []string{"files", "git", "system", "databases", "docker", "homebrew", "toolchains", "loginitems", "permissions", "browsers"}; !slices.Equal(names, want) {
 		t.Errorf("modules = %q, want %q", names, want)
 	}
 }
@@ -291,7 +291,7 @@ func TestDefaultAppModulesThatLeaveSteps(t *testing.T) {
 			names = append(names, m.Name())
 		}
 	}
-	if want := []string{"system", "loginitems", "permissions"}; !slices.Equal(names, want) {
+	if want := []string{"system", "loginitems", "permissions", "browsers"}; !slices.Equal(names, want) {
 		t.Errorf("modules with steps = %q, want %q", names, want)
 	}
 }
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/browsers/ ./cmd/mtm/`
Expected: FAIL. The new package has no code yet, and `defaultApp` doesn't list `browsers`:
```text
FAIL	github.com/momentmaker/mtm/internal/modules/browsers [build failed]
--- FAIL: TestDefaultAppModules (0.01s)
    main_test.go:283: modules = ["files" "git" "system" "databases" "docker" "homebrew" "toolchains" "loginitems" "permissions"], want ["files" "git" "system" "
--- FAIL: TestDefaultAppModulesThatLeaveSteps (0.00s)
    main_test.go:295: modules with steps = ["system" "loginitems" "permissions"], want ["system" "loginitems" "permissions" "browsers"]
FAIL
FAIL	github.com/momentmaker/mtm/cmd/mtm	0.990s
FAIL
```

- [ ] **Step 3: Implement the module**

`internal/modules/browsers/browsers.go`:
```go
// Package browsers records the old Mac's browser profiles and extensions,
// and whether each Chromium browser's Safe Storage key is in the keychain:
// without that key, the saved passwords and cookies Migration Assistant
// copies can't be decrypted on the new Mac.
package browsers

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const Name = "browsers"

type family int

const (
	chromium family = iota
	firefox
)

// browser is one browser mtm knows: its data folder, relative to the home
// folder, and for Chromium browsers the keychain item holding the key
// that encrypts saved passwords and cookies.
type browser struct {
	id          string
	name        string
	family      family
	dir         string
	safeStorage string
}

var known = []browser{
	{"chrome", "Chrome", chromium, "Library/Application Support/Google/Chrome", "Chrome Safe Storage"},
	{"brave", "Brave", chromium, "Library/Application Support/BraveSoftware/Brave-Browser", "Brave Safe Storage"},
	{"arc", "Arc", chromium, "Library/Application Support/Arc/User Data", "Arc Safe Storage"},
	{"firefox", "Firefox", firefox, "Library/Application Support/Firefox", ""},
}

func byID(id string) (browser, bool) {
	for _, b := range known {
		if b.id == id {
			return b, true
		}
	}
	return browser{}, false
}

// What prepare could see of Safari's data, which needs Full Disk Access.
const (
	SafariReadable   = "readable"
	SafariUnreadable = "unreadable"
	SafariAbsent     = "absent"
)

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return Name }

// Inventory lists the browsers that had profiles. Safari is one of the
// Safari constants.
type Inventory struct {
	Browsers []Browser `json:"browsers,omitempty"`
	Safari   string    `json:"safari"`
}

// Browser is one browser's profiles. SafeStorage is set when its Safe
// Storage keychain item existed.
type Browser struct {
	ID          string    `json:"id"`
	Profiles    []Profile `json:"profiles"`
	SafeStorage bool      `json:"safe_storage,omitempty"`
}

// Profile is a profile's folder, relative to the browser's data folder (or
// absolute, for a Firefox profile kept elsewhere), the name the browser
// shows for it, and its extension ids.
type Profile struct {
	Dir        string   `json:"dir"`
	Name       string   `json:"name,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
}

func (p Profile) label() string {
	if p.Name == "" || p.Name == p.Dir {
		return p.Dir
	}
	return p.Name + " (" + p.Dir + ")"
}

func (b Browser) extensions() int {
	n := 0
	for _, p := range b.Profiles {
		n += len(p.Extensions)
	}
	return n
}

// profiles reads a browser's profiles, or none when it has never run.
func profiles(home string, b browser) ([]Profile, error) {
	root := filepath.Join(home, filepath.FromSlash(b.dir))
	if b.family == firefox {
		return firefoxProfiles(root)
	}
	return chromiumProfiles(root)
}

// look records the browsers that have profiles. A browser whose data mtm
// can't read becomes a warning rather than an error, so it can't keep the
// rest of the migration from going ahead.
func look(env module.Env) (Inventory, []module.Finding, error) {
	inv := Inventory{Safari: safari(env.Home)}
	var problems []module.Finding
	for _, b := range known {
		ps, err := profiles(env.Home, b)
		if err != nil {
			problems = append(problems, module.Finding{
				Module: Name, ID: b.id + ":unreadable", Severity: module.SeverityWarn, Handler: module.Manual,
				Title:  "mtm can't read " + b.name + "'s profiles, so verify won't check them",
				Detail: err.Error(),
				Fix:    "Open " + b.name + " on the new Mac and check your profiles are there before you erase this Mac.",
			})
			continue
		}
		if len(ps) == 0 {
			continue
		}
		rec := Browser{ID: b.id, Profiles: ps}
		if b.safeStorage != "" {
			if rec.SafeStorage, err = hasKeychainItem(env, b.safeStorage); err != nil {
				return Inventory{}, nil, err
			}
		}
		inv.Browsers = append(inv.Browsers, rec)
	}
	return inv, problems, nil
}

// hasKeychainItem asks the keychain whether an item exists. Without -w or
// -g, security prints only the item's attributes, never its secret (spec
// 6.2). It exits 44 when there is no such item.
func hasKeychainItem(env module.Env, service string) (bool, error) {
	_, err := env.Run.Run(env.Ctx, runner.Command{Name: "/usr/bin/security", Args: []string{"find-generic-password", "-s", service}})
	if code, ok := runner.ExitCode(err); ok && code == 44 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look for %q in the keychain: %w", service, err)
	}
	return true, nil
}

// safari says whether Safari's data folder could be read. macOS protects
// it, so without Full Disk Access it can't.
func safari(home string) string {
	_, err := os.ReadDir(filepath.Join(home, "Library", "Safari"))
	switch {
	case err == nil:
		return SafariReadable
	case errors.Is(err, fs.ErrNotExist):
		return SafariAbsent
	}
	return SafariUnreadable
}

func (Module) Scan(env module.Env) ([]module.Finding, error) {
	inv, findings, err := look(env)
	if err != nil {
		return nil, err
	}
	for _, rec := range inv.Browsers {
		b, _ := byID(rec.ID)
		detail := "Migration Assistant copies them. verify checks they arrived."
		if b.family == chromium {
			detail += " On the new Mac you'll sign in to Google again; mtm checklist lists it."
		}
		findings = append(findings, module.Finding{
			Module: Name, ID: rec.ID, Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
			Title:  b.name + ": " + words.Count(len(rec.Profiles), "profile", "profiles") + ", " + words.Count(rec.extensions(), "extension", "extensions"),
			Detail: detail,
		})
	}
	if inv.Safari == SafariUnreadable {
		findings = append(findings, module.Finding{
			Module: Name, ID: "safari", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
			Title:  "Safari: mtm can't read its data without Full Disk Access",
			Detail: "Migration Assistant copies it, and its bookmarks, tabs, and passwords also sync through iCloud.",
		})
	}
	return findings, nil
}

func (Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	inv, problems, err := look(env)
	if err != nil {
		return module.Result{}, err
	}
	n := 0
	for _, b := range inv.Browsers {
		n += len(b.Profiles)
	}
	return module.Result{
		Inventory: inv,
		Done:      []string{"recorded " + words.Count(len(inv.Browsers), "browser", "browsers") + " with " + words.Count(n, "profile", "profiles")},
		Problems:  problems,
	}, nil
}

// Finish has nothing to restore: Migration Assistant copies the profiles,
// and the login keychain with the Safe Storage keys.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}

// Steps asks the user to sign in to Google again in each Chromium browser,
// because Google binds its sessions to the old Mac's hardware, and to check
// that other sites still know them.
func (Module) Steps(env module.Env) ([]checklist.Item, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	var items []checklist.Item
	for _, rec := range inv.Browsers {
		b, ok := byID(rec.ID)
		if !ok || len(rec.Profiles) == 0 {
			continue
		}
		if b.family == chromium {
			var names []string
			for _, p := range rec.Profiles {
				names = append(names, p.label())
			}
			items = append(items, checklist.Item{
				ID: Name + ":google:" + b.id, Module: Name, Where: b.name,
				Title:  "Sign in to Google again in " + b.name,
				Detail: "Google ties its sign-in to the Mac it happened on, so Gmail, YouTube, and Docs ask again. Do it in each profile that uses Google: " + strings.Join(names, ", ") + ".",
			})
		}
		items = append(items, checklist.Item{
			ID: Name + ":sites:" + b.id, Module: Name, Where: b.name,
			Title:  "Check you're still signed in to the sites you use most in " + b.name,
			Detail: "Your cookies came across with Migration Assistant, so most sites should still know you.",
		})
	}
	return items, nil
}

func (Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	var checks []module.Check
	for _, rec := range inv.Browsers {
		b, ok := byID(rec.ID)
		if !ok {
			checks = append(checks, module.Check{Module: Name, ID: Name + ":" + rec.ID, Status: module.StatusFail, Critical: true,
				Title: rec.ID, Detail: "recorded by a newer mtm; upgrade mtm on this Mac and run mtm verify again"})
			continue
		}
		cs, err := verifyBrowser(env, b, rec)
		if err != nil {
			return nil, err
		}
		checks = append(checks, cs...)
	}
	if c, ok := safariCheck(inv.Safari); ok {
		checks = append(checks, c)
	}
	return checks, nil
}

// verifyBrowser fails a missing profile or Safe Storage key: either loses
// data that only the old Mac still has. A missing extension only warns,
// since the extension store has it.
func verifyBrowser(env module.Env, b browser, rec Browser) ([]module.Check, error) {
	id := Name + ":" + b.id
	now, err := profiles(env.Home, b)
	if err != nil {
		return []module.Check{{Module: Name, ID: id + ":profiles", Status: module.StatusFail, Critical: true,
			Title: b.name + " profiles", Detail: "can't read them: " + err.Error()}}, nil
	}
	present := map[string]Profile{}
	for _, p := range now {
		present[p.Dir] = p
	}
	var missing, missingExt []string
	exts := 0
	for _, p := range rec.Profiles {
		got, ok := present[p.Dir]
		if !ok {
			missing = append(missing, p.label())
			continue
		}
		for _, e := range p.Extensions {
			exts++
			if !slices.Contains(got.Extensions, e) {
				missingExt = append(missingExt, p.label()+": "+e)
			}
		}
	}
	profilesCheck := module.Check{Module: Name, ID: id + ":profiles", Status: module.StatusPass, Critical: true,
		Title: b.name + " profiles", Detail: words.Count(len(rec.Profiles), "profile", "profiles") + " arrived"}
	if len(missing) > 0 {
		profilesCheck.Status = module.StatusFail
		profilesCheck.Detail = words.Count(len(missing), "profile is", "profiles are") + " missing; copy them from the old Mac's ~/" + b.dir
		profilesCheck.Paths = missing
	}
	checks := []module.Check{profilesCheck}
	if exts > 0 {
		c := module.Check{Module: Name, ID: id + ":extensions", Status: module.StatusPass,
			Title: b.name + " extensions", Detail: words.Count(exts, "extension", "extensions") + " arrived"}
		if len(missingExt) > 0 {
			c.Status = module.StatusWarn
			c.Detail = words.Count(len(missingExt), "extension is", "extensions are") + " missing; add them again from the extension store"
			c.Paths = missingExt
		}
		checks = append(checks, c)
	}
	if rec.SafeStorage {
		c, err := safeStorageCheck(env, b)
		if err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	return checks, nil
}

func safeStorageCheck(env module.Env, b browser) (module.Check, error) {
	c := module.Check{Module: Name, ID: Name + ":" + b.id + ":safe-storage", Status: module.StatusPass, Critical: true,
		Title: b.name + " Safe Storage key", Detail: "in the keychain"}
	found, err := hasKeychainItem(env, b.safeStorage)
	if err != nil {
		return module.Check{}, err
	}
	if !found {
		c.Status = module.StatusFail
		c.Detail = "missing from the keychain, so " + b.name + " can't read the saved passwords and cookies from the old Mac. " +
			"Before you erase it, copy the password of its \"" + b.safeStorage + "\" item in Keychain Access to the same item here, or export " + b.name + "'s passwords there"
	}
	return c, nil
}

func safariCheck(seen string) (module.Check, bool) {
	c := module.Check{Module: Name, ID: Name + ":safari", Status: module.StatusInfo, Title: "Safari"}
	switch seen {
	case SafariReadable:
		c.Detail = "its data was copied with your files; files checks it"
	case SafariUnreadable:
		c.Detail = "mtm couldn't read it on the old Mac; open Safari and check your bookmarks, which also sync through iCloud"
	default:
		return module.Check{}, false
	}
	return c, true
}
```

`internal/modules/browsers/profiles.go`:
```go
package browsers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// chromiumExtension is the form of a Chromium extension id; the
// Extensions folder also holds working folders such as Temp.
var chromiumExtension = regexp.MustCompile(`^[a-p]{32}$`)

// chromiumProfiles reads the profile list in Local State, which maps each
// profile's folder to the name the browser shows. Only the name is read:
// Local State also holds account details mtm has no use for. A browser
// with no Local State has never run, or left only leftovers, and has no
// profiles.
func chromiumProfiles(root string) ([]Profile, error) {
	path := filepath.Join(root, "Local State")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var state struct {
		Profile struct {
			InfoCache map[string]struct {
				Name string `json:"name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var out []Profile
	for _, dir := range slices.Sorted(maps.Keys(state.Profile.InfoCache)) {
		if !isFolderName(dir) {
			continue
		}
		folder := filepath.Join(root, dir)
		if _, err := os.Stat(folder); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		exts, err := chromiumExtensions(filepath.Join(folder, "Extensions"))
		if err != nil {
			return nil, err
		}
		out = append(out, Profile{Dir: dir, Name: state.Profile.InfoCache[dir].Name, Extensions: exts})
	}
	return out, nil
}

// isFolderName rejects a profile key that isn't one folder inside the
// browser's, so an edited Local State can't send mtm looking elsewhere.
func isFolderName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsRune(s, '/')
}

func chromiumExtensions(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && chromiumExtension.MatchString(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// firefoxProfiles reads profiles.ini. Each [Profile<n>] section has the
// profile's Name and Path, which is relative to the Firefox folder when
// IsRelative=1.
func firefoxProfiles(root string) ([]Profile, error) {
	path := filepath.Join(root, "profiles.ini")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	sections, err := iniSections(data)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var out []Profile
	for _, sec := range sections {
		if !strings.HasPrefix(sec.name, "Profile") || sec.keys["Path"] == "" {
			continue
		}
		p := Profile{Dir: sec.keys["Path"], Name: sec.keys["Name"]}
		folder := p.Dir
		if sec.keys["IsRelative"] == "1" {
			if !isRelativeFolder(p.Dir) {
				continue
			}
			folder = filepath.Join(root, filepath.FromSlash(p.Dir))
		}
		if _, err := os.Stat(folder); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if p.Extensions, err = firefoxExtensions(folder); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// isRelativeFolder rejects a relative profile path that climbs out of the
// Firefox folder.
func isRelativeFolder(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") {
		return false
	}
	return !slices.Contains(strings.Split(p, "/"), "..")
}

// firefoxExtensions reads the ids of the extensions the user installed
// from extensions.json. Firefox lists its built-in ones there too, under
// other locations.
func firefoxExtensions(profile string) ([]string, error) {
	path := filepath.Join(profile, "extensions.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc struct {
		Addons []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Location string `json:"location"`
		} `json:"addons"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var ids []string
	for _, a := range doc.Addons {
		if a.Type == "extension" && a.Location == "app-profile" && a.ID != "" {
			ids = append(ids, a.ID)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

type iniSection struct {
	name string
	keys map[string]string
}

// iniSections parses the simple INI Firefox writes: [Section] headers and
// Key=Value lines.
func iniSections(data []byte) ([]iniSection, error) {
	var out []iniSection
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			out = append(out, iniSection{name: line[1 : len(line)-1], keys: map[string]string{}})
		case len(out) > 0:
			if k, v, ok := strings.Cut(line, "="); ok {
				out[len(out)-1].keys[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	return out, sc.Err()
}
```

- [ ] **Step 4: Wire it in**

`browsers` goes after `permissions`. The no-network test prepares it too: with a temporary home folder it finds no browsers, so it never asks the keychain.

`cmd/mtm/main.go`:
```diff
--- a/cmd/mtm/main.go
+++ b/cmd/mtm/main.go
@@ -16,6 +16,7 @@ import (
 	"github.com/momentmaker/mtm/internal/app"
 	"github.com/momentmaker/mtm/internal/catalog"
 	"github.com/momentmaker/mtm/internal/module"
+	"github.com/momentmaker/mtm/internal/modules/browsers"
 	"github.com/momentmaker/mtm/internal/modules/databases"
 	"github.com/momentmaker/mtm/internal/modules/docker"
 	"github.com/momentmaker/mtm/internal/modules/files"
@@ -87,6 +88,7 @@ func defaultApp(home string) *app.App {
 		Modules: []module.Module{
 			files.New(cat.Volatile()...), git.New(), system.New(name, cat), databases.New(), docker.New(),
 			homebrew.New(name), toolchains.New(name), loginitems.New(os.Getuid()), permissions.New(cat),
+			browsers.New(),
 		},
 	}
 }
```

`cmd/mtm/nonetwork_integration_test.go`:
```diff
--- a/cmd/mtm/nonetwork_integration_test.go
+++ b/cmd/mtm/nonetwork_integration_test.go
@@ -142,9 +142,9 @@ func TestNoNetwork(t *testing.T) {
 		catalogFallback bool
 	}{
 		{[]string{"scan"}, []int{0, 1}, false, true},
-		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions"}, []int{0, 1}, true, true},
+		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions,browsers"}, []int{0, 1}, true, true},
 		{[]string{"prepare", "--undo"}, []int{0, 1}, false, false},
-		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions"}, []int{0, 1}, true, true},
+		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions,browsers"}, []int{0, 1}, true, true},
 		{[]string{"verify"}, []int{1}, false, false},
 	}
 	for _, s := range steps {
```

- [ ] **Step 5: Update the spec**

`docs/superpowers/specs/2026-11-02-mtm-design.md`:
```diff
--- a/docs/superpowers/specs/2026-11-02-mtm-design.md
+++ b/docs/superpowers/specs/2026-11-02-mtm-design.md
@@ -264,7 +264,7 @@ Files on the new Mac that are not in the record are ignored. Reports group resul
 ### 8.5 Verdict
 **SAFE TO WIPE** requires all of:
 1. No check in any module has status `fail`. This includes the preflight checks: FileVault is on, and `verify` is running on a different Mac from the one that ran `prepare` (compared by machine id).
-2. Every check marked critical has status `pass`, not `warn` or `info`. Critical checks are those from `git`, `secrets` identities, `docker` volumes, `databases`, and `critical`-area files.
+2. Every check marked critical has status `pass`, not `warn` or `info`. Critical checks are those from `git`, `secrets` identities, `docker` volumes, `databases`, `browsers` profiles and Safe Storage keys, and `critical`-area files.
 3. Every checklist item is either done or skipped with a recorded reason (`mtm checklist skip <id> --reason "..."`).
 
 Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0` safe, `1` not safe, `2` `mtm` itself failed. The other commands use `0` success, `1` completed with problems (such as NOT READY), and `2` `mtm` failed.
@@ -417,12 +417,17 @@ Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0`
 
 ### 9.9 `browsers`
 - **Chromium family (Chrome, Brave, Arc):**
-  - records profile names from `Local State` and extension ids per profile;
-  - records whether the browser's "Safe Storage" keychain item exists (presence only, section 6.2).
-- **Firefox:** profiles from `profiles.ini` and extension ids.
+  - records each profile's folder and the name the browser shows for it, from `Local State`, and the extension ids in each profile's `Extensions` folder. `Local State` also holds account details, which `mtm` doesn't record;
+  - records whether the browser's "Safe Storage" keychain item exists (presence only, section 6.2): `security find-generic-password -s "<Browser> Safe Storage"`, which exits 44 when there is none. The item holds the key that encrypts the browser's saved passwords and cookies.
+- **Firefox:** profiles from `profiles.ini`, and the ids of the extensions the user installed, from each profile's `extensions.json`.
 - **Safari:** relies on iCloud sync. Records only whether Safari data was readable.
-- `finish` adds checklist items: sign in to Google again in each Chromium profile, and confirm two or three named sites are still logged in.
-- `verify`: profiles and extensions present, and each Safe Storage item present.
+- A browser without `Local State` or `profiles.ini` has never run and isn't recorded. A profile list `mtm` can't read is a warning, and verify doesn't check that browser. Profile entries that point outside the browser's folder are ignored.
+- **Checklist steps** (section 7.3): per Chromium browser, sign in to Google again, naming the profiles to do it in; per browser, check that the sites used most are still signed in.
+- **`verify`:**
+  - each recorded profile present: a critical fail otherwise, since the old Mac has the only other copy;
+  - each extension present: a warn otherwise, since the extension store has it;
+  - each recorded Safe Storage item present: a critical fail otherwise, with how to copy it from the old Mac's Keychain Access. Without it the copied passwords and cookies can't be decrypted;
+  - Safari: an info check.
 
 ### 9.10 `secrets`
 - Records `~/.ssh` key names and types (`ssh-keygen -l` on public keys), and `gpg --list-secret-keys` identities (never key material).
```

- [ ] **Step 6: Run the tests to verify they pass**

Run:
```bash
gofmt -l .
go vet ./...
go vet -tags integration ./...
go test ./...
```
Expected: `gofmt -l .` prints nothing, and the rest pass.

Then run the no-network test, which uses a temporary home folder and is safe to run:
```bash
go test -tags integration -run TestNoNetwork -v ./cmd/mtm/
```
Expected: PASS, or SKIP with its reason, such as a Homebrew service running.

- [ ] **Step 7: Commit**

End the message with your `Co-Authored-By:` trailer (Global Constraints).

```bash
git add internal/modules/browsers/browsers_test.go cmd/mtm/main_test.go internal/modules/browsers/browsers.go internal/modules/browsers/profiles.go cmd/mtm/main.go cmd/mtm/nonetwork_integration_test.go docs/superpowers/specs/2026-11-02-mtm-design.md
git commit -m "Add the browsers module

Migration Assistant copies browser profiles, but a Chromium browser can
only read its saved passwords and cookies with the Safe Storage key in
the login keychain. browsers records each profile and extension and
whether that key exists, presence only, and verify fails a missing
profile or key before the old Mac is erased. Google ties its sign-in to
the old Mac, so finish lists signing in again."
```

---

### Task 4: The `secrets` module

**Files:**
- Create: `internal/modules/secrets/gpg.go`
- Create: `internal/modules/secrets/secrets.go`
- Create: `internal/modules/secrets/ssh.go`
- Modify: `cmd/mtm/main.go`
- Modify: `cmd/mtm/nonetwork_integration_test.go`
- Modify: `docs/superpowers/specs/2026-11-02-mtm-design.md`
- Test: `internal/modules/secrets/secrets_test.go`, `cmd/mtm/main_test.go` (modify)

**Interfaces:**
- Consumes:
  - `catalog.KindPasswordManager`, `catalog.KindSecureEnclaveAgent`, and `catalog.App.SSHAgent` (Task 2);
  - `module.Stepper`, `module.LoadInventory`, `module.ErrNotPrepared`, `runner.Command`, `words.Count`, and `checklist.Item` (earlier stages).
- Produces:
  - `secrets.Name = "secrets"`, `secrets.New(c *catalog.Catalog) Module`, and `Module{Catalog *catalog.Catalog; Getenv func(string) string; GPG []string}`.
  - `Module` implements `module.Module` and `module.Stepper`.
  - `secrets.Inventory{SSH []SSHKey; GPG []GPGKey; GPGUnlisted bool; Agent string; SecureEnclave bool; Fingerprints *int}`.
  - `SSHKey{Name, Type, Fingerprint string; Public, Private bool}` and `GPGKey{Fingerprint, Algorithm string; Created int64; Secret string}`.
  - Unexported, used by Task 7:
    - `(Module) recordedApps(env) ([]catalog.App, error)`: the catalog entries for the apps in the `system` record;
    - `usesSecureEnclave(inv Inventory, apps []catalog.App) bool`.
  - Check ids `secrets:ssh` and `secrets:gpg`, both critical, and `secrets:touch-id`, which resolves the step of the same id.
  - Step ids:
    - `secrets:icloud-keychain`;
    - `secrets:password-manager:<catalog id>`;
    - `secrets:ssh-agent:<catalog id>`;
    - `secrets:secure-enclave`;
    - `secrets:touch-id`.

**Why it works this way:**
- **No key material is read.**
  - Public keys are read, and their fingerprint is computed as `ssh-keygen -l` does: the SHA-256 of the key blob, in unpadded base64. `TestPrepareRecordsIdentitiesAndTheAgent` pins that against two keys whose fingerprints came from `ssh-keygen -l`.
  - Private keys are only looked for in the folder listing; `TestPrepareReadsNoPrivateKey` makes them unreadable to prove it.
  - gpg lists identities only.
  - Fingerprints stay in `manifest.json`. Reports name SSH keys by file and GPG keys by algorithm and date.
- **gpg only runs with a GnuPG folder,** because it would create `~/.gnupg`. It is looked for where Homebrew and GPG Suite install it.
- **Where the SSH agent is set:** `SSH_AUTH_SOCK` and `IdentityAgent` in `~/.ssh/config`. This author's Mac sets 1Password's agent through `IdentityAgent`, with `SSH_AUTH_SOCK` pointing elsewhere. Links are followed, since 1Password's setup makes `~/.1password/agent.sock` a link to its real socket.
- **Secure Enclave keys** come from three signals:
  - `sk-` key types: a security key, or macOS 26's own Secure Enclave keys through `/usr/lib/ssh-keychain.dylib`, which OpenSSH also calls `sk-`;
  - a `SecurityKeyProvider` or `SSH_SK_PROVIDER` naming that library;
  - an agent, or a recorded app, of kind `secure-enclave-agent`.
- **Touch ID:** `/usr/bin/bioutil -c` only counts fingerprints. bioutil's other flags change things (`-p` purges every fingerprint), and mtm never passes them. If the count is unknown, the step is listed anyway.
- **Testability:** `Getenv` and `GPG` are fields, so tests never see the developer's environment or gpg. The no-network test empties `GNUPGHOME`, `SSH_AUTH_SOCK`, and `SSH_SK_PROVIDER` for the same reason.

- [ ] **Step 1: Write the failing tests**

The two public keys below were made with `ssh-keygen` for this test and then discarded. Their fingerprints are what `ssh-keygen -l` printed. The gpg output follows GnuPG's documented colon format, the same field layout this author's gpg 2.5 prints. The fingerprints in it are made up.

`internal/modules/secrets/secrets_test.go`:
```go
package secrets

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/momentmaker/mtm/internal/catalog"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/testutil"
)

// Two throwaway public keys, with the fingerprints ssh-keygen -l gives
// them.
const (
	pubOne = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAqmsyYcNuPdggoXH1gw1fgwDVLiRDxWTPlHgEhANQsZ dev@example.com\n"
	fprOne = "SHA256:imvGqWemDeQlOrKAuQ8St8gKyGJ6OZ7B/hDd20ijtXM"
	pubTwo = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILqheZQLjOXtrKZ7KmQWukW6KKoCfwD73dFzjP78Dkk6 dev@example.com\n"
	pubSK  = "sk-ecdsa-sha2-nistp256@openssh.com c2stZWNkc2E= ssh:\n"
)

// secretKeys is gpg --with-colons --list-secret-keys output: an RSA key
// whose secret part is here, with a subkey, and an ed25519 key whose
// secret part is missing (a stub). The fingerprints are made up.
const secretKeys = `sec:u:4096:1:AAAABBBBCCCCDDDD:1625242223:::u:::scESC:::+:::23::0:
fpr:::::::::0123456789ABCDEF0123456789ABCDEF01234567:
grp:::::::::1111111111111111111111111111111111111111:
uid:u::::1625242223::0000000000000000000000000000000000000000::Dev <dev@example.com>::::::::::0:
ssb:u:4096:1:EEEEFFFF00001111:1625242223::::::e:::+:::23:
fpr:::::::::89ABCDEF0123456789ABCDEF0123456789ABCDEF:
grp:::::::::2222222222222222222222222222222222222222:
sec:u:255:22:1111222233334444:1729295723:1855526101::u:::scESC:::#::ed25519:::0:
fpr:::::::::FEDCBA9876543210FEDCBA9876543210FEDCBA98:
uid:u::::1729295723::0000000000000000000000000000000000000000::Old <old@example.com>::::::::::0:
`

const onePassword = "~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(fstest.MapFS{
		"1password.toml": {Data: []byte("id = \"1password\"\nname = \"1Password\"\nbundle_id = \"com.1password.1password\"\nkind = \"password-manager\"\nssh_agent = [\"" + onePassword + "\"]\n")},
		"secretive.toml": {Data: []byte("id = \"secretive\"\nname = \"Secretive\"\nbundle_id = \"com.maxgoedjen.Secretive.Host\"\nkind = \"secure-enclave-agent\"\nssh_agent = [\"~/Library/Containers/com.maxgoedjen.Secretive.SecretAgent/Data/socket.ssh\"]\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type fixture struct {
	mod  Module
	fake *runner.Fake
	home string
	env  map[string]string
}

const bioutil = "/usr/bin/bioutil -c"

// newFixture is a home folder with three SSH keys, an SSH config that
// uses 1Password's agent, a GnuPG folder, and a gpg that lists two keys.
// The gpg is a placeholder file the fake answers for.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{home: filepath.Join(root, "home"), env: map[string]string{}}
	gpg := filepath.Join(root, "bin", "gpg")
	testutil.WriteFile(t, gpg, "")
	f.mod = Module{Catalog: testCatalog(t), Getenv: func(k string) string { return f.env[k] }, GPG: []string{filepath.Join(root, "none", "gpg"), gpg}}
	f.fake = &runner.Fake{Responses: map[string]runner.Response{
		gpg + " " + strings.Join(gpgListArgs, " "): {Stdout: secretKeys},
		bioutil: {Stdout: "User 501:\t2 biometric template(s)\nOperation performed successfully.\n"},
	}}
	ssh := filepath.Join(f.home, ".ssh")
	testutil.WriteFile(t, filepath.Join(ssh, "id_ed25519"), "private")
	testutil.WriteFile(t, filepath.Join(ssh, "id_ed25519.pub"), pubOne)
	testutil.WriteFile(t, filepath.Join(ssh, "id_rsa"), "private")
	testutil.WriteFile(t, filepath.Join(ssh, "github.pub"), pubTwo)
	testutil.WriteFile(t, filepath.Join(ssh, "known_hosts"), "github.com ssh-ed25519 AAAA\n")
	testutil.WriteFile(t, filepath.Join(ssh, "config"), "Host *\n  IdentityAgent \""+onePassword+"\"\n")
	if err := os.MkdirAll(filepath.Join(f.home, ".gnupg"), 0o700); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) moduleEnv(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func (f *fixture) prepare(t *testing.T) (Inventory, *manifest.Manifest) {
	t.Helper()
	res, err := f.mod.Prepare(f.moduleEnv(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), res.Inventory); err != nil {
		t.Fatal(err)
	}
	return res.Inventory.(Inventory), m
}

func two() *int { n := 2; return &n }

func TestPrepareRecordsIdentitiesAndTheAgent(t *testing.T) {
	f := newFixture(t)
	inv, _ := f.prepare(t)
	want := Inventory{
		SSH: []SSHKey{
			{Name: "github", Type: "ssh-ed25519", Fingerprint: "SHA256:4I4uXsj2BJLVM2uwM1keNH2NJLceWl2Mw6BBEePE5iM", Public: true},
			{Name: "id_ed25519", Type: "ssh-ed25519", Fingerprint: fprOne, Public: true, Private: true},
			{Name: "id_rsa", Private: true},
		},
		GPG: []GPGKey{
			{Fingerprint: "0123456789ABCDEF0123456789ABCDEF01234567", Algorithm: "RSA 4096", Created: 1625242223, Secret: "+"},
			{Fingerprint: "FEDCBA9876543210FEDCBA9876543210FEDCBA98", Algorithm: "EdDSA ed25519", Created: 1729295723, Secret: "#"},
		},
		Agent:        "1password",
		Fingerprints: two(),
	}
	if !reflect.DeepEqual(inv, want) {
		t.Errorf("inventory = %+v\nwant        %+v", inv, want)
	}
}

func TestPrepareReadsNoPrivateKey(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		path := filepath.Join(f.home, ".ssh", name)
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(path, 0o600) })
	}
	if _, err := f.mod.Prepare(f.moduleEnv(nil), nil); err != nil {
		t.Errorf("prepare read a private key it can't open: %v", err)
	}
}

func TestPrepareDoesNotRunGPGWithoutAGnuPGFolder(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(filepath.Join(f.home, ".gnupg")); err != nil {
		t.Fatal(err)
	}
	inv, _ := f.prepare(t)
	if len(inv.GPG) != 0 || inv.GPGUnlisted {
		t.Errorf("gpg = %+v, unlisted %v; want none", inv.GPG, inv.GPGUnlisted)
	}
	for _, c := range f.fake.Calls() {
		if strings.HasSuffix(c.Name, "gpg") {
			t.Errorf("ran %s, which would create ~/.gnupg", c)
		}
	}
}

func TestPrepareWarnsWhenItCannotListGPGKeys(t *testing.T) {
	f := newFixture(t)
	f.mod.GPG = f.mod.GPG[:1]
	res, err := f.mod.Prepare(f.moduleEnv(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Inventory.(Inventory).GPGUnlisted || !slices.ContainsFunc(res.Problems, func(p module.Finding) bool { return p.ID == "gpg" }) {
		t.Errorf("result = %+v, want gpg unlisted and a warning", res)
	}
}

func TestSecureEnclaveIsFoundThreeWays(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, f *fixture)
	}{
		{"an sk- key", func(t *testing.T, f *fixture) {
			testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "id_ecdsa_sk_rk.pub"), pubSK)
		}},
		{"Secretive's agent", func(t *testing.T, f *fixture) {
			f.env["SSH_AUTH_SOCK"] = filepath.Join(f.home, "Library/Containers/com.maxgoedjen.Secretive.SecretAgent/Data/socket.ssh")
		}},
		{"macOS's provider", func(t *testing.T, f *fixture) {
			testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "config"), "Host *\n  SecurityKeyProvider=/usr/lib/ssh-keychain.dylib\n")
		}},
	}
	for _, tt := range tests {
		f := newFixture(t)
		tt.setup(t, f)
		res, err := f.mod.Prepare(f.moduleEnv(nil), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Inventory.(Inventory).SecureEnclave || !slices.ContainsFunc(res.Problems, func(p module.Finding) bool { return p.ID == "secure-enclave" }) {
			t.Errorf("%s: result = %+v, want the Secure Enclave found and a warning", tt.name, res)
		}
	}
}

func TestAgentIsFoundThroughALink(t *testing.T) {
	f := newFixture(t)
	testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "config"), "")
	real := expandHome(onePassword, f.home)
	testutil.WriteFile(t, real, "")
	link := filepath.Join(f.home, ".1password", "agent.sock")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	f.env["SSH_AUTH_SOCK"] = link
	if inv, _ := f.prepare(t); inv.Agent != "1password" {
		t.Errorf("agent = %q, want 1password", inv.Agent)
	}
}

func TestTouchIDIsUnknownWhenBioutilFails(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses[bioutil] = runner.Response{Code: 1, Stderr: "no biometric hardware"}
	if inv, _ := f.prepare(t); inv.Fingerprints != nil {
		t.Errorf("fingerprints = %d, want unknown", *inv.Fingerprints)
	}
}

func stepIDs(items []checklist.Item) []string {
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestStepsForPasswordManagersAgentsAndTouchID(t *testing.T) {
	f := newFixture(t)
	testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "id_ecdsa_sk_rk.pub"), pubSK)
	_, m := f.prepare(t)
	apps := []map[string]string{{"bundle_id": "com.1password.1password"}, {"bundle_id": "com.example.other"}}
	if err := m.SetModule("system", time.Now(), map[string]any{"apps": apps}); err != nil {
		t.Fatal(err)
	}
	items, err := f.mod.Steps(f.moduleEnv(m))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"secrets:icloud-keychain", "secrets:password-manager:1password", "secrets:ssh-agent:1password", "secrets:secure-enclave", "secrets:touch-id"}
	if got := stepIDs(items); !slices.Equal(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
	if se := items[3]; !strings.Contains(se.Detail, "~/.ssh/id_ecdsa_sk_rk") {
		t.Errorf("Secure Enclave step detail = %q, want the sk- key named", se.Detail)
	}
}

func TestStepsSkipTouchIDWithoutFingerprintsAndAskForKeysWithSecretive(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses[bioutil] = runner.Response{Stdout: "User 501:\t0 biometric template(s)\n"}
	testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "config"), "")
	_, m := f.prepare(t)
	apps := []map[string]string{{"bundle_id": "com.maxgoedjen.Secretive.Host"}}
	if err := m.SetModule("system", time.Now(), map[string]any{"apps": apps}); err != nil {
		t.Fatal(err)
	}
	items, err := f.mod.Steps(f.moduleEnv(m))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stepIDs(items), []string{"secrets:icloud-keychain", "secrets:secure-enclave"}; !slices.Equal(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
}

func check(t *testing.T, checks []module.Check, id string) module.Check {
	t.Helper()
	i := slices.IndexFunc(checks, func(c module.Check) bool { return c.ID == id })
	if i < 0 {
		t.Fatalf("no %s check in %+v", id, checks)
	}
	return checks[i]
}

// migrated is the fixture's home copied to a new Mac, with its record.
func migrated(t *testing.T) (*fixture, *manifest.Manifest) {
	t.Helper()
	f := newFixture(t)
	_, m := f.prepare(t)
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, f.home, newHome)
	f.home = newHome
	return f, m
}

func TestVerifyPassesWhenIdentitiesArrived(t *testing.T) {
	f, m := migrated(t)
	checks, err := f.mod.Verify(f.moduleEnv(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.Status != module.StatusPass {
			t.Errorf("%s = %s (%s), want pass", c.ID, c.Status, c.Detail)
		}
	}
	if ssh := check(t, checks, "secrets:ssh"); !ssh.Critical {
		t.Error("the SSH check isn't critical")
	}
	if tid := check(t, checks, "secrets:touch-id"); !slices.Equal(tid.Resolves, []string{"secrets:touch-id"}) {
		t.Errorf("Touch ID check = %+v, want it to close the step", tid)
	}
}

func TestVerifyFailsMissingOrReplacedSSHKeys(t *testing.T) {
	f, m := migrated(t)
	ssh := filepath.Join(f.home, ".ssh")
	if err := os.Remove(filepath.Join(ssh, "id_rsa")); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(ssh, "id_ed25519.pub"), pubTwo)
	checks, err := f.mod.Verify(f.moduleEnv(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := check(t, checks, "secrets:ssh")
	want := []string{"~/.ssh/id_ed25519.pub: a different key", "~/.ssh/id_rsa: private key missing"}
	if c.Status != module.StatusFail || !slices.Equal(c.Paths, want) {
		t.Errorf("SSH check = %+v, want a fail listing %q", c, want)
	}
	for _, p := range c.Paths {
		if strings.Contains(p, "SHA256:") {
			t.Errorf("a fingerprint reached the report: %q", p)
		}
	}
}

func TestVerifyFailsAMissingGPGSecretKey(t *testing.T) {
	f, m := migrated(t)
	gpg := f.mod.GPG[1] + " " + strings.Join(gpgListArgs, " ")
	f.fake.Responses[gpg] = runner.Response{Stdout: strings.Replace(secretKeys, ":+:::23::0:", ":#:::23::0:", 1)}
	checks, err := f.mod.Verify(f.moduleEnv(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := check(t, checks, "secrets:gpg")
	if c.Status != module.StatusFail || !c.Critical || !slices.Equal(c.Paths, []string{"the RSA 4096 key from 2021-07-02"}) {
		t.Errorf("GPG check = %+v, want a critical fail naming the RSA key", c)
	}
}

func TestVerifyFailsWhenGPGIsGone(t *testing.T) {
	f, m := migrated(t)
	f.mod.GPG = f.mod.GPG[:1]
	checks, err := f.mod.Verify(f.moduleEnv(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c := check(t, checks, "secrets:gpg"); c.Status != module.StatusFail || !strings.Contains(c.Detail, "gpg isn't installed") {
		t.Errorf("GPG check = %+v, want a fail saying gpg is missing", c)
	}
}

func TestParseSecretKeysRejectsAKeyWithoutAFingerprint(t *testing.T) {
	if _, err := parseSecretKeys("sec:u:4096:1:AAAA:1625242223:::u:::scESC:::+:::23::0:\n"); err == nil {
		t.Error("parsed a key with no fingerprint")
	}
}

func TestConfigLineForms(t *testing.T) {
	tests := []struct{ line, key, value string }{
		{"  IdentityAgent ~/a.sock", "IdentityAgent", "~/a.sock"},
		{"IdentityAgent=\"~/b c.sock\"", "IdentityAgent", "~/b c.sock"},
		{"identityagent = none", "identityagent", "none"},
		{"# IdentityAgent ~/x", "", ""},
	}
	for _, tt := range tests {
		if k, v := configLine(tt.line); k != tt.key || v != tt.value {
			t.Errorf("configLine(%q) = %q, %q; want %q, %q", tt.line, k, v, tt.key, tt.value)
		}
	}
}
```

`cmd/mtm/main_test.go`:
```diff
--- a/cmd/mtm/main_test.go
+++ b/cmd/mtm/main_test.go
@@ -279,7 +279,7 @@ func TestDefaultAppModules(t *testing.T) {
 	for _, m := range defaultApp(t.TempDir()).Modules {
 		names = append(names, m.Name())
 	}
-	if want := []string{"files", "git", "system", "databases", "docker", "homebrew", "toolchains", "loginitems", "permissions", "browsers"}; !slices.Equal(names, want) {
+	if want := []string{"files", "git", "system", "databases", "docker", "homebrew", "toolchains", "loginitems", "permissions", "browsers", "secrets"}; !slices.Equal(names, want) {
 		t.Errorf("modules = %q, want %q", names, want)
 	}
 }
@@ -291,7 +291,7 @@ func TestDefaultAppModulesThatLeaveSteps(t *testing.T) {
 			names = append(names, m.Name())
 		}
 	}
-	if want := []string{"system", "loginitems", "permissions", "browsers"}; !slices.Equal(names, want) {
+	if want := []string{"system", "loginitems", "permissions", "browsers", "secrets"}; !slices.Equal(names, want) {
 		t.Errorf("modules with steps = %q, want %q", names, want)
 	}
 }
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/secrets/ ./cmd/mtm/`
Expected: FAIL. The package has no code yet, and `defaultApp` doesn't list `secrets`:
```text
FAIL	github.com/momentmaker/mtm/internal/modules/secrets [build failed]
--- FAIL: TestDefaultAppModules (0.01s)
    main_test.go:283: modules = ["files" "git" "system" "databases" "docker" "homebrew" "toolchains" "loginitems" "permissions" "browsers"], want ["files" "git"
--- FAIL: TestDefaultAppModulesThatLeaveSteps (0.00s)
    main_test.go:295: modules with steps = ["system" "loginitems" "permissions" "browsers"], want ["system" "loginitems" "permissions" "browsers" "secrets"]
FAIL
FAIL	github.com/momentmaker/mtm/cmd/mtm	0.966s
FAIL
```

- [ ] **Step 3: Implement the module**

`internal/modules/secrets/secrets.go`:
```go
// Package secrets records the old Mac's SSH and GPG identities, never their
// key material, so verify can prove they arrived. It also finds what no
// copy can move: keys in the Secure Enclave, password managers and their
// SSH agents to turn on again, and Touch ID fingerprints.
package secrets

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/momentmaker/mtm/internal/catalog"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const Name = "secrets"

// gpgPaths are where Homebrew and GPG Suite install gpg.
var gpgPaths = []string{"/opt/homebrew/bin/gpg", "/usr/local/bin/gpg", "/usr/local/MacGPG2/bin/gpg"}

// Module reads the environment through Getenv and looks for gpg at the
// GPG paths, so tests can stand in for both.
type Module struct {
	Catalog *catalog.Catalog
	Getenv  func(string) string
	GPG     []string
}

func New(c *catalog.Catalog) Module { return Module{Catalog: c, Getenv: os.Getenv, GPG: gpgPaths} }

func (Module) Name() string { return Name }

// Inventory is the module's record.
//   - GPGUnlisted: a GnuPG folder existed but there was no gpg to list it.
//   - Agent: the catalog id of the SSH agent SSH used, when mtm knows it.
//   - SecureEnclave: SSH used keys kept in the Secure Enclave.
//   - Fingerprints: how many Touch ID fingerprints were enrolled, or nil
//     when bioutil couldn't say.
type Inventory struct {
	SSH           []SSHKey `json:"ssh,omitempty"`
	GPG           []GPGKey `json:"gpg,omitempty"`
	GPGUnlisted   bool     `json:"gpg_unlisted,omitempty"`
	Agent         string   `json:"agent,omitempty"`
	SecureEnclave bool     `json:"secure_enclave,omitempty"`
	Fingerprints  *int     `json:"fingerprints,omitempty"`
}

func (m Module) look(env module.Env) (Inventory, error) {
	var inv Inventory
	var err error
	if inv.SSH, err = sshKeys(env.Home); err != nil {
		return Inventory{}, err
	}
	inv.GPG, err = m.gpgKeys(env)
	switch {
	case errors.Is(err, errNoGPG):
		inv.GPGUnlisted = true
	case err != nil:
		return Inventory{}, err
	}
	if inv.Agent, inv.SecureEnclave, err = m.agent(env.Home); err != nil {
		return Inventory{}, err
	}
	inv.SecureEnclave = inv.SecureEnclave || slices.ContainsFunc(inv.SSH, SSHKey.hardware)
	inv.Fingerprints = fingerprints(env)
	return inv, nil
}

// agent finds the SSH agent SSH uses, from SSH_AUTH_SOCK and the
// IdentityAgent lines in ~/.ssh/config, and whether it, or a
// SecurityKeyProvider, keeps keys in the Secure Enclave.
func (m Module) agent(home string) (string, bool, error) {
	cfg, err := readSSHConfig(home)
	if err != nil {
		return "", false, err
	}
	secureEnclave := cfg.secureEnclave || strings.Contains(m.Getenv("SSH_SK_PROVIDER"), secureEnclaveProvider)
	sockets := cfg.agents
	if s := m.Getenv("SSH_AUTH_SOCK"); s != "" {
		sockets = append([]string{s}, sockets...)
	}
	for _, s := range sockets {
		if a, ok := agentApp(m.Catalog, s, home); ok {
			return a.ID, secureEnclave || a.Kind == catalog.KindSecureEnclaveAgent, nil
		}
	}
	return "", secureEnclave, nil
}

var templates = regexp.MustCompile(`(\d+) biometric template`)

// fingerprints counts the enrolled Touch ID fingerprints with bioutil's
// read-only --count. Any failure, as on a Mac without Touch ID, means
// unknown.
func fingerprints(env module.Env) *int {
	out, err := env.Run.Run(env.Ctx, runner.Command{Name: "/usr/bin/bioutil", Args: []string{"-c"}})
	if err != nil {
		return nil
	}
	m := templates.FindStringSubmatch(string(out.Stdout))
	if m == nil {
		return nil
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return nil
	}
	return &n
}

func (m Module) findings(inv Inventory, severity module.Severity) []module.Finding {
	var out []module.Finding
	if inv.SecureEnclave {
		out = append(out, module.Finding{
			Module: Name, ID: "secure-enclave", Severity: severity, Handler: module.Manual,
			Title:  "SSH keys in this Mac's Secure Enclave can't move to the new Mac",
			Detail: "A key made in the Secure Enclave stays in it. A key on a hardware security key keeps working once it's plugged in.",
			Fix:    "On the new Mac, make new keys and add them to GitHub and your servers; mtm checklist reminds you. Keep this Mac until they work.",
		})
	}
	if inv.GPGUnlisted {
		out = append(out, module.Finding{
			Module: Name, ID: "gpg", Severity: severity, Handler: module.Manual,
			Title:  "mtm can't list your GPG keys: gpg isn't installed where it looks",
			Detail: "Looked in " + strings.Join(m.GPG, ", ") + ".",
			Fix:    "verify won't check them. On the new Mac, run gpg -K and check your keys are there.",
		})
	}
	if a, ok := m.app(inv.Agent); ok && a.Kind == catalog.KindPasswordManager {
		out = append(out, module.Finding{
			Module: Name, ID: "ssh-agent", Severity: module.SeverityInfo, Handler: module.Manual,
			Title:  "SSH uses " + a.Name + "'s SSH agent",
			Detail: "On the new Mac, sign in to " + a.Name + " and turn its SSH agent on; mtm checklist lists it.",
		})
	}
	return out
}

func (m Module) app(id string) (catalog.App, bool) {
	if id == "" {
		return catalog.App{}, false
	}
	i := slices.IndexFunc(m.Catalog.Apps, func(a catalog.App) bool { return a.ID == id })
	if i < 0 {
		return catalog.App{}, false
	}
	return m.Catalog.Apps[i], true
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	inv, err := m.look(env)
	if err != nil {
		return nil, err
	}
	var out []module.Finding
	if len(inv.SSH) > 0 || len(inv.GPG) > 0 {
		out = append(out, module.Finding{
			Module: Name, ID: "identities", Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
			Title:  words.Count(len(inv.SSH), "SSH key", "SSH keys") + " and " + words.Count(len(inv.GPG), "GPG key", "GPG keys"),
			Detail: "Migration Assistant copies them. verify checks each one arrived.",
		})
	}
	return append(out, m.findings(inv, module.SeverityWarn)...), nil
}

func (m Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	inv, err := m.look(env)
	if err != nil {
		return module.Result{}, err
	}
	return module.Result{
		Inventory: inv,
		Done:      []string{"recorded " + words.Count(len(inv.SSH), "SSH key", "SSH keys") + " and " + words.Count(len(inv.GPG), "GPG key", "GPG keys")},
		Problems:  m.findings(inv, module.SeverityWarn),
	}, nil
}

// Finish has nothing to restore: Migration Assistant copies ~/.ssh,
// ~/.gnupg, and the login keychain.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}

// recordedApps are the catalog entries of the apps in the system module's
// record, where the password managers and Secure Enclave agents are found.
func (m Module) recordedApps(env module.Env) ([]catalog.App, error) {
	var sys struct {
		Apps []struct {
			BundleID string `json:"bundle_id"`
		} `json:"apps"`
	}
	if _, err := module.LoadInventory(env, "system", &sys); err != nil {
		if errors.Is(err, module.ErrNotPrepared) {
			return nil, nil
		}
		return nil, err
	}
	var out []catalog.App
	for _, a := range sys.Apps {
		if entry, ok := m.Catalog.ByBundleID(a.BundleID); ok {
			out = append(out, entry)
		}
	}
	return out, nil
}

// usesSecureEnclave is true when SSH used Secure Enclave keys, or the old
// Mac had an app that keeps them there.
func usesSecureEnclave(inv Inventory, apps []catalog.App) bool {
	return inv.SecureEnclave || slices.ContainsFunc(apps, func(a catalog.App) bool { return a.Kind == catalog.KindSecureEnclaveAgent })
}

const touchIDStep = Name + ":touch-id"

func (m Module) Steps(env module.Env) ([]checklist.Item, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	apps, err := m.recordedApps(env)
	if err != nil {
		return nil, err
	}
	items := []checklist.Item{{
		ID: Name + ":icloud-keychain", Module: Name, Where: "System Settings > your name > iCloud > Passwords",
		Title:  "Turn on iCloud Keychain, and check your passwords and passkeys are there",
		Detail: "Passkeys, and items saved only in this Mac's keychain, come back only through iCloud Keychain.",
	}}
	for _, a := range apps {
		if a.Kind == catalog.KindPasswordManager {
			items = append(items, checklist.Item{ID: Name + ":password-manager:" + a.ID, Module: Name, Where: a.Name, Title: "Sign in to " + a.Name})
		}
	}
	if a, ok := m.app(inv.Agent); ok && a.Kind == catalog.KindPasswordManager {
		items = append(items, checklist.Item{ID: Name + ":ssh-agent:" + a.ID, Module: Name, Where: a.Name,
			Title:  "Turn on " + a.Name + "'s SSH agent",
			Detail: "SSH used it on the old Mac, through SSH_AUTH_SOCK or IdentityAgent in ~/.ssh/config."})
	}
	if usesSecureEnclave(inv, apps) {
		items = append(items, checklist.Item{ID: Name + ":secure-enclave", Module: Name, Where: "Terminal",
			Title:  "Make new Secure Enclave SSH keys and add them to GitHub and your servers",
			Detail: "Keys in the old Mac's Secure Enclave can't leave it. Keys on a hardware security key keep working once it's plugged in." + hardwareKeys(inv.SSH)})
	}
	if inv.Fingerprints == nil || *inv.Fingerprints > 0 {
		items = append(items, checklist.Item{ID: touchIDStep, Module: Name, Where: "System Settings > Touch ID & Password", Title: "Add your fingerprints to Touch ID"})
	}
	return items, nil
}

func hardwareKeys(keys []SSHKey) string {
	var names []string
	for _, k := range keys {
		if k.hardware() {
			names = append(names, "~/.ssh/"+k.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " Keys of this kind: " + strings.Join(names, ", ") + "."
}

func (m Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	var checks []module.Check
	if len(inv.SSH) > 0 {
		c, err := verifySSH(env.Home, inv.SSH)
		if err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	if len(inv.GPG) > 0 || inv.GPGUnlisted {
		checks = append(checks, m.verifyGPG(env, inv))
	}
	if inv.Fingerprints == nil || *inv.Fingerprints > 0 {
		checks = append(checks, touchIDCheck(env))
	}
	return checks, nil
}

// verifySSH fails when a recorded key is missing or is a different key
// now: the old Mac holds the only other copy.
func verifySSH(home string, recorded []SSHKey) (module.Check, error) {
	now, err := sshKeys(home)
	if err != nil {
		return module.Check{}, err
	}
	var problems []string
	for _, k := range recorded {
		i := slices.IndexFunc(now, func(n SSHKey) bool { return n.Name == k.Name })
		var got SSHKey
		if i >= 0 {
			got = now[i]
		}
		switch {
		case k.Public && !got.Public:
			problems = append(problems, "~/.ssh/"+k.Name+".pub: missing")
		case k.Fingerprint != "" && got.Fingerprint != k.Fingerprint:
			problems = append(problems, "~/.ssh/"+k.Name+".pub: a different key")
		}
		if k.Private && !got.Private {
			problems = append(problems, "~/.ssh/"+k.Name+": private key missing")
		}
	}
	c := module.Check{Module: Name, ID: Name + ":ssh", Status: module.StatusPass, Critical: true, Title: "SSH keys",
		Detail: words.Count(len(recorded), "key", "keys") + " arrived"}
	if len(problems) > 0 {
		c.Status = module.StatusFail
		c.Detail = "copy them from the old Mac's ~/.ssh"
		c.Paths = problems
	}
	return c, nil
}

func (m Module) verifyGPG(env module.Env, inv Inventory) module.Check {
	c := module.Check{Module: Name, ID: Name + ":gpg", Status: module.StatusPass, Critical: true, Title: "GPG keys"}
	if inv.GPGUnlisted {
		c.Status = module.StatusWarn
		c.Detail = "mtm couldn't list them on the old Mac; run gpg -K here and check they're all there"
		return c
	}
	now, err := m.gpgKeys(env)
	if err != nil {
		c.Status = module.StatusFail
		c.Detail = err.Error()
		return c
	}
	var missing []string
	for _, k := range inv.GPG {
		i := slices.IndexFunc(now, func(n GPGKey) bool { return n.Fingerprint == k.Fingerprint })
		if i < 0 || (now[i].stub() && !k.stub()) {
			missing = append(missing, k.describe())
		}
	}
	c.Detail = words.Count(len(inv.GPG), "secret key", "secret keys") + " arrived"
	if len(missing) > 0 {
		c.Status = module.StatusFail
		c.Detail = "secret keys are missing; copy ~/.gnupg from the old Mac, or export them there with gpg --export-secret-keys"
		c.Paths = missing
	}
	return c
}

// touchIDCheck closes the Touch ID step once fingerprints are enrolled
// on this Mac.
func touchIDCheck(env module.Env) module.Check {
	c := module.Check{Module: Name, ID: touchIDStep, Status: module.StatusInfo, Title: "Touch ID",
		Detail: "no fingerprints yet; mtm checklist has the step"}
	if n := fingerprints(env); n != nil && *n > 0 {
		c.Status, c.Detail, c.Resolves = module.StatusPass, words.Count(*n, "fingerprint", "fingerprints")+" enrolled", []string{touchIDStep}
	}
	return c
}
```

`internal/modules/secrets/ssh.go`:
```go
package secrets

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/momentmaker/mtm/internal/catalog"
)

// SSHKey is a key in ~/.ssh, named by its private key's file. A key with a
// public half records the public key's type and fingerprint, which tell it
// from a new key with the same name. Only public keys are read; a private
// key is only looked for.
type SSHKey struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Public      bool   `json:"public,omitempty"`
	Private     bool   `json:"private,omitempty"`
}

// hardware is true for a key whose private half lives in a security key
// or, with macOS's ssh-keychain.dylib, in the Secure Enclave. OpenSSH
// calls both sk- keys.
func (k SSHKey) hardware() bool { return strings.HasPrefix(k.Type, "sk-") }

func sshDir(home string) string { return filepath.Join(home, ".ssh") }

// sshKeys lists the key pairs in ~/.ssh: every *.pub file, with its
// private half if one sits beside it, and id_* files with no public half.
// Other files there, such as config and known_hosts, aren't keys.
func sshKeys(home string) ([]SSHKey, error) {
	dir := sshDir(home)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		if e.Type().IsRegular() {
			names[e.Name()] = true
		}
	}
	var keys []SSHKey
	for _, e := range entries {
		name := e.Name()
		if !names[name] {
			continue
		}
		switch {
		case strings.HasSuffix(name, ".pub"):
			k, err := readPublicKey(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			k.Name = strings.TrimSuffix(name, ".pub")
			k.Private = names[k.Name]
			keys = append(keys, k)
		case strings.HasPrefix(name, "id_") && !names[name+".pub"]:
			keys = append(keys, SSHKey{Name: name, Private: true})
		}
	}
	return keys, nil
}

// readPublicKey reads a public key's type and computes its fingerprint as
// ssh-keygen -l does: the SHA-256 of the key blob. A file mtm can't parse
// is still recorded, by name only.
func readPublicKey(path string) (SSHKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SSHKey{}, fmt.Errorf("read %s: %w", path, err)
	}
	k := SSHKey{Public: true}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return k, nil
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return k, nil
	}
	sum := sha256.Sum256(blob)
	k.Type = fields[0]
	k.Fingerprint = "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	return k, nil
}

// sshConfig is what ~/.ssh/config says about agents: the IdentityAgent
// sockets it names, and whether a SecurityKeyProvider is macOS's Secure
// Enclave library. Include files aren't followed.
type sshConfig struct {
	agents        []string
	secureEnclave bool
}

const secureEnclaveProvider = "ssh-keychain.dylib"

func readSSHConfig(home string) (sshConfig, error) {
	path := filepath.Join(sshDir(home), "config")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return sshConfig{}, nil
	}
	if err != nil {
		return sshConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var c sshConfig
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		key, value := configLine(sc.Text())
		switch strings.ToLower(key) {
		case "identityagent":
			if v := expandHome(value, home); v != "" && v != "none" && v != "SSH_AUTH_SOCK" {
				c.agents = append(c.agents, v)
			}
		case "securitykeyprovider":
			c.secureEnclave = c.secureEnclave || strings.Contains(value, secureEnclaveProvider)
		}
	}
	if err := sc.Err(); err != nil {
		return sshConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	return c, nil
}

// configLine splits an ssh_config line into its keyword and argument.
// The two are separated by spaces or one "=", and the argument may be
// quoted.
func configLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", ""
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return line, ""
	}
	value := strings.TrimLeft(line[i:], " \t")
	value = strings.TrimSpace(strings.TrimPrefix(value, "="))
	return line[:i], strings.Trim(value, `"`)
}

// expandHome resolves the forms ssh_config uses for the home folder.
func expandHome(p, home string) string {
	switch {
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	case strings.HasPrefix(p, "%d/"):
		return filepath.Join(home, p[3:])
	}
	return p
}

// agentApp finds the catalog app whose SSH agent listens at socket. A
// socket is often a link to the app's own, such as ~/.1password/agent.sock,
// so links are followed when the socket exists.
func agentApp(c *catalog.Catalog, socket, home string) (catalog.App, bool) {
	want := []string{filepath.Clean(socket)}
	if real, err := filepath.EvalSymlinks(socket); err == nil {
		want = append(want, real)
	}
	for _, a := range c.Apps {
		for _, s := range a.SSHAgent {
			path := expandHome(s, home)
			candidates := []string{path}
			if real, err := filepath.EvalSymlinks(path); err == nil {
				candidates = append(candidates, real)
			}
			for _, w := range want {
				for _, p := range candidates {
					if w == p {
						return a, true
					}
				}
			}
		}
	}
	return catalog.App{}, false
}
```

`internal/modules/secrets/gpg.go`:
```go
package secrets

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
)

// GPGKey is a secret key's identity, never its material. Secret is how
// gpg reports the secret part: "+" here, "#" missing (a stub), or the
// serial number of the card that holds it.
type GPGKey struct {
	Fingerprint string `json:"fingerprint"`
	Algorithm   string `json:"algorithm"`
	Created     int64  `json:"created"`
	Secret      string `json:"secret"`
}

func (k GPGKey) stub() bool { return k.Secret == "#" }

// describe names a key without its fingerprint, which reports never show.
func (k GPGKey) describe() string {
	return fmt.Sprintf("the %s key from %s", k.Algorithm, time.Unix(k.Created, 0).UTC().Format("2006-01-02"))
}

// gpgListArgs list secret keys without changing the keyring: no trust
// database check, and nothing that could prompt.
var gpgListArgs = []string{"--batch", "--no-tty", "--no-auto-check-trustdb", "--with-colons", "--list-secret-keys"}

// gnupgHome is the folder gpg keeps keys in.
func (m Module) gnupgHome(home string) string {
	if dir := m.Getenv("GNUPGHOME"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".gnupg")
}

func (m Module) gpgPath() string {
	for _, p := range m.GPG {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// errNoGPG means a GnuPG folder exists but no gpg to list its keys.
var errNoGPG = errors.New("gpg isn't installed")

// gpgKeys lists the secret keys. A Mac without a GnuPG folder has none,
// and gpg isn't run, because it would create one.
func (m Module) gpgKeys(env module.Env) ([]GPGKey, error) {
	if _, err := os.Stat(m.gnupgHome(env.Home)); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	gpg := m.gpgPath()
	if gpg == "" {
		return nil, fmt.Errorf("%w (looked in %s)", errNoGPG, strings.Join(m.GPG, ", "))
	}
	out, err := env.Run.Run(env.Ctx, runner.Command{Name: gpg, Args: gpgListArgs})
	if err != nil {
		return nil, fmt.Errorf("list GPG keys: %w", err)
	}
	return parseSecretKeys(string(out.Stdout))
}

var gpgAlgorithms = map[string]string{"1": "RSA", "17": "DSA", "18": "ECDH", "19": "ECDSA", "22": "EdDSA"}

// parseSecretKeys reads gpg's colon format: a "sec" line per primary key,
// followed by its "fpr" line. Subkeys ("ssb") travel with their primary.
func parseSecretKeys(out string) ([]GPGKey, error) {
	var keys []GPGKey
	wantFpr := false
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, ":")
		switch {
		case f[0] == "sec" && len(f) >= 15:
			created, err := strconv.ParseInt(f[5], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse gpg output: creation time %q", f[5])
			}
			keys = append(keys, GPGKey{Algorithm: algorithm(f), Created: created, Secret: f[14]})
			wantFpr = true
		case f[0] == "fpr" && wantFpr && len(f) >= 10:
			keys[len(keys)-1].Fingerprint = f[9]
			wantFpr = false
		case f[0] == "ssb" || f[0] == "uid":
			wantFpr = false
		}
	}
	for _, k := range keys {
		if k.Fingerprint == "" {
			return nil, errors.New("parse gpg output: a secret key has no fingerprint")
		}
	}
	return keys, nil
}

// algorithm names a key's algorithm and size, or curve for elliptic keys.
func algorithm(f []string) string {
	name, ok := gpgAlgorithms[f[3]]
	if !ok {
		name = "algorithm " + f[3]
	}
	if len(f) > 16 && f[16] != "" {
		return name + " " + f[16]
	}
	return name + " " + f[2]
}
```

- [ ] **Step 4: Wire it in**

`cmd/mtm/main.go`:
```diff
--- a/cmd/mtm/main.go
+++ b/cmd/mtm/main.go
@@ -24,6 +24,7 @@ import (
 	"github.com/momentmaker/mtm/internal/modules/homebrew"
 	"github.com/momentmaker/mtm/internal/modules/loginitems"
 	"github.com/momentmaker/mtm/internal/modules/permissions"
+	"github.com/momentmaker/mtm/internal/modules/secrets"
 	"github.com/momentmaker/mtm/internal/modules/system"
 	"github.com/momentmaker/mtm/internal/modules/toolchains"
 	"github.com/momentmaker/mtm/internal/report"
@@ -88,7 +89,7 @@ func defaultApp(home string) *app.App {
 		Modules: []module.Module{
 			files.New(cat.Volatile()...), git.New(), system.New(name, cat), databases.New(), docker.New(),
 			homebrew.New(name), toolchains.New(name), loginitems.New(os.Getuid()), permissions.New(cat),
-			browsers.New(),
+			browsers.New(), secrets.New(cat),
 		},
 	}
 }
```

`cmd/mtm/nonetwork_integration_test.go`:
```diff
--- a/cmd/mtm/nonetwork_integration_test.go
+++ b/cmd/mtm/nonetwork_integration_test.go
@@ -142,9 +142,9 @@ func TestNoNetwork(t *testing.T) {
 		catalogFallback bool
 	}{
 		{[]string{"scan"}, []int{0, 1}, false, true},
-		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions,browsers"}, []int{0, 1}, true, true},
+		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions,browsers,secrets"}, []int{0, 1}, true, true},
 		{[]string{"prepare", "--undo"}, []int{0, 1}, false, false},
-		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions,browsers"}, []int{0, 1}, true, true},
+		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions,browsers,secrets"}, []int{0, 1}, true, true},
 		{[]string{"verify"}, []int{1}, false, false},
 	}
 	for _, s := range steps {
@@ -152,7 +152,9 @@ func TestNoNetwork(t *testing.T) {
 		cmd := exec.Command("/usr/bin/sandbox-exec", append([]string{"-p", sandboxProfile, bin}, s.args...)...)
 		// The temp home would hide Homebrew's package data, which every real
 		// install keeps in its cache; mtm itself checks that it is there.
-		cmd.Env = append(os.Environ(), "HOME="+home, "HOMEBREW_CACHE="+cache)
+		// The emptied variables would point secrets at the developer's own
+		// SSH agent and GnuPG folder.
+		cmd.Env = append(os.Environ(), "HOME="+home, "HOMEBREW_CACHE="+cache, "GNUPGHOME=", "SSH_AUTH_SOCK=", "SSH_SK_PROVIDER=")
 		out, err := cmd.CombinedOutput()
 		code := 0
 		var exitErr *exec.ExitError
```

- [ ] **Step 5: Update the spec**

`docs/superpowers/specs/2026-11-02-mtm-design.md`:
```diff
--- a/docs/superpowers/specs/2026-11-02-mtm-design.md
+++ b/docs/superpowers/specs/2026-11-02-mtm-design.md
@@ -430,15 +430,31 @@ Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0`
   - Safari: an info check.
 
 ### 9.10 `secrets`
-- Records `~/.ssh` key names and types (`ssh-keygen -l` on public keys), and `gpg --list-secret-keys` identities (never key material).
-- Detects Secure Enclave or hardware-backed SSH keys: Secretive installed, `SSH_AUTH_SOCK` pointing to a Secure Enclave agent, `sk-` key types.
-- Detects password managers from the catalog (1Password, Bitwarden).
-- **`finish` checklist:**
-  - turn on iCloud Keychain and check passkeys are present;
-  - sign in to the password manager and turn on its SSH agent if it was used;
-  - create new Secure Enclave keys and add them to GitHub and servers;
-  - enroll Touch ID fingerprints.
-- **`verify`:** SSH and GPG identities match (critical). The keychain items recorded by other modules are present.
+Identities are recorded, never key material.
+- **SSH keys** in `~/.ssh`:
+  - every `*.pub` file, with its type and fingerprint, computed as `ssh-keygen -l` does from the public key alone, and whether its private half sits beside it;
+  - `id_*` files without a public half, by name.
+  - Private keys are only looked for, never opened.
+- **GPG keys:** `gpg --batch --no-tty --no-auto-check-trustdb --with-colons --list-secret-keys` gives each secret key's fingerprint, algorithm, creation date, and where its secret part is: here, on a card, or missing.
+  - gpg runs only when a GnuPG folder (`$GNUPGHOME` or `~/.gnupg`) exists, since it would create one. Like any use of gpg, it may start gpg-agent.
+  - mtm looks for gpg in `/opt/homebrew/bin`, `/usr/local/bin`, and `/usr/local/MacGPG2/bin`. Without it, `prepare` warns, and `verify` asks the user to check with `gpg -K`.
+- **Secure Enclave keys** can't move, so `scan` and `prepare` warn about them. They are found from any of:
+  - `sk-` key types: a hardware security key, or the Secure Enclave through macOS's `/usr/lib/ssh-keychain.dylib`;
+  - a `SecurityKeyProvider` in `~/.ssh/config`, or `SSH_SK_PROVIDER`, naming that library;
+  - an SSH agent whose catalog `kind` is `secure-enclave-agent` (Secretive), or such an app in the `system` record.
+- **The SSH agent:** `SSH_AUTH_SOCK` and the `IdentityAgent` lines in `~/.ssh/config` (not its `Include` files), matched against the catalog's `ssh_agent` sockets, following links.
+- **Password managers:** the apps in the `system` record whose catalog `kind` is `password-manager` (1Password, Bitwarden).
+- **Touch ID:** how many fingerprints are enrolled, from `/usr/bin/bioutil -c`, which only reads.
+- **Checklist steps** (section 7.3):
+  - turn on iCloud Keychain and check passwords and passkeys are present;
+  - sign in to each password manager, and turn on its SSH agent if SSH used it;
+  - create new Secure Enclave keys and add them to GitHub and servers, when the old Mac used them;
+  - enroll Touch ID fingerprints, unless the old Mac had none.
+- **`verify`:**
+  - SSH keys and GPG secret keys match the record (critical). A missing key, or a public key with a different fingerprint, fails.
+  - Reports name SSH keys by file and GPG keys by algorithm and date, never by fingerprint.
+  - Enrolled fingerprints resolve the Touch ID step.
+  - The `browsers` module checks its own keychain items.
 
 ### 9.11 `system`
 - **Recorded:**
```

- [ ] **Step 6: Run the tests to verify they pass**

Run:
```bash
gofmt -l .
go vet ./...
go vet -tags integration ./...
go test ./...
```
Expected: `gofmt -l .` prints nothing, and the rest pass.

Then run the no-network test:
```bash
go test -tags integration -run TestNoNetwork -v ./cmd/mtm/
```
Expected: PASS, or SKIP with its reason.

- [ ] **Step 7: Commit**

End the message with your `Co-Authored-By:` trailer (Global Constraints).

```bash
git add internal/modules/secrets/secrets_test.go cmd/mtm/main_test.go internal/modules/secrets/secrets.go internal/modules/secrets/ssh.go internal/modules/secrets/gpg.go cmd/mtm/main.go cmd/mtm/nonetwork_integration_test.go docs/superpowers/specs/2026-11-02-mtm-design.md
git commit -m "Add the secrets module

SSH and GPG identities are the hardest things to get back once the old
Mac is erased, so verify now checks each one arrived, as a critical
check, without reading key material. secrets also finds what can't
move: Secure Enclave keys, password managers and their SSH agents to
turn on again, and Touch ID fingerprints, and lists each as a step."
```

---

### Task 5: A second checklist, for retiring the old Mac

The retirement steps need the same done, skip, open, and walk-through as `mtm checklist`. They live in their own file, `retire.json`, so a step closed on the old Mac can never count toward the new Mac's verdict: Part 1's F1b kept steps off the old Mac's `state.json` for the same reason. This task changes no behavior. It makes the checklist code work on either file.

**Files:**
- Modify: `cmd/mtm/checklist.go`
- Modify: `internal/app/checklist.go`
- Modify: `internal/report/checklist.go`
- Modify: `internal/state/state.go`
- Test: `internal/state/state_test.go` (modify), `internal/app/steps_test.go` (modify), `internal/app/checklist_test.go` (modify), `internal/report/checklist_test.go` (modify)

**Interfaces:**
- Consumes: `state.State`, `checklist.List`, and the Part 1 `cmd/mtm` checklist code (`walk`, `ask`, `parse`, `stdin`, `interactive`).
- Produces:
  - `state.RetireFileName = "retire.json"`, plus `state.LoadFile(st, name) (State, error)` and `state.SaveFile(st, name, s) error`. `Load` and `Save` keep working on `state.json`.
  - `app.Steps`, a checklist kept in `~/.mtm`:
    - its methods are `List() (checklist.List, error)`, `Done(ids ...string) error`, `Skip(id, reason string) error`, and `Open(ctx, id string) error`;
    - `(*App).Checklist() Steps` is the new Mac's (`state.json`), and `(*App).Retirement() Steps` is the old Mac's (`retire.json`);
    - they replace Part 1's `App.Checklist() (checklist.List, error)`, `App.MarkDone`, `App.Skip`, and `App.OpenStep`.
  - `report.ChecklistCommand = "checklist"` and `report.RetireCommand = "retire"`. `report.Checklist(w, l, all, command string)` takes the command that closes a step, for its hints and its empty-list message.
  - In `cmd/mtm`:
    - `stepList{command string; steps app.Steps}`, with `fail(stderr, sub string, a ...any)`;
    - `stepsCommand(ctx, s stepList, args, stdout, stderr) int`, which runs the walk-through or `list`, `done`, `skip`, and `open` for either list.
    - `cmdChecklist` is `stepsCommand` with the new Mac's checklist.

- [ ] **Step 1: Write the failing tests**

The app tests move to the new methods: `a.Checklist().Done(…)` replaces `a.MarkDone(…)`, and so on. Two new tests check that the two lists stay apart, and one checks the retire hint.

`internal/state/state_test.go`:
```diff
--- a/internal/state/state_test.go
+++ b/internal/state/state_test.go
@@ -45,3 +45,18 @@ func TestSaveLoadKeepsTheChecklist(t *testing.T) {
 		t.Errorf("entry = %+v, %v", e, ok)
 	}
 }
+
+func TestRetirementStepsAreKeptApart(t *testing.T) {
+	st, _ := stage.Open(t.TempDir())
+	var s State
+	s.Checklist.Add(checklist.Item{ID: "retire:erase", Module: "retire", Title: "Erase this Mac"})
+	if err := SaveFile(st, RetireFileName, s); err != nil {
+		t.Fatal(err)
+	}
+	if got, err := Load(st); err != nil || len(got.Checklist.Entries) != 0 {
+		t.Errorf("state.json = %+v, %v; want the retirement step kept out of it", got, err)
+	}
+	if got, err := LoadFile(st, RetireFileName); err != nil || len(got.Checklist.Entries) != 1 {
+		t.Errorf("retire.json = %+v, %v; want the step", got, err)
+	}
+}
```

`internal/app/steps_test.go`:
```diff
--- a/internal/app/steps_test.go
+++ b/internal/app/steps_test.go
@@ -31,23 +31,23 @@ func savedSteps(t *testing.T, items ...checklist.Item) string {
 func TestMarkDoneAndSkipAreSaved(t *testing.T) {
 	home := savedSteps(t, grantStep, licenseStep)
 	a := stepApp(home, newUUID)
-	if err := a.MarkDone(grantStep.ID); err != nil {
+	if err := a.Checklist().Done(grantStep.ID); err != nil {
 		t.Fatal(err)
 	}
-	if err := a.Skip(licenseStep.ID, "sold it"); err != nil {
+	if err := a.Checklist().Skip(licenseStep.ID, "sold it"); err != nil {
 		t.Fatal(err)
 	}
-	l, err := a.Checklist()
+	l, err := a.Checklist().List()
 	if err != nil || len(l.Pending()) != 0 {
 		t.Errorf("checklist = %+v, %v; want nothing open", l, err)
 	}
-	if err := a.MarkDone("steps:nope"); !errors.Is(err, checklist.ErrUnknownItem) {
+	if err := a.Checklist().Done("steps:nope"); !errors.Is(err, checklist.ErrUnknownItem) {
 		t.Errorf("err = %v, want ErrUnknownItem", err)
 	}
 }
 
 // noFallbackApp answers commands only from its canned responses, unlike
-// stepApp: if OpenStep let through a link it should refuse, or changed how
+// stepApp: if Open let through a link it should refuse, or changed how
 // it calls open, the test fails instead of opening it on this Mac.
 func noFallbackApp(home string) *App {
 	return &App{Runner: &runner.Fake{Responses: testutil.MacResponses(newUUID)}, Home: home, Version: "test", Now: time.Now}
@@ -60,7 +60,7 @@ func TestOpenStepOpensItsSettingsPane(t *testing.T) {
 	a := noFallbackApp(home)
 	fake := a.Runner.(*runner.Fake)
 	fake.Responses["/usr/bin/open "+linked.Link] = runner.Response{}
-	if err := a.OpenStep(ctx, linked.ID); err != nil {
+	if err := a.Checklist().Open(ctx, linked.ID); err != nil {
 		t.Fatal(err)
 	}
 	if calls := fake.Calls(); calls[len(calls)-1].String() != "/usr/bin/open "+linked.Link {
@@ -73,7 +73,7 @@ func TestOpenStepRefusesALinkMtmDidntMake(t *testing.T) {
 	odd.Link = "file:///Applications/Calculator.app"
 	home := savedSteps(t, odd)
 	a := noFallbackApp(home)
-	if err := a.OpenStep(ctx, odd.ID); !errors.Is(err, ErrRefusedLink) {
+	if err := a.Checklist().Open(ctx, odd.ID); !errors.Is(err, ErrRefusedLink) {
 		t.Errorf("err = %v", err)
 	}
 	if len(a.Runner.(*runner.Fake).Calls()) != 0 {
@@ -85,7 +85,34 @@ func TestOpenStepWithoutALinkNamesThePlace(t *testing.T) {
 	plain := licenseStep
 	plain.Where = "Dash > Settings > License"
 	home := savedSteps(t, plain)
-	if err := noFallbackApp(home).OpenStep(ctx, plain.ID); !errors.Is(err, ErrNoLink) || !strings.Contains(err.Error(), "Dash > Settings") {
+	if err := noFallbackApp(home).Checklist().Open(ctx, plain.ID); !errors.Is(err, ErrNoLink) || !strings.Contains(err.Error(), "Dash > Settings") {
 		t.Errorf("err = %v", err)
 	}
 }
+
+func TestRetirementStepsLiveApartFromTheChecklist(t *testing.T) {
+	home := savedSteps(t, grantStep)
+	st, err := stage.Existing(home)
+	if err != nil {
+		t.Fatal(err)
+	}
+	erase := checklist.Item{ID: "retire:erase", Module: "retire", Title: "Erase this Mac"}
+	var r state.State
+	r.Checklist.Add(erase)
+	if err := state.SaveFile(st, state.RetireFileName, r); err != nil {
+		t.Fatal(err)
+	}
+	a := stepApp(home, oldUUID)
+	if err := a.Checklist().Done(erase.ID); !errors.Is(err, checklist.ErrUnknownItem) {
+		t.Errorf("the new Mac's checklist found a retirement step: %v", err)
+	}
+	if err := a.Retirement().Done(erase.ID); err != nil {
+		t.Fatal(err)
+	}
+	if l, err := a.Retirement().List(); err != nil || len(l.Pending()) != 0 {
+		t.Errorf("retirement steps = %+v, %v; want the erase step done", l, err)
+	}
+	if l, err := a.Checklist().List(); err != nil || len(l.Pending()) != 1 {
+		t.Errorf("checklist = %+v, %v; want the grant step still open", l, err)
+	}
+}
```

`internal/app/checklist_test.go`:
```diff
--- a/internal/app/checklist_test.go
+++ b/internal/app/checklist_test.go
@@ -76,7 +76,7 @@ func skip(t *testing.T, home, id, reason string, at time.Time) {
 	t.Helper()
 	a := stepApp(home, newUUID)
 	a.Now = func() time.Time { return at }
-	if err := a.Skip(id, reason); err != nil {
+	if err := a.Checklist().Skip(id, reason); err != nil {
 		t.Fatal(err)
 	}
 }
@@ -234,7 +234,7 @@ func TestFinishThatAddsAStepNeedsAnotherVerifyBeforeCleanup(t *testing.T) {
 	if _, err := a.Finish(ctx, FinishOptions{}); err != nil {
 		t.Fatal(err)
 	}
-	if err := a.Skip(licenseStep.ID, "sold the license"); err != nil {
+	if err := a.Checklist().Skip(licenseStep.ID, "sold the license"); err != nil {
 		t.Fatal(err)
 	}
 	if rep, err := a.Verify(ctx, VerifyOptions{}); err != nil || !rep.Verdict.Safe {
```

`internal/report/checklist_test.go`:
```diff
--- a/internal/report/checklist_test.go
+++ b/internal/report/checklist_test.go
@@ -21,7 +21,7 @@ func sampleList() checklist.List {
 
 func TestChecklistShowsOpenStepsWithTheirCommand(t *testing.T) {
 	var out bytes.Buffer
-	Checklist(&out, sampleList(), false)
+	Checklist(&out, sampleList(), false, ChecklistCommand)
 	for _, want := range []string{"1 step open", "☐ Give Raycast Accessibility access", "System Settings > Privacy & Security > Accessibility", "→ mtm checklist done 'permissions:com.raycast.macos:Accessibility'"} {
 		if !strings.Contains(out.String(), want) {
 			t.Errorf("output missing %q:\n%s", want, out.String())
@@ -34,7 +34,7 @@ func TestChecklistShowsOpenStepsWithTheirCommand(t *testing.T) {
 
 func TestChecklistAllShowsSkippedStepsWithTheirReason(t *testing.T) {
 	var out bytes.Buffer
-	Checklist(&out, sampleList(), true)
+	Checklist(&out, sampleList(), true, ChecklistCommand)
 	if !strings.Contains(out.String(), "– Reactivate the Dash license") || !strings.Contains(out.String(), "skipped: sold it") {
 		t.Errorf("output:\n%s", out.String())
 	}
@@ -42,14 +42,14 @@ func TestChecklistAllShowsSkippedStepsWithTheirReason(t *testing.T) {
 
 func TestChecklistEmptyAndFinished(t *testing.T) {
 	var out bytes.Buffer
-	Checklist(&out, checklist.List{}, false)
+	Checklist(&out, checklist.List{}, false, ChecklistCommand)
 	if !strings.Contains(out.String(), "mtm finish and mtm verify fill it") {
 		t.Errorf("empty output:\n%s", out.String())
 	}
 	l := sampleList()
 	l.Done("permissions:com.raycast.macos:Accessibility", time.Now())
 	out.Reset()
-	Checklist(&out, l, false)
+	Checklist(&out, l, false, ChecklistCommand)
 	if !strings.Contains(out.String(), "Every checklist step is done or skipped.") {
 		t.Errorf("finished output:\n%s", out.String())
 	}
@@ -65,7 +65,7 @@ func TestChecklistQuotesAnIdWithSpacesInItsCommand(t *testing.T) {
 	var l checklist.List
 	l.Add(checklist.Item{ID: "permissions:com.raycast.macos:Screen Recording", Title: "Give Raycast Screen Recording access"})
 	var out bytes.Buffer
-	Checklist(&out, l, false)
+	Checklist(&out, l, false, ChecklistCommand)
 	if want := "→ mtm checklist done 'permissions:com.raycast.macos:Screen Recording'"; !strings.Contains(out.String(), want) {
 		t.Errorf("output missing %q:\n%s", want, out.String())
 	}
@@ -74,10 +74,23 @@ func TestChecklistQuotesAnIdWithSpacesInItsCommand(t *testing.T) {
 func TestChecklistShowsAStepSkippedWithoutAReasonAsOpen(t *testing.T) {
 	l := checklist.List{Entries: []checklist.Entry{{Item: checklist.Item{ID: "system:license:dash", Title: "Reactivate the Dash license"}, Status: checklist.Skipped}}}
 	var out bytes.Buffer
-	Checklist(&out, l, false)
+	Checklist(&out, l, false, ChecklistCommand)
 	for _, want := range []string{"1 step open", "☐ Reactivate the Dash license", "→ mtm checklist done 'system:license:dash'"} {
 		if !strings.Contains(out.String(), want) {
 			t.Errorf("output missing %q:\n%s", want, out.String())
 		}
 	}
 }
+
+func TestRetirementStepsNameTheRetireCommand(t *testing.T) {
+	var out bytes.Buffer
+	Checklist(&out, sampleList(), false, RetireCommand)
+	if !strings.Contains(out.String(), "→ mtm retire done 'permissions:com.raycast.macos:Accessibility'") {
+		t.Errorf("output:\n%s", out.String())
+	}
+	out.Reset()
+	Checklist(&out, checklist.List{}, false, RetireCommand)
+	if !strings.Contains(out.String(), "mtm retire adds them") {
+		t.Errorf("empty output:\n%s", out.String())
+	}
+}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/state/ ./internal/app/ ./internal/report/`
Expected: FAIL, because the build fails:
```text
FAIL	github.com/momentmaker/mtm/internal/state [build failed]
FAIL	github.com/momentmaker/mtm/internal/app [build failed]
FAIL	github.com/momentmaker/mtm/internal/report [build failed]
FAIL
# github.com/momentmaker/mtm/internal/state [github.com/momentmaker/mtm/internal/state.test]
internal/state/state_test.go:53:12: undefined: SaveFile
internal/state/state_test.go:53:25: undefined: RetireFileName
internal/state/state_test.go:59:17: undefined: LoadFile
```

- [ ] **Step 3: Implement**

`internal/state/state.go`:
```diff
--- a/internal/state/state.go
+++ b/internal/state/state.go
@@ -1,4 +1,5 @@
-// Package state keeps mtm's progress on the new Mac in ~/.mtm/state.json.
+// Package state keeps mtm's progress in ~/.mtm: the new Mac's in
+// state.json, and the old Mac's retirement steps in retire.json.
 package state
 
 import (
@@ -13,22 +14,33 @@ import (
 	"github.com/momentmaker/mtm/internal/stage"
 )
 
-const FileName = "state.json"
+// RetireFileName holds the retirement steps, apart from state.json, so a
+// step closed on the old Mac can never count toward the new Mac's
+// verdict, even if Migration Assistant runs again and copies ~/.mtm.
+const (
+	FileName       = "state.json"
+	RetireFileName = "retire.json"
+)
 
 type VerifyResult struct {
 	At   time.Time `json:"at"`
 	Safe bool      `json:"safe"`
 }
 
-// State holds the new Mac's checklist, with what the user did with each
-// step, and the last verify result.
+// State holds a checklist, with what the user did with each step, and in
+// state.json the last verify result.
 type State struct {
 	LastVerify *VerifyResult  `json:"last_verify,omitempty"`
 	Checklist  checklist.List `json:"checklist,omitzero"`
 }
 
-func Load(st *stage.Stage) (State, error) {
-	path := st.Path(FileName)
+func Load(st *stage.Stage) (State, error) { return LoadFile(st, FileName) }
+
+func Save(st *stage.Stage, s State) error { return SaveFile(st, FileName, s) }
+
+// LoadFile reads the named state file; a missing one is empty.
+func LoadFile(st *stage.Stage, name string) (State, error) {
+	path := st.Path(name)
 	data, err := os.ReadFile(path)
 	if errors.Is(err, fs.ErrNotExist) {
 		return State{}, nil
@@ -43,10 +55,10 @@ func Load(st *stage.Stage) (State, error) {
 	return s, nil
 }
 
-func Save(st *stage.Stage, s State) error {
+func SaveFile(st *stage.Stage, name string, s State) error {
 	data, err := json.MarshalIndent(s, "", "  ")
 	if err != nil {
-		return fmt.Errorf("encode state: %w", err)
+		return fmt.Errorf("encode %s: %w", name, err)
 	}
-	return st.WriteFile(FileName, data)
+	return st.WriteFile(name, data)
 }
```

`internal/app/checklist.go`:
```diff
--- a/internal/app/checklist.go
+++ b/internal/app/checklist.go
@@ -13,7 +13,7 @@ import (
 )
 
 // settingsLink is the only kind of link mtm opens: it made every
-// checklist link itself, and state.json is a file anyone with the account
+// checklist link itself, and a state file is one anyone with the account
 // could edit to point open at something else.
 const settingsLink = "x-apple.systempreferences:"
 
@@ -22,24 +22,35 @@ var (
 	ErrRefusedLink = errors.New("mtm opens only System Settings links")
 )
 
-// Checklist returns the steps finish left on this Mac, with what the user
-// did with each.
-func (a *App) Checklist() (checklist.List, error) {
-	st, err := stage.Existing(a.Home)
+// Steps is one checklist kept in ~/.mtm, with what the user did with each
+// step: the new Mac's, or the old Mac's retirement steps.
+type Steps struct {
+	app  *App
+	file string
+}
+
+// Checklist is the new Mac's checklist, which finish and verify fill.
+func (a *App) Checklist() Steps { return Steps{app: a, file: state.FileName} }
+
+// Retirement is the old Mac's retirement checklist, which retire fills.
+func (a *App) Retirement() Steps { return Steps{app: a, file: state.RetireFileName} }
+
+func (s Steps) List() (checklist.List, error) {
+	st, err := stage.Existing(s.app.Home)
 	if err != nil {
 		return checklist.List{}, err
 	}
-	s, err := state.Load(st)
+	saved, err := state.LoadFile(st, s.file)
 	if err != nil {
 		return checklist.List{}, err
 	}
-	return s.Checklist, nil
+	return saved.Checklist, nil
 }
 
-func (a *App) MarkDone(ids ...string) error {
-	return a.updateChecklist(func(l *checklist.List) error {
+func (s Steps) Done(ids ...string) error {
+	return s.update(func(l *checklist.List) error {
 		for _, id := range ids {
-			if err := l.Done(id, a.Now()); err != nil {
+			if err := l.Done(id, s.app.Now()); err != nil {
 				return err
 			}
 		}
@@ -47,28 +58,28 @@ func (a *App) MarkDone(ids ...string) error {
 	})
 }
 
-func (a *App) Skip(id, reason string) error {
-	return a.updateChecklist(func(l *checklist.List) error { return l.Skip(id, reason, a.Now()) })
+func (s Steps) Skip(id, reason string) error {
+	return s.update(func(l *checklist.List) error { return l.Skip(id, reason, s.app.Now()) })
 }
 
-func (a *App) updateChecklist(change func(*checklist.List) error) error {
-	st, err := stage.Existing(a.Home)
+func (s Steps) update(change func(*checklist.List) error) error {
+	st, err := stage.Existing(s.app.Home)
 	if err != nil {
 		return err
 	}
-	s, err := state.Load(st)
+	saved, err := state.LoadFile(st, s.file)
 	if err != nil {
 		return err
 	}
-	if err := change(&s.Checklist); err != nil {
+	if err := change(&saved.Checklist); err != nil {
 		return err
 	}
-	return state.Save(st, s)
+	return state.SaveFile(st, s.file, saved)
 }
 
-// OpenStep opens the System Settings pane a step happens in.
-func (a *App) OpenStep(ctx context.Context, id string) error {
-	l, err := a.Checklist()
+// Open opens the System Settings pane a step happens in.
+func (s Steps) Open(ctx context.Context, id string) error {
+	l, err := s.List()
 	if err != nil {
 		return err
 	}
@@ -82,7 +93,7 @@ func (a *App) OpenStep(ctx context.Context, id string) error {
 	if !strings.HasPrefix(e.Link, settingsLink) {
 		return fmt.Errorf("%w; refusing %q", ErrRefusedLink, e.Link)
 	}
-	if _, err := a.Runner.Run(ctx, runner.Command{Name: "/usr/bin/open", Args: []string{e.Link}}); err != nil {
+	if _, err := s.app.Runner.Run(ctx, runner.Command{Name: "/usr/bin/open", Args: []string{e.Link}}); err != nil {
 		return fmt.Errorf("open %q: %w", e.Where, err)
 	}
 	return nil
```

`internal/report/checklist.go`:
```diff
--- a/internal/report/checklist.go
+++ b/internal/report/checklist.go
@@ -19,12 +19,25 @@ func stepSymbol(e checklist.Entry) string {
 	return "–"
 }
 
+// The commands that keep a checklist: checklist on the new Mac, retire on
+// the old one.
+const (
+	ChecklistCommand = "checklist"
+	RetireCommand    = "retire"
+)
+
+// emptyChecklist says, per command, where its steps come from.
+var emptyChecklist = map[string]string{
+	ChecklistCommand: "The checklist is empty. mtm finish and mtm verify fill it on the new Mac.",
+	RetireCommand:    "There are no retirement steps yet. mtm retire adds them on the old Mac.",
+}
+
 // Checklist prints the open steps, or every step with all, each with the
-// command that closes it.
-func Checklist(w io.Writer, l checklist.List, all bool) {
+// mtm command that closes it.
+func Checklist(w io.Writer, l checklist.List, all bool, command string) {
 	open := len(l.Pending())
 	if len(l.Entries) == 0 {
-		fmt.Fprintln(w, "The checklist is empty. mtm finish and mtm verify fill it on the new Mac.")
+		fmt.Fprintln(w, emptyChecklist[command])
 		return
 	}
 	if open == 0 && !all {
@@ -40,7 +53,7 @@ func Checklist(w io.Writer, l checklist.List, all bool) {
 		writeStepBody(w, e)
 		switch {
 		case e.Open():
-			fmt.Fprintf(w, "      → mtm checklist done %s\n", clean(macos.ShellQuote(e.ID)))
+			fmt.Fprintf(w, "      → mtm %s done %s\n", command, clean(macos.ShellQuote(e.ID)))
 		case e.Status == checklist.Skipped:
 			fmt.Fprintf(w, "      skipped: %s\n", clean(e.Reason))
 		}
```

`cmd/mtm/checklist.go`:
```diff
--- a/cmd/mtm/checklist.go
+++ b/cmd/mtm/checklist.go
@@ -25,23 +25,45 @@ var (
 )
 
 func cmdChecklist(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
+	return stepsCommand(ctx, stepList{report.ChecklistCommand, a.Checklist()}, args, stdout, stderr)
+}
+
+// stepList is a checklist and the mtm command that keeps it, which names
+// it in messages and hints.
+type stepList struct {
+	command string
+	steps   app.Steps
+}
+
+// fail prints a message as the command's own, such as "mtm retire done: …".
+func (s stepList) fail(stderr io.Writer, sub string, a ...any) {
+	name := "mtm " + s.command
+	if sub != "" {
+		name += " " + sub
+	}
+	fmt.Fprintln(stderr, append([]any{name + ":"}, a...)...)
+}
+
+// stepsCommand walks through the open steps in a terminal and lists them
+// elsewhere, or runs one of list, done, skip, and open.
+func stepsCommand(ctx context.Context, s stepList, args []string, stdout, stderr io.Writer) int {
 	if len(args) == 0 {
 		if interactive() {
-			return walk(ctx, a, stdin, stdout, stderr)
+			return walk(ctx, s, stdin, stdout, stderr)
 		}
-		return listSteps(a, nil, stdout, stderr)
+		return listSteps(s, nil, stdout, stderr)
 	}
 	switch args[0] {
 	case "list":
-		return listSteps(a, args[1:], stdout, stderr)
+		return listSteps(s, args[1:], stdout, stderr)
 	case "done":
-		return markDone(a, args[1:], stdout, stderr)
+		return markDone(s, args[1:], stdout, stderr)
 	case "skip":
-		return skipStep(a, args[1:], stdout, stderr)
+		return skipStep(s, args[1:], stdout, stderr)
 	case "open":
-		return openStep(ctx, a, args[1:], stderr)
+		return openStep(ctx, s, args[1:], stderr)
 	}
-	fmt.Fprintf(stderr, "mtm checklist: unknown subcommand %q\n\n%s", args[0], usage)
+	fmt.Fprintf(stderr, "mtm %s: unknown subcommand %q\n\n%s", s.command, args[0], usage)
 	return exitFailed
 }
 
@@ -61,17 +83,17 @@ func parse(flags *flag.FlagSet, args []string) ([]string, error) {
 	}
 }
 
-func listSteps(a *app.App, args []string, stdout, stderr io.Writer) int {
-	flags := flag.NewFlagSet("checklist list", flag.ContinueOnError)
+func listSteps(s stepList, args []string, stdout, stderr io.Writer) int {
+	flags := flag.NewFlagSet(s.command+" list", flag.ContinueOnError)
 	flags.SetOutput(stderr)
 	all := flags.Bool("all", false, "also show steps already done or skipped")
 	asJSON := flags.Bool("json", false, "print JSON")
 	if _, err := parse(flags, args); err != nil {
 		return exitFailed
 	}
-	l, err := a.Checklist()
+	l, err := s.steps.List()
 	if err != nil {
-		fmt.Fprintln(stderr, "mtm checklist:", err)
+		s.fail(stderr, "", err)
 		return exitFailed
 	}
 	if *asJSON {
@@ -79,30 +101,30 @@ func listSteps(a *app.App, args []string, stdout, stderr io.Writer) int {
 			l = checklist.List{Entries: l.Pending()}
 		}
 		if err := report.JSON(stdout, l); err != nil {
-			fmt.Fprintln(stderr, "mtm checklist:", err)
+			s.fail(stderr, "", err)
 			return exitFailed
 		}
 		return exitOK
 	}
-	report.Checklist(stdout, l, *all)
+	report.Checklist(stdout, l, *all, s.command)
 	return exitOK
 }
 
-func markDone(a *app.App, args []string, stdout, stderr io.Writer) int {
+func markDone(s stepList, args []string, stdout, stderr io.Writer) int {
 	if len(args) == 0 {
-		fmt.Fprintln(stderr, "mtm checklist done: name the step ids to mark done")
+		s.fail(stderr, "done", "name the step ids to mark done")
 		return exitFailed
 	}
-	if err := a.MarkDone(args...); err != nil {
-		fmt.Fprintln(stderr, "mtm checklist done:", err)
+	if err := s.steps.Done(args...); err != nil {
+		s.fail(stderr, "done", err)
 		return exitFailed
 	}
 	fmt.Fprintf(stdout, "Marked done: %s\n", strings.Join(args, ", "))
 	return exitOK
 }
 
-func skipStep(a *app.App, args []string, stdout, stderr io.Writer) int {
-	flags := flag.NewFlagSet("checklist skip", flag.ContinueOnError)
+func skipStep(s stepList, args []string, stdout, stderr io.Writer) int {
+	flags := flag.NewFlagSet(s.command+" skip", flag.ContinueOnError)
 	flags.SetOutput(stderr)
 	reason := flags.String("reason", "", "why this step doesn't apply (required)")
 	ids, err := parse(flags, args)
@@ -110,24 +132,24 @@ func skipStep(a *app.App, args []string, stdout, stderr io.Writer) int {
 		return exitFailed
 	}
 	if len(ids) != 1 {
-		fmt.Fprintln(stderr, `mtm checklist skip: name one step id, with --reason "..."`)
+		s.fail(stderr, "skip", `name one step id, with --reason "..."`)
 		return exitFailed
 	}
-	if err := a.Skip(ids[0], *reason); err != nil {
-		fmt.Fprintln(stderr, "mtm checklist skip:", err)
+	if err := s.steps.Skip(ids[0], *reason); err != nil {
+		s.fail(stderr, "skip", err)
 		return exitFailed
 	}
 	fmt.Fprintf(stdout, "Skipped: %s\n", ids[0])
 	return exitOK
 }
 
-func openStep(ctx context.Context, a *app.App, args []string, stderr io.Writer) int {
+func openStep(ctx context.Context, s stepList, args []string, stderr io.Writer) int {
 	if len(args) != 1 {
-		fmt.Fprintln(stderr, "mtm checklist open: name one step id")
+		s.fail(stderr, "open", "name one step id")
 		return exitFailed
 	}
-	if err := a.OpenStep(ctx, args[0]); err != nil {
-		fmt.Fprintln(stderr, "mtm checklist open:", err)
+	if err := s.steps.Open(ctx, args[0]); err != nil {
+		s.fail(stderr, "open", err)
 		if errors.Is(err, app.ErrRefusedLink) || errors.Is(err, app.ErrNoLink) {
 			return exitProblems
 		}
@@ -139,42 +161,42 @@ func openStep(ctx context.Context, a *app.App, args []string, stderr io.Writer)
 // walk goes through the open steps one at a time: it opens each step's
 // System Settings pane and asks what happened, saving every answer at once
 // so quitting loses nothing.
-func walk(ctx context.Context, a *app.App, in io.Reader, stdout, stderr io.Writer) int {
-	l, err := a.Checklist()
+func walk(ctx context.Context, s stepList, in io.Reader, stdout, stderr io.Writer) int {
+	l, err := s.steps.List()
 	if err != nil {
-		fmt.Fprintln(stderr, "mtm checklist:", err)
+		s.fail(stderr, "", err)
 		return exitFailed
 	}
 	steps := l.Pending()
 	if len(steps) == 0 {
-		report.Checklist(stdout, l, false)
+		report.Checklist(stdout, l, false, s.command)
 		return exitOK
 	}
 	r := bufio.NewReader(in)
 	for i, e := range steps {
 		report.Step(stdout, i+1, len(steps), e)
 		if e.Link != "" {
-			if err := a.OpenStep(ctx, e.ID); err != nil {
+			if err := s.steps.Open(ctx, e.ID); err != nil {
 				fmt.Fprintln(stdout, "  Could not open it:", err)
 			}
 		}
-		if quit := ask(ctx, a, r, e, stdout); quit {
+		if quit := ask(ctx, s.steps, r, e, stdout); quit {
 			break
 		}
 	}
-	l, err = a.Checklist()
+	l, err = s.steps.List()
 	if err != nil {
-		fmt.Fprintln(stderr, "mtm checklist:", err)
+		s.fail(stderr, "", err)
 		return exitFailed
 	}
 	fmt.Fprintln(stdout)
-	report.Checklist(stdout, l, false)
+	report.Checklist(stdout, l, false, s.command)
 	return exitOK
 }
 
 // ask reads answers until one settles the step, and reports a quit or the
 // end of input.
-func ask(ctx context.Context, a *app.App, r *bufio.Reader, e checklist.Entry, out io.Writer) bool {
+func ask(ctx context.Context, steps app.Steps, r *bufio.Reader, e checklist.Entry, out io.Writer) bool {
 	for {
 		fmt.Fprint(out, "  [d]one, [s]kip, [l]ater, [q]uit? ")
 		answer, err := readLine(ctx, r)
@@ -183,7 +205,7 @@ func ask(ctx context.Context, a *app.App, r *bufio.Reader, e checklist.Entry, ou
 		}
 		switch strings.ToLower(answer) {
 		case "d", "done":
-			if err := a.MarkDone(e.ID); err != nil {
+			if err := steps.Done(e.ID); err != nil {
 				fmt.Fprintln(out, "  ", err)
 				continue
 			}
@@ -194,7 +216,7 @@ func ask(ctx context.Context, a *app.App, r *bufio.Reader, e checklist.Entry, ou
 			if err != nil {
 				return true
 			}
-			if err := a.Skip(e.ID, reason); err != nil {
+			if err := steps.Skip(e.ID, reason); err != nil {
 				fmt.Fprintln(out, "  ", err)
 				continue
 			}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
gofmt -l .
go vet ./...
go vet -tags integration ./...
go test ./...
```
Expected: `gofmt -l .` prints nothing, and the rest pass.

- [ ] **Step 5: Commit**

End the message with your `Co-Authored-By:` trailer (Global Constraints).

```bash
git add internal/state/state_test.go internal/app/steps_test.go internal/app/checklist_test.go internal/report/checklist_test.go internal/state/state.go internal/app/checklist.go internal/report/checklist.go cmd/mtm/checklist.go
git commit -m "Let the checklist code work on more than one list

mtm retire will keep the old Mac's steps in retire.json, apart from
state.json, so a step closed on the old Mac can never count toward the
new Mac's verdict. The checklist methods move onto a Steps value for
either file, and mtm checklist's commands take the list they work on."
```

---

### Task 6: What changed on the old Mac since `prepare`

**Files:**
- Create: `internal/modules/files/retire.go`
- Modify: `internal/module/module.go`
- Test: `internal/modules/files/retire_test.go`

**Interfaces:**
- Consumes:
  - `walkHome`, `excluded`, `groupOf`, `area`, `isCritical`, `maxPathsPerGroup`, and `Module.isVolatile` in package `files` (Stage 1 and Part 1);
  - `manifest.NewRecordReader` and `module.LoadInventory`.
- Produces:
  - `module.Retirement{Changes []Check; Steps []checklist.Item}` and `module.Retirer`, with `Retire(env Env, st *stage.Stage) (Retirement, error)`.
  - `files.Module` implements `module.Retirer`. Its `Changes` has one check per folder group, as verify's do, with id `files:<group>`:
    - status `warn` outside `~/Library`, and `info` inside it, where apps rewrite their state all the time;
    - `Detail` counts the changed, new, and unreadable paths, and `Paths` lists them (up to 200).

**How it finds changes without holding the record in memory:**
- The record and a fresh walk of the home folder visit paths in the same order: each folder's entries in name order, an entry's contents before the next entry.
- So `Retire` reads the record one entry ahead while it walks, and catches the record up to each path it visits. Entries it passes are paths deleted since prepare.
- That order isn't plain string order. `"a/b"` comes before `"a.txt"`, though `'/'` sorts after `'.'`, so `walkBefore` compares path segments.
  - `TestWalkBeforeFollowsTheWalk` pins the comparison.
  - `TestRetireKeepsItsPlacePastADeletedFile` fails if it is replaced by `a < b`. This plan's author checked that.

**What counts as a change:**
- A file or link that differs from its record in type, size, modification time, or link target is changed.
- A path the record doesn't have is new, even with an old time, as a copy from a drive keeps.
- Folders, cloud-only files, other volumes, and volatile paths aren't listed. A folder that can no longer be read is.

- [ ] **Step 1: Write the failing tests**

`internal/modules/files/retire_test.go`:
```go
package files

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

func TestWalkBeforeFollowsTheWalk(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"a", "a/b", true},
		{"a/b", "a.txt", true},
		{"a.txt", "a/b", false},
		{"B", "a", true},
		{"a/z", "b", true},
		{"a", "a", false},
	}
	for _, tt := range tests {
		if got := walkBefore(tt.a, tt.b); got != tt.want {
			t.Errorf("walkBefore(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func retireHome(t *testing.T, home string, m *manifest.Manifest) []module.Check {
	t.Helper()
	st, err := stage.Existing(home)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New().Retire(module.Env{Ctx: context.Background(), Home: home, Manifest: m}, st)
	if err != nil {
		t.Fatal(err)
	}
	return r.Changes
}

func TestRetireListsNothingWhenNothingChanged(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	if changes := retireHome(t, home, m); len(changes) != 0 {
		t.Errorf("changes = %+v, want none", changes)
	}
}

func TestRetireListsChangedAndNewFiles(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	testutil.WriteFile(t, filepath.Join(home, "Documents/notes.md"), "notes, longer now\n")
	testutil.WriteFile(t, filepath.Join(home, "Documents/a.md"), "new\n")
	testutil.WriteFile(t, filepath.Join(home, "Documents/drafts/b.md"), "new\n")
	testutil.WriteFile(t, filepath.Join(home, "Library/Preferences/q.plist"), "app state\n")
	testutil.WriteFile(t, filepath.Join(home, "GitHub/app/.DS_Store"), "volatile, rewritten\n")
	if err := os.Remove(filepath.Join(home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	changes := retireHome(t, home, m)
	want := []module.Check{
		{Module: Name, ID: "files:Documents", Status: module.StatusWarn, Title: "Documents", Detail: "1 changed, 2 new",
			Paths: []string{"Documents/a.md", "Documents/drafts/b.md", "Documents/notes.md"}},
		{Module: Name, ID: "files:Library/Preferences", Status: module.StatusInfo, Title: "Library/Preferences", Detail: "1 new",
			Paths: []string{"Library/Preferences/q.plist"}},
	}
	if !slices.EqualFunc(changes, want, checksEqual) {
		t.Errorf("changes = %+v\nwant      %+v", changes, want)
	}
}

func checksEqual(a, b module.Check) bool {
	return a.ID == b.ID && a.Status == b.Status && a.Title == b.Title && a.Detail == b.Detail && slices.Equal(a.Paths, b.Paths)
}

// A file copied in with its old time, as Finder and cp -p do, still
// isn't in the record.
func TestRetireListsANewFileWithAnOldTime(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	path := filepath.Join(home, "Documents/old-photo.jpg")
	testutil.WriteFile(t, path, "jpeg\n")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	changes := retireHome(t, home, m)
	if len(changes) != 1 || !slices.Equal(changes[0].Paths, []string{"Documents/old-photo.jpg"}) {
		t.Errorf("changes = %+v, want the copied-in file", changes)
	}
}

func TestRetireListsAFileRewrittenWithTheSameSize(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	path := filepath.Join(home, "Documents/notes.md")
	later := time.Now().Add(time.Minute)
	testutil.WriteFile(t, path, "NOTES\n")
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	changes := retireHome(t, home, m)
	if len(changes) != 1 || changes[0].Detail != "1 changed" {
		t.Errorf("changes = %+v, want notes.md changed", changes)
	}
}

func TestRetireListsAFolderItCanNoLongerRead(t *testing.T) {
	home := seedHome(t)
	m := prepareHome(t, home)
	dir := filepath.Join(home, "Documents")
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	changes := retireHome(t, home, m)
	if len(changes) != 1 || changes[0].Title != "Documents" || changes[0].Detail != "1 couldn't be read" {
		t.Errorf("changes = %+v, want Documents unreadable", changes)
	}
}

// The record holds Documents/a/x.md before Documents/a.txt, in walk order.
// Deleting the first must not make mtm lose its place and call the
// second new.
func TestRetireKeepsItsPlacePastADeletedFile(t *testing.T) {
	home := seedHome(t)
	testutil.WriteFile(t, filepath.Join(home, "Documents/a/x.md"), "x\n")
	testutil.WriteFile(t, filepath.Join(home, "Documents/a.txt"), "a\n")
	m := prepareHome(t, home)
	if err := os.Remove(filepath.Join(home, "Documents/a/x.md")); err != nil {
		t.Fatal(err)
	}
	if changes := retireHome(t, home, m); len(changes) != 0 {
		t.Errorf("changes = %+v, want none", changes)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/files/`
Expected: FAIL, because the build fails:
```text
FAIL	github.com/momentmaker/mtm/internal/modules/files [build failed]
FAIL
# github.com/momentmaker/mtm/internal/modules/files [github.com/momentmaker/mtm/internal/modules/files.test]
internal/modules/files/retire_test.go:30:13: undefined: walkBefore
internal/modules/files/retire_test.go:42:18: New().Retire undefined (type Module has no field or method Retire)
```

- [ ] **Step 3: Implement**

`internal/module/module.go`:
```diff
--- a/internal/module/module.go
+++ b/internal/module/module.go
@@ -111,6 +111,20 @@ type Stepper interface {
 	Steps(env Env) ([]checklist.Item, error)
 }
 
+// Retirement is what a module gives mtm retire on the old Mac. Changes are
+// what changed there since prepare, which the new Mac may not have; Steps
+// are things to do before the old Mac is erased, such as releasing a
+// license.
+type Retirement struct {
+	Changes []Check
+	Steps   []checklist.Item
+}
+
+// Retirer is implemented by modules with something for mtm retire.
+type Retirer interface {
+	Retire(env Env, st *stage.Stage) (Retirement, error)
+}
+
 // Undoer is implemented by modules whose prepare changes the old Mac, so
 // prepare --undo can put it back.
 type Undoer interface {
```

`internal/modules/files/retire.go`:
```go
package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
)

// Retire lists what changed in the home folder since prepare: files that
// differ from the record, and files it doesn't have. Migration Assistant
// may have copied each before or after it changed, so the user decides
// what to copy over. Deleted files aren't listed; verify reports what the
// new Mac is missing.
func (m Module) Retire(env module.Env, st *stage.Stage) (module.Retirement, error) {
	var stats Stats
	if _, err := module.LoadInventory(env, Name, &stats); err != nil {
		return module.Retirement{}, err
	}
	f, err := os.Open(st.Path(manifest.RecordName))
	if err != nil {
		return module.Retirement{}, fmt.Errorf("open file record: %w", err)
	}
	defer f.Close()
	rr, err := manifest.NewRecordReader(f)
	if err != nil {
		return module.Retirement{}, err
	}
	t, err := m.changes(env, rr)
	if err != nil {
		return module.Retirement{}, err
	}
	return module.Retirement{Changes: t.checks()}, nil
}

// recordCursor reads the record one entry ahead, so the walk can catch it
// up to each path it visits.
type recordCursor struct {
	rr   *manifest.RecordReader
	cur  manifest.Entry
	done bool
}

func (c *recordCursor) advance() error {
	e, err := c.rr.Next()
	if errors.Is(err, io.EOF) {
		c.done = true
		return nil
	}
	c.cur = e
	return err
}

// at returns the record's entry for rel, if it has one. The record is in
// walk order, so entries before rel are paths deleted since prepare.
func (c *recordCursor) at(rel string) (manifest.Entry, bool, error) {
	for !c.done && walkBefore(c.cur.Path, rel) {
		if err := c.advance(); err != nil {
			return manifest.Entry{}, false, err
		}
	}
	if c.done || c.cur.Path != rel {
		return manifest.Entry{}, false, nil
	}
	e := c.cur
	return e, true, c.advance()
}

// walkBefore reports whether the walk reaches a before b. It visits each
// folder's entries in name order, and an entry's contents before the next
// entry, so "a/b" comes before "a.txt" although it sorts after it as a
// string.
func walkBefore(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

func (m Module) changes(env module.Env, rr *manifest.RecordReader) (*changeTally, error) {
	c := &recordCursor{rr: rr}
	if err := c.advance(); err != nil {
		return nil, err
	}
	t := newChangeTally()
	err := walkHome(env.Ctx, env.Home, excluded, func(it item) error {
		rec, recorded, err := c.at(it.rel)
		if err != nil {
			return err
		}
		if m.isVolatile(it.rel) {
			return nil
		}
		switch {
		case it.unreadable:
			t.add(it.rel, unreadableNow)
		case it.mount, it.cloud, it.info.IsDir():
			// Nothing to compare: another volume, a file whose data is in
			// the cloud, or a folder, whose new files are listed themselves.
		case !recorded:
			t.add(it.rel, added)
		case differs(rec, it):
			t.add(it.rel, changed)
		}
		return nil
	})
	return t, err
}

// differs compares a file or link with its record. A file that was in the
// cloud at prepare and is downloaded now keeps its size and time.
func differs(rec manifest.Entry, it item) bool {
	if it.info.Mode()&fs.ModeSymlink != 0 {
		return rec.Type != manifest.TypeLink || rec.Target != it.target
	}
	if rec.Type != manifest.TypeFile && rec.Type != manifest.TypeCloud {
		return true
	}
	return rec.Size != it.info.Size() || rec.MTime != it.info.ModTime().UnixNano()
}

type changeKind int

const (
	changed changeKind = iota
	added
	unreadableNow
)

var changeLabels = []struct {
	kind  changeKind
	label string
}{
	{changed, "changed"},
	{added, "new"},
	{unreadableNow, "couldn't be read"},
}

type changeGroup struct {
	critical bool
	counts   map[changeKind]int
	paths    []string
	extra    int
}

type changeTally struct {
	groups map[string]*changeGroup
	order  []string
}

func newChangeTally() *changeTally { return &changeTally{groups: map[string]*changeGroup{}} }

func (t *changeTally) add(rel string, k changeKind) {
	name := groupOf(rel, k == unreadableNow)
	g := t.groups[name]
	if g == nil {
		g = &changeGroup{counts: map[changeKind]int{}}
		t.groups[name] = g
		t.order = append(t.order, name)
	}
	g.critical = g.critical || isCritical(area(rel))
	g.counts[k]++
	if len(g.paths) < maxPathsPerGroup {
		g.paths = append(g.paths, rel)
	} else {
		g.extra++
	}
}

// checks has one per folder group, as verify does. Changes to files
// outside ~/Library are a warning; changes inside it are app state, which
// apps rewrite all the time.
func (t *changeTally) checks() []module.Check {
	out := make([]module.Check, 0, len(t.order))
	for _, name := range t.order {
		g := t.groups[name]
		var parts []string
		for _, l := range changeLabels {
			if n := g.counts[l.kind]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, l.label))
			}
		}
		if g.extra > 0 {
			parts = append(parts, fmt.Sprintf("%d more paths not listed", g.extra))
		}
		status := module.StatusInfo
		if g.critical {
			status = module.StatusWarn
		}
		out = append(out, module.Check{Module: Name, ID: Name + ":" + name, Status: status, Title: name, Detail: strings.Join(parts, ", "), Paths: g.paths})
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
gofmt -l .
go vet ./...
go vet -tags integration ./...
go test ./...
```
Expected: `gofmt -l .` prints nothing, and the rest pass.

- [ ] **Step 5: Commit**

End the message with your `Co-Authored-By:` trailer (Global Constraints).

```bash
git add internal/modules/files/retire_test.go internal/module/module.go internal/modules/files/retire.go
git commit -m "List what changed in the home folder since prepare

Anything changed on the old Mac after prepare may not be on the new one,
and once the old Mac is erased it is gone. files now walks the home
folder alongside its record, in the record's own order so memory stays
bounded, and lists changed and new files for mtm retire."
```

---

### Task 7: `mtm retire`

**Files:**
- Create: `cmd/mtm/retire.go`
- Create: `internal/app/retire.go`
- Create: `internal/report/retire.go`
- Modify: `IMPLEMENTATION_PLAN.md`
- Modify: `cmd/mtm/main.go`
- Modify: `cmd/mtm/nonetwork_integration_test.go`
- Modify: `docs/superpowers/specs/2026-11-02-mtm-design.md`
- Modify: `internal/modules/secrets/secrets.go`
- Modify: `internal/modules/system/system.go`
- Test: `internal/app/retire_test.go`, `internal/modules/system/system_test.go` (modify), `internal/modules/secrets/secrets_test.go` (modify), `internal/report/retire_test.go`, `cmd/mtm/retire_test.go`

**Interfaces:**
- Consumes:
  - `module.Retirer`, `module.Retirement`, and `files.Module.Retire` (Task 6);
  - `app.Steps`, `(*App).Retirement()`, `stepList`, `stepsCommand`, `walk`, `state.LoadFile`, `state.SaveFile`, and `report.RetireCommand` (Task 5);
  - `catalog.License.Deactivate` and `DeactivateNote` (Task 2);
  - `secrets` `recordedApps` and `usesSecureEnclave` (Task 4);
  - `loadRecord`, `exportsFor`, `moduleFailure`, `preflight.Gather`, and `writeCheck` and `writeProblems` in `report` (earlier stages).
- Produces:
  - `app.ErrRetireOnNewMac`.
  - `app.RetireReport{PreparedAt time.Time; Changes []module.Check; Problems []module.Finding; Steps checklist.List}`.
  - `(*App).Retire(ctx) (RetireReport, error)`.
  - `system.Module` and `secrets.Module` implement `module.Retirer`:
    - `system` gives a step `system:deactivate:<catalog id>` for each recorded app whose license is marked `deactivate`;
    - `secrets` gives `secrets:retire-secure-enclave` when the old Mac used Secure Enclave keys.
  - `retire`'s own steps follow the modules': `retire:icloud`, `retire:devices`, and `retire:erase`.
  - `report.Retire(w, preparedAt, changes, problems, details)`.
  - `mtm retire [--details] [--json]` and `mtm retire list|done|skip|open`.

**How `mtm retire` behaves:**
- It runs only on the Mac that ran `prepare`, compared by machine id. Elsewhere it exits 1 with `ErrRetireOnNewMac`, as `prepare --undo` does.
- It prints what changed, then walks through the steps in a terminal, or lists them elsewhere.
- It exits 1 when a module failed or a file outside `~/Library` changed, so a script notices. Changes inside `~/Library` alone exit 0.
- Each run adds the steps again, keeping what the user did with those already there.

- [ ] **Step 1: Write the failing tests**

`TestRetireListsAFileChangedAfterPrepare` is the success criterion in `IMPLEMENTATION_PLAN.md`. It prepares a temporary home folder, edits a document, and runs retire.

`internal/app/retire_test.go`:
```go
package app

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

func retireOn(t *testing.T, a *App) RetireReport {
	t.Helper()
	rep, err := a.Retire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func stepIDs(l checklist.List) []string {
	var ids []string
	for _, e := range l.Entries {
		ids = append(ids, e.ID)
	}
	return ids
}

func TestRetireListsAFileChangedAfterPrepare(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	testutil.WriteFile(t, filepath.Join(old, "Documents/notes.md"), "notes, edited after prepare\n")
	rep := retireOn(t, newTestApp(old, fakeMac(old, oldUUID)))
	if len(rep.Changes) != 1 || rep.Changes[0].ID != "files:Documents" || !slices.Equal(rep.Changes[0].Paths, []string{"Documents/notes.md"}) {
		t.Errorf("changes = %+v, want Documents/notes.md", rep.Changes)
	}
	if want := []string{"retire:icloud", "retire:devices", "retire:erase"}; !slices.Equal(stepIDs(rep.Steps), want) {
		t.Errorf("steps = %q, want %q", stepIDs(rep.Steps), want)
	}
}

func TestRetireRefusesOnTheNewMac(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	newHome := migrate(t, old)
	if _, err := newTestApp(newHome, fakeMac(newHome, newUUID)).Retire(ctx); !errors.Is(err, ErrRetireOnNewMac) {
		t.Errorf("err = %v, want ErrRetireOnNewMac", err)
	}
}

func TestRetireKeepsWhatTheUserDidAndStaysOutOfTheChecklist(t *testing.T) {
	old := seedHome(t)
	prepareOld(t, old)
	a := newTestApp(old, fakeMac(old, oldUUID))
	retireOn(t, a)
	if err := a.Retirement().Skip("retire:devices", "it was never in my account"); err != nil {
		t.Fatal(err)
	}
	rep := retireOn(t, a)
	if e, _ := rep.Steps.Find("retire:devices"); e.Status != checklist.Skipped {
		t.Errorf("devices step = %+v, want it still skipped", e)
	}
	if l, err := a.Checklist().List(); err != nil || len(l.Entries) != 0 {
		t.Errorf("checklist = %+v, %v; want no retirement steps in it", l, err)
	}
}

// licenseRetirer leaves a step for mtm retire, as system does for a license.
type licenseRetirer struct{ finishOnly }

func (licenseRetirer) Retire(module.Env, *stage.Stage) (module.Retirement, error) {
	return module.Retirement{Steps: []checklist.Item{{ID: "lic:deactivate", Module: "lic", Title: "Deactivate the license"}}}, nil
}

// brokenRetire can't look, as when its part of the record is damaged.
type brokenRetire struct{ finishOnly }

func (brokenRetire) Retire(module.Env, *stage.Stage) (module.Retirement, error) {
	return module.Retirement{}, errors.New("record is damaged")
}

func TestRetirePutsModuleStepsFirstAndReportsAModuleThatFails(t *testing.T) {
	old := t.TempDir()
	mods := []module.Module{brokenRetire{finishOnly{name: "broken"}}, licenseRetirer{finishOnly{name: "lic"}}}
	mustPrepare(t, stepApp(old, oldUUID, mods...))
	rep := retireOn(t, stepApp(old, oldUUID, mods...))
	if len(rep.Problems) != 1 || rep.Problems[0].ID != "broken:error" || rep.Problems[0].Title != "broken: retire failed" {
		t.Errorf("problems = %+v, want broken's failure", rep.Problems)
	}
	if want := []string{"lic:deactivate", "retire:icloud", "retire:devices", "retire:erase"}; !slices.Equal(stepIDs(rep.Steps), want) {
		t.Errorf("steps = %q, want %q", stepIDs(rep.Steps), want)
	}
}
```

`internal/modules/system/system_test.go`:
```diff
--- a/internal/modules/system/system_test.go
+++ b/internal/modules/system/system_test.go
@@ -350,3 +350,27 @@ func TestStepsAreTheCatalogsForRecordedApps(t *testing.T) {
 		t.Errorf("steps = %+v\nwant %+v", items, want)
 	}
 }
+
+func TestRetireListsTheLicensesToDeactivate(t *testing.T) {
+	f := newFixture(t)
+	f.addApp(t, "CleanShot X.app", "pl.maketheweb.cleanshotx", "4.8", "CleanShot X", "arm64")
+	f.addApp(t, "Dash.app", "com.kapeli.dashdoc", "7.3", "Dash", "arm64")
+	m, st, _ := f.prepare(t)
+	c, err := catalog.Load(fstest.MapFS{
+		"cleanshot-x.toml": {Data: []byte("id = \"cleanshot-x\"\nname = \"CleanShot X\"\nbundle_id = \"pl.maketheweb.cleanshotx\"\n\n[license]\ndeactivate = true\ndeactivate_note = \"It counts Macs.\"\n")},
+		"dash.toml":        {Data: []byte("id = \"dash\"\nname = \"Dash\"\nbundle_id = \"com.kapeli.dashdoc\"\n\n[license]\nreactivate = true\n")},
+	})
+	if err != nil {
+		t.Fatal(err)
+	}
+	mod := f.module()
+	mod.Catalog = c
+	r, err := mod.Retire(f.env(m), st)
+	if err != nil {
+		t.Fatal(err)
+	}
+	want := []checklist.Item{{ID: "system:deactivate:cleanshot-x", Module: Name, Title: "Deactivate the CleanShot X license on this Mac", Detail: "It counts Macs.", Where: "CleanShot X"}}
+	if !slices.Equal(r.Steps, want) || len(r.Changes) != 0 {
+		t.Errorf("retirement = %+v\nwant steps %+v", r, want)
+	}
+}
```

`internal/modules/secrets/secrets_test.go`:
```diff
--- a/internal/modules/secrets/secrets_test.go
+++ b/internal/modules/secrets/secrets_test.go
@@ -383,3 +383,20 @@ func TestConfigLineForms(t *testing.T) {
 		}
 	}
 }
+
+func TestRetireAsksToRemoveSecureEnclaveKeysOnlyWhenThereAreSome(t *testing.T) {
+	f := newFixture(t)
+	_, m := f.prepare(t)
+	if r, err := f.mod.Retire(f.moduleEnv(m), nil); err != nil || len(r.Steps) != 0 {
+		t.Errorf("retirement = %+v, %v; want no steps without Secure Enclave keys", r, err)
+	}
+	testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "id_ecdsa_sk_rk.pub"), pubSK)
+	_, m = f.prepare(t)
+	r, err := f.mod.Retire(f.moduleEnv(m), nil)
+	if err != nil {
+		t.Fatal(err)
+	}
+	if got := stepIDs(r.Steps); !slices.Equal(got, []string{"secrets:retire-secure-enclave"}) {
+		t.Errorf("steps = %q, want the Secure Enclave key removal", got)
+	}
+}
```

`internal/report/retire_test.go`:
```go
package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/module"
)

func TestRetireListsChangesGroupedByFolder(t *testing.T) {
	var out bytes.Buffer
	changes := []module.Check{{Module: "files", ID: "files:Documents", Status: module.StatusWarn, Title: "Documents", Detail: "1 changed, 1 new",
		Paths: []string{"Documents/a.md", "Documents/notes.md"}}}
	Retire(&out, time.Now(), changes, nil, false)
	for _, want := range []string{"SAFE TO WIPE on the new Mac", "Changed on this Mac after mtm prepare", "! Documents  (1 changed, 1 new)", "Documents/notes.md", "Copy what the new Mac still needs"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRetireWithNothingChanged(t *testing.T) {
	var out bytes.Buffer
	Retire(&out, time.Now(), nil, nil, false)
	if !strings.Contains(out.String(), "Nothing in your home folder changed after mtm prepare") {
		t.Errorf("output:\n%s", out.String())
	}
}
```

`cmd/mtm/retire_test.go`:
```go
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/testutil"
)

func TestRetireWithoutATerminalListsChangesAndSteps(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	testutil.WriteFile(t, filepath.Join(os.Getenv("HOME"), "Documents/notes.md"), "notes, edited after prepare\n")
	notTerminal(t)
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"retire"}, &out, &errOut, macApp("OLD-MAC"))
	for _, want := range []string{"Documents/notes.md", "3 steps open", "mtm retire done 'retire:erase'"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s%s", want, out.String(), errOut.String())
		}
	}
	if code != exitProblems {
		t.Errorf("exit %d, want %d for a changed document", code, exitProblems)
	}
}

func TestRetireWithNothingChangedExitsZero(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	notTerminal(t)
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"retire"}, &out, &errOut, macApp("OLD-MAC")); code != exitOK || !strings.Contains(out.String(), "Nothing in your home folder changed") {
		t.Errorf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
}

func TestRetireOnTheNewMacRefuses(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"retire"}, &out, &errOut, macApp("NEW-MAC")); code != exitProblems || !strings.Contains(errOut.String(), "run mtm retire on the old Mac") {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}

func TestRetireStepsCanBeClosedOneAtATime(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	notTerminal(t)
	var out, errOut bytes.Buffer
	run(ctx, []string{"retire"}, &out, &errOut, macApp("OLD-MAC"))
	if code := run(ctx, []string{"retire", "done", "retire:icloud"}, &out, &errOut, macApp("OLD-MAC")); code != exitOK {
		t.Fatalf("done exit %d: %s", code, errOut.String())
	}
	out.Reset()
	if code := run(ctx, []string{"retire", "list"}, &out, &errOut, macApp("OLD-MAC")); code != exitOK || !strings.Contains(out.String(), "2 steps open") {
		t.Errorf("list exit %d:\n%s", code, out.String())
	}
}

func TestRetireWalksTheStepsInATerminal(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	asTerminal(t, "d\nq\n")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"retire"}, &out, &errOut, macApp("OLD-MAC")); code != exitOK {
		t.Fatalf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "[1/3] Sign out of iCloud on this Mac") || !strings.Contains(out.String(), "2 steps open") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestRetireJSON(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"retire", "--json"}, &out, &errOut, macApp("OLD-MAC")); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var rep struct {
		Steps struct {
			Entries []struct {
				ID string `json:"id"`
			} `json:"entries"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil || len(rep.Steps.Entries) != 3 {
		t.Errorf("JSON = %s, %v; want 3 steps", out.String(), err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ ./internal/modules/system/ ./internal/modules/secrets/ ./internal/report/ ./cmd/mtm/`
Expected: FAIL. The new names don't exist yet, and `mtm` doesn't know `retire`:
```text
FAIL	github.com/momentmaker/mtm/internal/app [build failed]
FAIL	github.com/momentmaker/mtm/internal/modules/system [build failed]
FAIL	github.com/momentmaker/mtm/internal/modules/secrets [build failed]
FAIL	github.com/momentmaker/mtm/internal/report [build failed]
--- FAIL: TestRetireWithoutATerminalListsChangesAndSteps (0.17s)
    retire_test.go:22: output missing "Documents/notes.md":
        mtm: unknown command "retire"
        
```

- [ ] **Step 3: Implement**

`internal/app/retire.go`:
```go
package app

import (
	"context"
	"errors"
	"time"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/preflight"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
)

var ErrRetireOnNewMac = errors.New("~/.mtm holds the record from another Mac; run mtm retire on the old Mac, the one that ran prepare")

// RetireReport is what retire found on the old Mac: what changed since
// prepare, modules that failed to look, and the retirement steps with what
// the user did with each so far.
type RetireReport struct {
	PreparedAt time.Time        `json:"prepared_at"`
	Changes    []module.Check   `json:"changes"`
	Problems   []module.Finding `json:"problems,omitempty"`
	Steps      checklist.List   `json:"steps"`
}

// retireSteps follow the modules' steps, which need the Mac still signed
// in. Signing out of iCloud turns off Find My, and the erase comes last.
var retireSteps = []checklist.Item{
	{ID: "retire:icloud", Module: "retire", Where: "System Settings > your name > Sign Out",
		Title:  "Sign out of iCloud on this Mac",
		Detail: "Signing out turns off Find My, which removes Activation Lock, so the next owner can set the Mac up."},
	{ID: "retire:devices", Module: "retire", Where: "On the new Mac: System Settings > your name, then this Mac in the device list",
		Title: "Remove this Mac from your Apple Account's devices"},
	{ID: "retire:erase", Module: "retire", Where: "System Settings > General > Transfer or Reset > Erase All Content and Settings",
		Title:  "Erase this Mac with Erase All Content and Settings",
		Detail: "It destroys the disk's encryption key, so nothing on it can be recovered. mtm never erases anything itself."},
}

// Retire lists what changed on the old Mac since prepare and adds the
// retirement steps to retire.json, keeping what the user did with steps
// already there. It runs only on the Mac that ran prepare.
func (a *App) Retire(ctx context.Context) (RetireReport, error) {
	st, err := stage.Existing(a.Home)
	if err != nil {
		return RetireReport{}, err
	}
	logFile, run, err := a.openLog(st, "retire")
	if err != nil {
		return RetireReport{}, err
	}
	defer logFile.Close()
	m, err := loadRecord(st)
	if err != nil {
		return RetireReport{}, err
	}
	info, err := preflight.Gather(ctx, run)
	if err != nil {
		return RetireReport{}, err
	}
	if info.MachineID != m.MachineID {
		return RetireReport{}, ErrRetireOnNewMac
	}
	env := a.env(ctx, module.OldMac, false, m, run, exportsFor(st, m))
	rep := RetireReport{PreparedAt: m.PreparedAt}
	var items []checklist.Item
	for _, mod := range a.Modules {
		r, ok := mod.(module.Retirer)
		if !ok {
			continue
		}
		if _, recorded := m.Modules[mod.Name()]; !recorded {
			continue
		}
		got, err := r.Retire(env, st)
		if err != nil {
			rep.Problems = append(rep.Problems, moduleFailure(mod.Name(), "retire", err))
			continue
		}
		rep.Changes = append(rep.Changes, got.Changes...)
		items = append(items, got.Steps...)
	}
	s, err := state.LoadFile(st, state.RetireFileName)
	if err != nil {
		return rep, err
	}
	s.Checklist.Add(append(items, retireSteps...)...)
	if err := state.SaveFile(st, state.RetireFileName, s); err != nil {
		return rep, err
	}
	rep.Steps = s.Checklist
	return rep, nil
}
```

`internal/modules/system/system.go`:
```diff
--- a/internal/modules/system/system.go
+++ b/internal/modules/system/system.go
@@ -260,6 +260,28 @@ func (m Module) appSteps(installed []apps.App) []checklist.Item {
 	return items
 }
 
+// Retire lists, for mtm retire on the old Mac, the licenses to release
+// before it is erased: those of recorded apps whose vendor counts Macs.
+func (m Module) Retire(env module.Env, _ *stage.Stage) (module.Retirement, error) {
+	var old Inventory
+	if _, err := module.LoadInventory(env, Name, &old); err != nil {
+		return module.Retirement{}, err
+	}
+	if m.Catalog == nil {
+		return module.Retirement{}, nil
+	}
+	var items []checklist.Item
+	for _, a := range old.Apps {
+		entry, ok := m.Catalog.ByBundleID(a.BundleID)
+		if !ok || !entry.License.Deactivate {
+			continue
+		}
+		items = append(items, checklist.Item{ID: Name + ":deactivate:" + entry.ID, Module: Name, Where: entry.Name,
+			Title: "Deactivate the " + entry.Name + " license on this Mac", Detail: entry.License.DeactivateNote})
+	}
+	return module.Retirement{Steps: items}, nil
+}
+
 // restoreEtc copies an exported /etc file back as root-owned and readable
 // by all: the export is mtm's private 0600 copy, and cp would keep that
 // mode, leaving a file such as /etc/hosts unreadable to everyone else.
```

`internal/modules/secrets/secrets.go`:
```diff
--- a/internal/modules/secrets/secrets.go
+++ b/internal/modules/secrets/secrets.go
@@ -254,6 +254,25 @@ func (m Module) Steps(env module.Env) ([]checklist.Item, error) {
 	return items, nil
 }
 
+// Retire asks, for mtm retire on the old Mac, for its Secure Enclave keys
+// to be removed from the services that trust them. They die with this Mac,
+// but stay trusted until removed.
+func (m Module) Retire(env module.Env, _ *stage.Stage) (module.Retirement, error) {
+	var inv Inventory
+	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
+		return module.Retirement{}, err
+	}
+	apps, err := m.recordedApps(env)
+	if err != nil || !usesSecureEnclave(inv, apps) {
+		return module.Retirement{}, err
+	}
+	return module.Retirement{Steps: []checklist.Item{{
+		ID: Name + ":retire-secure-enclave", Module: Name, Where: "GitHub > Settings > SSH and GPG keys, and each server's ~/.ssh/authorized_keys",
+		Title:  "Remove this Mac's Secure Enclave SSH keys from GitHub and your servers",
+		Detail: "Do it once the new Mac's keys work. Remove any passkey you made only on this Mac the same way, in each site's security settings.",
+	}}}, nil
+}
+
 func hardwareKeys(keys []SSHKey) string {
 	var names []string
 	for _, k := range keys {
```

`internal/report/retire.go`:
```go
package report

import (
	"fmt"
	"io"
	"time"

	"github.com/momentmaker/mtm/internal/module"
)

// Retire prints what changed on the old Mac since prepare. It's a list for
// the user to act on, not a verdict: Migration Assistant may have copied a
// change already, and only the user knows which ones matter.
func Retire(w io.Writer, preparedAt time.Time, changes []module.Check, problems []module.Finding, details bool) {
	fmt.Fprintln(w, "Do these only after mtm verify says SAFE TO WIPE on the new Mac.")
	fmt.Fprintln(w)
	writeProblems(w, "Problems", problems)
	since := preparedAt.Local().Format(time.RFC1123)
	if len(changes) == 0 {
		fmt.Fprintf(w, "Nothing in your home folder changed after mtm prepare (%s).\n\n", since)
		return
	}
	fmt.Fprintf(w, "Changed on this Mac after mtm prepare (%s)\n", since)
	for _, c := range changes {
		writeCheck(w, c, details)
	}
	fmt.Fprintln(w, "Copy what the new Mac still needs before you erase this one.")
	fmt.Fprintln(w)
}
```

`cmd/mtm/retire.go`:
```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/report"
)

// cmdRetire lists what changed on the old Mac since prepare, then walks
// through the retirement steps in a terminal, or lists them elsewhere.
// list, done, skip, and open work on those steps as they do for mtm
// checklist.
func cmdRetire(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	steps := stepList{report.RetireCommand, a.Retirement()}
	if len(args) > 0 {
		switch args[0] {
		case "list", "done", "skip", "open":
			return stepsCommand(ctx, steps, args, stdout, stderr)
		}
	}
	flags := flag.NewFlagSet("retire", flag.ContinueOnError)
	flags.SetOutput(stderr)
	details := flags.Bool("details", false, "list every changed path")
	asJSON := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "mtm retire: unknown subcommand %q\n\n%s", flags.Arg(0), usage)
		return exitFailed
	}
	rep, err := a.Retire(ctx)
	switch {
	case errors.Is(err, app.ErrRetireOnNewMac):
		fmt.Fprintln(stderr, "mtm retire:", err)
		return exitProblems
	case err != nil:
		fmt.Fprintln(stderr, "mtm retire:", err)
		return exitFailed
	}
	code := exitOK
	if needsAttention(rep) {
		code = exitProblems
	}
	if *asJSON {
		if err := report.JSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, "mtm retire:", err)
			return exitFailed
		}
		return code
	}
	report.Retire(stdout, rep.PreparedAt, rep.Changes, rep.Problems, *details)
	if interactive() {
		if walked := walk(ctx, steps, stdin, stdout, stderr); walked != exitOK {
			return walked
		}
		return code
	}
	report.Checklist(stdout, rep.Steps, false, report.RetireCommand)
	return code
}

// needsAttention is true when a module failed or a file outside ~/Library
// changed after prepare. App state inside ~/Library changes all the time,
// so it alone doesn't.
func needsAttention(rep app.RetireReport) bool {
	if len(rep.Problems) > 0 {
		return true
	}
	for _, c := range rep.Changes {
		if c.Status == module.StatusWarn || c.Status == module.StatusFail {
			return true
		}
	}
	return false
}
```

`cmd/mtm/main.go`:
```diff
--- a/cmd/mtm/main.go
+++ b/cmd/mtm/main.go
@@ -54,6 +54,9 @@ Usage:
   mtm checklist list [--all] [--json] | done <id>... | skip <id> --reason "..." | open <id>
   mtm verify [--only a,b] [--deep] [--details] [--json]
                                             check the new Mac against the record
+  mtm retire [--details] [--json]           on the old Mac, after SAFE TO WIPE: what changed since
+                                            prepare, then the steps before you erase it
+  mtm retire list [--all] [--json] | done <id>... | skip <id> --reason "..." | open <id>
   mtm cleanup [--force]                     delete ~/.mtm, and the --stage exports, after SAFE TO WIPE
   mtm version
 `
@@ -66,6 +69,7 @@ var commands = map[string]command{
 	"finish":    cmdFinish,
 	"checklist": cmdChecklist,
 	"verify":    cmdVerify,
+	"retire":    cmdRetire,
 	"cleanup":   cmdCleanup,
 }
 
```

`cmd/mtm/nonetwork_integration_test.go`:
```diff
--- a/cmd/mtm/nonetwork_integration_test.go
+++ b/cmd/mtm/nonetwork_integration_test.go
@@ -146,6 +146,7 @@ func TestNoNetwork(t *testing.T) {
 		{[]string{"prepare", "--undo"}, []int{0, 1}, false, false},
 		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains,permissions,browsers,secrets"}, []int{0, 1}, true, true},
 		{[]string{"verify"}, []int{1}, false, false},
+		{[]string{"retire"}, []int{0, 1}, false, false},
 	}
 	for _, s := range steps {
 		name := "mtm " + strings.Join(s.args, " ")
```

- [ ] **Step 4: Update the spec and the plan file**

`docs/superpowers/specs/2026-11-02-mtm-design.md`:
````diff
--- a/docs/superpowers/specs/2026-11-02-mtm-design.md
+++ b/docs/superpowers/specs/2026-11-02-mtm-design.md
@@ -81,7 +81,7 @@ Apple publishes no list of what Migration Assistant skips. The findings below co
 | 4 | New Mac | `mtm finish` | `mtm` is already present because Migration Assistant copies `/opt/homebrew`. Restores Docker data only if Migration Assistant didn't bring it intact, restarts services, re-registers launch agents, then starts the checklist. |
 | 5 | New Mac | `mtm checklist` | Walks through the manual steps, when run in a terminal. Each opens the right System Settings pane and waits for done, for a skip with a reason, or for later. Elsewhere it lists the open steps. `list [--all] [--json]`, `done <id>…`, `skip <id> --reason "…"`, and `open <id>` do the same one step at a time. |
 | 6 | New Mac | `mtm verify` | Compares the new Mac with the record. Prints **SAFE TO WIPE** or **NOT SAFE TO WIPE** with the reasons. Can be run any number of times. |
-| 7 | Old Mac | `mtm retire` | Lists anything on the old Mac that changed after `prepare`. Then walks through deactivating licenses, signing out of iCloud and turning off Find My, removing old Secure Enclave keys from services, and "Erase All Content and Settings". `mtm` never erases anything itself. |
+| 7 | Old Mac | `mtm retire` | After SAFE TO WIPE: lists anything on the old Mac that changed after `prepare`. Then walks through deactivating licenses, signing out of iCloud and turning off Find My, removing old Secure Enclave keys from services, and "Erase All Content and Settings", in a terminal; elsewhere it lists them. `list`, `done`, `skip`, and `open` work on these steps as they do for `mtm checklist`. `mtm` never erases anything itself. |
 | 8 | New Mac | `mtm cleanup` | After SAFE TO WIPE, deletes the exports and the record, including a `prepare --stage` folder when its drive is attached. Reminds the user to revoke the terminal's Full Disk Access. Refuses before SAFE TO WIPE unless `--force` is passed. |
 
 Every command can be re-run. `scan` and `verify` accept `--json`. `prepare`, `finish`, and `verify` accept `--only <module>`.
@@ -127,6 +127,8 @@ The checklist covers:
 - removing the old Mac's Secure Enclave SSH keys and passkeys from GitHub and servers once their replacements work;
 - "Erase All Content and Settings", which destroys the disk encryption key.
 
+The steps live in `~/.mtm/retire.json`, apart from the new Mac's `state.json`, so a step closed on the old Mac never counts toward the new Mac's verdict, even if Migration Assistant runs again. `retire` runs only on the Mac that ran `prepare`. It exits `1` when a module fails or a file outside `~/Library` changed; app state inside `~/Library` changes all the time, so it is listed as info.
+
 ### 6.6 Supply chain
 - Go with the standard library. The only third-party dependency is `github.com/BurntSushi/toml` (no transitive dependencies) for the catalog.
 - Dependencies are pinned in `go.sum`.
@@ -171,6 +173,7 @@ type Module interface {
 }
 ```
 - `Env` carries the runner, home directory, and the current mode (old Mac or new Mac). Logging wraps the runner. Modules that need the catalog get it through their constructors (`files.New`, `system.New`, `permissions.New`). `Env.Manifest` is the record: on the new Mac it is what `verify` and `finish` check against; during `prepare` it holds this Mac's previous record, if any, so a re-run can carry forward what the first run changed (for example, services it stopped).
+- Modules with something for `mtm retire` implement `Retire(env, st) (Retirement, error)` (`Retirer`): `Changes`, checks listing what changed on the old Mac since `prepare`, and `Steps`, checklist items for retiring it. `retire` puts the modules' steps before its own: signing out of iCloud, removing the Mac from the account's devices, and the erase.
 - Modules whose `prepare` changes the old Mac also implement `Undo(env, st) (Result, error)`, which `prepare --undo` calls. After a successful undo, that module's record is removed, so `verify` reports it not prepared until `prepare` runs again.
 - `Finding` has: module, id, severity (`info`, `warn`, `blocker`), handler (`migration-assistant`, `mtm`, `manual`), title, detail, fix.
 - `Result` has: what was done, artifacts written (each with its fingerprint), and problems.
@@ -225,6 +228,7 @@ volatile = ["~/Library/Application Support/com.raycast.macos/*.log"]
     postgres/        one <cluster>.sql.gz per running cluster (pg_dumpall output)
   docker-finish.json new Mac only: volumes finish found intact or restored
   state.json         new Mac only: checklist steps with their status and skip reasons, last verify result
+  retire.json        old Mac only: the retirement steps, with their status and skip reasons
   logs/              one log file per command run, redacted
 ```
 All paths in the record are relative to the home folder, so verification works if the username differs on the new Mac.
@@ -274,6 +278,7 @@ Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0`
 ### 9.1 `files`
 - `prepare` walks the home folder and writes `files.jsonl.gz` (section 8).
 - `verify` walks the same roots on the new Mac and applies section 8.4.
+- `retire` walks the old Mac's home folder again alongside the record, which is in the same walk order, so memory stays bounded. It lists files that differ from the record in type, size, time, or link target, and files the record doesn't have, even with an old time, as a copy from a drive keeps. Groups are those of section 8.4. Changes outside `~/Library` are warnings, those inside it info. Deleted files, folders, cloud-only files, and volatile paths aren't listed; a folder that can no longer be read is.
 - Fingerprinting streams with a bounded worker pool.
 
 ### 9.2 `git`
@@ -450,6 +455,7 @@ Identities are recorded, never key material.
   - sign in to each password manager, and turn on its SSH agent if SSH used it;
   - create new Secure Enclave keys and add them to GitHub and servers, when the old Mac used them;
   - enroll Touch ID fingerprints, unless the old Mac had none.
+- **Retirement step** (`mtm retire`): when the old Mac used Secure Enclave keys, remove them, and any passkey made only on that Mac, from GitHub and servers once the new Mac's keys work.
 - **`verify`:**
   - SSH keys and GPG secret keys match the record (critical). A missing key, or a public key with a different fingerprint, fails.
   - Reports name SSH keys by file and GPG keys by algorithm and date, never by fingerprint.
@@ -464,6 +470,7 @@ Identities are recorded, never key material.
   - app inventory of `/Applications` and `~/Applications`, including one level of subfolders, skipping Apple's own `com.apple.*` apps and symlinks. Each app records its bundle id, version (`plutil -extract … raw`), and architectures (`lipo -archs`). Home apps are recorded as `~/Applications/…`.
 - **`scan` and `prepare`:** warn about Intel-only apps. They need Rosetta on the new Mac, and Rosetta is limited after macOS 27.
 - **Checklist steps** (section 7.3): from the catalog, for the recorded apps: one to reactivate each license marked `reactivate`, and one per note.
+- **Retirement steps** (`mtm retire`): one to deactivate each license marked `deactivate`, for apps whose vendor counts Macs.
 - **`finish`:**
   - Prints `sudo softwareupdate --install-rosetta --agree-to-license` if Rosetta is needed and missing.
   - For each `/etc` file that differs, prints `sudo install -m 0644 -o root -g wheel <copy> /etc/<file>`. `install` rather than `cp`, because the private copy is 0600 and `cp` would give a new `/etc` file that mode.
@@ -500,7 +507,7 @@ Identities are recorded, never key material.
 - **Unit tests per module.** A fake `runner` replays fixtures of real command output captured on macOS 26. The author's M3 is the first source. No unit test executes real system commands.
 - **Table-driven tests** for the file classification in section 8.4 and the verdict rules in section 8.5.
 - **Integration tests on a temporary home folder** (`HOME` overridden) with real files and real git repos created in a temp directory. They cover unpushed commits, stashes, untracked `.env` files, cloud-only files (simulated with flags where the test can set them), modes, and symlinks.
-- **No-network test** behind the `integration` build tag: runs `mtm scan`, `prepare`, and `verify` on a temporary home folder under a `sandbox-exec` profile that denies network access, including every `brew` and `git` command they invoke, and asserts they succeed.
+- **No-network test** behind the `integration` build tag: runs `mtm scan`, `prepare`, `verify`, and `retire` on a temporary home folder under a `sandbox-exec` profile that denies network access, including every `brew` and `git` command they invoke, and asserts they succeed.
 - **Docker integration tests** behind the `integration` build tag, run against a real Docker Desktop. They cover:
   - create a volume and an image, export, remove, and restore;
   - compare content digests, including owners and modes;
````

`IMPLEMENTATION_PLAN.md`:
```diff
--- a/IMPLEMENTATION_PLAN.md
+++ b/IMPLEMENTATION_PLAN.md
@@ -141,7 +141,22 @@ Found in the final review.
 **Success Criteria**: `retire` lists a file changed on the old Mac after `prepare`. SSH and GPG identities are critical checks.
 **Tests**: module unit tests with fixtures; a `retire` integration test on a temp home folder.
 **Plan**: `docs/superpowers/plans/2026-11-03-mtm-stage4-part2-browsers-secrets-retire.md`
-**Status**: In Progress
+**Status**: Complete
+
+#### Known gaps carried out of Stage 4, Part 2
+Found while planning.
+- **Real-run validation pending:**
+  - Arc's Safe Storage item name, and the Bitwarden and Secretive agent sockets in the catalog, haven't been checked against real installs;
+  - Firefox is tested only with fixture files;
+  - the gpg fixtures were written from GnuPG's documented colon format: a scratch GnuPG folder's path was too long for gpg-agent's socket.
+- **Safe Storage is checked for presence only.** A browser that made a new key on the new Mac passes, though the old Mac's passwords and cookies can't be decrypted with it. Comparing the keys would mean reading them.
+- **SSH agents:** `Include` files in `~/.ssh/config` aren't followed, and only the terminal's `SSH_AUTH_SOCK` is seen.
+- **An `sk-` key can be on a security key, which moves, or in the Secure Enclave, which doesn't.** mtm can't tell which, so its step covers both.
+- **retire:**
+  - it lists only home-folder changes, not data elsewhere, such as Postgres in `/opt/homebrew/var` or Docker volumes, if services or containers ran again after `prepare`;
+  - it can't tell a change Migration Assistant already copied from one made after it;
+  - a step a newer catalog adds lands after the erase step.
+- **Touch ID's count** comes from `bioutil`'s text output. If the format changes, the count is unknown and the step is listed anyway.
 
 ## Stage 5: Ship
 **Goal**: GitHub Actions CI on Apple Silicon (`go test`, `go vet`, `gofmt`, `govulncheck`, catalog validation); end-to-end tests in Tart VMs (cloned VM as a perfect migration, injected faults); release builds with checksums and provenance attestations; a Homebrew tap; README and contributor guide (including how to add a catalog app).
```

- [ ] **Step 5: Run the tests to verify they pass**

Run:
```bash
gofmt -l .
go vet ./...
go vet -tags integration ./...
go test ./...
```
Expected: `gofmt -l .` prints nothing, and the rest pass.

Then run the no-network test, which now runs `mtm retire` too:
```bash
go test -tags integration -run TestNoNetwork -v ./cmd/mtm/
```
Expected: PASS, or SKIP with its reason.

- [ ] **Step 6: Commit**

End the message with your `Co-Authored-By:` trailer (Global Constraints).

```bash
git add internal/app/retire_test.go internal/modules/system/system_test.go internal/modules/secrets/secrets_test.go internal/report/retire_test.go cmd/mtm/retire_test.go internal/app/retire.go internal/modules/system/system.go internal/modules/secrets/secrets.go internal/report/retire.go cmd/mtm/retire.go cmd/mtm/main.go cmd/mtm/nonetwork_integration_test.go docs/superpowers/specs/2026-11-02-mtm-design.md IMPLEMENTATION_PLAN.md
git commit -m "Add mtm retire

After SAFE TO WIPE, the old Mac still holds whatever changed on it after
prepare, and it still has an iCloud account, Find My, licenses, and
Secure Enclave keys that services trust. retire lists the changes, then
walks through releasing those and erasing the Mac, keeping its steps in
retire.json so none can count toward the new Mac's verdict."
```
