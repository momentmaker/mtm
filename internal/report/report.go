// Package report renders mtm's results for people and for scripts. Every
// string passes through clean (or scrub, for JSON), so fingerprints and
// credentials never reach the terminal or a pasted bug report, and control
// characters in file names never reach the terminal raw.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/preflight"
	"github.com/momentmaker/mtm/internal/redact"
	"github.com/momentmaker/mtm/internal/verdict"
)

const shownPaths = 5

var fingerprintPattern = regexp.MustCompile(`sha256:[0-9a-f]+`)

// scrub masks fingerprints and credentials. JSON output uses it alone,
// because the encoder already escapes control characters.
func scrub(s string) string {
	return redact.Text(fingerprintPattern.ReplaceAllString(s, "[fingerprint]"))
}

// clean is scrub for terminal text. It also escapes every non-printing
// rune, so a name such as "Icon\r" or one holding an escape sequence can't
// move the cursor, clear the screen, or overwrite a line with a fake
// SAFE TO WIPE.
func clean(s string) string {
	s = scrub(s)
	if !needsEscape(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case strconv.IsPrint(r):
			b.WriteRune(r)
		default:
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		}
		i += size
	}
	return b.String()
}

// cleanPath quotes a path that needs escaping, so the escapes can't be
// mistaken for part of an ordinary name.
func cleanPath(p string) string {
	p = scrub(p)
	if needsEscape(p) {
		return strconv.Quote(p)
	}
	return p
}

func needsEscape(s string) bool {
	return !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool { return !strconv.IsPrint(r) })
}

func severitySymbol(s module.Severity) string {
	switch s {
	case module.SeverityBlocker:
		return "✗"
	case module.SeverityWarn:
		return "!"
	}
	return "·"
}

func statusSymbol(s module.Status) string {
	switch s {
	case module.StatusPass:
		return "✓"
	case module.StatusFail:
		return "✗"
	case module.StatusWarn:
		return "!"
	}
	return "·"
}

// Findings prints scan results: blockers first, then grouped by who
// handles each item.
func Findings(w io.Writer, findings []module.Finding) {
	sections := []struct {
		title string
		keep  func(module.Finding) bool
	}{
		{"Blockers: fix these before mtm prepare", func(f module.Finding) bool { return f.Severity == module.SeverityBlocker }},
		{"Migration Assistant handles", func(f module.Finding) bool {
			return f.Severity != module.SeverityBlocker && f.Handler == module.ByMigrationAssistant
		}},
		{"mtm handles", func(f module.Finding) bool { return f.Severity != module.SeverityBlocker && f.Handler == module.ByMtm }},
		{"You'll do these by hand, with a guide", func(f module.Finding) bool {
			return f.Severity != module.SeverityBlocker && f.Handler == module.Manual
		}},
	}
	for _, s := range sections {
		var picked []module.Finding
		for _, f := range findings {
			if s.keep(f) {
				picked = append(picked, f)
			}
		}
		if len(picked) == 0 {
			continue
		}
		fmt.Fprintln(w, s.title)
		for _, f := range picked {
			writeFinding(w, f)
		}
		fmt.Fprintln(w)
	}
}

func writeFinding(w io.Writer, f module.Finding) {
	fmt.Fprintf(w, "  %s %s\n", severitySymbol(f.Severity), clean(f.Title))
	if f.Detail != "" {
		fmt.Fprintf(w, "      %s\n", clean(f.Detail))
	}
	if f.Fix != "" {
		fmt.Fprintf(w, "      → %s\n", clean(f.Fix))
	}
}

const readyText = `READY FOR MIGRATION ASSISTANT

Next:
  1. Don't change anything on this Mac from now on. Changes made after prepare aren't checked.
  2. Turn on the new Mac. During its first-boot setup, choose to transfer from a Mac,
     not from a Time Machine backup. A Thunderbolt cable between the Macs is fastest.
  3. On this Mac, open Migration Assistant and choose "To another Mac".
  4. Select every category.
  5. When it finishes, open Terminal on the new Mac and run: mtm finish, then mtm verify
`

func writeDone(w io.Writer, done []string) {
	if len(done) == 0 {
		return
	}
	fmt.Fprintln(w, "Done")
	for _, d := range done {
		fmt.Fprintf(w, "  ✓ %s\n", clean(d))
	}
	fmt.Fprintln(w)
}

func writeProblems(w io.Writer, title string, problems []module.Finding) {
	if len(problems) == 0 {
		return
	}
	fmt.Fprintln(w, title)
	for _, p := range problems {
		writeFinding(w, p)
	}
	fmt.Fprintln(w)
}

func Prepare(w io.Writer, ready bool, done []string, problems []module.Finding) {
	writeDone(w, done)
	writeProblems(w, "Problems", problems)
	if !ready {
		fmt.Fprintln(w, "NOT READY FOR MIGRATION ASSISTANT")
		fmt.Fprintln(w, "Fix the problems marked ✗ and run mtm prepare again.")
		return
	}
	fmt.Fprint(w, readyText)
}

// Finish prints what finish did and what is left to do by hand.
func Finish(w io.Writer, done []string, problems []module.Finding) {
	writeDone(w, done)
	if module.HasBlocker(problems) {
		writeProblems(w, "Problems", problems)
		fmt.Fprintln(w, "Fix the problems marked ✗ and run mtm finish again.")
		return
	}
	writeProblems(w, "Do these by hand", problems)
	fmt.Fprintln(w, "Next: run mtm verify")
}

// Undo prints what prepare --undo put back. With problems, the Mac is not
// fully back in use until the user does the steps listed.
func Undo(w io.Writer, done []string, problems []module.Finding) {
	writeDone(w, done)
	writeProblems(w, "Problems", problems)
	if len(problems) > 0 {
		fmt.Fprintln(w, "Some things could not be put back; do the steps above, then run mtm prepare again before Migration Assistant.")
		return
	}
	fmt.Fprintln(w, "This Mac is back in use. Run mtm prepare again before Migration Assistant.")
}

func Verify(w io.Writer, checks []module.Check, v verdict.Verdict, details bool) {
	lastModule := ""
	for _, c := range checks {
		if c.Module != lastModule {
			if lastModule != "" {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, clean(c.Module))
			lastModule = c.Module
		}
		writeCheck(w, c, details)
	}
	fmt.Fprintln(w)
	if v.Safe {
		fmt.Fprintln(w, "SAFE TO WIPE")
		fmt.Fprintf(w, "Everything mtm checked arrived%s. You can erase the old Mac, then run mtm cleanup here.\n", checkedModules(checks))
		return
	}
	fmt.Fprintln(w, "NOT SAFE TO WIPE")
	for _, c := range v.Blocking {
		reason := c.Detail
		if reason == "" {
			reason = string(c.Status)
		}
		fmt.Fprintf(w, "  ✗ %s %s: %s\n", clean(c.Module), clean(c.Title), clean(reason))
	}
	for _, item := range v.Pending {
		fmt.Fprintf(w, "  ✗ checklist: %s\n", clean(item))
	}
}

// checkedModules names the modules whose checks the verdict rests on, so
// the SAFE banner claims no more than what was checked. Preflight checks
// the Macs, not the user's data.
func checkedModules(checks []module.Check) string {
	var names []string
	for _, c := range checks {
		if c.Module != preflight.Module && !slices.Contains(names, c.Module) {
			names = append(names, c.Module)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " (" + clean(strings.Join(names, ", ")) + ")"
}

func writeCheck(w io.Writer, c module.Check, details bool) {
	line := fmt.Sprintf("  %s %s", statusSymbol(c.Status), clean(c.Title))
	if c.Detail != "" {
		line += "  (" + clean(c.Detail) + ")"
	}
	fmt.Fprintln(w, line)
	paths := c.Paths
	if !details && len(paths) > shownPaths {
		paths = paths[:shownPaths]
	}
	for _, p := range paths {
		fmt.Fprintf(w, "      %s\n", cleanPath(p))
	}
	if hidden := len(c.Paths) - len(paths); hidden > 0 {
		fmt.Fprintf(w, "      … %d more (use --details)\n", hidden)
	}
}

func JSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	_, err = fmt.Fprintln(w, scrub(string(data)))
	return err
}
