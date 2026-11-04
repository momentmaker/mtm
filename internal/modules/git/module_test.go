package git

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
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
		".asdf/plugins/nodejs/.git",
		".nvm/.git",
		".pyenv/.git",
		".rbenv/.git",
		".tmux/plugins/tpm/.git",
		".vim/plugged/x/.git",
		".claude/plugins/cache/x/1.0.0/.git",
		".oh-my-zsh/.git",
		".local/share/nvim/lazy/x/.git",
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

func TestDiscoverSkipsFoldersItCannotRead(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{"GitHub/app/.git", "broken/inner/.git"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	broken := filepath.Join(home, "broken")
	f := &finder{ctx: context.Background(), home: home, maxDepth: 6, otherVolume: sameVolume, readDir: func(dir string) ([]fs.DirEntry, error) {
		if dir == broken {
			return nil, &fs.PathError{Op: "open", Path: dir, Err: syscall.EIO}
		}
		return os.ReadDir(dir)
	}}
	got, err := f.find()
	if err != nil {
		t.Fatalf("find: %v, want the broken folder skipped", err)
	}
	if want := []string{"GitHub/app"}; !slices.Equal(got, want) {
		t.Errorf("find = %q, want %q", got, want)
	}
}

func sameVolume(fs.FileInfo) bool { return false }

func TestDiscoverStaysOnTheHomeVolume(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{"GitHub/app/.git", "mnt/repo/.git"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f := &finder{ctx: context.Background(), home: home, maxDepth: 6, readDir: os.ReadDir,
		otherVolume: func(info fs.FileInfo) bool { return info.Name() == "mnt" }}
	got, err := f.find()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"GitHub/app"}; !slices.Equal(got, want) {
		t.Errorf("find = %q, want %q", got, want)
	}
}

func TestDiscoverFailsWhenHomeUnreadable(t *testing.T) {
	home := t.TempDir()
	if err := os.Chmod(home, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(home, 0o755) })
	if repos, err := discover(context.Background(), home, 6); err == nil {
		t.Errorf("discover = %q, nil; want an error for an unreadable home folder", repos)
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

func TestScanDoesNotAskToPushWork(t *testing.T) {
	home := t.TempDir()
	seedRepo(t, home)
	findings, err := New().Scan(testEnv(home, nil))
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(findings, func(f module.Finding) bool { return f.ID == "local-only:GitHub/app" })
	if i < 0 {
		t.Fatalf("no local-only:GitHub/app finding in %+v", findings)
	}
	if strings.HasPrefix(findings[i].Fix, "Push it") || !strings.Contains(findings[i].Fix, "SAFE TO WIPE") {
		t.Errorf("fix = %q, want Migration Assistant's copy and verify explained, not a push asked for", findings[i].Fix)
	}
}

// pushedRepoWithSecret makes a repo at rel whose commits are all on its
// remote, and whose only work of its own is an ignored .env file.
func pushedRepoWithSecret(t *testing.T, home, rel string) {
	t.Helper()
	testutil.IsolateGit(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	testutil.Git(t, t.TempDir(), "init", "-q", "--bare", remote)
	repo := filepath.Join(home, rel)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "init", "-q", "-b", "main")
	testutil.WriteFile(t, filepath.Join(repo, ".gitignore"), ".env\n")
	testutil.Git(t, repo, "add", ".gitignore")
	testutil.Git(t, repo, "commit", "-q", "-m", "first")
	testutil.Git(t, repo, "remote", "add", "origin", remote)
	testutil.Git(t, repo, "push", "-q", "origin", "main")
	testutil.WriteFile(t, filepath.Join(repo, ".env"), "SECRET=1\n")
}

func TestScanGroupsReposWhoseOnlyWorkIsSecretFiles(t *testing.T) {
	home := t.TempDir()
	pushedRepoWithSecret(t, home, "GitHub/api")
	pushedRepoWithSecret(t, home, "GitHub/web")
	seedRepo(t, home)
	findings, err := New().Scan(testEnv(home, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"GitHub/api", "GitHub/web"} {
		if slices.ContainsFunc(findings, func(f module.Finding) bool { return f.ID == "local-only:"+rel }) {
			t.Errorf("%s got its own warning; want it in the grouped line", rel)
		}
	}
	i := slices.IndexFunc(findings, func(f module.Finding) bool { return f.ID == "secret-files" })
	if i < 0 {
		t.Fatalf("findings = %+v, want one secret-files line", findings)
	}
	f := findings[i]
	if f.Severity != module.SeverityInfo || f.Title != "2 repos have ignored files that look like secrets or local config" || !strings.Contains(f.Detail, "~/GitHub/api (1), ~/GitHub/web (1)") {
		t.Errorf("finding = %+v", f)
	}
	if !slices.ContainsFunc(findings, func(f module.Finding) bool { return f.ID == "local-only:GitHub/app" }) {
		t.Error("the repo with unpushed work lost its own warning")
	}
}

func TestScanKeepsAWarningForAnyRealLocalWork(t *testing.T) {
	const rel = "GitHub/api"
	cases := []struct {
		name string
		add  func(t *testing.T, repo string)
	}{
		{"unpushed commit", func(t *testing.T, repo string) {
			testutil.WriteFile(t, filepath.Join(repo, "new.txt"), "x\n")
			testutil.Git(t, repo, "add", "new.txt")
			testutil.Git(t, repo, "commit", "-q", "-m", "local")
		}},
		{"stash", func(t *testing.T, repo string) {
			testutil.WriteFile(t, filepath.Join(repo, ".gitignore"), ".env\n# edited\n")
			testutil.Git(t, repo, "stash", "push", "-q")
		}},
		{"tracked file edit", func(t *testing.T, repo string) {
			testutil.WriteFile(t, filepath.Join(repo, ".gitignore"), ".env\n# edited\n")
		}},
		{"untracked file", func(t *testing.T, repo string) {
			testutil.WriteFile(t, filepath.Join(repo, "notes.txt"), "x\n")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			pushedRepoWithSecret(t, home, rel)
			c.add(t, filepath.Join(home, rel))
			findings, err := New().Scan(testEnv(home, nil))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(findings, func(f module.Finding) bool { return f.ID == "local-only:"+rel }) {
				t.Errorf("findings = %+v, want its own local-only warning", findings)
			}
			if slices.ContainsFunc(findings, func(f module.Finding) bool {
				return f.ID == "secret-files" && strings.Contains(f.Detail, "~/"+rel)
			}) {
				t.Errorf("findings = %+v, want it out of the secret-files line", findings)
			}
		})
	}
}

func TestScanReportsUncommittedChanges(t *testing.T) {
	home := t.TempDir()
	repo := seedRepo(t, home)
	testutil.WriteFile(t, filepath.Join(repo, "README.md"), "hello, edited\n")
	findings, err := New().Scan(testEnv(home, nil))
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(findings, func(f module.Finding) bool { return f.ID == "local-only:GitHub/app" })
	if i < 0 || !strings.Contains(findings[i].Detail, "1 file with uncommitted changes") {
		t.Errorf("findings = %+v, want the modified README counted", findings)
	}
}

func TestRisksCountChangedTrackedFiles(t *testing.T) {
	r := Repo{Status: []string{
		"# branch.head main",
		"1 .M N... 100644 100644 100644 h1 h2 a.go",
		"2 R. N... 100644 100644 100644 h1 h2 R100 b.go\x00old.go",
		"u UU N... 100644 100644 100644 100644 h1 h2 h3 c.go",
		"? notes.txt",
	}, Untracked: []string{"notes.txt"}}
	if got := strings.Join(r.risks(), ", "); !strings.Contains(got, "3 files with uncommitted changes") {
		t.Errorf("risks = %q, want 3 files with uncommitted changes", got)
	}
}

func TestGitIgnoresRepoRedirectingEnvironment(t *testing.T) {
	home := t.TempDir()
	repo := seedRepo(t, home)
	want := testutil.Git(t, repo, "rev-parse", "HEAD")
	other := filepath.Join(t.TempDir(), "other")
	os.MkdirAll(other, 0o755)
	testutil.Git(t, other, "init", "-q", "-b", "main")
	testutil.Git(t, other, "commit", "-q", "--allow-empty", "-m", "elsewhere")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)

	res, err := New().Prepare(testEnv(home, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Inventory.(Inventory).Repos[0].HeadCommit; got != want {
		t.Errorf("recorded HEAD %s, want %s from ~/GitHub/app, not the repo GIT_DIR names", got, want)
	}
}

func TestGitCommandsNeverPromptOrFetch(t *testing.T) {
	cmd := gitCLI{dir: "/r"}.command("status")
	for _, want := range []string{"GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0"} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("env = %q, missing %s", cmd.Env, want)
		}
	}
	for _, want := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE"} {
		if !slices.Contains(cmd.Unset, want) {
			t.Errorf("unset = %q, missing %s", cmd.Unset, want)
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

// seedHomeRepo runs git init in the home folder itself, with one committed
// dotfile, an ignored .env, and a pile of ordinary files.
func seedHomeRepo(t *testing.T) string {
	t.Helper()
	testutil.IsolateGit(t)
	home := t.TempDir()
	for i := range 50 {
		testutil.WriteFile(t, filepath.Join(home, "Documents", fmt.Sprintf("f%02d.txt", i)), "x")
	}
	testutil.WriteFile(t, filepath.Join(home, "Library/Preferences/p.plist"), "plist\n")
	testutil.WriteFile(t, filepath.Join(home, ".gitignore"), ".env\n")
	testutil.WriteFile(t, filepath.Join(home, ".env"), "SECRET=1\n")
	testutil.WriteFile(t, filepath.Join(home, ".zshrc"), "export A=1\n")
	testutil.Git(t, home, "init", "-q", "-b", "main")
	testutil.Git(t, home, "add", ".zshrc", ".gitignore")
	testutil.Git(t, home, "commit", "-q", "-m", "dotfiles")
	return home
}

func TestHomeFolderRepoRecordsCommitsOnly(t *testing.T) {
	home := seedHomeRepo(t)
	res, err := New().Prepare(testEnv(home, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	i := slices.IndexFunc(inv.Repos, func(r Repo) bool { return r.Path == "." })
	if i < 0 {
		t.Fatalf("repos = %+v, want the home folder repo", inv.Repos)
	}
	r := inv.Repos[i]
	if len(r.Untracked) != 0 || len(r.Important) != 0 || len(r.Fingerprints) != 0 {
		t.Errorf("home repo listed %d untracked, %d important, %d fingerprints; want none", len(r.Untracked), len(r.Important), len(r.Fingerprints))
	}
	if r.HeadCommit == "" {
		t.Error("home repo commit not recorded")
	}
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), res.Inventory); err != nil {
		t.Fatal(err)
	}
	if c := verify(t, copyHome(t, home), m)["~"]; c.Status != module.StatusPass {
		t.Errorf("home repo check = %+v, want pass on an identical copy", c)
	}
}

func TestScanWarnsAboutHomeFolderRepo(t *testing.T) {
	home := seedHomeRepo(t)
	findings, err := New().Scan(testEnv(home, nil))
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(findings, func(f module.Finding) bool { return f.ID == "home-repo" })
	if i < 0 {
		t.Fatalf("findings = %+v, want a home-repo warning", findings)
	}
	if f := findings[i]; f.Severity != module.SeverityWarn || !strings.Contains(f.Title+" "+f.Detail, "mtm checks its commits, and the files check covers the files in it") {
		t.Errorf("finding = %+v", f)
	}
}

func lowerFingerprintLimit(t *testing.T, n int) {
	t.Helper()
	old := maxFingerprintedFiles
	maxFingerprintedFiles = n
	t.Cleanup(func() { maxFingerprintedFiles = old })
}

func TestRepoWithTooManyUntrackedFilesSkipsFingerprints(t *testing.T) {
	home := t.TempDir()
	repo := seedRepo(t, home)
	testutil.WriteFile(t, filepath.Join(repo, "more.txt"), "more\n")
	lowerFingerprintLimit(t, 2)

	res, err := New().Prepare(testEnv(home, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := res.Inventory.(Inventory).Repos[0]
	if len(r.Fingerprints) != 0 || len(r.Untracked) != 2 {
		t.Errorf("fingerprints = %v, untracked = %q; want no fingerprints and both untracked files listed", r.Fingerprints, r.Untracked)
	}
	isLimit := func(f module.Finding) bool {
		return f.ID == "too-many-files:GitHub/app" && f.Severity == module.SeverityWarn
	}
	if !slices.ContainsFunc(res.Problems, isLimit) {
		t.Errorf("prepare problems = %+v, want a too-many-files warning", res.Problems)
	}
	findings, err := New().Scan(testEnv(home, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(findings, isLimit) {
		t.Errorf("scan findings = %+v, want a too-many-files warning", findings)
	}
}

func TestRepoAtTheFingerprintLimitIsFingerprinted(t *testing.T) {
	home := t.TempDir()
	seedRepo(t, home)
	lowerFingerprintLimit(t, 2)
	res, err := New().Prepare(testEnv(home, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := res.Inventory.(Inventory).Repos[0]; len(r.Fingerprints) != 2 || len(res.Problems) != 0 {
		t.Errorf("fingerprints = %d, problems = %+v; want 2 fingerprints and no warning", len(r.Fingerprints), res.Problems)
	}
}
