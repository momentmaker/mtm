package module

import (
	"errors"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
)

func TestLoadInventoryNeedsManifest(t *testing.T) {
	var v map[string]int
	if _, err := LoadInventory(Env{}, "files", &v); !errors.Is(err, ErrNotPrepared) {
		t.Errorf("err = %v, want ErrNotPrepared", err)
	}
}

func TestLoadInventoryMissingModule(t *testing.T) {
	var v map[string]int
	if _, err := LoadInventory(Env{Manifest: manifest.New("t")}, "files", &v); !errors.Is(err, ErrNotPrepared) {
		t.Errorf("err = %v, want ErrNotPrepared", err)
	}
}

func TestLoadInventoryDecodes(t *testing.T) {
	m := manifest.New("t")
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	if err := m.SetModule("files", at, map[string]int{"entries": 7}); err != nil {
		t.Fatal(err)
	}
	var v map[string]int
	got, err := LoadInventory(Env{Manifest: m}, "files", &v)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(at) || v["entries"] != 7 {
		t.Errorf("got %v %v", got, v)
	}
}

func TestHasBlocker(t *testing.T) {
	if HasBlocker([]Finding{{Severity: SeverityWarn}, {Severity: SeverityInfo}}) {
		t.Error("warn and info reported as blocker")
	}
	if !HasBlocker([]Finding{{Severity: SeverityWarn}, {Severity: SeverityBlocker}}) {
		t.Error("blocker not detected")
	}
}
