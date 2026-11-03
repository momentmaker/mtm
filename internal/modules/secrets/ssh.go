package secrets

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/momentmaker/mtm/internal/catalog"
)

// SSHKey is a key in ~/.ssh, named by its private key's file. A key with a
// public half records the public key's type and fingerprint, which tell it
// from a new key with the same name. Only public keys are read; a private
// key is only looked for.
type SSHKey struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Public      bool   `json:"public,omitempty"`
	Private     bool   `json:"private,omitempty"`
}

// hardware is true for a key whose private half lives in a security key
// or, with macOS's ssh-keychain.dylib, in the Secure Enclave. OpenSSH
// calls both sk- keys.
func (k SSHKey) hardware() bool { return strings.HasPrefix(k.Type, "sk-") }

func sshDir(home string) string { return filepath.Join(home, ".ssh") }

// sshKeys lists the key pairs in ~/.ssh: every *.pub file, with its
// private half if one sits beside it, and id_* files with no public half.
// Other files there, such as config and known_hosts, aren't keys.
func sshKeys(home string) ([]SSHKey, error) {
	dir := sshDir(home)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		if e.Type().IsRegular() {
			names[e.Name()] = true
		}
	}
	var keys []SSHKey
	for _, e := range entries {
		name := e.Name()
		if !names[name] {
			continue
		}
		switch {
		case strings.HasSuffix(name, ".pub"):
			k, err := readPublicKey(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			k.Name = strings.TrimSuffix(name, ".pub")
			k.Private = names[k.Name]
			keys = append(keys, k)
		case strings.HasPrefix(name, "id_") && !names[name+".pub"]:
			keys = append(keys, SSHKey{Name: name, Private: true})
		}
	}
	return keys, nil
}

// readPublicKey reads a public key's type and computes its fingerprint as
// ssh-keygen -l does: the SHA-256 of the key blob. A file mtm can't parse
// is still recorded, by name only.
func readPublicKey(path string) (SSHKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SSHKey{}, fmt.Errorf("read %s: %w", path, err)
	}
	k := SSHKey{Public: true}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return k, nil
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return k, nil
	}
	sum := sha256.Sum256(blob)
	k.Type = fields[0]
	k.Fingerprint = "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	return k, nil
}

// sshConfig is what ~/.ssh/config says about agents: the IdentityAgent
// sockets it names, and whether a SecurityKeyProvider is macOS's Secure
// Enclave library. Include files aren't followed.
type sshConfig struct {
	agents        []string
	secureEnclave bool
}

const secureEnclaveProvider = "ssh-keychain.dylib"

func readSSHConfig(home string) (sshConfig, error) {
	path := filepath.Join(sshDir(home), "config")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return sshConfig{}, nil
	}
	if err != nil {
		return sshConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var c sshConfig
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		key, value := configLine(sc.Text())
		switch strings.ToLower(key) {
		case "identityagent":
			if v := expandHome(value, home); v != "" && v != "none" && v != "SSH_AUTH_SOCK" {
				c.agents = append(c.agents, v)
			}
		case "securitykeyprovider":
			c.secureEnclave = c.secureEnclave || strings.Contains(value, secureEnclaveProvider)
		}
	}
	if err := sc.Err(); err != nil {
		return sshConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	return c, nil
}

// configLine splits an ssh_config line into its keyword and argument.
// The two are separated by spaces or one "=", and the argument may be
// quoted.
func configLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", ""
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return line, ""
	}
	value := strings.TrimLeft(line[i:], " \t")
	value = strings.TrimSpace(strings.TrimPrefix(value, "="))
	return line[:i], strings.Trim(value, `"`)
}

// expandHome resolves the forms ssh_config uses for the home folder.
func expandHome(p, home string) string {
	switch {
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	case strings.HasPrefix(p, "%d/"):
		return filepath.Join(home, p[3:])
	}
	return p
}

// agentApp finds the catalog app whose SSH agent listens at socket. A
// socket is often a link to the app's own, such as ~/.1password/agent.sock,
// so links are followed when the socket exists.
func agentApp(c *catalog.Catalog, socket, home string) (catalog.App, bool) {
	want := []string{filepath.Clean(socket)}
	if real, err := filepath.EvalSymlinks(socket); err == nil {
		want = append(want, real)
	}
	for _, a := range c.Apps {
		for _, s := range a.SSHAgent {
			path := expandHome(s, home)
			candidates := []string{path}
			if real, err := filepath.EvalSymlinks(path); err == nil {
				candidates = append(candidates, real)
			}
			for _, w := range want {
				for _, p := range candidates {
					if w == p {
						return a, true
					}
				}
			}
		}
	}
	return catalog.App{}, false
}
