// Package runner is the only place mtm executes system commands, so tests
// can replace every command with a fake.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/momentmaker/mtm/internal/redact"
)

// Command is one system command. Unset names variables removed from the
// current environment, and Env entries are then appended to it. Stdin,
// when set, is written to the command's standard input. Input streams
// standard input instead, and Stdout, when set, receives standard output
// as it arrives rather than in Output.Stdout: exports can be larger than
// memory. A command sets Stdin or Input, not both.
type Command struct {
	Name   string
	Args   []string
	Dir    string
	Unset  []string
	Env    []string
	Stdin  string
	Input  io.Reader
	Stdout io.Writer
}

// String is the unredacted command line. Fake uses it as the lookup key.
func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Display is the command line with secrets masked, safe for logs and errors.
func Display(c Command) string {
	return strings.Join(append([]string{c.Name}, redact.Args(c.Args)...), " ")
}

type Output struct {
	Stdout []byte
	Stderr []byte
	Code   int
}

// ExitError reports a command that ran but exited non-zero.
type ExitError struct {
	Command string
	Code    int
	Stderr  string
}

func (e *ExitError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("%s: exit status %d", e.Command, e.Code)
	}
	return fmt.Sprintf("%s: exit status %d: %s", e.Command, e.Code, e.Stderr)
}

// ExitCode returns the exit status carried by err when err is an *ExitError.
func ExitCode(err error) (int, bool) {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code, true
	}
	return 0, false
}

type Runner interface {
	Run(ctx context.Context, cmd Command) (Output, error)
}

const maxStderr = 2000

func newExitError(cmd Command, code int, stderr []byte) *ExitError {
	s := redact.Text(strings.TrimSpace(string(stderr)))
	if len(s) > maxStderr {
		s = s[:maxStderr] + "…"
	}
	return &ExitError{Command: Display(cmd), Code: code, Stderr: s}
}
