package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/module"
)

func TestRetireListsChangesGroupedByFolder(t *testing.T) {
	var out bytes.Buffer
	changes := []module.Check{{Module: "files", ID: "files:Documents", Status: module.StatusWarn, Title: "Documents", Detail: "1 changed, 1 new",
		Paths: []string{"Documents/a.md", "Documents/notes.md"}}}
	Retire(&out, time.Now(), changes, nil, false)
	for _, want := range []string{"SAFE TO WIPE on the new Mac", "Changed on this Mac after mtm prepare", "! Documents  (1 changed, 1 new)", "Documents/notes.md", "Copy what the new Mac still needs"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRetireWithNothingChanged(t *testing.T) {
	var out bytes.Buffer
	Retire(&out, time.Now(), nil, nil, false)
	if !strings.Contains(out.String(), "Nothing in your home folder changed after mtm prepare") {
		t.Errorf("output:\n%s", out.String())
	}
}
