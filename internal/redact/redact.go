// Package redact removes secret values from text mtm writes to logs and reports.
package redact

import (
	"regexp"
	"strings"
)

const Mask = "[REDACTED]"

var secretNameParts = []string{
	"password", "passwd", "passphrase", "secret", "token",
	"apikey", "api_key", "api-key", "credential", "private_key", "auth",
}

var (
	urlCredentials = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^\s/@]+@`)
	knownTokens    = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_\-]{20,}|AKIA[0-9A-Z]{16}|xox[abpr]-[A-Za-z0-9\-]{10,})`)
)

// Text masks credentials embedded in URLs and well-known token formats.
func Text(s string) string {
	s = urlCredentials.ReplaceAllString(s, "${1}"+Mask+"@")
	return knownTokens.ReplaceAllString(s, Mask)
}

// Args returns a copy of command arguments with secret values masked: the
// value after a secret-looking flag, the value in --secret=value and
// SECRET=value, and anything Text masks.
func Args(args []string) []string {
	out := make([]string, len(args))
	maskNext := false
	for i, arg := range args {
		switch {
		case maskNext:
			out[i] = Mask
			maskNext = false
		case strings.HasPrefix(arg, "-"):
			name, _, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			switch {
			case !isSecretName(name):
				out[i] = Text(arg)
			case hasValue:
				out[i] = arg[:strings.IndexByte(arg, '=')+1] + Mask
			default:
				out[i] = arg
				maskNext = true
			}
		default:
			if key, _, ok := strings.Cut(arg, "="); ok && isSecretName(key) {
				out[i] = key + "=" + Mask
			} else {
				out[i] = Text(arg)
			}
		}
	}
	return out
}

func isSecretName(name string) bool {
	lower := strings.ToLower(name)
	for _, part := range secretNameParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}
