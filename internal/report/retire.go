package report

import (
	"fmt"
	"io"
	"time"

	"github.com/momentmaker/mtm/internal/module"
)

// Retire prints what changed on the old Mac since prepare. It's a list for
// the user to act on, not a verdict: Migration Assistant may have copied a
// change already, and only the user knows which ones matter.
func Retire(w io.Writer, preparedAt time.Time, changes []module.Check, problems []module.Finding, details bool) {
	fmt.Fprintln(w, "Do these only after mtm verify says SAFE TO WIPE on the new Mac.")
	fmt.Fprintln(w)
	writeProblems(w, "Problems", problems)
	since := preparedAt.Local().Format(time.RFC1123)
	if len(changes) == 0 {
		fmt.Fprintf(w, "Nothing in your home folder changed after mtm prepare (%s).\n\n", since)
		return
	}
	fmt.Fprintf(w, "Changed on this Mac after mtm prepare (%s)\n", since)
	for _, c := range changes {
		writeCheck(w, c, details)
	}
	fmt.Fprintln(w, "Copy what the new Mac still needs before you erase this one.")
	fmt.Fprintln(w)
}
