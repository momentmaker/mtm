// Package files records the home folder on the old Mac and checks the copy
// on the new Mac.
package files

import (
	"fmt"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
)

const Name = "files"

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
	if n := len(stats.Unreadable); n > 0 {
		done = append(done, fmt.Sprintf("could not read %d folders; they will not be checked", n))
	}
	return module.Result{Inventory: stats, Done: done}, nil
}

// Finish has nothing to restore: Migration Assistant copies the files.
func (Module) Finish(module.Env, *stage.Stage) (module.Result, error) {
	return module.Result{}, nil
}
