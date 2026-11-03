package docker

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
)

func record(t *testing.T, inv Inventory) *manifest.Manifest {
	t.Helper()
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), inv); err != nil {
		t.Fatal(err)
	}
	return m
}

func ids(findings []module.Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.ID)
	}
	return out
}

func find(findings []module.Finding, id string) module.Finding {
	for _, f := range findings {
		if f.ID == id {
			return f
		}
	}
	return module.Finding{}
}

func (w *world) count(args string) int {
	n := 0
	for _, c := range w.fake.Calls() {
		if c.String() == w.mod.Docker+" "+args {
			n++
		}
	}
	return n
}

func (w *world) prepare(m *manifest.Manifest) (module.Result, Inventory) {
	w.t.Helper()
	res, err := w.mod.Prepare(w.env(m), w.st)
	if err != nil {
		w.t.Fatal(err)
	}
	inv, _ := res.Inventory.(Inventory)
	return res, inv
}

func TestScanSummarizesWhatPrepareWillDo(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	findings, err := w.mod.Scan(w.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"exports", "pulled", "running", "skipped"}; !slices.Equal(ids(findings), want) {
		t.Errorf("findings = %q, want %q", ids(findings), want)
	}
	if got := find(findings, "exports").Title; got != "2 volumes and 1 locally built image to export (418.6 MB)" {
		t.Errorf("exports title = %q", got)
	}
	if got := find(findings, "running").Title; got != "1 running container: acme-db-1" {
		t.Errorf("running title = %q", got)
	}
	if w.ran("container stop --time 60 dbcontainer") {
		t.Error("scan stopped a container")
	}
}

func TestScanBlocksWhenTheExportsDontFit(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	w.free = 10_100_000_000
	findings, err := w.mod.Scan(w.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if f := find(findings, "space"); f.Severity != module.SeverityBlocker || !strings.Contains(f.Fix, "--stage") {
		t.Errorf("space finding = %+v, want a blocker suggesting --stage", f)
	}
}

func TestScanWarnsAboutAContainerThatRestartsWithDocker(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	always := strings.Replace(strings.ReplaceAll(containerInspect, "HOME", w.home), "unless-stopped", "always", 1)
	w.respond("container inspect dbcontainer testcontainer holder", runner.Response{Stdout: always})
	findings, err := w.mod.Scan(w.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if f := find(findings, "restart:acme-db-1"); f.Fix != "Run: docker update --restart unless-stopped 'acme-db-1'" {
		t.Errorf("restart finding = %+v", f)
	}
}

func TestScanWithoutDockerDesktopFindsNothing(t *testing.T) {
	w := newWorld(t)
	w.mod.Docker = "/nonexistent/docker"
	if findings, err := w.mod.Scan(w.env(nil)); err != nil || len(findings) != 0 {
		t.Errorf("Scan = %+v, %v; want nothing", findings, err)
	}
}

func TestScanAsksToStartDockerDesktop(t *testing.T) {
	w := newWorld(t)
	os.Remove(socket(w.home))
	findings, err := w.mod.Scan(w.env(nil))
	if err != nil || !slices.Equal(ids(findings), []string{"not-running"}) {
		t.Errorf("Scan = %+v, %v; want not-running", findings, err)
	}
}

func gunzip(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPrepareStopsContainersAndExportsEverything(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	res, inv := w.prepare(record(t, Inventory{}))
	if !inv.Installed || inv.Engine != "29.2.0" {
		t.Errorf("inventory = %+v", inv)
	}
	if len(inv.Stopped) != 1 || inv.Stopped[0].ID != "dbcontainer" || inv.Stopped[0].WorkingDir != "~/GitHub/acme" {
		t.Errorf("stopped = %+v, want acme-db-1 with its Compose project", inv.Stopped)
	}
	if len(inv.Volumes) != 2 {
		t.Fatalf("volumes = %+v", inv.Volumes)
	}
	pg, cache := inv.Volumes[0], inv.Volumes[1]
	if pg.Digest != digestFor(t, pgdataTar(t)) || pg.Entries != 3 || cache.Digest != digestFor(t, cacheTar(t)) || cache.Entries != 0 {
		t.Errorf("digests = %+v / %+v", pg, cache)
	}
	if pg.Archive == nil || pg.Archive.Name != "docker/volumes/acme_pgdata.tar.gz" || w.exports.CheckArtifact(*pg.Archive) != nil {
		t.Fatalf("pgdata archive = %+v", pg.Archive)
	}
	if got := gunzip(t, w.exports.Path(pg.Archive.Name)); !bytes.Equal(got, pgdataTar(t)) {
		t.Error("the volume archive doesn't hold what docker container cp wrote")
	}
	web, postgres := inv.Images[0], inv.Images[1]
	if web.Archive == nil || web.Archive.Name != webArchive || web.Archive.Size != int64(len("web image archive")) {
		t.Errorf("web image = %+v", web)
	}
	if !postgres.Pulled || postgres.Archive != nil {
		t.Errorf("postgres image = %+v, want pulled without an archive", postgres)
	}
	if !w.ran("image rm "+EmptyImage) || !w.ran("container rm holder-pgdata") || !w.ran("container rm holder-cache") {
		t.Error("prepare left its empty image or a holder behind")
	}
	if want := []string{"quit"}; !slices.Equal(ids(res.Problems), want) {
		t.Errorf("problems = %+v, want only the quit Docker step", res.Problems)
	}
	if !slices.Contains(res.Done, "stopped 1 container: acme-db-1") || !slices.Contains(res.Done, "saved 1 locally built image (17 B)") {
		t.Errorf("done = %q", res.Done)
	}
}

func TestPrepareChecksSpaceBeforeStoppingAnything(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	w.free = 10_100_000_000
	_, err := w.mod.Prepare(w.env(record(t, Inventory{})), w.st)
	if err == nil || !strings.Contains(err.Error(), "don't fit") {
		t.Errorf("err = %v, want the exports don't fit", err)
	}
	if w.ran("container stop --time 60 dbcontainer") {
		t.Error("prepare stopped a container before finding the exports don't fit")
	}
}

func TestPrepareKeepsTheRecordWhileDockerIsNotRunning(t *testing.T) {
	w := newWorld(t)
	earlier := Inventory{Installed: true, Stopped: []Container{{ID: "dbcontainer", Name: "acme-db-1"}}}
	os.Remove(socket(w.home))
	res, inv := w.prepare(record(t, earlier))
	if !reflect.DeepEqual(inv.Stopped, earlier.Stopped) || !slices.Equal(ids(res.Problems), []string{"kept"}) {
		t.Errorf("result = %+v, want the earlier record kept with a warning", res)
	}
}

func TestPrepareFailsWhileDockerIsNotRunningWithoutARecord(t *testing.T) {
	w := newWorld(t)
	os.Remove(socket(w.home))
	_, err := w.mod.Prepare(w.env(record(t, Inventory{})), w.st)
	if err == nil || err.Error() != "Docker Desktop isn't running. Start it and run: mtm prepare --only docker" {
		t.Errorf("err = %v", err)
	}
}

func TestPrepareWithoutDockerDesktop(t *testing.T) {
	w := newWorld(t)
	w.mod.Docker = "/nonexistent/docker"
	_, inv := w.prepare(record(t, Inventory{}))
	if inv.Installed {
		t.Errorf("inventory = %+v, want not installed", inv)
	}
}

func TestPrepareStopFailureRecordsVolumesWithoutDigests(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	w.respond("container stop --time 60 dbcontainer", runner.Response{Code: 1, Stderr: "cannot stop"})
	res, inv := w.prepare(record(t, Inventory{}))
	if !module.HasBlocker(res.Problems) || !strings.Contains(find(res.Problems, "stop:acme-db-1").Fix, "docker container stop 'acme-db-1'") {
		t.Errorf("problems = %+v", res.Problems)
	}
	if len(inv.Volumes) != 2 || inv.Volumes[0].Digest != "" {
		t.Errorf("volumes = %+v, want both recorded without digests", inv.Volumes)
	}
	if w.ran("image import - " + EmptyImage) {
		t.Error("prepare exported volumes a running container could still change")
	}
}

func TestPrepareVolumeExportFailureBlocks(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	w.respond("container cp holder-cache:/vol -", runner.Response{Code: 1, Stderr: "Error response from daemon"})
	res, inv := w.prepare(record(t, Inventory{}))
	if f := find(res.Problems, "volume:cache"); f.Severity != module.SeverityBlocker {
		t.Errorf("problems = %+v, want a blocker for cache", res.Problems)
	}
	if inv.Volumes[0].Digest == "" || inv.Volumes[1].Digest != "" || inv.Volumes[1].Archive != nil {
		t.Errorf("volumes = %+v, want pgdata exported and cache recorded without a digest", inv.Volumes)
	}
	if _, err := os.Stat(w.exports.Path("docker/volumes/cache.tar.gz")); err == nil {
		t.Error("a failed export left an archive behind")
	}
}

func TestPrepareAgainKeepsAnUnchangedImageArchive(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	_, first := w.prepare(record(t, Inventory{}))
	res, second := w.prepare(record(t, first))
	if n := w.count("image save web:latest"); n != 1 {
		t.Errorf("image saved %d times, want once", n)
	}
	if *second.Images[0].Archive != *first.Images[0].Archive {
		t.Errorf("archive = %+v, want the earlier one", second.Images[0].Archive)
	}
	if !slices.Contains(res.Done, "kept 1 locally built image the earlier prepare saved") {
		t.Errorf("done = %q", res.Done)
	}
}

func TestPrepareAgainSavesAnImageWhoseArchiveChanged(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	_, first := w.prepare(record(t, Inventory{}))
	os.WriteFile(w.exports.Path(webArchive), []byte("tampered"), 0o600)
	w.prepare(record(t, first))
	if n := w.count("image save web:latest"); n != 2 {
		t.Errorf("image saved %d times, want again after its archive changed", n)
	}
}

func TestPrepareAgainKeepsContainersStoppedEarlier(t *testing.T) {
	w := newWorld(t)
	w.oldMac()
	stoppedNow := strings.Replace(strings.ReplaceAll(containerInspect, "HOME", w.home), `"Running": true`, `"Running": false`, 1)
	w.respond("container inspect dbcontainer testcontainer holder", runner.Response{Stdout: stoppedNow})
	earlier := Inventory{Installed: true, Stopped: []Container{{ID: "dbcontainer", Name: "acme-db-1"}, {ID: "gone", Name: "removed-since"}}}
	_, inv := w.prepare(record(t, earlier))
	if len(inv.Stopped) != 1 || inv.Stopped[0].ID != "dbcontainer" {
		t.Errorf("stopped = %+v, want only the container that still exists", inv.Stopped)
	}
	if w.ran("container stop --time 60 dbcontainer") {
		t.Error("prepare stopped a container that wasn't running")
	}
}

func TestUndoStartsWhatPrepareStopped(t *testing.T) {
	w := newWorld(t)
	w.respond("container start dbcontainer", runner.Response{Stdout: "dbcontainer\n"})
	res, err := w.mod.Undo(w.env(record(t, Inventory{Installed: true, Stopped: []Container{{ID: "dbcontainer", Name: "acme-db-1"}}})), w.st)
	if err != nil || !slices.Equal(res.Done, []string{"started 1 container: acme-db-1"}) {
		t.Errorf("Undo = %+v, %v", res, err)
	}
}

func TestUndoFailsWhileDockerIsNotRunning(t *testing.T) {
	w := newWorld(t)
	os.Remove(socket(w.home))
	if _, err := w.mod.Undo(w.env(record(t, Inventory{Installed: true, Stopped: []Container{{ID: "x", Name: "x"}}})), w.st); err == nil {
		t.Error("Undo succeeded without Docker Desktop, which would drop the record")
	}
}

func TestUndoWithNothingToStartRunsNoCommand(t *testing.T) {
	w := newWorld(t)
	if _, err := w.mod.Undo(w.env(record(t, Inventory{Installed: true})), w.st); err != nil {
		t.Fatal(err)
	}
	if n := len(w.fake.Calls()); n != 0 {
		t.Errorf("ran %d commands, want none", n)
	}
}
