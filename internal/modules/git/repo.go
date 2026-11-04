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
	"github.com/momentmaker/mtm/internal/words"
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

// redirectingEnv are variables that point git at a different repository,
// index, or object store than the one in g.dir. Inherited from the user's
// shell, they would make mtm record or check the wrong repo.
var redirectingEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE",
}

// gitEnv keeps git off the network (no lazy fetch of missing objects in a
// partial clone) and from waiting on a credential prompt.
var gitEnv = []string{"GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0"}

func (g gitCLI) command(args ...string) runner.Command {
	base := []string{"-C", g.dir, "--no-optional-locks", "-c", "core.fsmonitor=false"}
	return runner.Command{Name: "git", Args: append(base, args...), Unset: redirectingEnv, Env: gitEnv}
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
	status, err := g.output(statusArgs(rel)...)
	if err != nil {
		return r, err
	}
	var ignored []string
	r.Status, r.Untracked, ignored = parseStatus(status)
	r.Important = importantIgnored(ignored)
	if withFingerprints && !r.tooManyToFingerprint() {
		paths := append(append([]string(nil), r.Untracked...), r.Important...)
		if r.Fingerprints, err = fingerprint(abs, paths); err != nil {
			return r, err
		}
	}
	return r, nil
}

// statusArgs lists untracked and ignored files, except in a repo at the
// home folder itself. There they would be every file in the home folder,
// which the files module already records, and hashing them all would take
// hours.
func statusArgs(rel string) []string {
	if rel == "." {
		return []string{"status", "--porcelain=v2", "--branch", "-z", "--untracked-files=no"}
	}
	return []string{"status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all", "--ignored=matching"}
}

// maxFingerprintedFiles caps the untracked and important files fingerprinted
// per repo. Above it, the repo is usually a build or download folder, and
// the files module still records each file. It is a variable so tests can
// lower it.
var maxFingerprintedFiles = 5000

func (r Repo) tooManyToFingerprint() bool {
	return len(r.Untracked)+len(r.Important) > maxFingerprintedFiles
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
		out = append(out, words.Count(r.Unpushed, "commit", "commits")+" not on any remote")
	}
	if n := len(r.Stash); n > 0 {
		out = append(out, words.Count(n, "stash", "stashes"))
	}
	if n := r.changedTracked(); n > 0 {
		out = append(out, words.Count(n, "file with uncommitted changes", "files with uncommitted changes"))
	}
	if n := len(r.Untracked); n > 0 {
		out = append(out, words.Count(n, "untracked file", "untracked files"))
	}
	if n := len(r.Important); n > 0 {
		out = append(out, words.Count(n, "ignored file that looks like a secret or local config", "ignored files that look like secrets or local config"))
	}
	return out
}

// onlySecrets is true when the repo's only local work is ignored files that
// look like secrets or local config.
func (r Repo) onlySecrets() bool {
	return len(r.Important) > 0 && r.Unpushed == 0 && len(r.Stash) == 0 && r.changedTracked() == 0 && len(r.Untracked) == 0
}

// changedTracked counts the status records for tracked files with changes:
// ordinary (1), renamed or copied (2), and unmerged (u).
func (r Repo) changedTracked() int {
	n := 0
	for _, rec := range r.Status {
		if strings.HasPrefix(rec, "1 ") || strings.HasPrefix(rec, "2 ") || strings.HasPrefix(rec, "u ") {
			n++
		}
	}
	return n
}

func displayPath(rel string) string {
	if rel == "." {
		return "~"
	}
	return "~/" + rel
}
