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
