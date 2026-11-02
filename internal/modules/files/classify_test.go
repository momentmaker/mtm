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
	resolving := link
	resolving.Resolves = true
	dir := manifest.Entry{Path: "GitHub", Type: manifest.TypeDir, Area: manifest.AreaCritical}
	same := Observed{Exists: true, Type: manifest.TypeFile, Size: 10, MTime: 900}
	late := file
	late.MTime = 1_200
	lateHashed := late
	lateHashed.Hash = "sha256:aa"

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
		{"recorded after prepare, unchanged time, size differs", late, Observed{Exists: true, Type: manifest.TypeFile, Size: 11, MTime: 1_200}, notVolatile, Corrupt},
		{"recorded after prepare, unchanged time, fingerprint differs", lateHashed, withHash(Observed{Exists: true, Type: manifest.TypeFile, Size: 10, MTime: 1_200}, "sha256:bb"), notVolatile, Corrupt},
		{"recorded after prepare, newer time, size differs", late, Observed{Exists: true, Type: manifest.TypeFile, Size: 11, MTime: 1_300}, notVolatile, ChangedAfterPrepare},
		{"link target gone since prepare", resolving, Observed{Exists: true, Type: manifest.TypeLink, Target: "GitHub", MTime: 900}, notVolatile, Corrupt},
		{"link target gone, link rewritten after prepare", resolving, Observed{Exists: true, Type: manifest.TypeLink, Target: "GitHub", MTime: 1_500}, notVolatile, Corrupt},
		{"link target still there", resolving, Observed{Exists: true, Type: manifest.TypeLink, Target: "GitHub", MTime: 900, Resolves: true}, notVolatile, Match},
		{"old record, link dangling now", link, Observed{Exists: true, Type: manifest.TypeLink, Target: "GitHub", MTime: 900}, notVolatile, Match},
		{"mount point, critical", manifest.Entry{Path: "code", Type: manifest.TypeMount, Area: manifest.AreaCritical}, Observed{}, notVolatile, OnOtherVolume},
		{"mount point, app state", manifest.Entry{Path: "Library/Vol", Type: manifest.TypeMount, Area: manifest.AreaAppState}, Observed{}, notVolatile, OnOtherVolume},
		{"missing, unknown area", manifest.Entry{Path: "x", Type: manifest.TypeFile, Area: "future"}, Observed{}, notVolatile, MissingCritical},
		{"unknown type", manifest.Entry{Path: "x", Type: "future", Area: manifest.AreaCritical}, same, notVolatile, Corrupt},
		{"unknown type, missing", manifest.Entry{Path: "x", Type: "future", Area: manifest.AreaAppState}, Observed{}, notVolatile, Corrupt},
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

func TestUnknownAreaCountsAsCritical(t *testing.T) {
	if got := statusFor(NotChecked, "future"); got != module.StatusWarn {
		t.Errorf("not checked in an unknown area = %q, want warn", got)
	}
	tl := newTally()
	tl.add(manifest.Entry{Path: "Library/Future/x", Type: manifest.TypeFile, Area: "future"}, MissingCritical)
	if c := tl.checks()[0]; !c.Critical || c.Status != module.StatusFail {
		t.Errorf("check = %+v, want a critical fail", c)
	}
}

func TestMountPointNeverBlocks(t *testing.T) {
	for _, a := range []string{manifest.AreaCritical, manifest.AreaAppState} {
		if got := statusFor(OnOtherVolume, a); got != module.StatusInfo {
			t.Errorf("statusFor(OnOtherVolume, %s) = %q, want info", a, got)
		}
	}
	tl := newTally()
	tl.add(manifest.Entry{Path: "code", Type: manifest.TypeMount, Area: manifest.AreaCritical}, OnOtherVolume)
	c := tl.checks()[0]
	if c.Title != "code" || c.Status != module.StatusPass || c.Detail != "1 on another volume, not checked" {
		t.Errorf("check = %+v, want its own passing group noting the volume", c)
	}
}
