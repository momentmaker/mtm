package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/momentmaker/mtm/internal/redact"
)

// Logged writes every command it runs, and how it ended, to W with secrets masked.
type Logged struct {
	Inner Runner
	W     io.Writer

	mu sync.Mutex
}

func (l *Logged) Run(ctx context.Context, cmd Command) (Output, error) {
	out, err := l.Inner.Run(ctx, cmd)
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.W, "$ %s\n", Display(cmd))
	if cmd.Dir != "" {
		fmt.Fprintf(l.W, "  in %s\n", cmd.Dir)
	}
	var ee *ExitError
	switch {
	case err == nil:
		fmt.Fprintln(l.W, "  ok")
	case errors.As(err, &ee):
		fmt.Fprintf(l.W, "  exit %d: %s\n", ee.Code, ee.Stderr)
	default:
		fmt.Fprintf(l.W, "  error: %s\n", redact.Text(err.Error()))
	}
	return out, err
}
