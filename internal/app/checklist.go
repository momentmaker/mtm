package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/state"
)

// settingsLink is the only kind of link mtm opens: it made every
// checklist link itself, and state.json is a file anyone with the account
// could edit to point open at something else.
const settingsLink = "x-apple.systempreferences:"

var ErrNoLink = errors.New("this step has no link to open")

// Checklist returns the steps finish left on this Mac, with what the user
// did with each.
func (a *App) Checklist() (checklist.List, error) {
	st, err := stage.Existing(a.Home)
	if err != nil {
		return checklist.List{}, err
	}
	s, err := state.Load(st)
	if err != nil {
		return checklist.List{}, err
	}
	return s.Checklist, nil
}

func (a *App) MarkDone(ids ...string) error {
	return a.updateChecklist(func(l *checklist.List) error {
		for _, id := range ids {
			if err := l.Done(id, a.Now()); err != nil {
				return err
			}
		}
		return nil
	})
}

func (a *App) Skip(id, reason string) error {
	return a.updateChecklist(func(l *checklist.List) error { return l.Skip(id, reason, a.Now()) })
}

func (a *App) updateChecklist(change func(*checklist.List) error) error {
	st, err := stage.Existing(a.Home)
	if err != nil {
		return err
	}
	s, err := state.Load(st)
	if err != nil {
		return err
	}
	if err := change(&s.Checklist); err != nil {
		return err
	}
	return state.Save(st, s)
}

// OpenStep opens the System Settings pane a step happens in.
func (a *App) OpenStep(ctx context.Context, id string) error {
	l, err := a.Checklist()
	if err != nil {
		return err
	}
	e, ok := l.Find(id)
	if !ok {
		return fmt.Errorf("%w: %s", checklist.ErrUnknownItem, id)
	}
	if e.Link == "" {
		return fmt.Errorf("%w; do it in %s", ErrNoLink, e.Where)
	}
	if !strings.HasPrefix(e.Link, settingsLink) {
		return fmt.Errorf("refusing to open %q: mtm opens only System Settings links", e.Link)
	}
	if _, err := a.Runner.Run(ctx, runner.Command{Name: "/usr/bin/open", Args: []string{e.Link}}); err != nil {
		return fmt.Errorf("open %s: %w", e.Where, err)
	}
	return nil
}
