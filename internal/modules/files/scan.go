package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/momentmaker/mtm/internal/module"
)

// protectedDirs can only be read with Full Disk Access, so they show
// whether the terminal has it.
var protectedDirs = []string{"Library/Mail", "Library/Messages", "Library/Safari", "Library/Containers/com.apple.Safari"}

// criticalDirs are privacy-protected folders that hold critical data. If
// the terminal can't read them, verify can never say SAFE TO WIPE.
var criticalDirs = []string{"Desktop", "Documents", "Downloads", "Pictures/Photos Library.photoslibrary"}

func (Module) Scan(env module.Env) ([]module.Finding, error) {
	findings := []module.Finding{{
		Module:   Name,
		ID:       "home",
		Severity: module.SeverityInfo,
		Handler:  module.ByMigrationAssistant,
		Title:    "Home folder: files, dotfiles, and projects",
		Detail:   "Migration Assistant copies these. mtm records them in prepare and checks them in verify.",
	}}
	if blocked := unreadableDirs(env.Home, criticalDirs); len(blocked) > 0 {
		findings = append(findings, module.Finding{
			Module:   Name,
			ID:       "unreadable-critical",
			Severity: module.SeverityBlocker,
			Handler:  module.Manual,
			Title:    "Your terminal can't read some of your folders, so mtm verify could never say SAFE TO WIPE",
			Detail:   "Unreadable: " + strings.Join(blocked, ", "),
			Fix:      accessFix("mtm scan"),
		})
	}
	if blocked := unreadableDirs(env.Home, protectedDirs); len(blocked) > 0 {
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

// unreadableDirs returns the display paths of dirs that exist but can't be
// listed. Like the walk, it treats any failure other than absence as
// unreadable.
func unreadableDirs(home string, dirs []string) []string {
	var blocked []string
	for _, rel := range dirs {
		_, err := os.ReadDir(filepath.Join(home, filepath.FromSlash(rel)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			blocked = append(blocked, displayPath(rel))
		}
	}
	return blocked
}
