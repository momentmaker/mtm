// Package checklist tracks the steps no tool can do for the user, such as
// granting a privacy permission, until each is done or skipped with a
// reason. verify counts the rest as open (spec 8.5).
package checklist

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Item is one step. ID is stable across runs, "<module>:<key>", so finish
// can run again without losing what the user already did. Where names the
// place to do it, and Link opens it.
type Item struct {
	ID     string `json:"id"`
	Module string `json:"module"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Where  string `json:"where,omitempty"`
	Link   string `json:"link,omitempty"`
}

type Status string

const (
	Pending Status = "pending"
	Done    Status = "done"
	Skipped Status = "skipped"
)

type Entry struct {
	Item
	Status Status    `json:"status"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at,omitzero"`
}

type List struct {
	Entries []Entry `json:"entries,omitempty"`
}

var (
	ErrUnknownItem = errors.New("no checklist item has that id")
	ErrNoReason    = errors.New("skipping a step needs a reason")
)

// Add records new items as pending and refreshes the text of items it
// already has, keeping what the user did with them. It returns how many
// were new.
func (l *List) Add(items ...Item) int {
	added := 0
	for _, it := range items {
		if i := l.index(it.ID); i >= 0 {
			l.Entries[i].Item = it
			continue
		}
		l.Entries = append(l.Entries, Entry{Item: it, Status: Pending})
		added++
	}
	return added
}

func (l *List) Done(id string, at time.Time) error {
	i := l.index(id)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrUnknownItem, id)
	}
	l.Entries[i].Status, l.Entries[i].Reason, l.Entries[i].At = Done, "", at
	return nil
}

// Skip closes a step the user won't do. The reason is recorded because a
// skipped step still counts for the verdict only with one.
func (l *List) Skip(id, reason string, at time.Time) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrNoReason
	}
	i := l.index(id)
	if i < 0 {
		return fmt.Errorf("%w: %s", ErrUnknownItem, id)
	}
	l.Entries[i].Status, l.Entries[i].Reason, l.Entries[i].At = Skipped, reason, at
	return nil
}

func (l List) Pending() []Entry {
	var out []Entry
	for _, e := range l.Entries {
		if e.Status == Pending {
			out = append(out, e)
		}
	}
	return out
}

func (l List) Find(id string) (Entry, bool) {
	if i := l.index(id); i >= 0 {
		return l.Entries[i], true
	}
	return Entry{}, false
}

func (l List) index(id string) int {
	for i, e := range l.Entries {
		if e.ID == id {
			return i
		}
	}
	return -1
}
