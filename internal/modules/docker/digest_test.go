package docker

import (
	"archive/tar"
	"bytes"
	"strings"
	"testing"
)

type entry struct {
	name    string
	typ     byte
	mode    int64
	uid     int
	content string
	link    string
}

// volumeTar builds an archive shaped like docker container cp's output for
// /vol: a "vol" folder entry, then entries under "vol/".
func volumeTar(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: e.mode, Uid: e.uid, Gid: e.uid, Linkname: e.link}
		if e.typ == tar.TypeReg {
			hdr.Size = int64(len(e.content))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var (
	root    = entry{name: "vol", typ: tar.TypeDir, mode: 0o700, uid: 70}
	version = entry{name: "vol/PG_VERSION", typ: tar.TypeReg, mode: 0o600, uid: 70, content: "16\n"}
	sub     = entry{name: "vol/base", typ: tar.TypeDir, mode: 0o700, uid: 70}
	table   = entry{name: "vol/base/1", typ: tar.TypeReg, mode: 0o600, uid: 70, content: "rows"}
	link    = entry{name: "vol/latest", typ: tar.TypeSymlink, mode: 0o777, uid: 70, link: "PG_VERSION"}
)

func digestOf(t *testing.T, archive []byte) (string, int) {
	t.Helper()
	d := NewDigest()
	if _, err := d.Write(archive); err != nil {
		t.Fatal(err)
	}
	sum, n, err := d.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return sum, n
}

func TestDigestIgnoresArchiveOrder(t *testing.T) {
	a, n := digestOf(t, volumeTar(t, root, version, sub, table, link))
	b, _ := digestOf(t, volumeTar(t, root, link, sub, table, version))
	if a != b || !strings.HasPrefix(a, "sha256:") || n != 4 {
		t.Errorf("digests %s and %s with %d entries, want equal sha256 values and 4 entries", a, b, n)
	}
}

func TestDigestChangesWithContentModeOwnerAndLink(t *testing.T) {
	base, _ := digestOf(t, volumeTar(t, root, version, link))
	changed := []struct {
		name    string
		entries []entry
	}{
		{"content of the same size", []entry{root, {name: "vol/PG_VERSION", typ: tar.TypeReg, mode: 0o600, uid: 70, content: "17\n"}, link}},
		{"mode", []entry{root, {name: "vol/PG_VERSION", typ: tar.TypeReg, mode: 0o644, uid: 70, content: "16\n"}, link}},
		{"owner", []entry{root, {name: "vol/PG_VERSION", typ: tar.TypeReg, mode: 0o600, uid: 0, content: "16\n"}, link}},
		{"root folder owner", []entry{{name: "vol", typ: tar.TypeDir, mode: 0o700, uid: 0}, version, link}},
		{"link target", []entry{root, version, {name: "vol/latest", typ: tar.TypeSymlink, mode: 0o777, uid: 70, link: "base"}}},
	}
	for _, c := range changed {
		if got, _ := digestOf(t, volumeTar(t, c.entries...)); got == base {
			t.Errorf("a change of %s left the digest the same", c.name)
		}
	}
}

func TestDigestCountsAnEmptyVolume(t *testing.T) {
	if _, n := digestOf(t, volumeTar(t, root)); n != 0 {
		t.Errorf("entries = %d, want 0 for a volume with only its root", n)
	}
}

func TestDigestReadsPastDockersPadding(t *testing.T) {
	padded := append(volumeTar(t, root, version), make([]byte, 20*1024)...)
	want, _ := digestOf(t, volumeTar(t, root, version))
	if got, _ := digestOf(t, padded); got != want {
		t.Errorf("padding changed the digest")
	}
}

func TestDigestRejectsEntriesOutsideTheVolume(t *testing.T) {
	d := NewDigest()
	d.Write(volumeTar(t, root, entry{name: "etc/passwd", typ: tar.TypeReg, mode: 0o644, content: "x"}))
	if _, _, err := d.Finish(); err == nil || !strings.Contains(err.Error(), "unexpected entry") {
		t.Errorf("err = %v, want unexpected entry", err)
	}
}

func TestDigestFailsOnATruncatedArchive(t *testing.T) {
	archive := volumeTar(t, root, version, table)
	d := NewDigest()
	d.Write(archive[:700])
	if _, _, err := d.Finish(); err == nil {
		t.Error("a truncated archive produced a digest")
	}
}

func TestDigestWriterFailsAfterABadArchive(t *testing.T) {
	d := NewDigest()
	junk := bytes.Repeat([]byte("not a tar archive "), 100)
	var err error
	for i := 0; i < 10 && err == nil; i++ {
		_, err = d.Write(junk)
	}
	if err == nil {
		t.Error("writes kept succeeding after the archive turned out bad")
	}
	if _, _, ferr := d.Finish(); ferr == nil {
		t.Error("Finish reported no error")
	}
}
