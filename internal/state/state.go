// Package state keeps mtm's progress on the new Mac in ~/.mtm/state.json.
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

const FileName = "state.json"

type VerifyResult struct {
	At   time.Time `json:"at"`
	Safe bool      `json:"safe"`
}

// State holds the new Mac's checklist, with what the user did with each
// step, and the last verify result.
type State struct {
	LastVerify *VerifyResult  `json:"last_verify,omitempty"`
	Checklist  checklist.List `json:"checklist,omitzero"`
}

func Load(st *stage.Stage) (State, error) {
	path := st.Path(FileName)
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

func Save(st *stage.Stage, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return st.WriteFile(FileName, data)
}
