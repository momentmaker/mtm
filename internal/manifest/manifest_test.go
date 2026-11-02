package manifest

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/stage"
)

func openStage(t *testing.T) *stage.Stage {
	t.Helper()
	st, err := stage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSaveLoadRoundTrip(t *testing.T) {
	st := openStage(t)
	at := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	m := New("1.0.0")
	m.MacOSVersion, m.MacOSBuild, m.MachineID = "26.7.1", "25G241", "sha256:ab"
	if err := m.SetModule("files", at, map[string]int{"entries": 3}); err != nil {
		t.Fatal(err)
	}
	if err := Save(st, m); err != nil {
		t.Fatal(err)
	}
	got, err := Load(st)
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != Schema || got.MtmVersion != "1.0.0" || got.MacOSBuild != "25G241" || got.MachineID != "sha256:ab" {
		t.Errorf("loaded %+v", got)
	}
	var inv map[string]int
	preparedAt, err := got.Module("files", &inv)
	if err != nil {
		t.Fatal(err)
	}
	if !preparedAt.Equal(at) || inv["entries"] != 3 || !got.PreparedAt.Equal(at) {
		t.Errorf("module prepared %v inv %v manifest prepared %v", preparedAt, inv, got.PreparedAt)
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	st := openStage(t)
	st.WriteFile(FileName, []byte(`{"schema": 99}`))
	if _, err := Load(st); !errors.Is(err, ErrNewerSchema) {
		t.Errorf("err = %v, want ErrNewerSchema", err)
	}
}

func TestLoadMissingIsNotExist(t *testing.T) {
	if _, err := Load(openStage(t)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestModuleMissing(t *testing.T) {
	var v any
	if _, err := New("x").Module("git", &v); !errors.Is(err, ErrNoModule) {
		t.Errorf("err = %v, want ErrNoModule", err)
	}
}

func TestSetModuleKeepsLatestPreparedAt(t *testing.T) {
	m := New("x")
	later := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	m.SetModule("git", later, struct{}{})
	m.SetModule("files", later.Add(-time.Hour), struct{}{})
	if !m.PreparedAt.Equal(later) {
		t.Errorf("PreparedAt = %v, want %v", m.PreparedAt, later)
	}
}

func TestLoadRejectsIncompleteManifest(t *testing.T) {
	for name, data := range map[string]string{
		"schema 0":         `{"schema": 0, "machine_id": "sha256:ab"}`,
		"no schema":        `{"machine_id": "sha256:ab"}`,
		"no machine id":    `{"schema": 1}`,
		"empty object":     `{}`,
		"empty machine id": `{"schema": 1, "machine_id": ""}`,
	} {
		t.Run(name, func(t *testing.T) {
			st := openStage(t)
			st.WriteFile(FileName, []byte(data))
			if _, err := Load(st); !errors.Is(err, ErrInvalidManifest) {
				t.Errorf("err = %v, want ErrInvalidManifest", err)
			}
		})
	}
}
