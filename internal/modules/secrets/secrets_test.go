package secrets

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/momentmaker/mtm/internal/catalog"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/testutil"
)

// Two throwaway public keys, with the fingerprints ssh-keygen -l gives
// them.
const (
	pubOne = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAqmsyYcNuPdggoXH1gw1fgwDVLiRDxWTPlHgEhANQsZ dev@example.com\n"
	fprOne = "SHA256:imvGqWemDeQlOrKAuQ8St8gKyGJ6OZ7B/hDd20ijtXM"
	pubTwo = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILqheZQLjOXtrKZ7KmQWukW6KKoCfwD73dFzjP78Dkk6 dev@example.com\n"
	pubSK  = "sk-ecdsa-sha2-nistp256@openssh.com c2stZWNkc2E= ssh:\n"
)

// secretKeys is gpg --with-colons --list-secret-keys output: an RSA key
// whose secret part is here, with a subkey, and an ed25519 key whose
// secret part is missing (a stub). The fingerprints are made up.
const secretKeys = `sec:u:4096:1:AAAABBBBCCCCDDDD:1625242223:::u:::scESC:::+:::23::0:
fpr:::::::::0123456789ABCDEF0123456789ABCDEF01234567:
grp:::::::::1111111111111111111111111111111111111111:
uid:u::::1625242223::0000000000000000000000000000000000000000::Dev <dev@example.com>::::::::::0:
ssb:u:4096:1:EEEEFFFF00001111:1625242223::::::e:::+:::23:
fpr:::::::::89ABCDEF0123456789ABCDEF0123456789ABCDEF:
grp:::::::::2222222222222222222222222222222222222222:
sec:u:255:22:1111222233334444:1729295723:1855526101::u:::scESC:::#::ed25519:::0:
fpr:::::::::FEDCBA9876543210FEDCBA9876543210FEDCBA98:
uid:u::::1729295723::0000000000000000000000000000000000000000::Old <old@example.com>::::::::::0:
`

const onePassword = "~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(fstest.MapFS{
		"1password.toml": {Data: []byte("id = \"1password\"\nname = \"1Password\"\nbundle_id = \"com.1password.1password\"\nkind = \"password-manager\"\nssh_agent = [\"" + onePassword + "\"]\n")},
		"secretive.toml": {Data: []byte("id = \"secretive\"\nname = \"Secretive\"\nbundle_id = \"com.maxgoedjen.Secretive.Host\"\nkind = \"secure-enclave-agent\"\nssh_agent = [\"~/Library/Containers/com.maxgoedjen.Secretive.SecretAgent/Data/socket.ssh\"]\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type fixture struct {
	mod  Module
	fake *runner.Fake
	home string
	env  map[string]string
}

const bioutil = "/usr/bin/bioutil -c"

// newFixture is a home folder with three SSH keys, an SSH config that
// uses 1Password's agent, a GnuPG folder, and a gpg that lists two keys.
// The gpg is a placeholder file the fake answers for.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{home: filepath.Join(root, "home"), env: map[string]string{}}
	gpg := filepath.Join(root, "bin", "gpg")
	testutil.WriteFile(t, gpg, "")
	f.mod = Module{Catalog: testCatalog(t), Getenv: func(k string) string { return f.env[k] }, GPG: []string{filepath.Join(root, "none", "gpg"), gpg}}
	f.fake = &runner.Fake{Responses: map[string]runner.Response{
		gpg + " " + strings.Join(gpgListArgs, " "): {Stdout: secretKeys},
		bioutil: {Stdout: "User 501:\t2 biometric template(s)\nOperation performed successfully.\n"},
	}}
	ssh := filepath.Join(f.home, ".ssh")
	testutil.WriteFile(t, filepath.Join(ssh, "id_ed25519"), "private")
	testutil.WriteFile(t, filepath.Join(ssh, "id_ed25519.pub"), pubOne)
	testutil.WriteFile(t, filepath.Join(ssh, "id_rsa"), "private")
	testutil.WriteFile(t, filepath.Join(ssh, "github.pub"), pubTwo)
	testutil.WriteFile(t, filepath.Join(ssh, "known_hosts"), "github.com ssh-ed25519 AAAA\n")
	testutil.WriteFile(t, filepath.Join(ssh, "config"), "Host *\n  IdentityAgent \""+onePassword+"\"\n")
	if err := os.MkdirAll(filepath.Join(f.home, ".gnupg"), 0o700); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) moduleEnv(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func (f *fixture) prepare(t *testing.T) (Inventory, *manifest.Manifest) {
	t.Helper()
	res, err := f.mod.Prepare(f.moduleEnv(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), res.Inventory); err != nil {
		t.Fatal(err)
	}
	return res.Inventory.(Inventory), m
}

func two() *int { n := 2; return &n }

func TestPrepareRecordsIdentitiesAndTheAgent(t *testing.T) {
	f := newFixture(t)
	inv, _ := f.prepare(t)
	want := Inventory{
		SSH: []SSHKey{
			{Name: "github", Type: "ssh-ed25519", Fingerprint: "SHA256:4I4uXsj2BJLVM2uwM1keNH2NJLceWl2Mw6BBEePE5iM", Public: true},
			{Name: "id_ed25519", Type: "ssh-ed25519", Fingerprint: fprOne, Public: true, Private: true},
			{Name: "id_rsa", Private: true},
		},
		GPG: []GPGKey{
			{Fingerprint: "0123456789ABCDEF0123456789ABCDEF01234567", Algorithm: "RSA 4096", Created: 1625242223, Secret: "+"},
			{Fingerprint: "FEDCBA9876543210FEDCBA9876543210FEDCBA98", Algorithm: "EdDSA ed25519", Created: 1729295723, Secret: "#"},
		},
		Agent:        "1password",
		Fingerprints: two(),
	}
	if !reflect.DeepEqual(inv, want) {
		t.Errorf("inventory = %+v\nwant        %+v", inv, want)
	}
}

func TestPrepareReadsNoPrivateKey(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		path := filepath.Join(f.home, ".ssh", name)
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(path, 0o600) })
	}
	if _, err := f.mod.Prepare(f.moduleEnv(nil), nil); err != nil {
		t.Errorf("prepare read a private key it can't open: %v", err)
	}
}

func TestPrepareDoesNotRunGPGWithoutAGnuPGFolder(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(filepath.Join(f.home, ".gnupg")); err != nil {
		t.Fatal(err)
	}
	inv, _ := f.prepare(t)
	if len(inv.GPG) != 0 || inv.GPGUnlisted {
		t.Errorf("gpg = %+v, unlisted %v; want none", inv.GPG, inv.GPGUnlisted)
	}
	for _, c := range f.fake.Calls() {
		if strings.HasSuffix(c.Name, "gpg") {
			t.Errorf("ran %s, which would create ~/.gnupg", c)
		}
	}
}

func TestPrepareWarnsWhenItCannotListGPGKeys(t *testing.T) {
	f := newFixture(t)
	f.mod.GPG = f.mod.GPG[:1]
	res, err := f.mod.Prepare(f.moduleEnv(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Inventory.(Inventory).GPGUnlisted || !slices.ContainsFunc(res.Problems, func(p module.Finding) bool { return p.ID == "gpg" }) {
		t.Errorf("result = %+v, want gpg unlisted and a warning", res)
	}
}

func TestSecureEnclaveIsFoundThreeWays(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, f *fixture)
	}{
		{"an sk- key", func(t *testing.T, f *fixture) {
			testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "id_ecdsa_sk_rk.pub"), pubSK)
		}},
		{"Secretive's agent", func(t *testing.T, f *fixture) {
			f.env["SSH_AUTH_SOCK"] = filepath.Join(f.home, "Library/Containers/com.maxgoedjen.Secretive.SecretAgent/Data/socket.ssh")
		}},
		{"macOS's provider", func(t *testing.T, f *fixture) {
			testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "config"), "Host *\n  SecurityKeyProvider=/usr/lib/ssh-keychain.dylib\n")
		}},
	}
	for _, tt := range tests {
		f := newFixture(t)
		tt.setup(t, f)
		res, err := f.mod.Prepare(f.moduleEnv(nil), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Inventory.(Inventory).SecureEnclave || !slices.ContainsFunc(res.Problems, func(p module.Finding) bool { return p.ID == "secure-enclave" }) {
			t.Errorf("%s: result = %+v, want the Secure Enclave found and a warning", tt.name, res)
		}
	}
}

func TestAgentIsFoundThroughALink(t *testing.T) {
	f := newFixture(t)
	testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "config"), "")
	real := expandHome(onePassword, f.home)
	testutil.WriteFile(t, real, "")
	link := filepath.Join(f.home, ".1password", "agent.sock")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	f.env["SSH_AUTH_SOCK"] = link
	if inv, _ := f.prepare(t); inv.Agent != "1password" {
		t.Errorf("agent = %q, want 1password", inv.Agent)
	}
}

func TestTouchIDIsUnknownWhenBioutilFails(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses[bioutil] = runner.Response{Code: 1, Stderr: "no biometric hardware"}
	if inv, _ := f.prepare(t); inv.Fingerprints != nil {
		t.Errorf("fingerprints = %d, want unknown", *inv.Fingerprints)
	}
}

func stepIDs(items []checklist.Item) []string {
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestStepsForPasswordManagersAgentsAndTouchID(t *testing.T) {
	f := newFixture(t)
	testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "id_ecdsa_sk_rk.pub"), pubSK)
	_, m := f.prepare(t)
	apps := []map[string]string{{"bundle_id": "com.1password.1password"}, {"bundle_id": "com.example.other"}}
	if err := m.SetModule("system", time.Now(), map[string]any{"apps": apps}); err != nil {
		t.Fatal(err)
	}
	items, err := f.mod.Steps(f.moduleEnv(m))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"secrets:icloud-keychain", "secrets:password-manager:1password", "secrets:ssh-agent:1password", "secrets:secure-enclave", "secrets:touch-id"}
	if got := stepIDs(items); !slices.Equal(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
	if se := items[3]; !strings.Contains(se.Detail, "~/.ssh/id_ecdsa_sk_rk") {
		t.Errorf("Secure Enclave step detail = %q, want the sk- key named", se.Detail)
	}
}

func TestStepsSkipTouchIDWithoutFingerprintsAndAskForKeysWithSecretive(t *testing.T) {
	f := newFixture(t)
	f.fake.Responses[bioutil] = runner.Response{Stdout: "User 501:\t0 biometric template(s)\n"}
	testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "config"), "")
	_, m := f.prepare(t)
	apps := []map[string]string{{"bundle_id": "com.maxgoedjen.Secretive.Host"}}
	if err := m.SetModule("system", time.Now(), map[string]any{"apps": apps}); err != nil {
		t.Fatal(err)
	}
	items, err := f.mod.Steps(f.moduleEnv(m))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stepIDs(items), []string{"secrets:icloud-keychain", "secrets:secure-enclave"}; !slices.Equal(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
}

func check(t *testing.T, checks []module.Check, id string) module.Check {
	t.Helper()
	i := slices.IndexFunc(checks, func(c module.Check) bool { return c.ID == id })
	if i < 0 {
		t.Fatalf("no %s check in %+v", id, checks)
	}
	return checks[i]
}

// migrated is the fixture's home copied to a new Mac, with its record.
func migrated(t *testing.T) (*fixture, *manifest.Manifest) {
	t.Helper()
	f := newFixture(t)
	_, m := f.prepare(t)
	newHome := filepath.Join(t.TempDir(), "home")
	testutil.Copy(t, f.home, newHome)
	f.home = newHome
	return f, m
}

func TestVerifyPassesWhenIdentitiesArrived(t *testing.T) {
	f, m := migrated(t)
	checks, err := f.mod.Verify(f.moduleEnv(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.Status != module.StatusPass {
			t.Errorf("%s = %s (%s), want pass", c.ID, c.Status, c.Detail)
		}
	}
	if ssh := check(t, checks, "secrets:ssh"); !ssh.Critical {
		t.Error("the SSH check isn't critical")
	}
	if tid := check(t, checks, "secrets:touch-id"); !slices.Equal(tid.Resolves, []string{"secrets:touch-id"}) {
		t.Errorf("Touch ID check = %+v, want it to close the step", tid)
	}
}

func TestVerifyFailsMissingOrReplacedSSHKeys(t *testing.T) {
	f, m := migrated(t)
	ssh := filepath.Join(f.home, ".ssh")
	if err := os.Remove(filepath.Join(ssh, "id_rsa")); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(ssh, "id_ed25519.pub"), pubTwo)
	checks, err := f.mod.Verify(f.moduleEnv(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := check(t, checks, "secrets:ssh")
	want := []string{"~/.ssh/id_ed25519.pub: a different key", "~/.ssh/id_rsa: private key missing"}
	if c.Status != module.StatusFail || !slices.Equal(c.Paths, want) {
		t.Errorf("SSH check = %+v, want a fail listing %q", c, want)
	}
	for _, p := range c.Paths {
		if strings.Contains(p, "SHA256:") {
			t.Errorf("a fingerprint reached the report: %q", p)
		}
	}
}

func TestVerifyFailsAMissingGPGSecretKey(t *testing.T) {
	f, m := migrated(t)
	gpg := f.mod.GPG[1] + " " + strings.Join(gpgListArgs, " ")
	f.fake.Responses[gpg] = runner.Response{Stdout: strings.Replace(secretKeys, ":+:::23::0:", ":#:::23::0:", 1)}
	checks, err := f.mod.Verify(f.moduleEnv(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := check(t, checks, "secrets:gpg")
	if c.Status != module.StatusFail || !c.Critical || !slices.Equal(c.Paths, []string{"the RSA 4096 key from 2021-07-02"}) {
		t.Errorf("GPG check = %+v, want a critical fail naming the RSA key", c)
	}
}

func TestVerifyFailsWhenGPGIsGone(t *testing.T) {
	f, m := migrated(t)
	f.mod.GPG = f.mod.GPG[:1]
	checks, err := f.mod.Verify(f.moduleEnv(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c := check(t, checks, "secrets:gpg"); c.Status != module.StatusFail || !strings.Contains(c.Detail, "gpg isn't installed") {
		t.Errorf("GPG check = %+v, want a fail saying gpg is missing", c)
	}
}

func TestAnUnlistedGPGBecomesAStepNotABlock(t *testing.T) {
	f := newFixture(t)
	f.mod.GPG = f.mod.GPG[:1]
	_, m := f.prepare(t)
	items, err := f.mod.Steps(f.moduleEnv(m))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(stepIDs(items), "secrets:gpg-check") {
		t.Errorf("steps = %q, want secrets:gpg-check", stepIDs(items))
	}
	checks, err := f.mod.Verify(f.moduleEnv(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c := check(t, checks, "secrets:gpg"); c.Status != module.StatusInfo || c.Critical {
		t.Errorf("GPG check = %+v, want a non-critical info", c)
	}
}

func TestParseSecretKeysRejectsAKeyWithoutAFingerprint(t *testing.T) {
	if _, err := parseSecretKeys("sec:u:4096:1:AAAA:1625242223:::u:::scESC:::+:::23::0:\n"); err == nil {
		t.Error("parsed a key with no fingerprint")
	}
}

func TestConfigLineForms(t *testing.T) {
	tests := []struct{ line, key, value string }{
		{"  IdentityAgent ~/a.sock", "IdentityAgent", "~/a.sock"},
		{"IdentityAgent=\"~/b c.sock\"", "IdentityAgent", "~/b c.sock"},
		{"identityagent = none", "identityagent", "none"},
		{"# IdentityAgent ~/x", "", ""},
	}
	for _, tt := range tests {
		if k, v := configLine(tt.line); k != tt.key || v != tt.value {
			t.Errorf("configLine(%q) = %q, %q; want %q, %q", tt.line, k, v, tt.key, tt.value)
		}
	}
}

func TestRetireAsksToRemoveSecureEnclaveKeysOnlyWhenThereAreSome(t *testing.T) {
	f := newFixture(t)
	_, m := f.prepare(t)
	if r, err := f.mod.Retire(f.moduleEnv(m), nil); err != nil || len(r.Steps) != 0 {
		t.Errorf("retirement = %+v, %v; want no steps without Secure Enclave keys", r, err)
	}
	testutil.WriteFile(t, filepath.Join(f.home, ".ssh", "id_ecdsa_sk_rk.pub"), pubSK)
	_, m = f.prepare(t)
	r, err := f.mod.Retire(f.moduleEnv(m), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := stepIDs(r.Steps); !slices.Equal(got, []string{"secrets:retire-secure-enclave"}) {
		t.Errorf("steps = %q, want the Secure Enclave key removal", got)
	}
}
