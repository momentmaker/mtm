package files

import (
	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
)

type Outcome int

const (
	Match Outcome = iota
	ChangedAfterPrepare
	Corrupt
	MissingVolatile
	MissingCritical
	MissingAppState
	CloudOnly
	NotChecked
	OnOtherVolume
)

// Observed is what verify finds at a recorded path on the new Mac. Hash is
// filled only when the record has a fingerprint and the file was not
// modified after prepare.
type Observed struct {
	Exists     bool
	Unreadable bool
	Dataless   bool
	Type       string
	Size       int64
	MTime      int64
	Target     string
	Resolves   bool
	Hash       string
}

// Classify applies spec section 8.4 to one recorded entry. A file modified
// after prepare is a normal change, not damage. Folders are checked for
// presence only, because their times change whenever an entry is added.
// An entry type this mtm doesn't know comes from a newer or damaged record
// and can't be verified, so it fails. A link whose target existed at
// prepare and is gone now lost that data wherever it lived, even if the
// link itself was rewritten.
func Classify(rec manifest.Entry, obs Observed, preparedAt int64, isVolatile func(string) bool) Outcome {
	switch {
	case !knownType(rec.Type):
		return Corrupt
	case rec.Type == manifest.TypeMount:
		return OnOtherVolume
	case rec.Type == manifest.TypeCloud:
		return CloudOnly
	case rec.Type == manifest.TypeUnreadable:
		return NotChecked
	case !obs.Exists && isVolatile(rec.Path):
		return MissingVolatile
	case !obs.Exists && isCritical(rec.Area):
		return MissingCritical
	case !obs.Exists:
		return MissingAppState
	case obs.Unreadable:
		return NotChecked
	case obs.Dataless:
		return CloudOnly
	case rec.Type == manifest.TypeLink && rec.Resolves && obs.Type == manifest.TypeLink && !obs.Resolves:
		return Corrupt
	case rec.Type != manifest.TypeDir && changedAfterPrepare(rec, obs, preparedAt):
		return ChangedAfterPrepare
	case obs.Type != rec.Type:
		return Corrupt
	case rec.Type == manifest.TypeFile && (obs.Size != rec.Size || (rec.Hash != "" && obs.Hash != rec.Hash)):
		return Corrupt
	case rec.Type == manifest.TypeLink && obs.Target != rec.Target:
		return Corrupt
	}
	return Match
}

// changedAfterPrepare needs a time later than prepare that also differs
// from the recorded one. A file written during the walk, or dated in the
// future, keeps its recorded time when copied intact, so it still gets the
// corruption check.
func changedAfterPrepare(rec manifest.Entry, obs Observed, preparedAt int64) bool {
	return obs.MTime > preparedAt && obs.MTime != rec.MTime
}

func knownType(t string) bool {
	switch t {
	case manifest.TypeFile, manifest.TypeDir, manifest.TypeLink, manifest.TypeCloud, manifest.TypeUnreadable, manifest.TypeMount:
		return true
	}
	return false
}

// isCritical treats any area other than app state as critical, so an area
// added by a newer mtm, or a damaged one, fails safe.
func isCritical(area string) bool {
	return area != manifest.AreaAppState
}

// statusFor is statusOf with one exception: critical data that could not
// be read is unverified, so it must block SAFE TO WIPE (spec 8.5 rule 2).
func statusFor(o Outcome, area string) module.Status {
	if o == NotChecked && isCritical(area) {
		return module.StatusWarn
	}
	return statusOf(o)
}

func statusOf(o Outcome) module.Status {
	switch o {
	case Match:
		return module.StatusPass
	case MissingAppState:
		return module.StatusWarn
	case MissingCritical, Corrupt:
		return module.StatusFail
	}
	return module.StatusInfo
}
