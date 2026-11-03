package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/report"
)

// stdin and interactive are the walk-through's input; tests replace them.
var (
	stdin       io.Reader = os.Stdin
	interactive           = func() bool {
		info, err := os.Stdin.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0
	}
)

func cmdChecklist(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	return stepsCommand(ctx, stepList{report.ChecklistCommand, a.Checklist()}, args, stdout, stderr)
}

// stepList is a checklist and the mtm command that keeps it, which names
// it in messages and hints.
type stepList struct {
	command string
	steps   app.Steps
}

// fail prints a message as the command's own, such as "mtm retire done: …".
func (s stepList) fail(stderr io.Writer, sub string, a ...any) {
	name := "mtm " + s.command
	if sub != "" {
		name += " " + sub
	}
	fmt.Fprintln(stderr, append([]any{name + ":"}, a...)...)
}

// stepsCommand walks through the open steps in a terminal and lists them
// elsewhere, or runs one of list, done, skip, and open.
func stepsCommand(ctx context.Context, s stepList, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		if interactive() {
			return walk(ctx, s, stdin, stdout, stderr)
		}
		return listSteps(s, nil, stdout, stderr)
	}
	switch args[0] {
	case "list":
		return listSteps(s, args[1:], stdout, stderr)
	case "done":
		return markDone(s, args[1:], stdout, stderr)
	case "skip":
		return skipStep(s, args[1:], stdout, stderr)
	case "open":
		return openStep(ctx, s, args[1:], stderr)
	}
	fmt.Fprintf(stderr, "mtm %s: unknown subcommand %q\n\n%s", s.command, args[0], usage)
	return exitFailed
}

// parse reads flags wherever they sit among the arguments, so
// "skip <id> --reason ..." works as well as "skip --reason ... <id>".
func parse(flags *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		if flags.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, flags.Arg(0))
		args = flags.Args()[1:]
	}
}

func listSteps(s stepList, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(s.command+" list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	all := flags.Bool("all", false, "also show steps already done or skipped")
	asJSON := flags.Bool("json", false, "print JSON")
	if _, err := parse(flags, args); err != nil {
		return exitFailed
	}
	l, err := s.steps.List()
	if err != nil {
		s.fail(stderr, "", err)
		return exitFailed
	}
	if *asJSON {
		if !*all {
			l = checklist.List{Entries: l.Pending()}
		}
		if err := report.JSON(stdout, l); err != nil {
			s.fail(stderr, "", err)
			return exitFailed
		}
		return exitOK
	}
	report.Checklist(stdout, l, *all, s.command)
	return exitOK
}

func markDone(s stepList, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		s.fail(stderr, "done", "name the step ids to mark done")
		return exitFailed
	}
	if err := s.steps.Done(args...); err != nil {
		s.fail(stderr, "done", err)
		return exitFailed
	}
	fmt.Fprintf(stdout, "Marked done: %s\n", strings.Join(args, ", "))
	return exitOK
}

func skipStep(s stepList, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(s.command+" skip", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reason := flags.String("reason", "", "why this step doesn't apply (required)")
	ids, err := parse(flags, args)
	if err != nil {
		return exitFailed
	}
	if len(ids) != 1 {
		s.fail(stderr, "skip", `name one step id, with --reason "..."`)
		return exitFailed
	}
	if err := s.steps.Skip(ids[0], *reason); err != nil {
		s.fail(stderr, "skip", err)
		return exitFailed
	}
	fmt.Fprintf(stdout, "Skipped: %s\n", ids[0])
	return exitOK
}

func openStep(ctx context.Context, s stepList, args []string, stderr io.Writer) int {
	if len(args) != 1 {
		s.fail(stderr, "open", "name one step id")
		return exitFailed
	}
	if err := s.steps.Open(ctx, args[0]); err != nil {
		s.fail(stderr, "open", err)
		if errors.Is(err, app.ErrRefusedLink) || errors.Is(err, app.ErrNoLink) {
			return exitProblems
		}
		return exitFailed
	}
	return exitOK
}

// walk goes through the open steps one at a time: it opens each step's
// System Settings pane and asks what happened, saving every answer at once
// so quitting loses nothing.
func walk(ctx context.Context, s stepList, in io.Reader, stdout, stderr io.Writer) int {
	l, err := s.steps.List()
	if err != nil {
		s.fail(stderr, "", err)
		return exitFailed
	}
	steps := l.Pending()
	if len(steps) == 0 {
		report.Checklist(stdout, l, false, s.command)
		return exitOK
	}
	r := bufio.NewReader(in)
	for i, e := range steps {
		report.Step(stdout, i+1, len(steps), e)
		if e.Link != "" {
			if err := s.steps.Open(ctx, e.ID); err != nil {
				fmt.Fprintln(stdout, "  Could not open it:", err)
			}
		}
		if quit := ask(ctx, s.steps, r, e, stdout); quit {
			break
		}
	}
	l, err = s.steps.List()
	if err != nil {
		s.fail(stderr, "", err)
		return exitFailed
	}
	fmt.Fprintln(stdout)
	report.Checklist(stdout, l, false, s.command)
	return exitOK
}

// ask reads answers until one settles the step, and reports a quit or the
// end of input.
func ask(ctx context.Context, steps app.Steps, r *bufio.Reader, e checklist.Entry, out io.Writer) bool {
	for {
		fmt.Fprint(out, "  [d]one, [s]kip, [l]ater, [q]uit? ")
		answer, err := readLine(ctx, r)
		if err != nil {
			return true
		}
		switch strings.ToLower(answer) {
		case "d", "done":
			if err := steps.Done(e.ID); err != nil {
				fmt.Fprintln(out, "  ", err)
				continue
			}
			return false
		case "s", "skip":
			fmt.Fprint(out, "  Why doesn't it apply? ")
			reason, err := readLine(ctx, r)
			if err != nil {
				return true
			}
			if err := steps.Skip(e.ID, reason); err != nil {
				fmt.Fprintln(out, "  ", err)
				continue
			}
			return false
		case "l", "later", "":
			return false
		case "q", "quit":
			return true
		}
	}
}

// readLine gives up when ctx is done: Ctrl-C cancels ctx but cannot
// interrupt a read blocked on the terminal.
func readLine(ctx context.Context, r *bufio.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := r.ReadString('\n')
		done <- result{line, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-done:
		if res.err != nil && (res.line == "" || !errors.Is(res.err, io.EOF)) {
			return "", res.err
		}
		return strings.TrimSpace(res.line), nil
	}
}
