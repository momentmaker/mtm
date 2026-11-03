package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
)

// Retire lists what changed in the home folder since prepare: files that
// differ from the record, and files it doesn't have. Migration Assistant
// may have copied each before or after it changed, so the user decides
// what to copy over. Deleted files aren't listed; verify reports what the
// new Mac is missing.
func (m Module) Retire(env module.Env, st *stage.Stage) (module.Retirement, error) {
	var stats Stats
	if _, err := module.LoadInventory(env, Name, &stats); err != nil {
		return module.Retirement{}, err
	}
	f, err := os.Open(st.Path(manifest.RecordName))
	if err != nil {
		return module.Retirement{}, fmt.Errorf("open file record: %w", err)
	}
	defer f.Close()
	rr, err := manifest.NewRecordReader(f)
	if err != nil {
		return module.Retirement{}, err
	}
	t, err := m.changes(env, rr)
	if err != nil {
		return module.Retirement{}, err
	}
	return module.Retirement{Changes: t.checks()}, nil
}

// recordCursor reads the record one entry ahead, so the walk can catch it
// up to each path it visits.
type recordCursor struct {
	rr   *manifest.RecordReader
	cur  manifest.Entry
	done bool
}

func (c *recordCursor) advance() error {
	e, err := c.rr.Next()
	if errors.Is(err, io.EOF) {
		c.done = true
		return nil
	}
	c.cur = e
	return err
}

// at returns the record's entry for rel, if it has one. The record is in
// walk order, so entries before rel are paths deleted since prepare.
func (c *recordCursor) at(rel string) (manifest.Entry, bool, error) {
	for !c.done && walkBefore(c.cur.Path, rel) {
		if err := c.advance(); err != nil {
			return manifest.Entry{}, false, err
		}
	}
	if c.done || c.cur.Path != rel {
		return manifest.Entry{}, false, nil
	}
	e := c.cur
	return e, true, c.advance()
}

// walkBefore reports whether the walk reaches a before b. It visits each
// folder's entries in name order, and an entry's contents before the next
// entry, so "a/b" comes before "a.txt" although it sorts after it as a
// string.
func walkBefore(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

func (m Module) changes(env module.Env, rr *manifest.RecordReader) (*changeTally, error) {
	c := &recordCursor{rr: rr}
	if err := c.advance(); err != nil {
		return nil, err
	}
	t := newChangeTally()
	unseen := ""
	err := walkHome(env.Ctx, env.Home, excluded, func(it item) error {
		rec, recorded, err := c.at(it.rel)
		if err != nil {
			return err
		}
		if unseen != "" && strings.HasPrefix(it.rel, unseen+"/") {
			return nil
		}
		// Prepare already reported what it couldn't read. With no recorded
		// size or time there is nothing to compare, and nothing inside an
		// unread folder was recorded. The same goes for a folder that was
		// cloud-only or on another volume then: prepare didn't go inside.
		if recorded && rec.Type == manifest.TypeUnreadable {
			if it.info != nil && it.info.IsDir() {
				unseen = it.rel
			}
			return nil
		}
		if recorded && (rec.Type == manifest.TypeCloud || rec.Type == manifest.TypeMount) && it.info != nil && it.info.IsDir() {
			unseen = it.rel
			return nil
		}
		if m.isVolatile(it.rel) {
			return nil
		}
		switch {
		case it.unreadable:
			t.add(it.rel, unreadableNow)
		case it.mount, it.cloud, it.info.IsDir():
			// Nothing to compare: another volume, a file whose data is in
			// the cloud, or a folder, whose new files are listed themselves.
		case !recorded:
			t.add(it.rel, added)
		case differs(rec, it):
			t.add(it.rel, changed)
		}
		return nil
	})
	return t, err
}

// differs compares a file or link with its record. A file that was in the
// cloud at prepare and is downloaded now keeps its size and time.
func differs(rec manifest.Entry, it item) bool {
	if it.info.Mode()&fs.ModeSymlink != 0 {
		return rec.Type != manifest.TypeLink || rec.Target != it.target
	}
	if rec.Type != manifest.TypeFile && rec.Type != manifest.TypeCloud {
		return true
	}
	return rec.Size != it.info.Size() || rec.MTime != it.info.ModTime().UnixNano()
}

type changeKind int

const (
	changed changeKind = iota
	added
	unreadableNow
)

var changeLabels = []struct {
	kind  changeKind
	label string
}{
	{changed, "changed"},
	{added, "new"},
	{unreadableNow, "couldn't be read"},
}

type changeGroup struct {
	critical bool
	counts   map[changeKind]int
	paths    []string
	extra    int
}

type changeTally struct {
	groups map[string]*changeGroup
	order  []string
}

func newChangeTally() *changeTally { return &changeTally{groups: map[string]*changeGroup{}} }

func (t *changeTally) add(rel string, k changeKind) {
	name := groupOf(rel, k == unreadableNow)
	g := t.groups[name]
	if g == nil {
		g = &changeGroup{counts: map[changeKind]int{}}
		t.groups[name] = g
		t.order = append(t.order, name)
	}
	g.critical = g.critical || isCritical(area(rel))
	g.counts[k]++
	if len(g.paths) < maxPathsPerGroup {
		g.paths = append(g.paths, rel)
	} else {
		g.extra++
	}
}

// checks has one per folder group, as verify does. Changes to files
// outside ~/Library are a warning; changes inside it are app state, which
// apps rewrite all the time.
func (t *changeTally) checks() []module.Check {
	out := make([]module.Check, 0, len(t.order))
	for _, name := range t.order {
		g := t.groups[name]
		var parts []string
		for _, l := range changeLabels {
			if n := g.counts[l.kind]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, l.label))
			}
		}
		if g.extra > 0 {
			parts = append(parts, fmt.Sprintf("%d more paths not listed", g.extra))
		}
		status := module.StatusInfo
		if g.critical {
			status = module.StatusWarn
		}
		out = append(out, module.Check{Module: Name, ID: Name + ":" + name, Status: status, Title: name, Detail: strings.Join(parts, ", "), Paths: g.paths})
	}
	return out
}
