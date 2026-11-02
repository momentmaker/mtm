// Package fsinfo holds file-system helpers shared by modules.
package fsinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// sfDataless is SF_DATALESS from <sys/stat.h>: the item's content lives in
// the cloud, and reading it would start a download.
const sfDataless = 0x40000000

func IsDataless(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Flags&sfDataless != 0
}

// OtherVolume returns a test that reports whether an item is on a
// different volume from ref. When either volume can't be told, the item
// counts as on the same volume, so callers keep walking into it.
func OtherVolume(ref fs.FileInfo) func(fs.FileInfo) bool {
	dev, ok := device(ref)
	return func(info fs.FileInfo) bool {
		d, known := device(info)
		return ok && known && d != dev
	}
}

func device(info fs.FileInfo) (int32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Dev, true
}

// HashFile returns "sha256:" followed by the hex SHA-256 of the file's content.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
