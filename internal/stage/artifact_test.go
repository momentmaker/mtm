package stage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newExports(t *testing.T) *Stage {
	t.Helper()
	s, err := OpenDir(filepath.Join(t.TempDir(), "exports"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func writeArtifact(t *testing.T, s *Stage, name, content string) Artifact {
	t.Helper()
	f, err := s.CreateArtifact(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	a, err := f.Commit()
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestArtifactRecordsSizeAndFingerprint(t *testing.T) {
	s := newExports(t)
	a := writeArtifact(t, s, "docker/volumes/db.tar.gz", "hello")
	want := Artifact{Name: "docker/volumes/db.tar.gz", Size: 5, SHA256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"}
	if a != want {
		t.Errorf("artifact = %+v, want %+v", a, want)
	}
	info, err := os.Stat(s.Path(a.Name))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("stat = %v, %v; want mode 0600", info, err)
	}
	if err := s.CheckArtifact(a); err != nil {
		t.Errorf("CheckArtifact = %v", err)
	}
}

func TestCheckArtifactCatchesAChangedFile(t *testing.T) {
	s := newExports(t)
	a := writeArtifact(t, s, "postgres/main.sql.gz", "hello")
	if err := os.WriteFile(s.Path(a.Name), []byte("jello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckArtifact(a); !errors.Is(err, ErrArtifactChanged) {
		t.Errorf("err = %v, want ErrArtifactChanged", err)
	}
}

func TestCheckArtifactFailsForAMissingFile(t *testing.T) {
	s := newExports(t)
	if err := s.CheckArtifact(Artifact{Name: "docker/images/gone.tar", Size: 1, SHA256: "00"}); err == nil {
		t.Error("CheckArtifact passed for a missing file")
	}
}

func TestArtifactNamesStayInsideTheFolder(t *testing.T) {
	s := newExports(t)
	if _, err := s.CreateArtifact("../escape"); err == nil {
		t.Error("CreateArtifact accepted ../escape")
	}
	if err := s.CheckArtifact(Artifact{Name: "/etc/hosts"}); err == nil {
		t.Error("CheckArtifact accepted /etc/hosts")
	}
}

func TestCheckArtifactRefusesAnOpenFolder(t *testing.T) {
	s := newExports(t)
	a := writeArtifact(t, s, "x", "hello")
	if err := os.Chmod(s.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckArtifact(a); err == nil {
		t.Error("CheckArtifact trusted a folder others can read")
	}
}

func TestOpenDirCreatesAPrivateFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mtm-exports")
	if _, err := OpenDir(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("stat = %v, %v; want mode 0700", info, err)
	}
}
