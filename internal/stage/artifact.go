package stage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

// Artifact is an export: its path inside the exports folder, its size, and
// its SHA-256, so finish can refuse a damaged or swapped file.
type Artifact struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

var ErrArtifactChanged = errors.New("the export doesn't match the fingerprint prepare recorded")

// ArtifactFile is a private file that fingerprints what is written to it.
type ArtifactFile struct {
	file *File
	hash hash.Hash
	size int64
	name string
}

func (s *Stage) CreateArtifact(name string) (*ArtifactFile, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("export name %q leaves %s", name, s.Root)
	}
	f, err := s.Create(name)
	if err != nil {
		return nil, err
	}
	return &ArtifactFile{file: f, hash: sha256.New(), name: name}, nil
}

func (a *ArtifactFile) Write(p []byte) (int, error) {
	n, err := a.file.Write(p)
	a.hash.Write(p[:n])
	a.size += int64(n)
	return n, err
}

// Commit saves the file under its name and returns its record.
func (a *ArtifactFile) Commit() (Artifact, error) {
	if err := a.file.Commit(); err != nil {
		return Artifact{}, err
	}
	return Artifact{Name: a.name, Size: a.size, SHA256: hex.EncodeToString(a.hash.Sum(nil))}, nil
}

func (a *ArtifactFile) Abort() { a.file.Abort() }

// CheckArtifact reads a's file in full and compares it with its record. It
// first checks the exports folder is private and this user's, as it is
// before mtm reads anything from ~/.mtm.
func (s *Stage) CheckArtifact(a Artifact) error {
	if !filepath.IsLocal(a.Name) {
		return fmt.Errorf("export name %q leaves %s", a.Name, s.Root)
	}
	if err := checkSafe(s.Root); err != nil {
		return err
	}
	path := s.Path(a.Name)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open export: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return fmt.Errorf("read export %s: %w", path, err)
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return fmt.Errorf("%w: %s", ErrArtifactChanged, path)
	}
	return nil
}
