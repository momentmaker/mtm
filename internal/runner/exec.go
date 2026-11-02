package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// waitDelay bounds how long Run waits for output after a command exits. A
// login shell can leave a background child, such as ssh-agent, holding
// stdout open; without a bound Run would wait for that child, past any
// timeout, because the context only kills the shell.
const waitDelay = 2 * time.Second

// Exec runs commands on the real system. Standard input is empty unless
// Command.Stdin is set, so no command can stop to prompt the user.
type Exec struct{}

func (Exec) Run(ctx context.Context, cmd Command) (Output, error) {
	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	c.WaitDelay = waitDelay
	c.Dir = cmd.Dir
	if len(cmd.Env) > 0 || len(cmd.Unset) > 0 {
		c.Env = append(without(os.Environ(), cmd.Unset), cmd.Env...)
	}
	if cmd.Stdin != "" {
		c.Stdin = strings.NewReader(cmd.Stdin)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	out := Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if errors.Is(err, exec.ErrWaitDelay) && c.ProcessState != nil && c.ProcessState.Success() {
		// The command exited 0 and only a child it left behind held the
		// pipes; anything that child writes later is dropped, not waited for.
		return out, nil
	}
	if err == nil {
		return out, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		out.Code = exitErr.ExitCode()
		return out, newExitError(cmd, out.Code, out.Stderr)
	}
	return out, fmt.Errorf("%s: %w", Display(cmd), err)
}

func without(env, names []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(names, name) {
			out = append(out, kv)
		}
	}
	return out
}
