//go:build integration

package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
)

// These tests run against the developer's own Docker Desktop. They touch
// only volumes, images, and containers named mtm-test-…, and call the
// export and restore steps directly. They never call Prepare, Finish, or
// Undo, which stop and start the developer's containers.

func requireDocker(t *testing.T) (module.Env, Module) {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("needs an Apple Silicon Mac")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	exports, err := stage.OpenDir(filepath.Join(t.TempDir(), "exports"))
	if err != nil {
		t.Fatal(err)
	}
	env := module.Env{
		Ctx: context.Background(), Run: runner.Exec{}, Home: home, Exports: exports,
		Free: func(string) (uint64, error) { return 1 << 40, nil },
	}
	m := Module{Docker: DesktopCLI, Empty: fmt.Sprintf("mtm-test/empty:%d", time.Now().UnixNano())}
	if !m.installed() || m.engine(env) == "" {
		t.Skip("needs Docker Desktop running")
	}
	if err := m.importEmpty(env); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.docker(env, "image", "rm", m.Empty) })
	return env, m
}

// testVolume names a volume for this test and removes it, and any holder
// left mounting it, when the test ends.
func testVolume(t *testing.T, env module.Env, m Module) string {
	t.Helper()
	name := fmt.Sprintf("mtm-test-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		out, _ := m.docker(env, "container", "ls", "--all", "--quiet", "--filter", "volume="+name)
		for _, id := range strings.Fields(string(out.Stdout)) {
			m.docker(env, "container", "rm", "--force", id)
		}
		m.docker(env, "volume", "rm", "--force", name)
	})
	return name
}

func mustDocker(t *testing.T, env module.Env, m Module, args ...string) string {
	t.Helper()
	out, err := m.docker(env, args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out.Stdout))
}

func TestDockerVolumeRoundTrip(t *testing.T) {
	env, m := requireDocker(t)
	name := testVolume(t, env, m)
	seed := volumeTar(t, root, version, sub, table, link)
	want := digestFor(t, seed)

	if err := m.writeVolume(env, name, bytes.NewReader(seed)); err != nil {
		t.Fatal(err)
	}
	if sum, n, err := m.volumeDigest(env, name); err != nil || sum != want || n != 4 {
		t.Fatalf("seeded volume = %s with %d entries, %v; want the seed's digest: owners and modes must survive Docker", sum, n, err)
	}
	v, err := m.exportVolume(env, Volume{Name: name, Labels: map[string]string{"dev.mtm.test": "1"}})
	if err != nil || v.Digest != want || v.Archive == nil {
		t.Fatalf("export = %+v, %v", v, err)
	}

	if how, p := m.settle(env, v, true); p != nil || how != intact {
		t.Errorf("intact volume settled as %q, %+v", how, p)
	}

	mustDocker(t, env, m, "volume", "rm", name)
	if how, p := m.settle(env, v, false); p != nil || how != restored {
		t.Fatalf("missing volume settled as %q, %+v", how, p)
	}
	if label := mustDocker(t, env, m, "volume", "inspect", "--format", `{{index .Labels "dev.mtm.test"}}`, name); label != "1" {
		t.Errorf("restored volume label = %q, want 1", label)
	}

	mustDocker(t, env, m, "volume", "rm", name)
	mustDocker(t, env, m, "volume", "create", name)
	if how, p := m.settle(env, v, true); p != nil || how != restored {
		t.Errorf("empty volume settled as %q, %+v", how, p)
	}
}

func TestDockerNeverOverwritesAVolumeHoldingOtherData(t *testing.T) {
	env, m := requireDocker(t)
	name := testVolume(t, env, m)
	if err := m.writeVolume(env, name, bytes.NewReader(volumeTar(t, root, version))); err != nil {
		t.Fatal(err)
	}
	v, err := m.exportVolume(env, Volume{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	extra := volumeTar(t, root, entry{name: "vol/new-on-this-mac", typ: tar.TypeReg, mode: 0o644, content: "new"})
	if err := m.writeVolume(env, name, bytes.NewReader(extra)); err != nil {
		t.Fatal(err)
	}
	before, _, err := m.volumeDigest(env, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, p := m.settle(env, v, true); p == nil || p.Severity != module.SeverityBlocker {
		t.Fatalf("settle = %+v, want a blocker", p)
	}
	if after, _, _ := m.volumeDigest(env, name); after != before {
		t.Error("settle changed a volume that holds other data")
	}
}

func TestDockerImageRoundTrip(t *testing.T) {
	env, m := requireDocker(t)
	tag := fmt.Sprintf("mtm-test/image:%d", time.Now().UnixNano())
	cmd := m.command(env, "image", "import", "-", tag)
	cmd.Input = bytes.NewReader(volumeTar(t, root, version))
	if _, err := env.Run.Run(env.Ctx, cmd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.docker(env, "image", "rm", "--force", tag) })
	images, err := m.images(env)
	if err != nil {
		t.Fatal(err)
	}
	var img Image
	for _, i := range images {
		if len(i.Tags) > 0 && i.Tags[0] == tag {
			img = i
		}
	}
	if img.ID == "" || img.Pulled {
		t.Fatalf("image = %+v, want an imported image counted as locally built", img)
	}
	a, err := m.saveImage(env, img)
	if err != nil {
		t.Fatal(err)
	}
	img.Archive = &a
	mustDocker(t, env, m, "image", "rm", tag)
	if err := m.loadImage(env, img); err != nil {
		t.Fatal(err)
	}
	ids, _, err := m.present(env)
	if err != nil || !ids[img.ID] {
		t.Errorf("after load, image %s present = %v, %v", img.ID, ids[img.ID], err)
	}
}
