package verdict

import (
	"testing"

	"github.com/momentmaker/mtm/internal/module"
)

func TestDecide(t *testing.T) {
	pass := module.Check{ID: "a", Status: module.StatusPass, Critical: true}
	warn := module.Check{ID: "b", Status: module.StatusWarn}
	info := module.Check{ID: "c", Status: module.StatusInfo}
	fail := module.Check{ID: "d", Status: module.StatusFail}
	criticalWarn := module.Check{ID: "e", Status: module.StatusWarn, Critical: true}
	criticalInfo := module.Check{ID: "f", Status: module.StatusInfo, Critical: true}

	tests := []struct {
		name     string
		checks   []module.Check
		pending  []string
		safe     bool
		blocking int
	}{
		{"all pass", []module.Check{pass}, nil, true, 0},
		{"non-critical warn and info allowed", []module.Check{pass, warn, info}, nil, true, 0},
		{"any fail blocks", []module.Check{pass, fail}, nil, false, 1},
		{"critical warn blocks", []module.Check{criticalWarn}, nil, false, 1},
		{"critical info blocks", []module.Check{criticalInfo}, nil, false, 1},
		{"open checklist item blocks", []module.Check{pass}, []string{"Grant Accessibility to Raycast"}, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Decide(tt.checks, tt.pending)
			if v.Safe != tt.safe || len(v.Blocking) != tt.blocking {
				t.Errorf("Decide = safe %v blocking %d, want safe %v blocking %d", v.Safe, len(v.Blocking), tt.safe, tt.blocking)
			}
		})
	}
}
