package git

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/momentmaker/mtm/internal/pathset"
)

// important matches ignored files worth fingerprinting: secrets and local
// configuration that exist nowhere but this Mac.
var important = pathset.MustNew(".env*", "*.pem", "*.key", "*.p12", "*.local.*")

// parseRefs reads `git for-each-ref --format=%(refname) %(objectname)`.
// Remote-tracking refs are skipped because their commits live on the
// remote. The stash is read separately with its full list.
func parseRefs(out []byte) map[string]string {
	refs := map[string]string{}
	for _, line := range parseLines(out) {
		name, sha, ok := strings.Cut(line, " ")
		if !ok || strings.HasPrefix(name, "refs/remotes/") || name == "refs/stash" {
			continue
		}
		refs[name] = sha
	}
	return refs
}

func parseLines(out []byte) []string {
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func parseCount(out []byte) (int, error) {
	s := strings.TrimSpace(string(out))
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("parse commit count %q: %w", s, err)
	}
	return n, nil
}

// parseStatus splits `git status --porcelain=v2 -z` output into records
// for comparison, untracked paths, and ignored paths. A rename record keeps
// its original path, which -z sends as the next field.
func parseStatus(out []byte) (records, untracked, ignored []string) {
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		switch f[0] {
		case '?':
			untracked = append(untracked, strings.TrimPrefix(f, "? "))
			records = append(records, f)
		case '!':
			ignored = append(ignored, strings.TrimPrefix(f, "! "))
		case '2':
			if i+1 < len(fields) {
				f += "\x00" + fields[i+1]
				i++
			}
			records = append(records, f)
		default:
			records = append(records, f)
		}
	}
	return records, untracked, ignored
}

// parseMissing returns the object names `git cat-file --batch-check` reported missing.
func parseMissing(out []byte) []string {
	var missing []string
	for _, line := range parseLines(out) {
		if name, ok := strings.CutSuffix(line, " missing"); ok {
			missing = append(missing, name)
		}
	}
	return missing
}

func importantIgnored(ignored []string) []string {
	var out []string
	for _, p := range ignored {
		if !strings.HasSuffix(p, "/") && important.Match(p) {
			out = append(out, p)
		}
	}
	return out
}
