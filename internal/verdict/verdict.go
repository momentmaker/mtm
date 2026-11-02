// Package verdict decides whether the old Mac is safe to wipe.
package verdict

import "github.com/momentmaker/mtm/internal/module"

type Verdict struct {
	Safe     bool           `json:"safe"`
	Blocking []module.Check `json:"blocking,omitempty"`
	Pending  []string       `json:"pending_checklist,omitempty"`
}

// Decide applies spec section 8.5: no failed check, every critical check
// passed, and no checklist item left open.
func Decide(checks []module.Check, pending []string) Verdict {
	v := Verdict{Pending: pending}
	for _, c := range checks {
		if c.Status == module.StatusFail || (c.Critical && c.Status != module.StatusPass) {
			v.Blocking = append(v.Blocking, c)
		}
	}
	v.Safe = len(v.Blocking) == 0 && len(pending) == 0
	return v
}
