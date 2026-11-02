// Package manifest defines manifest.json and the file record, the two
// files prepare writes and verify reads.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/momentmaker/mtm/internal/stage"
)

const (
	Schema     = 1
	FileName   = "manifest.json"
	RecordName = "files.jsonl.gz"
)

var (
	ErrNewerSchema = errors.New("the record was written by a newer mtm; upgrade mtm on this Mac")
	ErrNoModule    = errors.New("module not in manifest")
)

type Manifest struct {
	Schema       int                     `json:"schema"`
	MtmVersion   string                  `json:"mtm_version"`
	MacOSVersion string                  `json:"macos_version"`
	MacOSBuild   string                  `json:"macos_build"`
	Hostname     string                  `json:"hostname"`
	MachineID    string                  `json:"machine_id"`
	PreparedAt   time.Time               `json:"prepared_at"`
	Modules      map[string]ModuleRecord `json:"modules"`
}

type ModuleRecord struct {
	PreparedAt time.Time       `json:"prepared_at"`
	Inventory  json.RawMessage `json:"inventory"`
}

func New(mtmVersion string) *Manifest {
	return &Manifest{Schema: Schema, MtmVersion: mtmVersion, Modules: map[string]ModuleRecord{}}
}

func Load(st *stage.Stage) (*Manifest, error) {
	path := st.Path(FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Schema > Schema {
		return nil, fmt.Errorf("%w (record schema %d, this mtm reads up to %d)", ErrNewerSchema, m.Schema, Schema)
	}
	if m.Modules == nil {
		m.Modules = map[string]ModuleRecord{}
	}
	return &m, nil
}

func Save(st *stage.Stage, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	return st.WriteFile(FileName, data)
}

func (m *Manifest) SetModule(name string, at time.Time, inventory any) error {
	raw, err := json.Marshal(inventory)
	if err != nil {
		return fmt.Errorf("encode %s inventory: %w", name, err)
	}
	m.Modules[name] = ModuleRecord{PreparedAt: at, Inventory: raw}
	if at.After(m.PreparedAt) {
		m.PreparedAt = at
	}
	return nil
}

func (m *Manifest) Module(name string, inventory any) (time.Time, error) {
	rec, ok := m.Modules[name]
	if !ok {
		return time.Time{}, fmt.Errorf("%s: %w", name, ErrNoModule)
	}
	if err := json.Unmarshal(rec.Inventory, inventory); err != nil {
		return time.Time{}, fmt.Errorf("decode %s inventory: %w", name, err)
	}
	return rec.PreparedAt, nil
}
