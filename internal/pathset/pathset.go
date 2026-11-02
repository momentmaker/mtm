// Package pathset matches home-relative, slash-separated paths against
// simple patterns.
//
// Pattern forms:
//
//	"Library/Caches/"   a folder and everything under it (globs allowed)
//	"Library/*/x.log"   matched against the whole path
//	".DS_Store"         no slash: matched against the base name at any depth
//
// A leading "~/" is ignored.
package pathset

import (
	"fmt"
	"path"
	"strings"
)

type Set struct {
	dirs  []string
	whole []string
	base  []string
}

func New(patterns ...string) (Set, error) {
	var s Set
	for _, p := range patterns {
		p = strings.TrimPrefix(p, "~/")
		if p == "" {
			continue
		}
		if _, err := path.Match(p, ""); err != nil {
			return Set{}, fmt.Errorf("pattern %q: %w", p, err)
		}
		switch {
		case strings.HasSuffix(p, "/"):
			s.dirs = append(s.dirs, strings.TrimSuffix(p, "/"))
		case strings.Contains(p, "/"):
			s.whole = append(s.whole, p)
		default:
			s.base = append(s.base, p)
		}
	}
	return s, nil
}

func MustNew(patterns ...string) Set {
	s, err := New(patterns...)
	if err != nil {
		panic(err)
	}
	return s
}

func (s Set) Match(rel string) bool {
	for _, d := range s.dirs {
		if matchDir(d, rel) {
			return true
		}
	}
	for _, w := range s.whole {
		if ok, _ := path.Match(w, rel); ok {
			return true
		}
	}
	base := path.Base(rel)
	for _, b := range s.base {
		if ok, _ := path.Match(b, base); ok {
			return true
		}
	}
	return false
}

// matchDir reports whether rel is the folder pattern or inside it, by
// matching the pattern against rel's leading path components.
func matchDir(pattern, rel string) bool {
	n := strings.Count(pattern, "/") + 1
	parts := strings.SplitN(rel, "/", n+1)
	if len(parts) < n {
		return false
	}
	ok, _ := path.Match(pattern, strings.Join(parts[:n], "/"))
	return ok
}
