package system

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/momentmaker/mtm/internal/apps"
	"github.com/momentmaker/mtm/internal/catalog"
	"github.com/momentmaker/mtm/internal/checklist"
	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

const user = "dev"

type fixture struct {
	home, etc, apps, shell, rosetta string
	fake                            *runner.Fake
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	f := fixture{
		home:    filepath.Join(root, "my home"),
		etc:     filepath.Join(root, "etc"),
		apps:    filepath.Join(root, "Applications"),
		shell:   filepath.Join(root, "bin", "fish"),
		rosetta: filepath.Join(root, "rosetta"),
		fake:    &runner.Fake{Responses: map[string]runner.Response{}},
	}
	testutil.WriteFile(t, f.shell, "#!/bin/sh\n")
	if err := os.Chmod(f.shell, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(f.etc, "hosts"), "127.0.0.1 localhost\n")
	testutil.WriteFile(t, filepath.Join(f.etc, "shells"), "# shells\n/bin/zsh\n"+f.shell+"\n")
	testutil.WriteFile(t, filepath.Join(f.etc, "paths.d", "MacGPG2"), "/usr/local/MacGPG2/bin\n")
	if err := os.MkdirAll(f.home, 0o755); err != nil {
		t.Fatal(err)
	}
	f.setShell(f.shell)
	f.addApp(t, "Ghostty.app", "com.mitchellh.ghostty", "1.4.0", "ghostty", "x86_64 arm64")
	f.addApp(t, "sioyek.app", "info.sioyek.sioyek", "2.0.0", "sioyek", "x86_64")
	return f
}

func (f fixture) setShell(shell string) {
	f.fake.Responses["/usr/bin/dscl . -read /Users/"+user+" UserShell"] = runner.Response{Stdout: "UserShell: " + shell + "\n"}
}

func (f fixture) addApp(t *testing.T, rel, id, version, exe, archs string) {
	t.Helper()
	dir := filepath.Join(f.apps, rel)
	if err := os.MkdirAll(filepath.Join(dir, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(dir, "Contents", "Info.plist")
	for key, val := range map[string]string{"CFBundleIdentifier": id, "CFBundleShortVersionString": version, "CFBundleExecutable": exe} {
		f.fake.Responses["/usr/bin/plutil -extract "+key+" raw -o - "+plist] = runner.Response{Stdout: val + "\n"}
	}
	f.fake.Responses["/usr/bin/lipo -archs "+filepath.Join(dir, "Contents", "MacOS", exe)] = runner.Response{Stdout: archs + "\n"}
}

func (f fixture) module() Module {
	return Module{
		User:        user,
		EtcDir:      f.etc,
		AppRoots:    func(string) []apps.Root { return []apps.Root{{Dir: f.apps, Display: "/Applications"}} },
		RosettaPath: f.rosetta,
	}
}

func (f fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Home: f.home, Manifest: m}
}

func (f fixture) prepare(t *testing.T) (*manifest.Manifest, *stage.Stage, module.Result) {
	t.Helper()
	st, err := stage.Open(f.home)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.New("test")
	res, err := f.module().Prepare(f.env(m), st)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetModule(Name, time.Now(), res.Inventory); err != nil {
		t.Fatal(err)
	}
	return m, st, res
}

func (f fixture) verify(t *testing.T, m *manifest.Manifest, st *stage.Stage) map[string]module.Check {
	t.Helper()
	checks, err := f.module().Verify(f.env(m), st)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]module.Check{}
	for _, c := range checks {
		byID[c.ID] = c
	}
	return byID
}

func TestPrepareRecordsShellEtcAndApps(t *testing.T) {
	f := newFixture(t)
	_, st, res := f.prepare(t)
	inv := res.Inventory.(Inventory)
	if inv.Shell != f.shell || !inv.ShellListed || len(inv.Apps) != 2 {
		t.Errorf("inventory = %+v", inv)
	}
	keys := slices.Sorted(maps.Keys(inv.Etc))
	if want := []string{"hosts", "paths.d/MacGPG2", "shells"}; !slices.Equal(keys, want) {
		t.Errorf("etc files = %q, want %q", keys, want)
	}
	copied, err := os.ReadFile(st.Path("exports", "etc", "hosts"))
	if err != nil || string(copied) != "127.0.0.1 localhost\n" {
		t.Errorf("copied hosts = %q, %v", copied, err)
	}
	if !slices.ContainsFunc(res.Problems, func(p module.Finding) bool {
		return p.ID == "intel-only" && strings.Contains(p.Detail, "sioyek")
	}) {
		t.Errorf("problems = %+v, want an Intel-only warning naming sioyek", res.Problems)
	}
}

func TestVerifyPassesWhenNothingChanged(t *testing.T) {
	f := newFixture(t)
	testutil.WriteFile(t, f.rosetta, "")
	m, st, _ := f.prepare(t)
	for id, c := range f.verify(t, m, st) {
		if c.Status != module.StatusPass {
			t.Errorf("%s: %s (%s)", id, c.Status, c.Detail)
		}
	}
}

func TestVerifyReportsChanges(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, f fixture)
		id     string
		want   module.Status
	}{
		{"edited /etc/hosts", func(t *testing.T, f fixture) {
			testutil.WriteFile(t, filepath.Join(f.etc, "hosts"), "10.0.0.1 nas\n")
		}, "system:etc", module.StatusWarn},
		{"app missing", func(t *testing.T, f fixture) {
			os.RemoveAll(filepath.Join(f.apps, "sioyek.app"))
		}, "system:apps", module.StatusFail},
		{"older app", func(t *testing.T, f fixture) {
			f.addApp(t, "Ghostty.app", "com.mitchellh.ghostty", "1.3.9", "ghostty", "x86_64 arm64")
		}, "system:apps", module.StatusWarn},
		{"shell changed", func(t *testing.T, f fixture) { f.setShell("/bin/zsh") }, "system:shell", module.StatusWarn},
		{"shell missing", func(t *testing.T, f fixture) { os.Remove(f.shell) }, "system:shell", module.StatusFail},
		{"no Rosetta", func(t *testing.T, f fixture) { os.Remove(f.rosetta) }, "system:rosetta", module.StatusWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			testutil.WriteFile(t, f.rosetta, "")
			m, st, _ := f.prepare(t)
			tt.change(t, f)
			if c := f.verify(t, m, st)[tt.id]; c.Status != tt.want {
				t.Errorf("%s = %s (%s), want %s", tt.id, c.Status, c.Detail, tt.want)
			}
		})
	}
}

func TestFinishPrintsRestoreCommands(t *testing.T) {
	f := newFixture(t)
	m, st, _ := f.prepare(t)
	testutil.WriteFile(t, filepath.Join(f.etc, "hosts"), "10.0.0.1 nas\n")
	f.setShell("/bin/zsh")
	res, err := f.module().Finish(f.env(m), st)
	if err != nil {
		t.Fatal(err)
	}
	fixes := map[string]string{}
	for _, p := range res.Problems {
		fixes[p.ID] = p.Fix
	}
	if want := "sudo install -m 0644 -o root -g wheel '" + st.Path("exports", "etc", "hosts") + "' '/etc/hosts'"; !strings.Contains(fixes["etc:hosts"], want) {
		t.Errorf("hosts fix = %q, want it to contain %q", fixes["etc:hosts"], want)
	}
	if !strings.Contains(fixes["rosetta"], "softwareupdate --install-rosetta") {
		t.Errorf("rosetta fix = %q", fixes["rosetta"])
	}
	if want := "chsh -s '" + f.shell + "'"; !strings.Contains(fixes["shell"], want) {
		t.Errorf("shell fix = %q, want %q", fixes["shell"], want)
	}
}

func TestFinishWithNothingToDo(t *testing.T) {
	f := newFixture(t)
	testutil.WriteFile(t, f.rosetta, "")
	m, st, _ := f.prepare(t)
	res, err := f.module().Finish(f.env(m), st)
	if err != nil || len(res.Problems) != 0 || len(res.Done) != 1 {
		t.Errorf("Finish = %+v, %v", res, err)
	}
}

func TestScanSummarizes(t *testing.T) {
	f := newFixture(t)
	findings, err := f.module().Scan(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, fd := range findings {
		ids = append(ids, fd.ID)
	}
	if want := []string{"apps", "shell", "intel-only"}; !slices.Equal(ids, want) {
		t.Errorf("finding ids = %q, want %q", ids, want)
	}
}

func TestFinishQuotesEtcDestination(t *testing.T) {
	f := newFixture(t)
	rel := "paths.d/my tool's"
	testutil.WriteFile(t, filepath.Join(f.etc, filepath.FromSlash(rel)), "/opt/tool/bin\n")
	m, st, _ := f.prepare(t)
	testutil.WriteFile(t, filepath.Join(f.etc, filepath.FromSlash(rel)), "/opt/other/bin\n")
	res, err := f.module().Finish(f.env(m), st)
	if err != nil {
		t.Fatal(err)
	}
	var fix string
	for _, p := range res.Problems {
		if p.ID == "etc:"+rel {
			fix = p.Fix
		}
	}
	if want := `'/etc/paths.d/my tool'\''s'`; !strings.Contains(fix, want) {
		t.Errorf("fix = %q, want it to contain %q", fix, want)
	}
}

func fixesByID(res module.Result) map[string]string {
	fixes := map[string]string{}
	for _, p := range res.Problems {
		fixes[p.ID] = p.Fix
	}
	return fixes
}

func TestFinishAddsAnUnlistedShellAndSwitchesInOnePasteableLine(t *testing.T) {
	f := newFixture(t)
	m, st, _ := f.prepare(t)
	testutil.WriteFile(t, filepath.Join(f.etc, "shells"), "# shells\n/bin/zsh\n")
	f.setShell("/bin/zsh")
	res, err := f.module().Finish(f.env(m), st)
	if err != nil {
		t.Fatal(err)
	}
	q := macos.ShellQuote(f.shell)
	if got, want := fixesByID(res)["shell"], "Run: echo "+q+" | sudo tee -a /etc/shells >/dev/null && chsh -s "+q; got != want {
		t.Errorf("shell fix = %q, want %q", got, want)
	}
}

func makeUnreadable(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads files whatever their mode")
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareSkipsAnUnreadableEtcFileAndWarns(t *testing.T) {
	for _, rel := range []string{"hosts", "shells"} {
		t.Run(rel, func(t *testing.T) {
			f := newFixture(t)
			makeUnreadable(t, filepath.Join(f.etc, rel))
			_, st, res := f.prepare(t)
			inv := res.Inventory.(Inventory)
			if _, ok := inv.Etc[rel]; ok || len(inv.Etc) != 2 {
				t.Errorf("etc files = %q, want the two readable ones", slices.Sorted(maps.Keys(inv.Etc)))
			}
			if _, err := os.Stat(st.Path("exports", "etc", rel)); !os.IsNotExist(err) {
				t.Errorf("copied an unreadable file: %v", err)
			}
			want := "Couldn't read /etc/" + rel + "; it won't be restored"
			if !slices.ContainsFunc(res.Problems, func(p module.Finding) bool {
				return p.Severity == module.SeverityWarn && p.Title == want
			}) {
				t.Errorf("problems = %+v, want a warning %q", res.Problems, want)
			}
		})
	}
}

func TestVerifyAndFinishTreatAnUnreadableRecordedEtcFileAsDiffering(t *testing.T) {
	f := newFixture(t)
	m, st, _ := f.prepare(t)
	makeUnreadable(t, filepath.Join(f.etc, "hosts"))
	if c := f.verify(t, m, st)["system:etc"]; c.Status != module.StatusWarn || !slices.Equal(c.Paths, []string{"/etc/hosts"}) {
		t.Errorf("system:etc = %s %q (%s), want a warning naming /etc/hosts", c.Status, c.Paths, c.Detail)
	}
	res, err := f.module().Finish(f.env(m), st)
	if err != nil {
		t.Fatal(err)
	}
	if want := "sudo install -m 0644 -o root -g wheel " + macos.ShellQuote(st.Path("exports", "etc", "hosts")) + " '/etc/hosts'"; !strings.Contains(fixesByID(res)["etc:hosts"], want) {
		t.Errorf("hosts fix = %q, want it to contain %q", fixesByID(res)["etc:hosts"], want)
	}
}

func TestStepsAreTheCatalogsForRecordedApps(t *testing.T) {
	f := newFixture(t)
	f.addApp(t, "Dash.app", "com.kapeli.dashdoc", "7.3", "Dash", "arm64")
	f.addApp(t, "Signal.app", "org.whispersystems.signal-desktop", "7.80", "Signal", "arm64")
	m, _, _ := f.prepare(t)
	c, err := catalog.Load(fstest.MapFS{
		"dash.toml":   {Data: []byte("id = \"dash\"\nname = \"Dash\"\nbundle_id = \"com.kapeli.dashdoc\"\n\n[license]\nreactivate = true\nnote = \"Enter your license.\"\n")},
		"signal.toml": {Data: []byte("id = \"signal\"\nname = \"Signal\"\nbundle_id = \"org.whispersystems.signal-desktop\"\nnotes = [\"Link it again.\"]\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	mod := f.module()
	mod.Catalog = c
	items, err := mod.Steps(f.env(m))
	if err != nil {
		t.Fatal(err)
	}
	want := []checklist.Item{
		{ID: "system:license:dash", Module: Name, Title: "Reactivate the Dash license", Detail: "Enter your license.", Where: "Dash"},
		{ID: "system:note:signal:1", Module: Name, Title: "Signal: Link it again.", Where: "Signal"},
	}
	if !slices.Equal(items, want) {
		t.Errorf("steps = %+v\nwant %+v", items, want)
	}
}

func TestRetireListsTheLicensesToDeactivate(t *testing.T) {
	f := newFixture(t)
	f.addApp(t, "CleanShot X.app", "pl.maketheweb.cleanshotx", "4.8", "CleanShot X", "arm64")
	f.addApp(t, "Dash.app", "com.kapeli.dashdoc", "7.3", "Dash", "arm64")
	m, st, _ := f.prepare(t)
	c, err := catalog.Load(fstest.MapFS{
		"cleanshot-x.toml": {Data: []byte("id = \"cleanshot-x\"\nname = \"CleanShot X\"\nbundle_id = \"pl.maketheweb.cleanshotx\"\n\n[license]\ndeactivate = true\ndeactivate_note = \"It counts Macs.\"\n")},
		"dash.toml":        {Data: []byte("id = \"dash\"\nname = \"Dash\"\nbundle_id = \"com.kapeli.dashdoc\"\n\n[license]\nreactivate = true\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	mod := f.module()
	mod.Catalog = c
	r, err := mod.Retire(f.env(m), st)
	if err != nil {
		t.Fatal(err)
	}
	want := []checklist.Item{{ID: "system:deactivate:cleanshot-x", Module: Name, Title: "Deactivate the CleanShot X license on this Mac", Detail: "It counts Macs.", Where: "CleanShot X"}}
	if !slices.Equal(r.Steps, want) || len(r.Changes) != 0 {
		t.Errorf("retirement = %+v\nwant steps %+v", r, want)
	}
}
