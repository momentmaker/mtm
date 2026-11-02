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
	"github.com/momentmaker/mtm/internal/words"
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
		problems = append(problems, words.Count(len(missing), "recorded commit is", "recorded commits are")+" missing")
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
