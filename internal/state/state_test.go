package state

import (
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/stage"
)

func TestLoadMissingIsZero(t *testing.T) {
	st, _ := stage.Open(t.TempDir())
	s, err := Load(st)
	if err != nil || s.LastVerify != nil {
		t.Errorf("Load = %+v, %v; want zero state", s, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	st, _ := stage.Open(t.TempDir())
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if err := Save(st, State{LastVerify: &VerifyResult{At: at, Safe: true}}); err != nil {
		t.Fatal(err)
	}
	s, err := Load(st)
	if err != nil || s.LastVerify == nil || !s.LastVerify.Safe || !s.LastVerify.At.Equal(at) {
		t.Errorf("Load = %+v, %v", s, err)
	}
}

func TestSaveLoadKeepsTheChecklist(t *testing.T) {
	st, _ := stage.Open(t.TempDir())
	var s State
	s.Checklist.Add(checklist.Item{ID: "system:license:dash", Module: "system", Title: "Reactivate Dash"})
	s.Checklist.Skip("system:license:dash", "no longer use it", time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
	if err := Save(st, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(st)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := got.Checklist.Find("system:license:dash")
	if !ok || e.Status != checklist.Skipped || e.Reason != "no longer use it" {
		t.Errorf("entry = %+v, %v", e, ok)
	}
}

func TestRetirementStepsAreKeptApart(t *testing.T) {
	st, _ := stage.Open(t.TempDir())
	var s State
	s.Checklist.Add(checklist.Item{ID: "retire:erase", Module: "retire", Title: "Erase this Mac"})
	if err := SaveFile(st, RetireFileName, s); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(st); err != nil || len(got.Checklist.Entries) != 0 {
		t.Errorf("state.json = %+v, %v; want the retirement step kept out of it", got, err)
	}
	if got, err := LoadFile(st, RetireFileName); err != nil || len(got.Checklist.Entries) != 1 {
		t.Errorf("retire.json = %+v, %v; want the step", got, err)
	}
}
