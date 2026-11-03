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
// checklist link itself, and a state file is one anyone with the account
// could edit to point open at something else.
const settingsLink = "x-apple.systempreferences:"

var (
	ErrNoLink      = errors.New("this step has no link to open")
	ErrRefusedLink = errors.New("mtm opens only System Settings links")
)

// Steps is one checklist kept in ~/.mtm, with what the user did with each
// step: the new Mac's, or the old Mac's retirement steps.
type Steps struct {
	app  *App
	file string
}

// Checklist is the new Mac's checklist, which finish and verify fill.
func (a *App) Checklist() Steps { return Steps{app: a, file: state.FileName} }

// Retirement is the old Mac's retirement checklist, which retire fills.
func (a *App) Retirement() Steps { return Steps{app: a, file: state.RetireFileName} }

func (s Steps) List() (checklist.List, error) {
	st, err := stage.Existing(s.app.Home)
	if err != nil {
		return checklist.List{}, err
	}
	saved, err := state.LoadFile(st, s.file)
	if err != nil {
		return checklist.List{}, err
	}
	return saved.Checklist, nil
}

func (s Steps) Done(ids ...string) error {
	return s.update(func(l *checklist.List) error {
		for _, id := range ids {
			if err := l.Done(id, s.app.Now()); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s Steps) Skip(id, reason string) error {
	return s.update(func(l *checklist.List) error { return l.Skip(id, reason, s.app.Now()) })
}

func (s Steps) update(change func(*checklist.List) error) error {
	st, err := stage.Existing(s.app.Home)
	if err != nil {
		return err
	}
	saved, err := state.LoadFile(st, s.file)
	if err != nil {
		return err
	}
	if err := change(&saved.Checklist); err != nil {
		return err
	}
	return state.SaveFile(st, s.file, saved)
}

// Open opens the System Settings pane a step happens in.
func (s Steps) Open(ctx context.Context, id string) error {
	l, err := s.List()
	if err != nil {
		return err
	}
	e, ok := l.Find(id)
	if !ok {
		return fmt.Errorf("%w: %s", checklist.ErrUnknownItem, id)
	}
	if e.Link == "" {
		return fmt.Errorf("%w; do it in %q", ErrNoLink, e.Where)
	}
	if !strings.HasPrefix(e.Link, settingsLink) {
		return fmt.Errorf("%w; refusing %q", ErrRefusedLink, e.Link)
	}
	if _, err := s.app.Runner.Run(ctx, runner.Command{Name: "/usr/bin/open", Args: []string{e.Link}}); err != nil {
		return fmt.Errorf("open %q: %w", e.Where, err)
	}
	return nil
}
