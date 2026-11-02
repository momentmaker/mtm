package files

import (
	"testing"

	"github.com/momentmaker/mtm/internal/manifest"
)

func TestArea(t *testing.T) {
	tests := map[string]string{
		"Library":                     manifest.AreaAppState,
		"Library/Preferences/x.plist": manifest.AreaAppState,
		"GitHub/app/main.go":          manifest.AreaCritical,
		"LibraryNotes/todo.md":        manifest.AreaCritical,
		".zshrc":                      manifest.AreaCritical,
	}
	for rel, want := range tests {
		if got := area(rel); got != want {
			t.Errorf("area(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestShouldFingerprint(t *testing.T) {
	tests := []struct {
		rel  string
		deep bool
		want bool
	}{
		{".zshrc", false, true},
		{".ssh/id_ed25519", false, true},
		{".gnupg/pubring.kbx", false, true},
		{".config/git/config", false, true},
		{"GitHub/app/main.go", false, false},
		{"GitHub/app/.env", false, false},
		{"GitHub/app/main.go", true, true},
	}
	for _, tt := range tests {
		if got := shouldFingerprint(tt.rel, tt.deep); got != tt.want {
			t.Errorf("shouldFingerprint(%q, %v) = %v, want %v", tt.rel, tt.deep, got, tt.want)
		}
	}
}

func TestExcludedAndVolatile(t *testing.T) {
	for _, rel := range []string{
		".mtm/manifest.json",
		".Trash/old.txt",
		"Library/Caches/com.apple.Safari",
		"Library/Logs/DiagnosticReports/x.ips",
		"Library/Containers/com.apple.mail/Data/Library/Caches/x",
		"Library/Containers/com.docker.docker/Data/vms/0/data/Docker.raw",
	} {
		if !excluded.Match(rel) {
			t.Errorf("excluded.Match(%q) = false, want true", rel)
		}
	}
	if excluded.Match("Library/Preferences/com.apple.dock.plist") {
		t.Error("preferences excluded")
	}
	for _, rel := range []string{"GitHub/.DS_Store", ".zcompdump-host-5.9", "Library/Saved Application State/x/data.data"} {
		if !volatile.Match(rel) {
			t.Errorf("volatile.Match(%q) = false, want true", rel)
		}
	}
}
