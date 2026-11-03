package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/report"
)

// cmdRetire lists what changed on the old Mac since prepare, then walks
// through the retirement steps in a terminal, or lists them elsewhere.
// list, done, skip, and open work on those steps as they do for mtm
// checklist.
func cmdRetire(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	steps := stepList{report.RetireCommand, a.Retirement()}
	if len(args) > 0 {
		switch args[0] {
		case "list", "done", "skip", "open":
			return stepsCommand(ctx, steps, args, stdout, stderr)
		}
	}
	flags := flag.NewFlagSet("retire", flag.ContinueOnError)
	flags.SetOutput(stderr)
	details := flags.Bool("details", false, "list every changed path")
	asJSON := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "mtm retire: unknown subcommand %q\n\n%s", flags.Arg(0), usage)
		return exitFailed
	}
	rep, err := a.Retire(ctx)
	switch {
	case errors.Is(err, app.ErrRetireOnNewMac):
		fmt.Fprintln(stderr, "mtm retire:", err)
		return exitProblems
	case err != nil:
		fmt.Fprintln(stderr, "mtm retire:", err)
		return exitFailed
	}
	code := exitOK
	if needsAttention(rep) {
		code = exitProblems
	}
	if *asJSON {
		if err := report.JSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, "mtm retire:", err)
			return exitFailed
		}
		return code
	}
	report.Retire(stdout, rep.PreparedAt, rep.Changes, rep.Problems, *details)
	if interactive() {
		if walked := walk(ctx, steps, stdin, stdout, stderr); walked != exitOK {
			return walked
		}
		return code
	}
	report.Checklist(stdout, rep.Steps, false, report.RetireCommand)
	return code
}

// needsAttention is true when a module failed or a file outside ~/Library
// changed after prepare. App state inside ~/Library changes all the time,
// so it alone doesn't.
func needsAttention(rep app.RetireReport) bool {
	if len(rep.Problems) > 0 {
		return true
	}
	for _, c := range rep.Changes {
		if c.Status == module.StatusWarn || c.Status == module.StatusFail {
			return true
		}
	}
	return false
}
