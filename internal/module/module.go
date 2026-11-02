// Package module defines the contract every mtm module implements.
package module

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
)

type Machine string

const (
	OldMac Machine = "old"
	NewMac Machine = "new"
)

// Env is what a module gets to work with. Manifest is the record: on the
// new Mac, the one verify and finish check against; during prepare, this
// Mac's previous record (empty on the first run); nil during scan.
type Env struct {
	Ctx      context.Context
	Run      runner.Runner
	Home     string
	Machine  Machine
	Deep     bool
	Manifest *manifest.Manifest
}

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarn    Severity = "warn"
	SeverityBlocker Severity = "blocker"
)

type Handler string

const (
	ByMigrationAssistant Handler = "migration-assistant"
	ByMtm                Handler = "mtm"
	Manual               Handler = "manual"
)

type Finding struct {
	Module   string   `json:"module"`
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Handler  Handler  `json:"handler"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Fix      string   `json:"fix,omitempty"`
}

type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusWarn Status = "warn"
	StatusInfo Status = "info"
)

type Check struct {
	Module   string   `json:"module"`
	ID       string   `json:"id"`
	Status   Status   `json:"status"`
	Critical bool     `json:"critical"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Paths    []string `json:"paths,omitempty"`
}

// Result is what Prepare and Finish return. Inventory is stored in the
// manifest under the module's name. A Problems entry with SeverityBlocker
// makes the run NOT READY.
type Result struct {
	Inventory any
	Done      []string
	Problems  []Finding
}

type Module interface {
	Name() string
	Scan(env Env) ([]Finding, error)
	Prepare(env Env, st *stage.Stage) (Result, error)
	Finish(env Env, st *stage.Stage) (Result, error)
	Verify(env Env, st *stage.Stage) ([]Check, error)
}

// Undoer is implemented by modules whose prepare changes the old Mac, so
// prepare --undo can put it back.
type Undoer interface {
	Undo(env Env, st *stage.Stage) (Result, error)
}

var ErrNotPrepared = errors.New("not recorded by mtm prepare on the old Mac")

// LoadInventory decodes the named module's inventory from env.Manifest and
// returns the time that module was prepared.
func LoadInventory(env Env, name string, v any) (time.Time, error) {
	if env.Manifest == nil {
		return time.Time{}, fmt.Errorf("%s: %w", name, ErrNotPrepared)
	}
	at, err := env.Manifest.Module(name, v)
	if errors.Is(err, manifest.ErrNoModule) {
		return time.Time{}, fmt.Errorf("%s: %w", name, ErrNotPrepared)
	}
	return at, err
}

func HasBlocker(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == SeverityBlocker {
			return true
		}
	}
	return false
}
