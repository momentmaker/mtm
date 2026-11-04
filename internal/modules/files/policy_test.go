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
	for _, rel := range []string{
		"GitHub/.DS_Store",
		".zcompdump-host-5.9",
		"Library/Saved Application State/x/data.data",
		".zsh_sessions/A1B2.session",
		".bash_sessions/A1B2.history",
		".npm/_logs/2026-11-01T10_00_00_000Z-debug-0.log",
		".npm/_cacache/content-v2/sha512/ab/cd",
		".cache/pip/http/x",
		".vscode/extensions/ms-python.python-2026.1.0/package.json",
		".cursor/extensions/x-1.0.0/package.json",
		".cargo/registry/cache/index.crates.io/serde-1.0.0.crate",
		"go/pkg/mod/cache/download/x/@v/v1.0.0.zip",
		".gradle/caches/modules-2/files-2.1/x.jar",
		".m2/repository/org/x/1.0/x-1.0.jar",
		".bun/install/cache/x@1.0.0/package.json",
	} {
		if !volatile.Match(rel) {
			t.Errorf("volatile.Match(%q) = false, want true", rel)
		}
	}
	for _, rel := range []string{".npm/npmrc", ".vscode/settings.json", ".cargo/config.toml", "go/pkg/mod/github.com/x/y@v1/y.go", ".gradle/gradle.properties", ".m2/settings.xml", "GitHub/app/.cache/x"} {
		if volatile.Match(rel) {
			t.Errorf("volatile.Match(%q) = true, want false", rel)
		}
	}
}

func TestMissingGitLockIsVolatile(t *testing.T) {
	m := New()
	for _, rel := range []string{
		"GitHub/app/.git/index.lock",
		"GitHub/app/.git/objects/maintenance.lock",
		"GitHub/app/.git/refs/heads/main.lock",
		".git/index.lock",
	} {
		rec := manifest.Entry{Path: rel, Type: manifest.TypeFile, Area: manifest.AreaCritical}
		if got := Classify(rec, Observed{}, 0, m.isVolatile); got != MissingVolatile {
			t.Errorf("Classify(%q) = %v, want MissingVolatile", rel, got)
		}
	}
}

func TestMissingProjectLockfileStaysCritical(t *testing.T) {
	m := New()
	for _, rel := range []string{
		"GitHub/app/Cargo.lock",
		"GitHub/app/yarn.lock",
		"GitHub/app/.github/Cargo.lock",
		"GitHub/app/.git/config",
	} {
		rec := manifest.Entry{Path: rel, Type: manifest.TypeFile, Area: manifest.AreaCritical}
		if got := Classify(rec, Observed{}, 0, m.isVolatile); got != MissingCritical {
			t.Errorf("Classify(%q) = %v, want MissingCritical", rel, got)
		}
	}
}
