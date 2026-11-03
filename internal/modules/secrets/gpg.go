package secrets

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
)

// GPGKey is a secret key's identity, never its material. Secret is how
// gpg reports the secret part: "+" here, "#" missing (a stub), or the
// serial number of the card that holds it.
type GPGKey struct {
	Fingerprint string `json:"fingerprint"`
	Algorithm   string `json:"algorithm"`
	Created     int64  `json:"created"`
	Secret      string `json:"secret"`
}

func (k GPGKey) stub() bool { return k.Secret == "#" }

// describe names a key without its fingerprint, which reports never show.
func (k GPGKey) describe() string {
	return fmt.Sprintf("the %s key from %s", k.Algorithm, time.Unix(k.Created, 0).UTC().Format("2006-01-02"))
}

// gpgListArgs list secret keys without changing the keyring: no trust
// database check, and nothing that could prompt.
var gpgListArgs = []string{"--batch", "--no-tty", "--no-auto-check-trustdb", "--with-colons", "--list-secret-keys"}

// gnupgHome is the folder gpg keeps keys in.
func (m Module) gnupgHome(home string) string {
	if dir := m.Getenv("GNUPGHOME"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".gnupg")
}

func (m Module) gpgPath() string {
	for _, p := range m.GPG {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// errNoGPG means a GnuPG folder exists but no gpg to list its keys.
var errNoGPG = errors.New("gpg isn't installed")

// gpgKeys lists the secret keys. A Mac without a GnuPG folder has none,
// and gpg isn't run, because it would create one.
func (m Module) gpgKeys(env module.Env) ([]GPGKey, error) {
	if _, err := os.Stat(m.gnupgHome(env.Home)); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	gpg := m.gpgPath()
	if gpg == "" {
		return nil, fmt.Errorf("%w (looked in %s)", errNoGPG, strings.Join(m.GPG, ", "))
	}
	out, err := env.Run.Run(env.Ctx, runner.Command{Name: gpg, Args: gpgListArgs})
	if err != nil {
		return nil, fmt.Errorf("list GPG keys: %w", err)
	}
	return parseSecretKeys(string(out.Stdout))
}

var gpgAlgorithms = map[string]string{"1": "RSA", "17": "DSA", "18": "ECDH", "19": "ECDSA", "22": "EdDSA"}

// parseSecretKeys reads gpg's colon format: a "sec" line per primary key,
// followed by its "fpr" line. Subkeys ("ssb") travel with their primary.
func parseSecretKeys(out string) ([]GPGKey, error) {
	var keys []GPGKey
	wantFpr := false
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, ":")
		switch {
		case f[0] == "sec" && len(f) >= 15:
			created, err := strconv.ParseInt(f[5], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse gpg output: creation time %q", f[5])
			}
			keys = append(keys, GPGKey{Algorithm: algorithm(f), Created: created, Secret: f[14]})
			wantFpr = true
		case f[0] == "fpr" && wantFpr && len(f) >= 10:
			keys[len(keys)-1].Fingerprint = f[9]
			wantFpr = false
		case f[0] == "ssb" || f[0] == "uid":
			wantFpr = false
		}
	}
	for _, k := range keys {
		if k.Fingerprint == "" {
			return nil, errors.New("parse gpg output: a secret key has no fingerprint")
		}
	}
	return keys, nil
}

// algorithm names a key's algorithm and size, or curve for elliptic keys.
func algorithm(f []string) string {
	name, ok := gpgAlgorithms[f[3]]
	if !ok {
		name = "algorithm " + f[3]
	}
	if len(f) > 16 && f[16] != "" {
		return name + " " + f[16]
	}
	return name + " " + f[2]
}
