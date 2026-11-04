# mtm Scan Fixes from the First Real Run: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix what `mtm scan`'s first run on the author's own Mac showed was misleading. The scan ran on 2026-11-04, at commit 7e5712d.

**What the run showed:**
1. **Tool-managed clones reported as the user's work.** Clones that tools keep and download again were reported as "work that exists only on this Mac": `~/.asdf/plugin-index`, `~/.asdf/repository`, and `~/.claude/plugins/cache/…`.
2. **"Push it if you can."** That's wrong advice for ignored secret files such as `.env`, and unneeded for work the user chooses not to push. Migration Assistant copies both, and verify checks them.
3. **A long list.** Seventeen repos whose only local work was such secret files each got their own warning, burying the real ones.
4. **`java unknown`.** That's macOS's `/usr/bin/java` placeholder, with no JDK installed, recorded as a tool.

**Architecture:** two small, independent changes.
- **The `git` module:**
  - skips the clones tools manage, at discovery, as it already skips package caches;
  - groups the repos whose only local work is ignored secret files into one info finding;
  - rewrites the remaining warning's fix text.
- **The `toolchains` module:** treats a `/usr/bin` placeholder that fails to report a version as not installed.

**Tech Stack:** Go 1.26. No new dependency.

**Spec:** `docs/superpowers/specs/2026-11-02-mtm-design.md`. Each task updates its section: §9.2 `git` and §9.4 `toolchains`.

**How this plan was checked:**
- Both tasks were written and run in a scratch copy, each as its own commit.
- The gate is clean after each.
- The red steps are the real failures, from the tests on the previous code.

**Diffs** are unified diffs against the file as the previous task left it. Apply them with `git apply`, or by hand.

## Global Constraints

- **Module:** module path `github.com/momentmaker/mtm`, `go 1.26`. The only third-party dependency is `github.com/BurntSushi/toml` v1.6.0.
- **Verify stays the safety net.** Nothing here weakens a verify check.
  - A skipped clone's files are still recorded and checked by the `files` module.
  - Grouped secret files are still fingerprinted and checked by `git`'s verify.
- **Reports and logs** never hold fingerprints, file contents, or environment values (spec 6.3).
- **The developer's Mac is never changed by tests:**
  - Tests use `t.TempDir()` homes and `testutil.IsolateGit`.
  - Never run mtm against the real home folder.
- **Before every commit:** `gofmt -l .` prints nothing, and `go vet ./...`, `go vet -tags integration ./...`, and `go test -race ./...` pass.
- **Commit messages** explain why and end with a `Co-Authored-By:` trailer naming the model that wrote the commit, as your harness instructs.

## Review Focus

1. **A repo with real local-only work that gets grouped or silenced.** A repo with unpushed commits and secret files keeps its own warning, which counts both.
   - Task 1: `TestScanGroupsReposWhoseOnlyWorkIsSecretFiles`, which asserts `GitHub/app` keeps its warning.
2. **The user's own repo inside a skipped folder.** It's no longer a git check, but its files are still recorded by `files`, and spec §9.2 says so. This is pinned by the skip list being explicit, with no wildcards.
   - Task 1: `TestDiscoverFindsReposAndSkipsNoise`.
3. **A real tool that can't report its version gets dropped.** Only placeholders in `/usr/bin` are dropped; anything else stays `unknown` and is required on the new Mac.
   - Task 2: `TestAFailingToolOutsideAppleFolderStaysUnknown`.
4. **A placeholder that works** (a real `python3` from the Command Line Tools) is still recorded.
   - Task 2: `TestApplePlaceholdersCountAsNotInstalled`.
5. **The warning's advice:** it explains Migration Assistant's copy, and that the Mac mustn't be erased before SAFE TO WIPE, rather than asking for a push.
   - Task 1: `TestScanDoesNotAskToPushWork`.

## File Structure

```
internal/modules/git/discover.go     (modify) skip the clones tools manage
internal/modules/git/module.go       (modify) group secret-only repos; new fix text
internal/modules/git/repo.go         (modify) Repo.onlySecrets
internal/modules/git/module_test.go  (modify)
internal/modules/toolchains/toolchains.go       (modify) Apple placeholders count as not installed
internal/modules/toolchains/toolchains_test.go  (modify)
docs/superpowers/specs/2026-11-02-mtm-design.md (modify) §9.2, §9.4
```

---

### Task 1: The git scan reports only the user's own local work, without asking for pushes

**Files:**
- Modify: `internal/modules/git/discover.go`, `internal/modules/git/module.go`, `internal/modules/git/repo.go`, and `docs/superpowers/specs/2026-11-02-mtm-design.md` (§9.2)
- Test: `internal/modules/git/module_test.go` (modify)

**Interfaces:**
- Consumes: `Repo.risks`, `Repo.Important`, `Repo.Unpushed`, `Repo.Stash`, `Repo.Untracked`, `Repo.changedTracked`, `displayPath`, and `words.Count` (Stage 1).
- Produces:
  - `Repo.onlySecrets() bool`;
  - the finding id `secret-files`, with severity info, titled "N repos have ignored files that look like secrets or local config";
  - the `localOnlyFix` text.

- [ ] **Step 1: Write the failing tests**

`internal/modules/git/module_test.go`:
```diff
--- a/internal/modules/git/module_test.go
+++ b/internal/modules/git/module_test.go
@@ -95,6 +95,10 @@ func TestDiscoverFindsReposAndSkipsNoise(t *testing.T) {
 		"GitHub/app/node_modules/pkg/.git",
 		"Library/Application Support/x/.git",
 		".cargo/registry/index/x/.git",
+		".asdf/plugins/nodejs/.git",
+		".claude/plugins/cache/x/1.0.0/.git",
+		".oh-my-zsh/.git",
+		".local/share/nvim/lazy/x/.git",
 		"deep/1/2/3/4/5/.git",
 		"deep/1/2/3/4/5/6/.git",
 	} {
@@ -184,6 +188,66 @@ func TestScanWarnsAboutLocalOnlyWork(t *testing.T) {
 	}
 }
 
+func TestScanDoesNotAskToPushWork(t *testing.T) {
+	home := t.TempDir()
+	seedRepo(t, home)
+	findings, err := New().Scan(testEnv(home, nil))
+	if err != nil {
+		t.Fatal(err)
+	}
+	i := slices.IndexFunc(findings, func(f module.Finding) bool { return f.ID == "local-only:GitHub/app" })
+	if i < 0 || strings.HasPrefix(findings[i].Fix, "Push it") || !strings.Contains(findings[i].Fix, "SAFE TO WIPE") {
+		t.Errorf("fix = %q, want Migration Assistant's copy and verify explained, not a push asked for", findings[i].Fix)
+	}
+}
+
+// pushedRepoWithSecret makes a repo at rel whose commits are all on its
+// remote, and whose only work of its own is an ignored .env file.
+func pushedRepoWithSecret(t *testing.T, home, rel string) {
+	t.Helper()
+	testutil.IsolateGit(t)
+	remote := filepath.Join(t.TempDir(), "remote.git")
+	testutil.Git(t, t.TempDir(), "init", "-q", "--bare", remote)
+	repo := filepath.Join(home, rel)
+	if err := os.MkdirAll(repo, 0o755); err != nil {
+		t.Fatal(err)
+	}
+	testutil.Git(t, repo, "init", "-q", "-b", "main")
+	testutil.WriteFile(t, filepath.Join(repo, ".gitignore"), ".env\n")
+	testutil.Git(t, repo, "add", ".gitignore")
+	testutil.Git(t, repo, "commit", "-q", "-m", "first")
+	testutil.Git(t, repo, "remote", "add", "origin", remote)
+	testutil.Git(t, repo, "push", "-q", "origin", "main")
+	testutil.WriteFile(t, filepath.Join(repo, ".env"), "SECRET=1\n")
+}
+
+func TestScanGroupsReposWhoseOnlyWorkIsSecretFiles(t *testing.T) {
+	home := t.TempDir()
+	pushedRepoWithSecret(t, home, "GitHub/api")
+	pushedRepoWithSecret(t, home, "GitHub/web")
+	seedRepo(t, home)
+	findings, err := New().Scan(testEnv(home, nil))
+	if err != nil {
+		t.Fatal(err)
+	}
+	for _, rel := range []string{"GitHub/api", "GitHub/web"} {
+		if slices.ContainsFunc(findings, func(f module.Finding) bool { return f.ID == "local-only:"+rel }) {
+			t.Errorf("%s got its own warning; want it in the grouped line", rel)
+		}
+	}
+	i := slices.IndexFunc(findings, func(f module.Finding) bool { return f.ID == "secret-files" })
+	if i < 0 {
+		t.Fatalf("findings = %+v, want one secret-files line", findings)
+	}
+	f := findings[i]
+	if f.Severity != module.SeverityInfo || f.Title != "2 repos have ignored files that look like secrets or local config" || !strings.Contains(f.Detail, "~/GitHub/api (1), ~/GitHub/web (1)") {
+		t.Errorf("finding = %+v", f)
+	}
+	if !slices.ContainsFunc(findings, func(f module.Finding) bool { return f.ID == "local-only:GitHub/app" }) {
+		t.Error("the repo with unpushed work lost its own warning")
+	}
+}
+
 func TestScanReportsUncommittedChanges(t *testing.T) {
 	home := t.TempDir()
 	repo := seedRepo(t, home)
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/modules/git/`
Expected: FAIL in three tests:
```text
--- FAIL: TestDiscoverFindsReposAndSkipsNoise
--- FAIL: TestScanDoesNotAskToPushWork
--- FAIL: TestScanGroupsReposWhoseOnlyWorkIsSecretFiles
```

- [ ] **Step 3: Implement**

`internal/modules/git/discover.go`:
```diff
--- a/internal/modules/git/discover.go
+++ b/internal/modules/git/discover.go
@@ -15,10 +15,16 @@ import (
 )
 
 // skipDirs holds folders that never contain the user's own repos: app
-// state, caches, and package-manager checkouts.
+// state, caches, package-manager checkouts, and the clones version
+// managers, shell frameworks, and editor and agent plugin managers keep
+// and download again. Their files are still recorded and checked by the
+// files module; only the git checks skip them.
 var skipDirs = pathset.MustNew(
 	"Library/", ".Trash/", ".mtm/", ".cache/", ".npm/", ".rustup/",
 	".cargo/registry/", ".cargo/git/", "go/pkg/",
+	".asdf/", ".nvm/", ".pyenv/", ".rbenv/",
+	".oh-my-zsh/", ".tmux/plugins/", ".vim/plugged/", ".local/share/nvim/",
+	".claude/plugins/",
 	"node_modules", ".git",
 )
 
```

`internal/modules/git/repo.go`:
```diff
--- a/internal/modules/git/repo.go
+++ b/internal/modules/git/repo.go
@@ -217,6 +217,12 @@ func (r Repo) risks() []string {
 	return out
 }
 
+// onlySecrets is true when the repo's only local work is ignored files that
+// look like secrets or local config.
+func (r Repo) onlySecrets() bool {
+	return len(r.Important) > 0 && r.Unpushed == 0 && len(r.Stash) == 0 && r.changedTracked() == 0 && len(r.Untracked) == 0
+}
+
 // changedTracked counts the status records for tracked files with changes:
 // ordinary (1), renamed or copied (2), and unmerged (u).
 func (r Repo) changedTracked() int {
```

`internal/modules/git/module.go`:
```diff
--- a/internal/modules/git/module.go
+++ b/internal/modules/git/module.go
@@ -41,6 +41,7 @@ func (m Module) Scan(env module.Env) ([]module.Finding, error) {
 		Title:    words.Count(len(repos), "git repository", "git repositories") + " found",
 		Detail:   "Migration Assistant copies them. mtm checks that every recorded commit and untracked file arrived.",
 	}}
+	var secretsOnly []string
 	for _, rel := range repos {
 		if err := env.Ctx.Err(); err != nil {
 			return nil, err
@@ -63,7 +64,9 @@ func (m Module) Scan(env module.Env) ([]module.Finding, error) {
 		if r.tooManyToFingerprint() {
 			findings = append(findings, tooManyFiles(r))
 		}
-		if risks := r.risks(); len(risks) > 0 {
+		if r.onlySecrets() {
+			secretsOnly = append(secretsOnly, fmt.Sprintf("%s (%d)", displayPath(rel), len(r.Important)))
+		} else if risks := r.risks(); len(risks) > 0 {
 			findings = append(findings, module.Finding{
 				Module:   Name,
 				ID:       "local-only:" + rel,
@@ -71,13 +74,34 @@ func (m Module) Scan(env module.Env) ([]module.Finding, error) {
 				Handler:  module.ByMigrationAssistant,
 				Title:    displayPath(rel) + ": work that exists only on this Mac",
 				Detail:   strings.Join(risks, ", "),
-				Fix:      "Push it if you can. Either way, don't wipe this Mac until mtm verify says SAFE TO WIPE.",
+				Fix:      localOnlyFix,
 			})
 		}
 	}
+	if len(secretsOnly) > 0 {
+		findings = append(findings, secretFiles(secretsOnly))
+	}
 	return findings, nil
 }
 
+// localOnlyFix doesn't ask for a push: Migration Assistant copies the work
+// and verify checks it, so what matters is keeping this Mac until then.
+const localOnlyFix = "Migration Assistant copies it, and mtm verify checks every commit and file arrived. Until then this Mac has the only copy: push it first if you want another, and don't erase this Mac before mtm verify says SAFE TO WIPE."
+
+// secretFiles gathers the repos whose only local work is ignored files
+// that look like secrets, such as .env files. They belong outside git, so
+// one line says they're copied and checked, instead of a warning per repo.
+func secretFiles(repos []string) module.Finding {
+	return module.Finding{
+		Module:   Name,
+		ID:       "secret-files",
+		Severity: module.SeverityInfo,
+		Handler:  module.ByMigrationAssistant,
+		Title:    words.Count(len(repos), "repo has", "repos have") + " ignored files that look like secrets or local config",
+		Detail:   "Migration Assistant copies them, and mtm verify checks each one arrived: " + strings.Join(repos, ", "),
+	}
+}
+
 func (m Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
 	repos, err := discover(env.Ctx, env.Home, m.MaxDepth)
 	if err != nil {
```

- [ ] **Step 4: Update the spec**

`docs/superpowers/specs/2026-11-02-mtm-design.md`:
```diff
--- a/docs/superpowers/specs/2026-11-02-mtm-design.md
+++ b/docs/superpowers/specs/2026-11-02-mtm-design.md
@@ -285,7 +285,7 @@ Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0`
 - Git's own lock files (`<name>.lock` inside a `.git` folder) are volatile: git creates one around each write and removes it moments later, so one the walk catches is usually gone before the copy.
 
 ### 9.2 `git`
-- **Finding repos:** `.git` directories under the home folder, excluding `~/Library` and `node_modules`. Search depth is configurable.
+- **Finding repos:** `.git` directories under the home folder, excluding `~/Library`, `node_modules`, package-manager caches, and the clones that tools keep and download again: version managers (`~/.asdf`, `~/.nvm`, `~/.pyenv`, `~/.rbenv`), shell and editor plugin managers (`~/.oh-my-zsh`, `~/.tmux/plugins`, `~/.vim/plugged`, `~/.local/share/nvim`), and agent plugins (`~/.claude/plugins`). The files module still records and checks the files in them. Search depth is configurable.
 - **Every git command** runs with `--no-optional-locks` and `-c core.fsmonitor=false`, so `mtm` never rewrites a repo's index or starts background daemons.
 - **Recorded per repo:**
   - local refs with commit hashes (`git for-each-ref`). Remote-tracking refs are skipped, because their commits exist on the remote;
@@ -294,7 +294,8 @@ Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0`
   - status (`git status --porcelain=v2 --branch -z --untracked-files=all --ignored=matching`; NUL-separated, so any file name parses);
   - the number of commits not on any remote (`git rev-list --count --branches --not --remotes`). This also covers local branches without an upstream;
   - fingerprints and modification times of untracked files, and of ignored files matching the important-file patterns (`.env*`, `*.pem`, `*.key`, `*.p12`, `*.local.*`).
-- **`scan`:** unpushed commits, stashes, untracked files, and important ignored files are warnings. Migration Assistant copies them, but they exist nowhere else once the old Mac is wiped.
+- **`scan`:** unpushed commits, stashes, uncommitted changes, and untracked files are a warning per repo. Migration Assistant copies them and verify checks them, but until then they exist nowhere else, so the old Mac must not be erased before SAFE TO WIPE. mtm doesn't ask for a push.
+  - Repos whose only local work is important ignored files, such as `.env` files, which belong outside git, get one info line between them, naming each repo.
 - **`verify`** (a critical check per repo) fails when:
   - the repo is missing or unreadable;
   - any recorded commit (refs, HEAD, stash) is missing from the object database (`git cat-file --batch-check`);
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
git add internal/modules/git/module_test.go internal/modules/git/discover.go internal/modules/git/module.go internal/modules/git/repo.go docs/superpowers/specs/2026-11-02-mtm-design.md
git commit -m "Report only the user's own local git work, without asking for pushes

The first real scan reported the clones asdf and Claude Code manage as
work that exists only on this Mac, and told the user to push secret .env
files. Migration Assistant copies both and verify checks them. Tool-managed
clones are now skipped like package caches, repos whose only local work is
ignored secret files share one info line, and the warning explains the
copy and SAFE TO WIPE instead of asking for a push."
```

---

### Task 2: macOS's placeholder tools count as not installed

**Files:**
- Modify: `internal/modules/toolchains/toolchains.go`, and `docs/superpowers/specs/2026-11-02-mtm-design.md` (§9.4)
- Test: `internal/modules/toolchains/toolchains_test.go` (modify)

**Interfaces:**
- Consumes: `version`, `lookPath`, and the `newFixture` test helper (Stage 2).
- Produces: `appleToolsDir`, a package variable equal to `"/usr/bin"`, which tests override; and the constant `unknownVersion = "unknown"`.

- [ ] **Step 1: Write the failing tests**

`internal/modules/toolchains/toolchains_test.go`:
```diff
--- a/internal/modules/toolchains/toolchains_test.go
+++ b/internal/modules/toolchains/toolchains_test.go
@@ -190,3 +190,42 @@ func TestScanListsTools(t *testing.T) {
 		t.Errorf("Scan = %+v, %v", findings, err)
 	}
 }
+
+// TestApplePlaceholdersCountAsNotInstalled: macOS's /usr/bin/java with no
+// JDK fails to report a version. That's a tool that isn't installed, so it
+// isn't recorded, while a working tool in the same folder is.
+func TestApplePlaceholdersCountAsNotInstalled(t *testing.T) {
+	f := newFixture(t, "/opt/homebrew/bin/fish")
+	apple := t.TempDir()
+	old := appleToolsDir
+	appleToolsDir = apple
+	t.Cleanup(func() { appleToolsDir = old })
+	for _, name := range []string{"java", "python3"} {
+		testutil.WriteFile(t, filepath.Join(apple, name), "#!/bin/sh\n")
+		os.Chmod(filepath.Join(apple, name), 0o755)
+	}
+	f.fake.Responses[pathCommand("/opt/homebrew/bin/fish").String()] = runner.Response{Stdout: pathMarker + apple + "\n"}
+	f.fake.Responses[filepath.Join(apple, "java")+" -version"] = runner.Response{Code: 1, Stderr: "The operation couldn’t be completed. Unable to locate a Java Runtime.\n"}
+	f.fake.Responses[filepath.Join(apple, "python3")+" --version"] = runner.Response{Stdout: "Python 3.9.6\n"}
+	inv, err := Module{User: "dev"}.collect(f.env(nil))
+	if err != nil {
+		t.Fatal(err)
+	}
+	if want := []Tool{{Name: "python3", Path: filepath.Join(apple, "python3"), Version: "Python 3.9.6"}}; !slices.Equal(inv.Tools, want) {
+		t.Errorf("tools = %+v, want only python3", inv.Tools)
+	}
+}
+
+// TestAFailingToolOutsideAppleFolderStaysUnknown keeps a real tool that
+// can't report its version, so verify still requires it.
+func TestAFailingToolOutsideAppleFolderStaysUnknown(t *testing.T) {
+	f := newFixture(t, "/opt/homebrew/bin/fish")
+	f.fake.Responses[filepath.Join(f.bin2, "go")+" version"] = runner.Response{Code: 1}
+	inv, err := Module{User: "dev"}.collect(f.env(nil))
+	if err != nil {
+		t.Fatal(err)
+	}
+	if !slices.Contains(inv.Tools, Tool{Name: "go", Path: "~/bin/go", Version: "unknown"}) {
+		t.Errorf("tools = %+v, want go kept as unknown", inv.Tools)
+	}
+}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/modules/toolchains/`
Expected: FAIL to build, because `appleToolsDir` doesn't exist yet:
```text
internal/modules/toolchains/toolchains_test.go:200:9: undefined: appleToolsDir
FAIL	github.com/momentmaker/mtm/internal/modules/toolchains [build failed]
```

- [ ] **Step 3: Implement**

`internal/modules/toolchains/toolchains.go`:
```diff
--- a/internal/modules/toolchains/toolchains.go
+++ b/internal/modules/toolchains/toolchains.go
@@ -23,6 +23,7 @@ const (
 	pathMarker     = "__MTM_PATH__="
 	shellTimeout   = 20 * time.Second
 	versionTimeout = 10 * time.Second
+	unknownVersion = "unknown"
 )
 
 type tool struct {
@@ -30,6 +31,11 @@ type tool struct {
 	args []string
 }
 
+// appleToolsDir holds macOS's placeholders for java, python3, and git,
+// which only offer to install the real tool. A variable so tests can stand
+// in for it.
+var appleToolsDir = "/usr/bin"
+
 var known = []tool{
 	{"brew", []string{"--version"}},
 	{"git", []string{"--version"}},
@@ -90,7 +96,13 @@ func (m Module) collect(env module.Env) (Inventory, error) {
 		if abs == "" {
 			continue
 		}
-		inv.Tools = append(inv.Tools, Tool{Name: t.name, Path: homeRelative(env.Home, abs), Version: version(env, abs, t.args, path)})
+		v := version(env, abs, t.args, path)
+		if v == unknownVersion && filepath.Dir(abs) == appleToolsDir {
+			// A placeholder that can't report a version is a tool that isn't
+			// installed, not one to require on the new Mac.
+			continue
+		}
+		inv.Tools = append(inv.Tools, Tool{Name: t.name, Path: homeRelative(env.Home, abs), Version: v})
 	}
 	return inv, nil
 }
@@ -156,7 +168,7 @@ func version(env module.Env, abs string, args []string, path string) string {
 	defer cancel()
 	out, err := env.Run.Run(ctx, runner.Command{Name: abs, Args: args, Dir: env.Home, Env: append([]string{"PATH=" + path}, quietHomebrew...)})
 	if err != nil {
-		return "unknown"
+		return unknownVersion
 	}
 	if line := firstLine(out.Stdout); line != "" {
 		return line
@@ -164,7 +176,7 @@ func version(env module.Env, abs string, args []string, path string) string {
 	if line := firstLine(out.Stderr); line != "" {
 		return line
 	}
-	return "unknown"
+	return unknownVersion
 }
 
 func firstLine(b []byte) string {
```

- [ ] **Step 4: Update the spec**

`docs/superpowers/specs/2026-11-02-mtm-design.md`:
```diff
--- a/docs/superpowers/specs/2026-11-02-mtm-design.md
+++ b/docs/superpowers/specs/2026-11-02-mtm-design.md
@@ -337,6 +337,7 @@ Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0`
     - other shells use `-l -c`.
   - The shell prints the PATH after a marker, so stray output from startup files and a non-zero exit after printing don't matter. The shell has a 20-second timeout.
   - Each tool is then found in that PATH and run directly (`--version`; `go version`; `java -version` on stderr), with a 10-second timeout.
+  - macOS keeps placeholders for `java`, `python3`, and `git` in `/usr/bin` that only offer to install the real tool. One there that fails to report a version counts as not installed, so it isn't recorded or required on the new Mac.
 - Paths inside the home folder are recorded as `~/…`, so they compare across usernames.
 - The global `~/.tool-versions` and `mise` config are dotfiles, which the `files` module already fingerprints.
 - `verify` requires the same paths and versions on the new Mac. A difference, a missing tool, or a login shell that no longer prints a PATH is a fail, with both values shown.
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
git add internal/modules/toolchains/toolchains_test.go internal/modules/toolchains/toolchains.go docs/superpowers/specs/2026-11-02-mtm-design.md
git commit -m "Count macOS's placeholder tools as not installed

The first real scan listed java as unknown: macOS's /usr/bin/java with no
JDK only offers to install one. Recording it would make verify require a
placeholder. A /usr/bin tool that can't report its version now counts as
not installed, while a real tool elsewhere that fails stays unknown and
required."
```
