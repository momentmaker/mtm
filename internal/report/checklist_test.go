package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/checklist"
)

func sampleList() checklist.List {
	var l checklist.List
	l.Add(
		checklist.Item{ID: "permissions:com.raycast.macos:Accessibility", Title: "Give Raycast Accessibility access", Where: "System Settings > Privacy & Security > Accessibility"},
		checklist.Item{ID: "system:license:dash", Title: "Reactivate the Dash license", Detail: "Enter your Dash license again if it asks."},
	)
	l.Skip("system:license:dash", "sold it", time.Now())
	return l
}

func TestChecklistShowsOpenStepsWithTheirCommand(t *testing.T) {
	var out bytes.Buffer
	Checklist(&out, sampleList(), false)
	for _, want := range []string{"1 step open", "☐ Give Raycast Accessibility access", "System Settings > Privacy & Security > Accessibility", "→ mtm checklist done 'permissions:com.raycast.macos:Accessibility'"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Dash") {
		t.Errorf("a skipped step showed without --all:\n%s", out.String())
	}
}

func TestChecklistAllShowsSkippedStepsWithTheirReason(t *testing.T) {
	var out bytes.Buffer
	Checklist(&out, sampleList(), true)
	if !strings.Contains(out.String(), "– Reactivate the Dash license") || !strings.Contains(out.String(), "skipped: sold it") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestChecklistEmptyAndFinished(t *testing.T) {
	var out bytes.Buffer
	Checklist(&out, checklist.List{}, false)
	if !strings.Contains(out.String(), "mtm finish fills it") {
		t.Errorf("empty output:\n%s", out.String())
	}
	l := sampleList()
	l.Done("permissions:com.raycast.macos:Accessibility", time.Now())
	out.Reset()
	Checklist(&out, l, false)
	if !strings.Contains(out.String(), "Every checklist step is done or skipped.") {
		t.Errorf("finished output:\n%s", out.String())
	}
}

func TestStepEscapesControlCharacters(t *testing.T) {
	var out bytes.Buffer
	Step(&out, 1, 2, checklist.Entry{Item: checklist.Item{Title: "Give \x1b[2JEvil access"}})
	assertNoRawControls(t, out.String())
}

func TestChecklistQuotesAnIdWithSpacesInItsCommand(t *testing.T) {
	var l checklist.List
	l.Add(checklist.Item{ID: "permissions:com.raycast.macos:Screen Recording", Title: "Give Raycast Screen Recording access"})
	var out bytes.Buffer
	Checklist(&out, l, false)
	if want := "→ mtm checklist done 'permissions:com.raycast.macos:Screen Recording'"; !strings.Contains(out.String(), want) {
		t.Errorf("output missing %q:\n%s", want, out.String())
	}
}
