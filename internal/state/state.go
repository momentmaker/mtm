// Package state keeps mtm's progress in ~/.mtm: the new Mac's in
// state.json, and the old Mac's retirement steps in retire.json.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/stage"
)

// RetireFileName holds the retirement steps, apart from state.json, so a
// step closed on the old Mac can never count toward the new Mac's
// verdict, even if Migration Assistant runs again and copies ~/.mtm.
const (
	FileName       = "state.json"
	RetireFileName = "retire.json"
)

type VerifyResult struct {
	At   time.Time `json:"at"`
	Safe bool      `json:"safe"`
}

// State holds a checklist, with what the user did with each step, and in
// state.json the last verify result.
type State struct {
	LastVerify *VerifyResult  `json:"last_verify,omitempty"`
	Checklist  checklist.List `json:"checklist,omitzero"`
}

func Load(st *stage.Stage) (State, error) { return LoadFile(st, FileName) }

func Save(st *stage.Stage, s State) error { return SaveFile(st, FileName, s) }

// LoadFile reads the named state file; a missing one is empty.
func LoadFile(st *stage.Stage, name string) (State, error) {
	path := st.Path(name)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

func SaveFile(st *stage.Stage, name string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	return st.WriteFile(name, data)
}
