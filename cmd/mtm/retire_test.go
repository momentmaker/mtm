package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/testutil"
)

func TestRetireWithoutATerminalListsChangesAndSteps(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	testutil.WriteFile(t, filepath.Join(os.Getenv("HOME"), "Documents/notes.md"), "notes, edited after prepare\n")
	notTerminal(t)
	var out, errOut bytes.Buffer
	code := run(ctx, []string{"retire"}, &out, &errOut, macApp("OLD-MAC"))
	for _, want := range []string{"Documents/notes.md", "3 steps open", "mtm retire done 'retire:erase'"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s%s", want, out.String(), errOut.String())
		}
	}
	if code != exitProblems {
		t.Errorf("exit %d, want %d for a changed document", code, exitProblems)
	}
}

func TestRetireWithNothingChangedExitsZero(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	notTerminal(t)
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"retire"}, &out, &errOut, macApp("OLD-MAC")); code != exitOK || !strings.Contains(out.String(), "Nothing in your home folder changed") {
		t.Errorf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
}

func TestRetireOnTheNewMacRefuses(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"retire"}, &out, &errOut, macApp("NEW-MAC")); code != exitProblems || !strings.Contains(errOut.String(), "run mtm retire on the old Mac") {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}

func TestRetireStepsCanBeClosedOneAtATime(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	notTerminal(t)
	var out, errOut bytes.Buffer
	run(ctx, []string{"retire"}, &out, &errOut, macApp("OLD-MAC"))
	if code := run(ctx, []string{"retire", "done", "retire:icloud"}, &out, &errOut, macApp("OLD-MAC")); code != exitOK {
		t.Fatalf("done exit %d: %s", code, errOut.String())
	}
	out.Reset()
	if code := run(ctx, []string{"retire", "list"}, &out, &errOut, macApp("OLD-MAC")); code != exitOK || !strings.Contains(out.String(), "2 steps open") {
		t.Errorf("list exit %d:\n%s", code, out.String())
	}
}

func TestRetireWalksTheStepsInATerminal(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	asTerminal(t, "d\nq\n")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"retire"}, &out, &errOut, macApp("OLD-MAC")); code != exitOK {
		t.Fatalf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "[1/3] Sign out of iCloud on this Mac") || !strings.Contains(out.String(), "2 steps open") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestRetireJSON(t *testing.T) {
	preparedHome(t, "OLD-MAC")
	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"retire", "--json"}, &out, &errOut, macApp("OLD-MAC")); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var rep struct {
		Steps struct {
			Entries []struct {
				ID string `json:"id"`
			} `json:"entries"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil || len(rep.Steps.Entries) != 3 {
		t.Errorf("JSON = %s, %v; want 3 steps", out.String(), err)
	}
}
