package report

import (
	"fmt"
	"io"

	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/words"
)

func stepSymbol(s checklist.Status) string {
	switch s {
	case checklist.Done:
		return "✓"
	case checklist.Skipped:
		return "–"
	}
	return "☐"
}

// Checklist prints the open steps, or every step with all, each with the
// command that closes it.
func Checklist(w io.Writer, l checklist.List, all bool) {
	open := len(l.Pending())
	if len(l.Entries) == 0 {
		fmt.Fprintln(w, "The checklist is empty. mtm finish fills it on the new Mac.")
		return
	}
	if open == 0 && !all {
		fmt.Fprintln(w, "Every checklist step is done or skipped.")
		return
	}
	fmt.Fprintf(w, "%s open\n", words.Count(open, "step", "steps"))
	for _, e := range l.Entries {
		if e.Status != checklist.Pending && !all {
			continue
		}
		fmt.Fprintf(w, "  %s %s\n", stepSymbol(e.Status), clean(e.Title))
		writeStepBody(w, e)
		switch e.Status {
		case checklist.Pending:
			fmt.Fprintf(w, "      → mtm checklist done %s\n", clean(e.ID))
		case checklist.Skipped:
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
