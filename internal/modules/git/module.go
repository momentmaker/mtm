// Package git records repositories on the old Mac and checks that every
// recorded commit and untracked file arrived on the new Mac.
package git

import (
	"fmt"
	"strings"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
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
		Title:    words.Count(len(repos), "git repository", "git repositories") + " found",
		Detail:   "Migration Assistant copies them. mtm checks that every recorded commit and untracked file arrived.",
	}}
	var secretsOnly []string
	for _, rel := range repos {
		if err := env.Ctx.Err(); err != nil {
			return nil, err
		}
		r, err := collect(env, rel, false)
		if err != nil {
			findings = append(findings, unreadableRepo(rel, err))
			continue
		}
		if rel == "." {
			findings = append(findings, module.Finding{
				Module:   Name,
				ID:       "home-repo",
				Severity: module.SeverityWarn,
				Handler:  module.ByMigrationAssistant,
				Title:    "Your home folder is a git repository",
				Detail:   "mtm checks its commits, and the files check covers the files in it.",
			})
		}
		if r.tooManyToFingerprint() {
			findings = append(findings, tooManyFiles(r))
		}
		if r.onlySecrets() {
			secretsOnly = append(secretsOnly, fmt.Sprintf("%s (%d)", displayPath(rel), len(r.Important)))
		} else if risks := r.risks(); len(risks) > 0 {
			findings = append(findings, module.Finding{
				Module:   Name,
				ID:       "local-only:" + rel,
				Severity: module.SeverityWarn,
				Handler:  module.ByMigrationAssistant,
				Title:    displayPath(rel) + ": work that exists only on this Mac",
				Detail:   strings.Join(risks, ", "),
				Fix:      localOnlyFix,
			})
		}
	}
	if len(secretsOnly) > 0 {
		findings = append(findings, secretFiles(secretsOnly))
	}
	return findings, nil
}

// localOnlyFix doesn't ask for a push: Migration Assistant copies the work
// and verify checks it, so what matters is keeping this Mac until then.
const localOnlyFix = "Migration Assistant copies it, and mtm verify checks every commit and file arrived. Until then this Mac has the only copy: push it first if you want another, and don't erase this Mac before mtm verify says SAFE TO WIPE."

// secretFiles gathers the repos whose only local work is ignored files
// that look like secrets, such as .env files. They belong outside git, so
// one line says they're copied and checked, instead of a warning per repo.
func secretFiles(repos []string) module.Finding {
	return module.Finding{
		Module:   Name,
		ID:       "secret-files",
		Severity: module.SeverityInfo,
		Handler:  module.ByMigrationAssistant,
		Title:    words.Count(len(repos), "repo has", "repos have") + " ignored files that look like secrets or local config",
		Detail:   "Migration Assistant copies them, and mtm verify checks each one arrived: " + strings.Join(repos, ", "),
	}
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
		if r.tooManyToFingerprint() {
			res.Problems = append(res.Problems, tooManyFiles(r))
		}
		inv.Repos = append(inv.Repos, r)
	}
	res.Inventory = inv
	res.Done = []string{"recorded " + words.Count(len(inv.Repos), "repository", "repositories")}
	return res, nil
}

// Finish has nothing to restore: Migration Assistant copies the repos.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}

func tooManyFiles(r Repo) module.Finding {
	return module.Finding{
		Module:   Name,
		ID:       "too-many-files:" + r.Path,
		Severity: module.SeverityWarn,
		Handler:  module.ByMigrationAssistant,
		Title:    displayPath(r.Path) + ": too many untracked files to fingerprint",
		Detail: fmt.Sprintf("%d untracked and important ignored files, more than %d. mtm checks this repository's commits, and the files check still records these files by size and time.",
			len(r.Untracked)+len(r.Important), maxFingerprintedFiles),
		Fix: "If they are build output or downloads, add them to .gitignore. Otherwise there is nothing to do.",
	}
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
