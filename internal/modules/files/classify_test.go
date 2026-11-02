package files

import (
	"testing"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
)

func withHash(o Observed, h string) Observed {
	o.Hash = h
	return o
}

func TestClassify(t *testing.T) {
	const prepared = int64(1_000)
	notVolatile := func(string) bool { return false }
	isVolatile := func(string) bool { return true }

	file := manifest.Entry{Path: "GitHub/a.go", Type: manifest.TypeFile, Size: 10, MTime: 900, Area: manifest.AreaCritical}
	hashed := file
	hashed.Hash = "sha256:aa"
	appFile := manifest.Entry{Path: "Library/Preferences/p.plist", Type: manifest.TypeFile, Size: 5, MTime: 900, Area: manifest.AreaAppState}
	link := manifest.Entry{Path: "link", Type: manifest.TypeLink, Target: "GitHub", MTime: 900, Area: manifest.AreaCritical}
	dir := manifest.Entry{Path: "GitHub", Type: manifest.TypeDir, Area: manifest.AreaCritical}
	same := Observed{Exists: true, Type: manifest.TypeFile, Size: 10, MTime: 900}

	tests := []struct {
		name     string
		rec      manifest.Entry
		obs      Observed
		volatile func(string) bool
		want     Outcome
	}{
		{"identical", file, same, notVolatile, Match},
		{"identical with fingerprint", hashed, withHash(same, "sha256:aa"), notVolatile, Match},
		{"modified after prepare", file, Observed{Exists: true, Type: manifest.TypeFile, Size: 99, MTime: 1_500}, notVolatile, ChangedAfterPrepare},
		{"size differs, old time", file, Observed{Exists: true, Type: manifest.TypeFile, Size: 11, MTime: 900}, notVolatile, Corrupt},
		{"fingerprint differs, old time", hashed, withHash(same, "sha256:bb"), notVolatile, Corrupt},
		{"missing critical", file, Observed{}, notVolatile, MissingCritical},
		{"missing app state", appFile, Observed{}, notVolatile, MissingAppState},
		{"missing volatile", file, Observed{}, isVolatile, MissingVolatile},
		{"recorded cloud-only", manifest.Entry{Path: "x", Type: manifest.TypeCloud, Area: manifest.AreaCritical}, Observed{}, notVolatile, CloudOnly},
		{"recorded unreadable", manifest.Entry{Path: "x", Type: manifest.TypeUnreadable, Area: manifest.AreaCritical}, Observed{}, notVolatile, NotChecked},
		{"unreadable now", file, Observed{Exists: true, Unreadable: true}, notVolatile, NotChecked},
		{"evicted to the cloud", file, Observed{Exists: true, Dataless: true, Type: manifest.TypeFile}, notVolatile, CloudOnly},
		{"symlink target changed", link, Observed{Exists: true, Type: manifest.TypeLink, Target: "Elsewhere", MTime: 900}, notVolatile, Corrupt},
		{"file became a folder", file, Observed{Exists: true, Type: manifest.TypeDir, MTime: 900}, notVolatile, Corrupt},
		{"folder with new entries", dir, Observed{Exists: true, Type: manifest.TypeDir, MTime: 1_500}, notVolatile, Match},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.rec, tt.obs, prepared, tt.volatile); got != tt.want {
				t.Errorf("Classify = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatusOf(t *testing.T) {
	tests := map[Outcome]module.Status{
		Match:               module.StatusPass,
		ChangedAfterPrepare: module.StatusInfo,
		MissingVolatile:     module.StatusInfo,
		CloudOnly:           module.StatusInfo,
		NotChecked:          module.StatusInfo,
		MissingAppState:     module.StatusWarn,
		MissingCritical:     module.StatusFail,
		Corrupt:             module.StatusFail,
	}
	for o, want := range tests {
		if got := statusOf(o); got != want {
			t.Errorf("statusOf(%v) = %q, want %q", o, got, want)
		}
	}
}

func TestStatusForUnreadableDependsOnArea(t *testing.T) {
	if got := statusFor(NotChecked, manifest.AreaCritical); got != module.StatusWarn {
		t.Errorf("critical not checked = %q, want warn", got)
	}
	if got := statusFor(NotChecked, manifest.AreaAppState); got != module.StatusInfo {
		t.Errorf("app state not checked = %q, want info", got)
	}
	for _, o := range []Outcome{Match, ChangedAfterPrepare, MissingVolatile, CloudOnly, MissingAppState, MissingCritical, Corrupt} {
		for _, a := range []string{manifest.AreaCritical, manifest.AreaAppState} {
			if got, want := statusFor(o, a), statusOf(o); got != want {
				t.Errorf("statusFor(%v, %s) = %q, want %q", o, a, got, want)
			}
		}
	}
}
