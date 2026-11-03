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
	if len(args) == 0 {
		if interactive() {
			return walk(ctx, a, stdin, stdout, stderr)
		}
		return listSteps(a, nil, stdout, stderr)
	}
	switch args[0] {
	case "list":
		return listSteps(a, args[1:], stdout, stderr)
	case "done":
		return markDone(a, args[1:], stdout, stderr)
	case "skip":
		return skipStep(a, args[1:], stdout, stderr)
	case "open":
		return openStep(ctx, a, args[1:], stderr)
	}
	fmt.Fprintf(stderr, "mtm checklist: unknown subcommand %q\n\n%s", args[0], usage)
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

func listSteps(a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("checklist list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	all := flags.Bool("all", false, "also show steps already done or skipped")
	asJSON := flags.Bool("json", false, "print JSON")
	if _, err := parse(flags, args); err != nil {
		return exitFailed
	}
	l, err := a.Checklist()
	if err != nil {
		fmt.Fprintln(stderr, "mtm checklist:", err)
		return exitFailed
	}
	if *asJSON {
		if err := report.JSON(stdout, l); err != nil {
			fmt.Fprintln(stderr, "mtm checklist:", err)
			return exitFailed
		}
		return exitOK
	}
	report.Checklist(stdout, l, *all)
	return exitOK
}

func markDone(a *app.App, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "mtm checklist done: name the step ids to mark done")
		return exitFailed
	}
	if err := a.MarkDone(args...); err != nil {
		fmt.Fprintln(stderr, "mtm checklist done:", err)
		return exitFailed
	}
	fmt.Fprintf(stdout, "Marked done: %s\n", strings.Join(args, ", "))
	return exitOK
}

func skipStep(a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("checklist skip", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reason := flags.String("reason", "", "why this step doesn't apply (required)")
	ids, err := parse(flags, args)
	if err != nil {
		return exitFailed
	}
	if len(ids) != 1 {
		fmt.Fprintln(stderr, `mtm checklist skip: name one step id, with --reason "..."`)
		return exitFailed
	}
	if err := a.Skip(ids[0], *reason); err != nil {
		fmt.Fprintln(stderr, "mtm checklist skip:", err)
		return exitFailed
	}
	fmt.Fprintf(stdout, "Skipped: %s\n", ids[0])
	return exitOK
}

func openStep(ctx context.Context, a *app.App, args []string, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "mtm checklist open: name one step id")
		return exitFailed
	}
	if err := a.OpenStep(ctx, args[0]); err != nil {
		fmt.Fprintln(stderr, "mtm checklist open:", err)
		return exitFailed
	}
	return exitOK
}

// walk goes through the open steps one at a time: it opens each step's
// System Settings pane and asks what happened, saving every answer at once
// so quitting loses nothing.
func walk(ctx context.Context, a *app.App, in io.Reader, stdout, stderr io.Writer) int {
	l, err := a.Checklist()
	if err != nil {
		fmt.Fprintln(stderr, "mtm checklist:", err)
		return exitFailed
	}
	steps := l.Pending()
	if len(steps) == 0 {
		report.Checklist(stdout, l, false)
		return exitOK
	}
	r := bufio.NewReader(in)
	for i, e := range steps {
		report.Step(stdout, i+1, len(steps), e)
		if e.Link != "" {
			if err := a.OpenStep(ctx, e.ID); err != nil {
				fmt.Fprintln(stdout, "  Could not open it:", err)
			}
		}
		if quit := ask(a, r, e, stdout); quit {
			break
		}
	}
	l, err = a.Checklist()
	if err != nil {
		fmt.Fprintln(stderr, "mtm checklist:", err)
		return exitFailed
	}
	fmt.Fprintln(stdout)
	report.Checklist(stdout, l, false)
	return exitOK
}

// ask reads answers until one settles the step, and reports a quit or the
// end of input.
func ask(a *app.App, r *bufio.Reader, e checklist.Entry, out io.Writer) bool {
	for {
		fmt.Fprint(out, "  [d]one, [s]kip, [l]ater, [q]uit? ")
		answer, err := readLine(r)
		if err != nil {
			return true
		}
		switch strings.ToLower(answer) {
		case "d", "done":
			if err := a.MarkDone(e.ID); err != nil {
				fmt.Fprintln(out, "  ", err)
				continue
			}
			return false
		case "s", "skip":
			fmt.Fprint(out, "  Why doesn't it apply? ")
			reason, err := readLine(r)
			if err != nil {
				return true
			}
			if err := a.Skip(e.ID, reason); err != nil {
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

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
