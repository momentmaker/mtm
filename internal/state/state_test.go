package state

import (
	"testing"
	"time"

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
