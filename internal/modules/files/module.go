// Package files records the home folder on the old Mac and checks the copy
// on the new Mac.
package files

import (
	"fmt"
	"strings"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
)

const (
	Name      = "files"
	maxListed = 20
)

type Module struct{}

func New() Module { return Module{} }

func (Module) Name() string { return Name }

func (Module) Prepare(env module.Env, st *stage.Stage) (module.Result, error) {
	f, err := st.Create(manifest.RecordName)
	if err != nil {
		return module.Result{}, err
	}
	rw := manifest.NewRecordWriter(f)
	stats, err := writeRecord(env.Ctx, env.Home, rw, env.Deep)
	if err == nil {
		err = rw.Close()
	}
	if err != nil {
		f.Abort()
		return module.Result{}, err
	}
	if err := f.Commit(); err != nil {
		return module.Result{}, err
	}
	done := []string{fmt.Sprintf("recorded %d items, %d fingerprinted", stats.Entries, stats.Fingerprinted)}
	if stats.Cloud > 0 {
		done = append(done, fmt.Sprintf("left %d cloud-only items untouched", stats.Cloud))
	}
	if n := countAppState(stats.Unreadable); n > 0 {
		done = append(done, fmt.Sprintf("could not read %s in ~/Library; they will not be checked", plural(n, "folder", "folders")))
	}
	return module.Result{Inventory: stats, Done: done, Problems: prepareProblems(stats)}, nil
}

// Finish has nothing to restore: Migration Assistant copies the files.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}

// prepareProblems turns what the walk could not cover into findings.
// Unreadable critical data blocks, because verify can never vouch for it.
func prepareProblems(stats Stats) []module.Finding {
	var problems []module.Finding
	var unreadable pathList
	for _, p := range stats.Unreadable {
		if area(p) == manifest.AreaCritical {
			unreadable.add(displayPath(p))
		}
	}
	if unreadable.len() > 0 {
		problems = append(problems, module.Finding{
			Module:   Name,
			ID:       "unreadable",
			Severity: module.SeverityBlocker,
			Handler:  module.Manual,
			Title:    "mtm could not read some of your files, so mtm verify can never say SAFE TO WIPE",
			Detail:   "Unreadable: " + unreadable.String(),
			Fix:      accessFix("mtm prepare"),
		})
	}
	if n := stats.outside.len(); n > 0 {
		problems = append(problems, module.Finding{
			Module:   Name,
			ID:       "outside-links",
			Severity: module.SeverityWarn,
			Handler:  module.Manual,
			Title:    plural(n, "link in your home folder leads", "links in your home folder lead") + " outside it",
			Detail:   stats.outside.String(),
			Fix:      "mtm only checks your home folder. Make sure those locations are copied or backed up before you erase this Mac.",
		})
	}
	if stats.mounts.len() > 0 {
		problems = append(problems, module.Finding{
			Module:   Name,
			ID:       "mounts",
			Severity: module.SeverityWarn,
			Handler:  module.Manual,
			Title:    "Separate volumes in your home folder: mtm doesn't check them",
			Detail:   stats.mounts.String(),
			Fix:      "If one of these is on this Mac's internal disk, back it up before you erase this Mac.",
		})
	}
	return problems
}

func accessFix(command string) string {
	return "Give your terminal Full Disk Access (System Settings > Privacy & Security > Full Disk Access). " +
		"If a folder belongs to another user, such as root, take it back with: sudo chown -R $(id -un) ~/<folder>. " +
		"Then run " + command + " again."
}

func countAppState(paths []string) int {
	n := 0
	for _, p := range paths {
		if area(p) == manifest.AreaAppState {
			n++
		}
	}
	return n
}

// pathList keeps the first maxListed paths it is given and counts the
// rest, so a finding stays readable and memory stays bounded.
type pathList struct {
	shown []string
	more  int
}

func (l *pathList) add(p string) {
	if len(l.shown) < maxListed {
		l.shown = append(l.shown, p)
		return
	}
	l.more++
}

func (l pathList) len() int { return len(l.shown) + l.more }

func (l pathList) String() string {
	s := strings.Join(l.shown, ", ")
	if l.more > 0 {
		s += fmt.Sprintf(", and %d more", l.more)
	}
	return s
}

func displayPath(rel string) string { return "~/" + rel }

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
