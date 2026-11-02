package runner

import (
	"context"
	"fmt"
	"sync"
)

// Response is a canned result for one command line.
type Response struct {
	Stdout string
	Stderr string
	Code   int
	Err    error
}

// Fake answers commands from Responses, keyed by Command.String(). Commands
// without a response go to Fallback when it is set, and fail otherwise.
type Fake struct {
	Responses map[string]Response
	Fallback  Runner

	mu    sync.Mutex
	calls []Command
}

func (f *Fake) Run(ctx context.Context, cmd Command) (Output, error) {
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	resp, ok := f.Responses[cmd.String()]
	f.mu.Unlock()
	if !ok {
		if f.Fallback != nil {
			return f.Fallback.Run(ctx, cmd)
		}
		return Output{}, fmt.Errorf("fake runner: unexpected command: %s", cmd.String())
	}
	out := Output{Stdout: []byte(resp.Stdout), Stderr: []byte(resp.Stderr), Code: resp.Code}
	switch {
	case resp.Err != nil:
		return out, resp.Err
	case resp.Code != 0:
		return out, newExitError(cmd, resp.Code, out.Stderr)
	}
	return out, nil
}

// Calls returns the commands run so far, in order.
func (f *Fake) Calls() []Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Command(nil), f.calls...)
}
