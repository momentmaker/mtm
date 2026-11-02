package manifest

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	TypeFile       = "f"
	TypeDir        = "d"
	TypeLink       = "l"
	TypeCloud      = "cloud"
	TypeUnreadable = "unreadable"

	AreaCritical = "critical"
	AreaAppState = "appstate"
)

// Entry is one line of the file record. Path is relative to the home
// folder and uses forward slashes. MTime is in Unix nanoseconds.
type Entry struct {
	Path   string `json:"p"`
	Type   string `json:"t"`
	Size   int64  `json:"s,omitempty"`
	MTime  int64  `json:"m,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`
	Hash   string `json:"h,omitempty"`
	Target string `json:"target,omitempty"`
	Area   string `json:"area"`
}

type RecordWriter struct {
	gz  *gzip.Writer
	enc *json.Encoder
}

func NewRecordWriter(w io.Writer) *RecordWriter {
	gz := gzip.NewWriter(w)
	return &RecordWriter{gz: gz, enc: json.NewEncoder(gz)}
}

func (w *RecordWriter) Write(e Entry) error {
	if err := w.enc.Encode(e); err != nil {
		return fmt.Errorf("write record entry %q: %w", e.Path, err)
	}
	return nil
}

// Close flushes the record. It does not close the underlying writer.
func (w *RecordWriter) Close() error {
	if err := w.gz.Close(); err != nil {
		return fmt.Errorf("finish file record: %w", err)
	}
	return nil
}

type RecordReader struct {
	dec *json.Decoder
}

func NewRecordReader(r io.Reader) (*RecordReader, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("open file record: %w", err)
	}
	return &RecordReader{dec: json.NewDecoder(gz)}, nil
}

// Next returns the next entry, or io.EOF after the last one.
func (r *RecordReader) Next() (Entry, error) {
	var e Entry
	if err := r.dec.Decode(&e); err != nil {
		if errors.Is(err, io.EOF) {
			return Entry{}, io.EOF
		}
		return Entry{}, fmt.Errorf("read file record: %w", err)
	}
	return e, nil
}
