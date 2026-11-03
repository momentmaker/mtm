package docker

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// archiveRoot is the first path element of every entry docker container cp
// writes for /vol, the folder mtm mounts each volume at.
const archiveRoot = "vol"

// Digest computes a volume's content digest from the tar stream docker
// container cp writes, as the stream passes through: SHA-256 over one line
// per entry, sorted by path, holding the entry's type, permission bits,
// owner and group ids, size, link target, and content SHA-256. Sorting
// makes it independent of tar order. Owners count because a database
// refuses a data folder it doesn't own. Modification times don't.
type Digest struct {
	pw   *io.PipeWriter
	done chan struct{}

	sum     string
	entries int
	err     error
}

func NewDigest() *Digest {
	pr, pw := io.Pipe()
	d := &Digest{pw: pw, done: make(chan struct{})}
	go func() {
		defer close(d.done)
		d.sum, d.entries, d.err = digestTar(pr)
		// Docker pads the archive past its end marker; reading the rest
		// keeps the writer from blocking. After an error, closing makes
		// the writer fail instead.
		if d.err == nil {
			_, d.err = io.Copy(io.Discard, pr)
		}
		pr.CloseWithError(d.err)
	}()
	return d
}

func (d *Digest) Write(p []byte) (int, error) { return d.pw.Write(p) }

// Finish ends the stream and returns the digest and the number of entries
// below the volume's root folder; 0 means the volume is empty.
func (d *Digest) Finish() (string, int, error) {
	d.pw.Close()
	<-d.done
	return d.sum, d.entries, d.err
}

func digestTar(r io.Reader) (string, int, error) {
	tr := tar.NewReader(r)
	var lines []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", 0, fmt.Errorf("read the volume's archive: %w", err)
		}
		line, err := entryLine(hdr, tr)
		if err != nil {
			return "", 0, err
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "", 0, errors.New("read the volume's archive: it has no entries")
	}
	slices.Sort(lines)
	h := sha256.New()
	for _, l := range lines {
		io.WriteString(h, l)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), len(lines) - 1, nil
}

func entryLine(hdr *tar.Header, content io.Reader) (string, error) {
	rel, err := relative(hdr.Name)
	if err != nil {
		return "", err
	}
	var size int64
	sum := ""
	if hdr.Typeflag == tar.TypeReg {
		h := sha256.New()
		n, err := io.Copy(h, content)
		if err != nil {
			return "", fmt.Errorf("read %s in the volume's archive: %w", rel, err)
		}
		size, sum = n, hex.EncodeToString(h.Sum(nil))
	}
	fields := []string{
		strconv.Quote(rel), string(hdr.Typeflag), strconv.FormatInt(hdr.Mode&0o7777, 8),
		strconv.Itoa(hdr.Uid), strconv.Itoa(hdr.Gid), strconv.FormatInt(size, 10),
		strconv.Quote(hdr.Linkname), sum,
	}
	return strings.Join(fields, " ") + "\n", nil
}

// relative turns "vol/a/b" into "a/b" and "vol" into ".".
func relative(name string) (string, error) {
	name = strings.TrimSuffix(name, "/")
	if name == archiveRoot {
		return ".", nil
	}
	rel, ok := strings.CutPrefix(name, archiveRoot+"/")
	if !ok || rel == "" {
		return "", fmt.Errorf("read the volume's archive: unexpected entry %q", name)
	}
	return rel, nil
}
