package report

import (
	"fmt"
	"io"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/words"
)

func stepSymbol(e checklist.Entry) string {
	switch {
	case e.Open():
		return "☐"
	case e.Status == checklist.Done:
		return "✓"
	}
	return "–"
}

// The commands that keep a checklist: checklist on the new Mac, retire on
// the old one.
const (
	ChecklistCommand = "checklist"
	RetireCommand    = "retire"
)

// emptyChecklist says, per command, where its steps come from.
var emptyChecklist = map[string]string{
	ChecklistCommand: "The checklist is empty. mtm finish and mtm verify fill it on the new Mac.",
	RetireCommand:    "There are no retirement steps yet. mtm retire adds them on the old Mac.",
}

// Checklist prints the open steps, or every step with all, each with the
// mtm command that closes it.
func Checklist(w io.Writer, l checklist.List, all bool, command string) {
	open := len(l.Pending())
	if len(l.Entries) == 0 {
		fmt.Fprintln(w, emptyChecklist[command])
		return
	}
	if open == 0 && !all {
		fmt.Fprintln(w, "Every checklist step is done or skipped.")
		return
	}
	fmt.Fprintf(w, "%s open\n", words.Count(open, "step", "steps"))
	for _, e := range l.Entries {
		if !e.Open() && !all {
			continue
		}
		fmt.Fprintf(w, "  %s %s\n", stepSymbol(e), clean(e.Title))
		writeStepBody(w, e)
		switch {
		case e.Open():
			fmt.Fprintf(w, "      → mtm %s done %s\n", command, clean(macos.ShellQuote(e.ID)))
		case e.Status == checklist.Skipped:
			fmt.Fprintf(w, "      skipped: %s\n", clean(e.Reason))
		}
	}
}

func writeStepBody(w io.Writer, e checklist.Entry) {
	if e.Where != "" {
		fmt.Fprintf(w, "      %s\n", clean(e.Where))
	}
	if e.Detail != "" {
		fmt.Fprintf(w, "      %s\n", clean(e.Detail))
	}
}

// Step prints one step of the walk-through: the nth of total.
func Step(w io.Writer, n, total int, e checklist.Entry) {
	fmt.Fprintf(w, "\n[%d/%d] %s\n", n, total, clean(e.Title))
	writeStepBody(w, e)
}
