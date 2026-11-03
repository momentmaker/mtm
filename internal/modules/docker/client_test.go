package docker

import (
	"bytes"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/momentmaker/mtm/internal/runner"
)

func TestEngineNeedsTheSocketInTheHomeFolder(t *testing.T) {
	w := newWorld(t)
	if err := os.Remove(socket(w.home)); err != nil {
		t.Fatal(err)
	}
	if v := w.mod.engine(w.env(nil)); v != "" || len(w.fake.Calls()) != 0 {
		t.Errorf("engine = %q after %d commands; want none without the socket", v, len(w.fake.Calls()))
	}
}

func TestDockerCommandsTargetDesktopThroughTheHomeSocket(t *testing.T) {
	w := newWorld(t)
	if v := w.mod.engine(w.env(nil)); v != "29.2.0" {
		t.Fatalf("engine = %q", v)
	}
	c := w.fake.Calls()[0]
	if !slices.Contains(c.Env, "DOCKER_HOST=unix://"+w.home+"/.docker/run/docker.sock") {
		t.Errorf("env = %q, want DOCKER_HOST at the home folder's socket", c.Env)
	}
	if !slices.Contains(c.Unset, "DOCKER_HOST") || !slices.Contains(c.Unset, "DOCKER_CONTEXT") {
		t.Errorf("unset = %q, want DOCKER_HOST and DOCKER_CONTEXT", c.Unset)
	}
}

func TestImagesTellPulledFromBuilt(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	images, err := w.mod.images(w.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []Image{
		{ID: webID, Tags: []string{"web:latest"}, Size: 141949797},
		{ID: postgresID, Tags: []string{"postgres:16-alpine"}, Size: 108190238, Pulled: true},
	}
	if !reflect.DeepEqual(images, want) {
		t.Errorf("images = %+v, want %+v", images, want)
	}
}

func TestParseImagesWithoutIdentityOrDescriptor(t *testing.T) {
	// A loaded image under the containerd store has no Identity; under the
	// classic store, only pulled and pushed images have repo digests.
	data := []byte(`[
	  {"Id": "sha256:aa", "RepoTags": ["tool:1"], "RepoDigests": ["tool@sha256:aa"], "Size": 1, "Descriptor": {"digest": "sha256:aa"}},
	  {"Id": "sha256:bb", "RepoTags": ["redis:7"], "RepoDigests": ["redis@sha256:cc"], "Size": 2},
	  {"Id": "sha256:dd", "RepoTags": ["app:dev"], "RepoDigests": [], "Size": 3},
	  {"Id": "sha256:ee", "RepoTags": [], "RepoDigests": [], "Size": 4}
	]`)
	images, err := parseImages(data)
	if err != nil {
		t.Fatal(err)
	}
	var pulled []bool
	for _, img := range images {
		pulled = append(pulled, img.Pulled)
	}
	if !slices.Equal(pulled, []bool{false, true, false}) {
		t.Errorf("pulled = %v, want [false true false] and the untagged image skipped", pulled)
	}
}

func TestVolumesSkipWhatMtmCantExport(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	vols, skipped, err := w.mod.volumes(w.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []Volume{
		{Name: "acme_pgdata", Labels: map[string]string{"com.docker.compose.project": "acme", "com.docker.compose.volume": "pgdata"}, Size: 224_300_000},
		{Name: "cache", Labels: map[string]string{"com.docker.volume.anonymous": ""}, Size: 52_380_000},
	}
	if !reflect.DeepEqual(vols, want) {
		t.Errorf("volumes = %+v, want %+v", vols, want)
	}
	if !slices.Equal(skipped, []string{"nfs (driver options, such as a bind to a folder)"}) {
		t.Errorf("skipped = %q", skipped)
	}
}

func TestParseSize(t *testing.T) {
	tests := map[string]int64{"0B": 0, "512B": 512, "8.28kB": 8280, "73.85MB": 73_850_000, "1.5GB": 1_500_000_000, "N/A": 0, "": 0}
	for in, want := range tests {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestContainersReadComposeLabelsAndSkipHolders(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	cs, err := w.mod.containers(w.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []container{
		{Container: Container{ID: "dbcontainer", Name: "acme-db-1", Project: "acme", WorkingDir: "~/GitHub/acme",
			ConfigFiles: []string{"~/GitHub/acme/docker-compose.yml", "/opt/shared/compose.yml"}},
			Running: true, Restart: "unless-stopped", Volumes: []string{"acme_pgdata"}},
		{Container: Container{ID: "testcontainer", Name: "acme-yt-testdb"}, Restart: "no", Volumes: []string{"cache"}},
	}
	if !reflect.DeepEqual(cs, want) {
		t.Errorf("containers = %+v\nwant %+v", cs, want)
	}
}

func TestVolumeDigestReadsThroughAHolderAndRemovesIt(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	sum, n, err := w.mod.volumeDigest(w.env(nil), "acme_pgdata")
	if err != nil {
		t.Fatal(err)
	}
	if sum != digestFor(t, pgdataTar(t)) || n != 3 {
		t.Errorf("digest %s with %d entries", sum, n)
	}
	if !w.ran("container rm holder-pgdata") {
		t.Error("the holder container was left behind")
	}
}

func TestVolumeDigestRemovesTheHolderWhenReadingFails(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	w.respond("container cp holder-pgdata:/vol -", runner.Response{Code: 1, Stderr: "Error response from daemon"})
	if _, _, err := w.mod.volumeDigest(w.env(nil), "acme_pgdata"); err == nil {
		t.Fatal("volumeDigest succeeded")
	}
	if !w.ran("container rm holder-pgdata") {
		t.Error("the holder container was left behind")
	}
}

func TestWriteVolumeStreamsTheArchiveKeepingOwners(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	archive := pgdataTar(t)
	if err := w.mod.writeVolume(w.env(nil), "acme_pgdata", bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	if got := w.stdin("container cp --archive --quiet - holder-pgdata:/"); got != string(archive) {
		t.Errorf("docker container cp read %d bytes, want the %d-byte archive", len(got), len(archive))
	}
	if !w.ran("container rm holder-pgdata") {
		t.Error("the holder container was left behind")
	}
}

func TestImportEmptyFeedsAnEmptyArchive(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	if err := w.mod.importEmpty(w.env(nil)); err != nil {
		t.Fatal(err)
	}
	if got := w.stdin("image import - " + EmptyImage); got != string(make([]byte, 1024)) {
		t.Errorf("docker image import read %q", got)
	}
}

func TestRemoveLeftoversRemovesOnlyMtmsHolders(t *testing.T) {
	w := newWorld(t)
	w.respond("container ls --all --quiet --filter label=dev.mtm.temporary", runner.Response{Stdout: "old1\nold2\n"})
	w.respond("container rm old1 old2", runner.Response{})
	if err := w.mod.removeLeftovers(w.env(nil)); err != nil {
		t.Fatal(err)
	}
	if !w.ran("container rm old1 old2") {
		t.Error("leftovers were not removed")
	}
}

func TestHomeRelativeRoundTrips(t *testing.T) {
	for _, p := range []string{"/Users/dev", "/Users/dev/GitHub/acme", "/opt/shared/compose.yml"} {
		if got := expandHome("/Users/dev", homeRelative("/Users/dev", p)); got != p {
			t.Errorf("round trip of %s = %s", p, got)
		}
	}
	if got := homeRelative("/Users/dev", "/Users/devops/x"); got != "/Users/devops/x" {
		t.Errorf("homeRelative treated a sibling folder as home: %s", got)
	}
}
