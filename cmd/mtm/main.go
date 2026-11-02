// Command mtm moves a developer's Mac to a new Mac, alongside Migration Assistant.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/momentmaker/mtm/internal/app"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/modules/files"
	"github.com/momentmaker/mtm/internal/modules/git"
	"github.com/momentmaker/mtm/internal/report"
	"github.com/momentmaker/mtm/internal/runner"
)

var version = "dev"

const (
	exitOK       = 0
	exitProblems = 1
	exitFailed   = 2
)

const usage = `mtm moves a developer's Mac to a new Mac, alongside Migration Assistant.

Usage:
  mtm scan [--json]                         check this Mac before migrating (changes nothing)
  mtm prepare [--only a,b] [--deep]         record this Mac before Migration Assistant
  mtm prepare --undo                        put this Mac back in use if you call off the migration
  mtm finish                                on the new Mac: restore what prepare set aside
  mtm verify [--only a,b] [--deep] [--details] [--json]
                                            check the new Mac against the record
  mtm cleanup [--force]                     delete ~/.mtm after SAFE TO WIPE
  mtm version
`

type command func(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int

var commands = map[string]command{
	"scan":    cmdScan,
	"prepare": cmdPrepare,
	"finish":  cmdFinish,
	"verify":  cmdVerify,
	"cleanup": cmdCleanup,
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, defaultApp)
	stop()
	os.Exit(code)
}

func defaultApp(home string) *app.App {
	return &app.App{
		Runner:  runner.Exec{},
		Home:    home,
		Version: version,
		Now:     time.Now,
		Modules: []module.Module{files.New(), git.New()},
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, newApp func(home string) *app.App) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitFailed
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintln(stdout, "mtm", version)
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "mtm: unknown command %q\n\n%s", args[0], usage)
		return exitFailed
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "mtm:", err)
		return exitFailed
	}
	return cmd(ctx, newApp(home), args[1:], stdout, stderr)
}

func cmdScan(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	rep := a.Scan(ctx)
	if *asJSON {
		if err := report.JSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, "mtm scan:", err)
			return exitFailed
		}
	} else {
		report.Findings(stdout, rep.Findings)
	}
	if module.HasBlocker(rep.Findings) {
		return exitProblems
	}
	return exitOK
}

func cmdPrepare(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	only := flags.String("only", "", "comma-separated modules to prepare")
	deep := flags.Bool("deep", false, "fingerprint every file (slow)")
	undo := flags.Bool("undo", false, "put this Mac back in use: restart what prepare stopped")
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	if *undo {
		if *only != "" || *deep {
			fmt.Fprintln(stderr, "mtm prepare: --undo can't be combined with --only or --deep")
			return exitFailed
		}
		return runUndo(ctx, a, stdout, stderr)
	}
	rep, err := a.Prepare(ctx, app.PrepareOptions{Only: splitList(*only), Deep: *deep})
	if err != nil {
		fmt.Fprintln(stderr, "mtm prepare:", err)
		return exitFailed
	}
	report.Prepare(stdout, rep.Ready, rep.Done, rep.Problems)
	if !rep.Ready {
		return exitProblems
	}
	return exitOK
}

func runUndo(ctx context.Context, a *app.App, stdout, stderr io.Writer) int {
	rep, err := a.Undo(ctx)
	switch {
	case errors.Is(err, app.ErrUndoOnNewMac):
		fmt.Fprintln(stderr, "mtm prepare --undo:", err)
		return exitProblems
	case err != nil:
		fmt.Fprintln(stderr, "mtm prepare --undo:", err)
		return exitFailed
	}
	report.Undo(stdout, rep.Done, rep.Problems)
	if len(rep.Problems) > 0 {
		return exitProblems
	}
	return exitOK
}

func cmdFinish(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("finish", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	rep, err := a.Finish(ctx)
	switch {
	case errors.Is(err, app.ErrFinishOnOldMac):
		fmt.Fprintln(stderr, "mtm finish:", err)
		return exitProblems
	case err != nil:
		fmt.Fprintln(stderr, "mtm finish:", err)
		return exitFailed
	}
	report.Finish(stdout, rep.Done, rep.Problems)
	if len(rep.Problems) > 0 {
		return exitProblems
	}
	return exitOK
}

func cmdVerify(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	only := flags.String("only", "", "comma-separated modules to verify")
	deep := flags.Bool("deep", false, "also run git fsck on every repo")
	details := flags.Bool("details", false, "list every path")
	asJSON := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	rep, err := a.Verify(ctx, app.VerifyOptions{Only: splitList(*only), Deep: *deep})
	if err != nil {
		fmt.Fprintln(stderr, "mtm verify:", err)
		return exitFailed
	}
	if *asJSON {
		if err := report.JSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, "mtm verify:", err)
			return exitFailed
		}
	} else {
		report.Verify(stdout, rep.Checks, rep.Verdict, *details)
	}
	if !rep.Verdict.Safe {
		return exitProblems
	}
	return exitOK
}

func cmdCleanup(_ context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	force := flags.Bool("force", false, "delete ~/.mtm even without SAFE TO WIPE")
	if err := flags.Parse(args); err != nil {
		return exitFailed
	}
	removed, err := a.Cleanup(*force)
	switch {
	case errors.Is(err, app.ErrNotVerified):
		fmt.Fprintln(stderr, "mtm cleanup:", err)
		return exitProblems
	case err != nil:
		fmt.Fprintln(stderr, "mtm cleanup:", err)
		return exitFailed
	case !removed:
		fmt.Fprintln(stdout, "Nothing to clean up: ~/.mtm does not exist.")
		return exitOK
	}
	fmt.Fprintln(stdout, "Deleted ~/.mtm.")
	fmt.Fprintln(stdout, "If you gave your terminal Full Disk Access for mtm, remove it now:")
	fmt.Fprintln(stdout, "System Settings > Privacy & Security > Full Disk Access.")
	return exitOK
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
