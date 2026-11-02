package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Exec runs commands on the real system. Standard input is empty unless
// Command.Stdin is set, so no command can stop to prompt the user.
type Exec struct{}

func (Exec) Run(ctx context.Context, cmd Command) (Output, error) {
	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	c.Dir = cmd.Dir
	if len(cmd.Env) > 0 {
		c.Env = append(os.Environ(), cmd.Env...)
	}
	if cmd.Stdin != "" {
		c.Stdin = strings.NewReader(cmd.Stdin)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	out := Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
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
