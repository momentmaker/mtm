package manifest

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"
)

func TestRecordRoundTrip(t *testing.T) {
	want := []Entry{
		{Path: "Docs/résumé.pdf", Type: TypeFile, Size: 10, MTime: 1793500000123456789, Mode: 0o644, Area: AreaCritical},
		{Path: "odd\nname", Type: TypeFile, Size: 0, Mode: 0, Hash: "sha256:00", Area: AreaCritical},
		{Path: "with space/link", Type: TypeLink, Target: "../x", Resolves: true, Area: AreaCritical},
		{Path: "Library/Preferences", Type: TypeDir, Mode: 0o700, Area: AreaAppState},
	}
	var buf bytes.Buffer
	w := NewRecordWriter(&buf)
	for _, e := range want {
		if err := w.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewRecordReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var got []Entry
	for {
		e, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, e)
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestRecordReaderRejectsGarbage(t *testing.T) {
	if _, err := NewRecordReader(bytes.NewReader([]byte("not gzip"))); err == nil {
		t.Error("NewRecordReader accepted non-gzip input")
	}
}
