package git

import (
	"context"
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
