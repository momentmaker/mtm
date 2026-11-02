package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/momentmaker/mtm/internal/module"
)

// protectedDirs can only be read with Full Disk Access, so they show
// whether the terminal has it.
var protectedDirs = []string{"Library/Mail", "Library/Messages", "Library/Safari", "Library/Containers/com.apple.Safari"}

func (Module) Scan(env module.Env) ([]module.Finding, error) {
	findings := []module.Finding{{
		Module:   Name,
		ID:       "home",
		Severity: module.SeverityInfo,
		Handler:  module.ByMigrationAssistant,
		Title:    "Home folder: files, dotfiles, and projects",
		Detail:   "Migration Assistant copies these. mtm records them in prepare and checks them in verify.",
	}}
	var blocked []string
	for _, rel := range protectedDirs {
		if _, err := os.ReadDir(filepath.Join(env.Home, filepath.FromSlash(rel))); errors.Is(err, fs.ErrPermission) {
			blocked = append(blocked, "~/"+rel)
		}
	}
	if len(blocked) > 0 {
		findings = append(findings, module.Finding{
			Module:   Name,
			ID:       "full-disk-access",
			Severity: module.SeverityWarn,
			Handler:  module.Manual,
			Title:    "Your terminal has no Full Disk Access, so some folders can't be checked",
			Detail:   "Unreadable: " + strings.Join(blocked, ", "),
			Fix:      "Optional: System Settings > Privacy & Security > Full Disk Access, add your terminal app, then re-run. mtm cleanup reminds you to remove it afterward.",
		})
	}
	return findings, nil
}
