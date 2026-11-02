package files

import (
	"strings"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/pathset"
)

// excluded paths are never recorded: caches, the trash, logs, mtm's own
// folder, and the Docker Desktop VM, which the docker module checks.
var excluded = pathset.MustNew(
	".mtm/",
	".Trash/",
	"Library/Caches/",
	"Library/Logs/",
	"Library/Containers/*/Data/Library/Caches/",
	"Library/Group Containers/*/Library/Caches/",
	"Library/Containers/com.docker.docker/Data/vms/",
)

// volatile paths come and go on their own, so a missing one is not an error.
var volatile = pathset.MustNew(
	".DS_Store",
	".zcompdump*",
	"Library/Saved Application State/",
)

var fingerprinted = pathset.MustNew(".ssh/", ".gnupg/", ".config/")

func area(rel string) string {
	if rel == "Library" || strings.HasPrefix(rel, "Library/") {
		return manifest.AreaAppState
	}
	return manifest.AreaCritical
}

// shouldFingerprint reports whether a regular file gets a content
// fingerprint: top-level dotfiles and everything in fingerprinted folders,
// or every file when deep is set.
func shouldFingerprint(rel string, deep bool) bool {
	if deep {
		return true
	}
	isTopLevelDotfile := !strings.Contains(rel, "/") && strings.HasPrefix(rel, ".")
	return isTopLevelDotfile || fingerprinted.Match(rel)
}
