package checklist

import (
	"errors"
	"testing"
	"time"
)

var (
	raycast = Item{ID: "permissions:com.raycast.macos:Accessibility", Module: "permissions", Title: "Give Raycast Accessibility access"}
	license = Item{ID: "system:license:dash", Module: "system", Title: "Reactivate Dash"}
	now     = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
)

func TestAddKeepsWhatTheUserDidAndRefreshesText(t *testing.T) {
	var l List
	if n := l.Add(raycast, license); n != 2 {
		t.Fatalf("added %d, want 2", n)
	}
	if err := l.Done(raycast.ID, now); err != nil {
		t.Fatal(err)
	}
	renamed := raycast
	renamed.Title = "Give Raycast Accessibility access again"
	if n := l.Add(renamed, license); n != 0 {
		t.Errorf("added %d on a re-run, want 0", n)
	}
	e, ok := l.Find(raycast.ID)
	if !ok || e.Status != Done || e.Title != renamed.Title || !e.At.Equal(now) {
		t.Errorf("entry = %+v", e)
	}
	if p := l.Pending(); len(p) != 1 || p[0].ID != license.ID {
		t.Errorf("pending = %+v", p)
	}
}

func TestSkipNeedsAReason(t *testing.T) {
	var l List
	l.Add(license)
	if err := l.Skip(license.ID, "  ", now); !errors.Is(err, ErrNoReason) {
		t.Errorf("err = %v, want ErrNoReason", err)
	}
	if err := l.Skip(license.ID, " sold my Dash license ", now); err != nil {
		t.Fatal(err)
	}
	if e, _ := l.Find(license.ID); e.Status != Skipped || e.Reason != "sold my Dash license" {
		t.Errorf("entry = %+v", e)
	}
	if len(l.Pending()) != 0 {
		t.Error("a skipped step is still pending")
	}
}

func TestDoneAfterSkipClearsTheReason(t *testing.T) {
	var l List
	l.Add(license)
	l.Skip(license.ID, "later", now)
	l.Done(license.ID, now)
	if e, _ := l.Find(license.ID); e.Status != Done || e.Reason != "" {
		t.Errorf("entry = %+v", e)
	}
}

func TestUnknownIDsAreErrors(t *testing.T) {
	var l List
	if err := l.Done("nope", now); !errors.Is(err, ErrUnknownItem) {
		t.Errorf("Done err = %v", err)
	}
	if err := l.Skip("nope", "why", now); !errors.Is(err, ErrUnknownItem) {
		t.Errorf("Skip err = %v", err)
	}
}
