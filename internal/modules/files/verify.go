package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
)

const (
	HomeGroup        = "home folder files"
	maxPathsPerGroup = 200
)

func (Module) Verify(env module.Env, st *stage.Stage) ([]module.Check, error) {
	var stats Stats
	preparedAt, err := module.LoadInventory(env, Name, &stats)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(st.Path(manifest.RecordName))
	if err != nil {
		return nil, fmt.Errorf("open file record: %w", err)
	}
	defer f.Close()
	rr, err := manifest.NewRecordReader(f)
	if err != nil {
		return nil, err
	}
	prepared := preparedAt.UnixNano()
	t := newTally()
	for {
		if err := env.Ctx.Err(); err != nil {
			return nil, err
		}
		rec, err := rr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		obs, err := observe(env.Home, rec, prepared)
		if err != nil {
			return nil, err
		}
		t.add(rec, Classify(rec, obs, prepared, volatile.Match))
	}
	return t.checks(), nil
}

// observe looks at a recorded path on the new Mac. Only files, folders, and
// links are looked at; every other entry is judged from the record alone.
func observe(home string, rec manifest.Entry, preparedAt int64) (Observed, error) {
	if rec.Type != manifest.TypeFile && rec.Type != manifest.TypeDir && rec.Type != manifest.TypeLink {
		return Observed{}, nil
	}
	abs := filepath.Join(home, filepath.FromSlash(rec.Path))
	info, err := os.Lstat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return Observed{}, nil
	case errors.Is(err, fs.ErrPermission):
		return Observed{Exists: true, Unreadable: true}, nil
	case err != nil:
		return Observed{}, fmt.Errorf("inspect %s: %w", abs, err)
	}
	obs := Observed{Exists: true, Size: info.Size(), MTime: info.ModTime().UnixNano(), Dataless: fsinfo.IsDataless(info)}
	switch mode := info.Mode(); {
	case mode&fs.ModeSymlink != 0:
		obs.Type = manifest.TypeLink
		if obs.Target, err = os.Readlink(abs); err != nil {
			return Observed{}, fmt.Errorf("read link %s: %w", abs, err)
		}
		if rec.Resolves {
			_, err := os.Stat(abs)
			if errors.Is(err, fs.ErrPermission) {
				return Observed{Exists: true, Unreadable: true}, nil
			}
			obs.Resolves = err == nil
		}
	case mode.IsDir():
		obs.Type = manifest.TypeDir
	default:
		obs.Type = manifest.TypeFile
	}
	needsHash := rec.Hash != "" && obs.Type == manifest.TypeFile && !obs.Dataless && obs.Size == rec.Size && !changedAfterPrepare(rec, obs, preparedAt)
	if !needsHash {
		return obs, nil
	}
	obs.Hash, err = fsinfo.HashFile(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Observed{}, nil
	case errors.Is(err, fs.ErrPermission):
		return Observed{Exists: true, Unreadable: true}, nil
	case err != nil:
		return Observed{}, fmt.Errorf("fingerprint %s: %w", abs, err)
	}
	return obs, nil
}

type group struct {
	name     string
	critical bool
	status   module.Status
	counts   map[Outcome]int
	paths    []string
	extra    int
}

type tally struct {
	groups map[string]*group
	order  []string
}

func newTally() *tally { return &tally{groups: map[string]*group{}} }

func (t *tally) add(rec manifest.Entry, o Outcome) {
	name := groupOf(rec.Path, rec.Type == manifest.TypeDir || rec.Type == manifest.TypeUnreadable)
	g := t.groups[name]
	if g == nil {
		g = &group{name: name, status: module.StatusPass, counts: map[Outcome]int{}}
		t.groups[name] = g
		t.order = append(t.order, name)
	}
	g.critical = g.critical || isCritical(rec.Area)
	g.counts[o]++
	s := statusFor(o, rec.Area)
	g.raise(s)
	if s == module.StatusFail || s == module.StatusWarn {
		if len(g.paths) < maxPathsPerGroup {
			g.paths = append(g.paths, rec.Path)
		} else {
			g.extra++
		}
	}
}

func (t *tally) checks() []module.Check {
	out := make([]module.Check, 0, len(t.order))
	for _, name := range t.order {
		g := t.groups[name]
		out = append(out, module.Check{
			Module:   Name,
			ID:       Name + ":" + name,
			Status:   g.status,
			Critical: g.critical,
			Title:    name,
			Detail:   g.detail(),
			Paths:    g.paths,
		})
	}
	return out
}

// raise follows spec 8.4: fail beats warn, and info outcomes leave a group
// at pass.
func (g *group) raise(s module.Status) {
	switch {
	case s == module.StatusFail:
		g.status = module.StatusFail
	case s == module.StatusWarn && g.status != module.StatusFail:
		g.status = module.StatusWarn
	}
}

var outcomeLabels = []struct {
	outcome Outcome
	label   string
}{
	{Match, "match"},
	{ChangedAfterPrepare, "changed after prepare"},
	{MissingVolatile, "missing (expected)"},
	{CloudOnly, "cloud-only"},
	{NotChecked, "not checked (unreadable)"},
	{MissingAppState, "missing"},
	{MissingCritical, "missing"},
	{Corrupt, "corrupted"},
}

// unreadableCriticalLabel replaces the NotChecked label in critical groups
// and comes last, because it is why the group blocks and says how to fix it.
const unreadableCriticalLabel = "unreadable, not checked — give your terminal Full Disk Access (System Settings > Privacy & Security) or fix the folder's permissions, and run mtm verify again; if mtm prepare could not read them either, fix that on the old Mac and run mtm prepare first"

func (g *group) detail() string {
	var parts []string
	for _, l := range outcomeLabels {
		if g.critical && l.outcome == NotChecked {
			continue
		}
		if n := g.counts[l.outcome]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, l.label))
		}
	}
	if g.extra > 0 {
		parts = append(parts, fmt.Sprintf("%d more paths not listed", g.extra))
	}
	if n := g.counts[NotChecked]; g.critical && n > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", n, unreadableCriticalLabel))
	}
	return strings.Join(parts, ", ")
}

// groupOf names the check a path belongs to: its top-level folder, the
// first two levels inside Library, or HomeGroup for files directly in the
// home folder.
func groupOf(rel string, isDir bool) string {
	parts := strings.Split(rel, "/")
	if !isDir {
		parts = parts[:len(parts)-1]
	}
	switch {
	case len(parts) == 0:
		return HomeGroup
	case parts[0] == "Library" && len(parts) >= 2:
		return "Library/" + parts[1]
	}
	return parts[0]
}
