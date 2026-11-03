# mtm Stage 5, Part 1: CI, Release Pipeline, and Docs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the public `momentmaker/mtm` repository shippable, without shipping anything yet:
- a license and the docs a user, a contributor, and a security reporter need;
- CI that runs the gate on Apple Silicon;
- a release pipeline whose snapshot runs in CI on every push.

**Architecture:**
- **Docs:** four files at the repository root, with a Go test that keeps the README's usage text the same as `mtm help`.
- **CI:** two GitHub Actions jobs on `macos-26`:
  - `test` runs the gate and govulncheck;
  - `release-dry-run` builds a GoReleaser snapshot and checks the archive, the binary's version, and the formula.
- **Releases:** a release workflow on `v*` tags runs GoReleaser for `darwin/arm64`. It publishes the archive and `checksums.txt`, attests the archives' build provenance, and writes the formula to `momentmaker/homebrew-tap`. This follows the author's kaijutsu project.
- **Not in this part:** nothing is tagged, and no secret is set. The first release, `v0.1.0`, and the Tart end-to-end suite come in Part 2, after the author's own migration.

**Tech Stack:**
- Go 1.26.
- GitHub Actions (`macos-26`), with `actions/checkout` v7.0.1, `actions/setup-go` v7.0.0, `goreleaser/goreleaser-action` v7.3.0, and `actions/attest-build-provenance` v4.2.2, each pinned by commit.
- GoReleaser v2.19.0, and `govulncheck` v1.9.0.

**Spec:** `docs/superpowers/specs/2026-11-02-mtm-design.md`. This part covers §6.6 (supply chain), §11 (CI), and §12.1 (notarization), which Task 2 updates.

**Decisions the user made for this part (2026-11-03):**
- The repository is public `momentmaker/mtm`; it was created and `main` was pushed before planning.
- The license is MIT.
- Releases use GoReleaser, as kaijutsu does, publishing to `momentmaker/homebrew-tap`.
- No tag until after the author's own migration.
- The Tart suite is planned and run on the new Mac, since this one has about 15 GB free.

**How this plan was checked:**
- Both tasks were written and run in a scratch copy of the repository, each as its own commit.
- `gofmt -l .`, `go vet ./...`, `go vet -tags integration ./...`, and `go test ./...` are clean after each.
- Task 1's red step was captured with its test on the previous code.
- Task 2's CI and release files couldn't be run on the author's Mac, so their first run is the branch's first push (Task 2, Step 7). Its notes list what was checked.

**Diffs:** a modified file's change is a unified diff against the file as the previous task left it. Apply it with `git apply`, or by hand. A new file is given in full.

## Global Constraints

- **Module:**
  - module path `github.com/momentmaker/mtm`, `go 1.26`;
  - the only third-party Go dependency is `github.com/BurntSushi/toml` v1.6.0 (spec 6.6).
  - This part adds no Go dependency. GoReleaser and govulncheck run in CI only.
- **Supply chain (spec 6.6, as Task 2 updates it):**
  - Actions are pinned to full commit hashes, and tools to exact versions.
  - Releases are built for `darwin/arm64` only, with `-trimpath`, `CGO_ENABLED=0`, and commit-time timestamps. They ship SHA-256 checksums and build provenance attestations.
  - The formula requires Apple Silicon and macOS 26. A formula, not a cask: mtm never removes Gatekeeper's quarantine flag.
- **Workflow permissions:**
  - CI has only `contents: read`.
  - The release workflow has `contents: write`, `id-token: write`, and `attestations: write`.
  - Checkout never keeps credentials: `persist-credentials: false`.
- **Nothing is published in this part:** no tag, no release, no change to `momentmaker/homebrew-tap`, and no secret.
  - Repository changes are only those listed in "After the tasks", which the user approves with this plan.
  - Implementers never push. The controller pushes the branch for CI.
- **Docs state only what the spec says** mtm does; they make no new promises.
- **The developer's Mac is never changed by tests:**
  - Never run `mtm prepare`, `finish`, `verify`, `cleanup`, `checklist`, or `retire` against the real home folder.
  - Never install tools, and never run GoReleaser or downloaded binaries on the author's Mac.
- **Before every commit:** `gofmt -l .` prints nothing, and `go vet ./...`, `go vet -tags integration ./...`, and `go test ./...` pass.
- **Commit messages** explain why and end with a `Co-Authored-By:` trailer naming the model that wrote the commit, as your harness instructs.

## Review Focus

These are the five conditions most likely to bite someone using what this part builds. Each is pinned by the task that owns it.

1. **The README's usage text drifting from `mtm help`,** so a new user follows commands that don't exist.
   - Task 1: `TestReadmeShowsTheCurrentUsage`.
2. **A released binary that says `mtm dev`,** because the version wasn't set at build time. Bug reports then can't name a version.
   - Task 2: the dry run's "The built binary reports its snapshot version".
3. **A formula that installs on an Intel Mac or an older macOS,** where mtm refuses to run.
   - Task 2: the dry run's "The formula requires Apple Silicon and macOS 26".
4. **A release built from an untidy `go.mod`, or with an old Go that has a known security fix missing.**
   - Task 2: the `go mod tidy -diff` hook, `check-latest: true`, and the govulncheck step.
5. **A workflow that can write to the repository, or leaves its token behind, on every push.**
   - Task 2: CI's `contents: read`, and `persist-credentials: false` on every checkout.

## File Structure

```
LICENSE                          MIT
README.md                        what mtm does, requirements, install, use, safety
CONTRIBUTING.md                  build and test, the test-safety rules, code rules, adding a catalog app
SECURITY.md                      scope, private reporting, verifying a release
cmd/mtm/main_test.go             (modify) TestReadmeShowsTheCurrentUsage
.github/workflows/ci.yml         test and release-dry-run jobs on macos-26
.github/workflows/release.yml    GoReleaser and provenance attestations on v* tags
.goreleaser.yaml                 darwin/arm64 build, archive, checksums, formula
docs/superpowers/specs/2026-11-02-mtm-design.md   (modify) §6.6, §11, §12.1
IMPLEMENTATION_PLAN.md           (modify) Stage 5 in two parts; Part 1's known gaps
```

---

### Task 1: License, README, contributor guide, and security policy

**Files:**
- Create: `LICENSE`, `README.md`, `CONTRIBUTING.md`, `SECURITY.md`
- Modify: `IMPLEMENTATION_PLAN.md` (Stage 5 becomes two parts)
- Test: `cmd/mtm/main_test.go` (modify)

**Interfaces:**
- Consumes: the `usage` constant in `cmd/mtm/main.go`.
- Produces: `TestReadmeShowsTheCurrentUsage`, which fails when `README.md`'s ```` ```text ```` usage block differs from `usage`. Task 2's docs link to these files.

**Notes:**
- **Facts:** every claim in `README.md` and `SECURITY.md` restates the spec (§2, §3, §5, §6). Don't add claims.
- **Copyright:** `LICENSE` uses the copyright line of the author's other MIT project, kaijutsu.
- **Vulnerability reports:** `SECURITY.md` points to GitHub's private vulnerability reporting. The controller turns it on for the repository after this task (see "After the tasks").

- [ ] **Step 1: Write the failing test**

`cmd/mtm/main_test.go`:
````diff
--- a/cmd/mtm/main_test.go
+++ b/cmd/mtm/main_test.go
@@ -43,6 +43,18 @@ func TestVersion(t *testing.T) {
 	}
 }
 
+// TestReadmeShowsTheCurrentUsage keeps the README's usage block the same as
+// mtm help, so the first thing a new user reads matches the tool.
+func TestReadmeShowsTheCurrentUsage(t *testing.T) {
+	data, err := os.ReadFile("../../README.md")
+	if err != nil {
+		t.Fatal(err)
+	}
+	if !strings.Contains(string(data), "```text\n"+usage+"```") {
+		t.Error("README.md's usage block differs from mtm help; copy the usage text from cmd/mtm/main.go")
+	}
+}
+
 func TestNoArgsShowsUsage(t *testing.T) {
 	var out, errOut bytes.Buffer
 	if code := run(ctx, nil, &out, &errOut, noApp(t)); code != exitFailed || !strings.Contains(errOut.String(), "Usage:") {
````

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/mtm/ -run TestReadmeShowsTheCurrentUsage`
Expected: FAIL, because there is no README yet:
```text
--- FAIL: TestReadmeShowsTheCurrentUsage (0.00s)
    main_test.go:51: open ../../README.md: no such file or directory
FAIL
```

- [ ] **Step 3: Write the files**

`LICENSE`:
```text
MIT License

Copyright (c) 2026 momentmaker and mtm contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

`README.md`:
````markdown
# mtm

Move a developer's Apple Silicon Mac to a new one, alongside Migration Assistant, and prove nothing was lost before you erase the old one.

> **Status: not released yet.** mtm hasn't been through a real migration yet. That run is planned, and it comes before the first release. Until then, read what each command does before you run it.

## Why

Migration Assistant copies files well. It doesn't cover:
- container, VM, and database data;
- state tied to the old Mac's hardware or security: privacy permissions, Secure Enclave keys, passkeys, Google sessions, and Touch ID;
- login items and licenses;
- any check that the copy is complete before the old Mac is erased.

mtm works around Migration Assistant.
- **Before it,** mtm records the old Mac and exports what Migration Assistant handles badly.
- **After it,** mtm restores those exports, lists the steps no tool can do for you, and checks the new Mac against the record. It ends with **SAFE TO WIPE**, or with the list of what's missing.

## Requirements

- Apple Silicon on both Macs, with macOS 26 or later.
- FileVault on, on both Macs.
- Migration Assistant run directly Mac to Mac, during the new Mac's first-boot setup. A Time Machine backup isn't supported.

## Install

Once released:

```sh
brew install momentmaker/tap/mtm
```

From source, with Go 1.26:

```sh
go install github.com/momentmaker/mtm/cmd/mtm@latest
```

## Use

| Step | Where | Command | What happens |
|---|---|---|---|
| 1 | Old Mac | `mtm scan` | Reports what Migration Assistant, mtm, and you will each handle. It changes nothing. |
| 2 | Old Mac | `mtm prepare` | Records this Mac and exports Docker volumes, locally built images, and Postgres. It stops services cleanly. Run it last, just before Migration Assistant. |
| 3 | Both | Migration Assistant | Run directly Mac to Mac, during the new Mac's setup, with every category selected. |
| 4 | New Mac | `mtm finish` | Restores what prepare set aside, and restarts services and launch agents. |
| 5 | New Mac | `mtm checklist` | Walks you through the steps no tool can do, such as privacy permissions, licenses, and signing in again. |
| 6 | New Mac | `mtm verify` | Checks the new Mac against the record, and prints **SAFE TO WIPE** or **NOT SAFE TO WIPE** with the reasons. |
| 7 | Old Mac | `mtm retire` | After SAFE TO WIPE: lists what changed since prepare, then walks through licenses, iCloud, and the erase. |
| 8 | New Mac | `mtm cleanup` | Deletes mtm's exports and record. |

```text
mtm moves a developer's Mac to a new Mac, alongside Migration Assistant.

Usage:
  mtm scan [--json]                         check this Mac before migrating (changes nothing)
  mtm prepare [--only a,b] [--deep] [--stage /Volumes/X]
                                            record this Mac before Migration Assistant;
                                            --stage puts exports on an encrypted drive
  mtm prepare --undo                        put this Mac back in use if you call off the migration
  mtm finish [--only a,b]                   on the new Mac: restore what prepare set aside
  mtm finish --only databases --restore-dump [--force]
                                            load the Postgres safety-net dump
  mtm checklist                             on the new Mac: go through the steps left to do by hand
  mtm checklist list [--all] [--json] | done <id>... | skip <id> --reason "..." | open <id>
  mtm verify [--only a,b] [--deep] [--details] [--json]
                                            check the new Mac against the record
  mtm retire [--details] [--json]           on the old Mac, after SAFE TO WIPE: what changed since
                                            prepare, then the steps before you erase it
  mtm retire list [--all] [--json] | done <id>... | skip <id> --reason "..." | open <id>
  mtm cleanup [--force]                     delete ~/.mtm, and the --stage exports, after SAFE TO WIPE
  mtm version
```

**Exit codes:**
- `verify`: `0` safe to wipe, `1` not safe, `2` mtm itself failed.
- Every other command: `0` success, `1` finished with problems or refused, `2` mtm failed or the command line was wrong.

## What it checks

- **Files:** your home folder, with a fingerprint for each dotfile at its top and everything in `~/.ssh`, `~/.gnupg`, and `~/.config`. Cloud-only files are never opened.
- **Git:** every repo's commits, stashes, and untracked and ignored secret-like files.
- **Homebrew and toolchains:** your Homebrew services, and the paths and versions of your tools.
- **Docker Desktop:** your volumes and locally built images.
- **Postgres:** Homebrew Postgres, with a safety-net dump.
- **Login items and privacy permissions:** your launch agents, login items, and privacy permissions.
- **Browsers:** Chrome, Brave, Arc, Firefox, and Safari profiles, and each Chromium browser's keychain key.
- **Identities:** your SSH and GPG identities.
- **Apps:** your apps, your login shell, and the `/etc` files you've edited.

The app catalog in [`catalog/`](catalog/README.md) says what each app needs by hand on a new Mac.

## Safety

- **No network:** mtm never makes network connections. Steps that need the network are printed for you to run.
- **No secrets read:** it never reads secret contents. Keychain items are checked for presence only, private keys are only looked for, and secret files are fingerprinted, never copied or printed.
- **No security weakened:** it never turns off a security feature or stores your password. Steps that need root are printed for you to run with `sudo`.
- **No data deleted:** it never deletes or overwrites your data. `prepare` writes only inside `~/.mtm`, or the `--stage` folder, and `cleanup` deletes only those.
- **Its own folder:** `~/.mtm` is private (mode `0700`) and excluded from Time Machine.
- **Reports:** reports hold paths, counts, and statuses, never fingerprints or file contents, so they are safe to paste into an issue.
- **Full Disk Access is optional.** With it, mtm reads your privacy permissions directly instead of inferring them from its catalog. `cleanup` reminds you to revoke it.

The full design is in [`docs/superpowers/specs/2026-11-02-mtm-design.md`](docs/superpowers/specs/2026-11-02-mtm-design.md). To report a vulnerability, see [SECURITY.md](SECURITY.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). To add an app to the catalog, see [`catalog/README.md`](catalog/README.md).

## License

MIT; see [LICENSE](LICENSE). Some catalog entries started from [tape](https://github.com/vitorgalvao/tape)'s definitions, credited in [`catalog/README.md`](catalog/README.md).
````

`CONTRIBUTING.md`:
````markdown
# Contributing to mtm

Thanks for helping. mtm runs on the Mac someone is about to erase, so the bar is careful code with tests that prove it.

## Build and test

You need Go 1.26 on an Apple Silicon Mac.

```sh
go build ./cmd/mtm
gofmt -l .                       # prints nothing when the code is formatted
go vet ./...
go vet -tags integration ./...
go test -race ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
```

CI runs the same commands on every push and pull request. It also builds a release snapshot with GoReleaser and checks the binary reports its version.

### Integration tests

These run with `-tags integration`. Each skips itself when its host is missing:
- `TestNoNetwork` (`cmd/mtm`) runs `scan`, `prepare`, `verify`, and `retire` under a `sandbox-exec` profile that denies network access. It uses a temporary home folder.
  - It needs FileVault on and Homebrew in `/opt/homebrew`.
  - It skips when a Homebrew service is running, because `prepare` would stop it.
- The Docker tests (`internal/modules/docker`) need Docker Desktop running. They create and remove only volumes, images, and containers named `mtm-test-…`.
- The Postgres test (`internal/modules/databases`) needs a Homebrew `postgresql@N`. It works on a throwaway cluster in a temporary folder.

```sh
go test -tags integration -run TestNoNetwork -v ./cmd/mtm/
```

## Tests must never change the Mac they run on

Most contributors run the tests on the Mac they work on every day. So:
- **Fake every system command.** Unit tests replace system commands with `runner.Fake` (`internal/runner`) and its canned output. A fake that could reach a command touching real data (the keychain, privacy databases, gpg, bioutil, Docker, launchctl) has no `Fallback`.
- **Use temporary folders.** Fixture home folders, keys, and browser folders live in `t.TempDir()`. No test reads the real `~/.ssh`, `~/.gnupg`, browser data, keychain, or privacy databases.
- **Stop and start nothing.** No test stops, starts, loads, or opens anything: no Homebrew services, launch agents, containers, or `/usr/bin/open`.
- **Docker tests touch only `mtm-test-…` resources.** They never call the docker module's `Prepare`, `Finish`, or `Undo` against the real engine.
- **Never run mtm on your real home folder** while you develop. Use a temporary `HOME`.

## Code

- **Modules never print.** All output goes through `internal/report`, which masks fingerprints and credentials.
- **System commands run only through `internal/runner`.** Apple's tools are called by absolute path.
- **Comments explain why.** They don't restate the code, and there's no commented-out code.
- **Write the failing test first,** then the code that makes it pass.
- **Commit messages say why** the change is needed.

The design, including the security rules every change must keep, is in [`docs/superpowers/specs/2026-11-02-mtm-design.md`](docs/superpowers/specs/2026-11-02-mtm-design.md). Known gaps are in [`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md).

## Adding an app to the catalog

Add one TOML file to [`catalog/`](catalog/). The format and the steps are in [`catalog/README.md`](catalog/README.md). Then run `go test ./internal/catalog/`; CI rejects a file those tests reject.

## Reporting security problems

Please don't open a public issue. See [SECURITY.md](SECURITY.md).
````

`SECURITY.md`:
````markdown
# Security policy

## Scope

mtm runs as you, on the Mac you're about to erase and on its replacement.
- **What it reads:** your home folder; a fingerprint of important files; the presence of keychain items (never their contents); and, with Full Disk Access, the privacy permissions databases.
- **What it runs:** your Homebrew, Docker Desktop, git, and Postgres tools.
- **What it writes:** only inside `~/.mtm`, or the `--stage` folder you give `prepare`. Those hold your Docker volume exports and Postgres dumps, which can contain sensitive data.

The rules it follows are in section 6 of the [design](docs/superpowers/specs/2026-11-02-mtm-design.md):
- no network connections;
- no secret contents read;
- no security setting weakened;
- no user data deleted or overwritten;
- `~/.mtm` private and excluded from Time Machine.

A way to break any of these is a vulnerability.

## Reporting a vulnerability

Report it privately with [GitHub's vulnerability reporting](https://github.com/momentmaker/mtm/security/advisories/new) (the **Security** tab, then **Report a vulnerability**). Please include:
- the mtm version (`mtm version`) and your macOS version;
- steps to reproduce;
- what an attacker gains.

Don't attach your real exports, dumps, `~/.mtm` contents, or logs. Describe them instead. Please don't open a public issue for an unfixed vulnerability.

We aim to acknowledge a report within 7 days.

## Supported versions

Fixes go into the latest release.

## Verifying a release

Each release archive has a SHA-256 checksum in `checksums.txt` and a GitHub build provenance attestation. To check where an archive came from:

```sh
gh attestation verify mtm_<version>_darwin_arm64.tar.gz --repo momentmaker/mtm
```
````

- [ ] **Step 4: Split Stage 5 in the plan file**

`IMPLEMENTATION_PLAN.md`:
```diff
--- a/IMPLEMENTATION_PLAN.md
+++ b/IMPLEMENTATION_PLAN.md
@@ -162,7 +162,28 @@ Found while planning.
 - **Touch ID's count** comes from `bioutil`'s text output. If the format changes, the count is unknown and the step is listed anyway.
 
 ## Stage 5: Ship
-**Goal**: GitHub Actions CI on Apple Silicon (`go test`, `go vet`, `gofmt`, `govulncheck`, catalog validation); end-to-end tests in Tart VMs (cloned VM as a perfect migration, injected faults); release builds with checksums and provenance attestations; a Homebrew tap; README and contributor guide (including how to add a catalog app).
-**Success Criteria**: tagging a release produces checksummed binaries and a working `brew install`. The Tart end-to-end suite passes locally.
-**Tests**: the Tart suite; a release dry run.
+Stage 5 is built in two parts. The first ships everything but the first release. The second runs on the new Mac after the author's own migration, which is mtm's first real-world run. This Mac has too little free disk for Tart's macOS VM images.
+
+### Stage 5, Part 1: CI, release pipeline, and docs
+**Goal**:
+- `LICENSE` (MIT), `README.md`, `CONTRIBUTING.md` (including how to add a catalog app), and `SECURITY.md`;
+- GitHub Actions CI on `macos-26` (`gofmt`, `go vet`, `go test -race`, `govulncheck`, catalog validation);
+- the GoReleaser release pipeline: `darwin/arm64`, checksums, provenance attestations, and a formula in `momentmaker/homebrew-tap`;
+- a snapshot release dry run in CI.
+
+Nothing is tagged or released.
+
+**Success Criteria**: CI passes on GitHub for a push. The snapshot dry run builds the `darwin_arm64` archive, `checksums.txt`, and a formula, and the built binary reports its version.
+**Tests**: `TestReadmeShowsTheCurrentUsage`; the CI run on GitHub, including the dry run.
+**Plan**: `docs/superpowers/plans/2026-11-03-mtm-stage5-part1-ci-release-docs.md`
+**Status**: In Progress
+
+### Stage 5, Part 2: Real migration, end-to-end suite, and first release
+**Goal**:
+- the author's own migration to the new Mac with mtm, which settles the "Real-run validation pending" items above;
+- the Tart end-to-end suite: a cloned VM as a perfect migration, with injected faults;
+- the `HOMEBREW_TAP_TOKEN` secret, the `v0.1.0` tag, and a working `brew install momentmaker/tap/mtm`.
+
+**Success Criteria**: tagging a release produces checksummed, attested binaries and a working `brew install`. The Tart end-to-end suite passes locally.
+**Tests**: the Tart suite; the release itself.
 **Status**: Not Started
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
gofmt -l .
go vet ./...
go vet -tags integration ./...
go test -race ./...
```
Expected: `gofmt -l .` prints nothing, and the rest pass.

- [ ] **Step 6: Commit**

End the message with your `Co-Authored-By:` trailer (Global Constraints).

```bash
git add LICENSE README.md CONTRIBUTING.md SECURITY.md cmd/mtm/main_test.go IMPLEMENTATION_PLAN.md
git commit -m "Add the license, README, contributor guide, and security policy

mtm is public now. A user needs to know what it does to their Mac before
they run it, a contributor needs the rules that keep tests from touching
the Mac they run on, and someone who finds a hole needs a private way to
report it. A test keeps the README's usage text the same as mtm help."
```

---

### Task 2: CI and the release pipeline

**Files:**
- Create: `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.goreleaser.yaml`
- Modify: `docs/superpowers/specs/2026-11-02-mtm-design.md` (§6.6, §11, §12.1), `IMPLEMENTATION_PLAN.md` (Part 1's known gaps)
- Test: the CI run on GitHub; there's no Go test in this task. The controller pushes the branch after review.

**Interfaces:**
- Consumes: `main.version` in `cmd/mtm/main.go`, set at build time with `-X main.version=<version>`. `mtm version` prints `mtm <version>`.
- Produces:
  - **CI jobs `test` and `release-dry-run`** on every push and pull request.
  - **A `Release` workflow** on `v*.*.*` tags. It runs nothing until Part 2 tags `v0.1.0`, and it needs the `HOMEBREW_TAP_TOKEN` repository secret.

**Why it works this way:**
- **Pins:**
  - Actions are pinned to full commit hashes, with the tag in a comment; the hashes were read from each repository's tag.
  - Tools are pinned to exact versions: `govulncheck` v1.9.0 and GoReleaser v2.19.0.
  - Every input used exists in the pinned version's `action.yml`.
- **Go version:** `setup-go` uses `go.mod`'s `go 1.26` with `check-latest: true`, so CI and releases get the newest 1.26 patch. Go 1.26.1 fails `govulncheck`: GO-2026-4869 in `archive/tar`, which `internal/modules/docker` reaches.
- **The runner:** `macos-26` is GitHub's Apple Silicon runner for macOS 26, generally available since February 2026.
- **The integration tests** skip themselves there: the runners have no FileVault, Docker Desktop, or Homebrew Postgres.
- **A formula, not a cask.** GoReleaser marks `brews` deprecated in favor of casks. But a cask for an un-notarized binary needs its quarantine flag removed, which mtm won't do, and a formula's download carries no quarantine flag. Because `goreleaser check` exits 2 on deprecated settings, the dry run builds a snapshot instead of running `check`.
- **The snapshot:**
  - It versions itself `0.0.1-next`, since there are no tags.
  - The config names `HOMEBREW_TAP_TOKEN`, so the dry run sets it empty. A snapshot never publishes.
- **Reproducible builds:** `-trimpath`, `CGO_ENABLED=0`, and `mod_timestamp` set to the commit's time mean a rebuild of the same commit with the same Go version gives the same archive.
- **A clean module:** `go mod tidy -diff` fails the release if `go.mod` or `go.sum` isn't tidy, rather than rewriting them.
- **What was checked before the plan was written:**
  - `go mod tidy -diff` passes.
  - `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=0.0.1-next"` builds an `mtm` that prints `mtm 0.0.1-next`.
  - `govulncheck` v1.9.0 runs, and finds only the Go 1.26.1 issue above.
  - actionlint and GoReleaser couldn't be run on the author's Mac, so the CI run is these files' first execution.

- [ ] **Step 1: Write the CI workflow**

`.github/workflows/ci.yml`:
```yaml
# Every push and pull request runs the same checks as a contributor's
# gate. Actions are pinned to full commit hashes and tools to exact
# versions (spec 6.6), so an upstream change can't alter the build unseen.
name: CI

on:
  push:
    branches: ["**"]
  pull_request:

permissions:
  contents: read

jobs:
  test:
    name: Test
    runs-on: macos-26
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false

      # check-latest takes the newest Go 1.26 patch, which carries the
      # standard library's security fixes.
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
          check-latest: true

      - name: gofmt
        run: |
          unformatted=$(gofmt -l .)
          if [ -n "$unformatted" ]; then
            echo "These files need gofmt:"
            echo "$unformatted"
            exit 1
          fi

      - name: go vet
        run: go vet ./...

      - name: go vet with the integration tag
        run: go vet -tags integration ./...

      - name: go test
        run: go test -race ./...

      - name: govulncheck
        run: go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

  release-dry-run:
    name: Release dry run
    runs-on: macos-26
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          fetch-depth: 0
          persist-credentials: false

      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
          check-latest: true

      # A snapshot builds everything a release would and publishes nothing.
      # The tap token is never used, but the config names it, so it must
      # be set.
      - uses: goreleaser/goreleaser-action@a4f02ab5355a589de60d917a659ce617ed19b3de # v7.3.0
        with:
          distribution: goreleaser
          version: v2.19.0
          args: release --snapshot --clean
        env:
          HOMEBREW_TAP_TOKEN: ""

      - name: The archive and checksums exist
        run: |
          ls dist/mtm_*_darwin_arm64.tar.gz
          grep -q '_darwin_arm64.tar.gz$' dist/checksums.txt

      - name: The built binary reports its snapshot version
        run: |
          version=$(dist/mtm_darwin_arm64*/mtm version)
          echo "$version"
          case "$version" in
            "mtm "*-next) ;;
            *) echo "unexpected version output"; exit 1 ;;
          esac

      - name: The formula requires Apple Silicon and macOS 26
        run: |
          formula=$(find dist -name mtm.rb)
          cat "$formula"
          grep -q 'depends_on arch: :arm64' "$formula"
          grep -q 'depends_on macos: :tahoe' "$formula"
```

- [ ] **Step 2: Write the GoReleaser config**

`.goreleaser.yaml`:
````yaml
# GoReleaser builds mtm's releases when a v* tag is pushed, and CI builds a
# snapshot of the same on every push (.github/workflows/ci.yml).
version: 2

project_name: mtm

before:
  hooks:
    # Fail rather than rewrite go.mod or go.sum: a release builds exactly
    # what was committed.
    - go mod tidy -diff

builds:
  - id: mtm
    main: ./cmd/mtm
    binary: mtm
    env:
      - CGO_ENABLED=0
    # mtm supports Apple Silicon only (spec 2).
    goos:
      - darwin
    goarch:
      - arm64
    flags:
      - -trimpath
    ldflags:
      - -s -w -X main.version={{ .Version }}
    # The commit's time, not the build's, so a rebuild of the same commit
    # with the same Go version gives the same archive.
    mod_timestamp: "{{ .CommitTimestamp }}"

archives:
  - id: mtm
    formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - LICENSE
      - README.md

checksum:
  name_template: checksums.txt

snapshot:
  version_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc

release:
  github:
    owner: momentmaker
    name: mtm
  prerelease: auto
  header: |
    Install with Homebrew:

    ```
    brew install momentmaker/tap/mtm
    ```

    Check where an archive came from:

    ```
    gh attestation verify mtm_{{ .Version }}_darwin_arm64.tar.gz --repo momentmaker/mtm
    ```

# A formula, not a cask: a binary Homebrew downloads for a formula carries no
# quarantine flag, so the un-notarized mtm runs without removing Gatekeeper's
# check (spec 6.6). GoReleaser marks brews deprecated in favor of casks.
brews:
  - name: mtm
    repository:
      owner: momentmaker
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    homepage: https://github.com/momentmaker/mtm
    description: "Move a developer's Mac to a new Mac alongside Migration Assistant, and prove nothing was lost"
    license: MIT
    custom_block: |
      depends_on arch: :arm64
      depends_on macos: :tahoe
    install: |
      bin.install "mtm"
    test: |
      assert_match version.to_s, shell_output("#{bin}/mtm version")
````

- [ ] **Step 3: Write the release workflow**

`.github/workflows/release.yml`:
```yaml
# Pushing a v* tag publishes a release: the darwin/arm64 archive,
# checksums.txt, a build provenance attestation for each archive, and the
# formula in momentmaker/homebrew-tap. HOMEBREW_TAP_TOKEN must be a token
# that can push to that repository.
name: Release

on:
  push:
    tags: ["v*.*.*"]

permissions:
  contents: write
  id-token: write
  attestations: write

jobs:
  release:
    name: Release
    runs-on: macos-26
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          fetch-depth: 0
          persist-credentials: false

      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
          check-latest: true

      - name: go test
        run: go test -race ./...

      - uses: goreleaser/goreleaser-action@a4f02ab5355a589de60d917a659ce617ed19b3de # v7.3.0
        with:
          distribution: goreleaser
          version: v2.19.0
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}

      - uses: actions/attest-build-provenance@4d101475d8b20a2381f78447822ac1eab6504dd8 # v4.2.2
        with:
          subject-checksums: dist/checksums.txt
```

- [ ] **Step 4: Update the spec and record the known gaps**

`docs/superpowers/specs/2026-11-02-mtm-design.md`:
```diff
--- a/docs/superpowers/specs/2026-11-02-mtm-design.md
+++ b/docs/superpowers/specs/2026-11-02-mtm-design.md
@@ -131,11 +131,13 @@ The steps live in `~/.mtm/retire.json`, apart from the new Mac's `state.json`, s
 
 ### 6.6 Supply chain
 - Go with the standard library. The only third-party dependency is `github.com/BurntSushi/toml` (no transitive dependencies) for the catalog.
-- Dependencies are pinned in `go.sum`.
+- Dependencies are pinned in `go.sum`. GitHub Actions are pinned to full commit hashes, and tools CI runs (`govulncheck`, GoReleaser) to exact versions.
 - CI runs `govulncheck` on every change.
-- Release binaries come with SHA-256 checksums and GitHub build provenance attestations.
-- Distributed through a Homebrew tap under the project's GitHub organization.
-- Apple notarization of release binaries is planned and depends on an Apple Developer Program membership (section 12).
+- Releases are built by GoReleaser when a `v*` tag is pushed:
+  - for `darwin/arm64` only, with `-trimpath`, `CGO_ENABLED=0`, and file times set to the commit's time, so a rebuild of the same commit with the same Go version gives the same binary;
+  - with SHA-256 checksums in `checksums.txt`, and a GitHub build provenance attestation for each archive, which `gh attestation verify` checks.
+- Distributed through a Homebrew formula in the `momentmaker/homebrew-tap` tap. The formula requires Apple Silicon and macOS 26.
+- Version 1 isn't notarized (section 12). A binary Homebrew downloads for a formula carries no quarantine flag, so it runs without notarization.
 
 ## 7. Architecture
 
@@ -524,10 +526,13 @@ Identities are recorded, never key material.
   3. Inject faults: delete a critical file, corrupt a file without changing its time, change a repo's refs, remove a Docker volume, change a file on the source after `prepare`.
   4. Check that `verify` reports each fault with the right outcome and that `retire` flags the late change.
 - **Real Migration Assistant run.** The author's own migration from the M3 is the first real-world test. Findings feed the catalog, fixtures, and the "unconfirmed" rows in section 3.
-- **CI** (GitHub Actions, Apple Silicon runners): `go test ./...`, `go vet`, `gofmt` check, `govulncheck`, catalog validation, and reproducible release builds with checksums and provenance attestations.
+- **CI** (GitHub Actions, `macos-26` Apple Silicon runners), on every push and pull request:
+  - the `gofmt` check, `go vet` with and without the `integration` tag, `go test -race ./...` (which includes catalog validation), and `govulncheck`;
+  - a GoReleaser snapshot build, whose binary must report its snapshot version, and whose formula must require Apple Silicon and macOS 26.
+  - The integration tests skip themselves on CI runners, which have no FileVault, Docker Desktop, or Homebrew Postgres.
 
 ## 12. Open questions and risks
-1. **Notarization** needs an Apple Developer Program membership. If none is available, version 1 ships through a Homebrew formula and checksummed release binaries without notarization.
+1. **Notarization** needs an Apple Developer Program membership. Version 1 ships without it, through a Homebrew formula and checksummed, attested release binaries (section 6.6).
 2. **The macOS 26 "Privacy & Security Settings" checkbox** in Migration Assistant has undocumented scope. If it does migrate privacy permissions, the `permissions` module's verify step marks those checklist items done automatically. The design works either way.
 3. **Whether Migration Assistant copies `Docker.raw` intact** is unconfirmed. The design handles both outcomes (section 9.5). `Docker.raw` is also a sparse file: on the author's Mac it is 494 GB with 49 GB in use. If Migration Assistant doesn't keep it sparse, the new Mac needs the full size free.
 4. **Google session binding (Device Bound Session Credentials)** comes from a single news source. The design treats re-sign-in as a checklist item, which costs little if it turns out unnecessary.
```

`IMPLEMENTATION_PLAN.md`:
```diff
--- a/IMPLEMENTATION_PLAN.md
+++ b/IMPLEMENTATION_PLAN.md
@@ -178,6 +178,14 @@ Nothing is tagged or released.
 **Plan**: `docs/superpowers/plans/2026-11-03-mtm-stage5-part1-ci-release-docs.md`
 **Status**: In Progress
 
+#### Known gaps carried out of Stage 5, Part 1
+Found while planning.
+- **The CI and release files ran first on GitHub.** The author's Mac couldn't run actionlint or GoReleaser, so a branch's first push was their first run. The release workflow runs only on a tag, so its first run is `v0.1.0` (Part 2).
+- **GoReleaser marks `brews` deprecated** in favor of Homebrew casks, so `goreleaser check` exits 2. CI's dry run builds a snapshot instead.
+  - A cask for an un-notarized binary needs its quarantine flag removed, which mtm won't do.
+  - If GoReleaser removes `brews`, the formula must be written another way.
+- **The author's Go toolchain:** Go 1.26.1 has GO-2026-4869 (`archive/tar`), so `govulncheck` fails on it until Go is upgraded to 1.26.2 or later. CI and releases take the newest 1.26 patch.
+
 ### Stage 5, Part 2: Real migration, end-to-end suite, and first release
 **Goal**:
 - the author's own migration to the new Mac with mtm, which settles the "Real-run validation pending" items above;
```

- [ ] **Step 5: Run the local gate**

```bash
gofmt -l .
go vet ./...
go vet -tags integration ./...
go test -race ./...
```
Expected: `gofmt -l .` prints nothing, and the rest pass.

Also run `go mod tidy -diff`. Expected: no output, exit 0.

- [ ] **Step 6: Commit**

End the message with your `Co-Authored-By:` trailer (Global Constraints).

```bash
git add .github/workflows/ci.yml .github/workflows/release.yml .goreleaser.yaml docs/superpowers/specs/2026-11-02-mtm-design.md IMPLEMENTATION_PLAN.md
git commit -m "Add CI and the release pipeline

Every push now runs the gate, govulncheck, and a GoReleaser snapshot on an
Apple Silicon runner, so a change that breaks the build, the release, or
the formula is caught before a tag. A v* tag publishes the darwin/arm64
archive with checksums and provenance attestations, and the formula in
momentmaker/homebrew-tap. Actions are pinned to commits and tools to
versions, so nothing upstream changes the build unseen."
```

- [ ] **Step 7: CI on GitHub (controller)**

The implementer doesn't push. After the task review, the controller:
1. pushes the branch: `git push -u origin stage-5-part1`;
2. watches the run: `gh run watch --repo momentmaker/mtm --exit-status`, and if needed `gh run view --log-failed`.

Expected: both jobs pass.
- **`test`:** gofmt, both vets, `go test -race`, and govulncheck.
- **`release-dry-run`:**
  - the archive and checksum lines exist;
  - the binary prints `mtm 0.0.1-next`;
  - the formula has `depends_on arch: :arm64` and `depends_on macos: :tahoe`.

A failure here enters the fix loop like a review finding. The run log is the evidence.

---

### After the tasks (controller)

These change the GitHub repository, so the user approves them with this plan:
1. **Turn on private vulnerability reporting,** which `SECURITY.md` points to: `gh api -X PUT repos/momentmaker/mtm/private-vulnerability-reporting`.
2. **Merge after the final review,** on the user's choice: fast-forward `main` and push it. CI then runs on `main`.

Not in this part:
- the `HOMEBREW_TAP_TOKEN` secret;
- any tag or release.

Both belong to Part 2, after the author's migration.
