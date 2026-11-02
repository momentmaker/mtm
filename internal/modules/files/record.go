package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"sync"

	"github.com/momentmaker/mtm/internal/fsinfo"
	"github.com/momentmaker/mtm/internal/manifest"
)

// Stats summarizes a file record. It is the files module's inventory.
type Stats struct {
	Entries       int      `json:"entries"`
	Fingerprinted int      `json:"fingerprinted"`
	Cloud         int      `json:"cloud"`
	Unreadable    []string `json:"unreadable,omitempty"`
}

func (s *Stats) add(e manifest.Entry) {
	s.Entries++
	switch {
	case e.Hash != "":
		s.Fingerprinted++
	case e.Type == manifest.TypeCloud:
		s.Cloud++
	case e.Type == manifest.TypeUnreadable:
		s.Unreadable = append(s.Unreadable, e.Path)
	}
}

type pending struct {
	entry manifest.Entry
	abs   string
	done  chan error
}

// writeRecord walks home and writes the file record to w. Files are
// fingerprinted in parallel while the record keeps walk order, and memory
// stays bounded by the channel sizes.
func writeRecord(ctx context.Context, home string, w *manifest.RecordWriter, deep bool) (Stats, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workers := runtime.NumCPU()
	jobs := make(chan *pending, workers*4)
	ordered := make(chan *pending, workers*64)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				hash, err := fsinfo.HashFile(p.abs)
				p.entry.Hash = hash
				p.done <- err
			}
		}()
	}

	walkErr := make(chan error, 1)
	go func() {
		defer close(ordered)
		defer close(jobs)
		walkErr <- walkHome(ctx, home, excluded, func(it item) error {
			p := &pending{entry: toEntry(it)}
			if p.entry.Type == manifest.TypeFile && shouldFingerprint(it.rel, deep) {
				p.abs = it.abs
				p.done = make(chan error, 1)
				select {
				case jobs <- p:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			select {
			case ordered <- p:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	var stats Stats
	var writeErr error
	for p := range ordered {
		if p.done != nil {
			if err := <-p.done; err != nil {
				switch {
				case errors.Is(err, fs.ErrNotExist):
					continue
				case errors.Is(err, fs.ErrPermission):
					p.entry = manifest.Entry{Path: p.entry.Path, Type: manifest.TypeUnreadable, Area: p.entry.Area}
				case writeErr == nil:
					writeErr = fmt.Errorf("fingerprint %s: %w", p.abs, err)
					cancel()
					continue
				default:
					continue
				}
			}
		}
		if writeErr != nil {
			continue
		}
		if err := w.Write(p.entry); err != nil {
			writeErr = err
			cancel()
			continue
		}
		stats.add(p.entry)
	}
	wg.Wait()
	if writeErr != nil {
		return stats, writeErr
	}
	if err := <-walkErr; err != nil {
		return stats, err
	}
	return stats, nil
}

func toEntry(it item) manifest.Entry {
	e := manifest.Entry{Path: it.rel, Area: area(it.rel)}
	switch {
	case it.unreadable:
		e.Type = manifest.TypeUnreadable
	case it.cloud:
		e.Type = manifest.TypeCloud
		e.Size = it.info.Size()
		e.MTime = it.info.ModTime().UnixNano()
	case it.info.Mode()&fs.ModeSymlink != 0:
		e.Type = manifest.TypeLink
		e.Target = it.target
		e.MTime = it.info.ModTime().UnixNano()
	case it.info.IsDir():
		e.Type = manifest.TypeDir
		e.Mode = uint32(it.info.Mode().Perm())
	default:
		e.Type = manifest.TypeFile
		e.Size = it.info.Size()
		e.MTime = it.info.ModTime().UnixNano()
		e.Mode = uint32(it.info.Mode().Perm())
	}
	return e
}
