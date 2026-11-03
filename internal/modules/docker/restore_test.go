package docker

import (
	"archive/tar"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
)

const imagesHere = `{"ID": "sha256:97b7d22c446fad509c336466f3185b8b27d1f17866c845f762e3a62cdaf3942f", "Repository": "web", "Tag": "latest"}
{"ID": "sha256:97ff59a4e30e08d1c11bdcd9455e7832368c0572b576c9092cde2df4ae5552a3", "Repository": "postgres", "Tag": "16-alpine"}
`

// migrated runs prepare in a world shaped like oldMac, then turns it into
// the new Mac right after Migration Assistant copied Docker's disk image
// intact: the same volumes, images, and (stopped) containers.
func migrated(t *testing.T) (*world, *manifest.Manifest) {
	t.Helper()
	w := newWorld(t)
	w.oldMac()
	_, inv := w.prepare(record(t, Inventory{}))
	w.respond("image ls --no-trunc --format json", runner.Response{Stdout: imagesHere})
	w.respond("container start dbcontainer", runner.Response{Stdout: "dbcontainer\n"})
	return w, record(t, inv)
}

func (w *world) finish(m *manifest.Manifest) module.Result {
	w.t.Helper()
	res, err := w.mod.Finish(w.env(m), w.st)
	if err != nil {
		w.t.Fatal(err)
	}
	return res
}

func TestFinishSkipsTheRestoreWhenEveryVolumeArrivedIntact(t *testing.T) {
	w, m := migrated(t)
	res := w.finish(m)
	if len(res.Problems) != 0 || !slices.Equal(res.Done, []string{"found 2 volumes intact", "started 1 container: acme-db-1"}) {
		t.Errorf("finish = %+v", res)
	}
	if w.ran("container cp --archive --quiet - holder-pgdata:/") || w.ran("container cp --archive --quiet - holder-cache:/") {
		t.Error("finish restored a volume that arrived intact")
	}
	settled, err := loadSettled(w.st)
	if err != nil || settled["acme_pgdata"] != intact || settled["cache"] != intact {
		t.Errorf("settled = %v, %v", settled, err)
	}
}

func TestFinishRestoresAMissingVolumeWithItsLabels(t *testing.T) {
	w, m := migrated(t)
	w.respond("volume ls --quiet", runner.Response{Stdout: "cache\n"})
	create := "volume create --label com.docker.compose.project=acme --label com.docker.compose.volume=pgdata acme_pgdata"
	w.respond(create, runner.Response{Stdout: "acme_pgdata\n"})
	res := w.finish(m)
	if !w.ran(create) {
		t.Error("finish didn't create the missing volume with its labels")
	}
	if got := w.stdin("container cp --archive --quiet - holder-pgdata:/"); got != string(pgdataTar(t)) {
		t.Error("finish didn't stream the volume's archive into it")
	}
	if !slices.Contains(res.Done, "restored 1 volume from the old Mac's exports") {
		t.Errorf("done = %q", res.Done)
	}
	if settled, _ := loadSettled(w.st); settled["acme_pgdata"] != restored {
		t.Errorf("settled = %v", settled)
	}
}

func TestFinishRestoresAnEmptyVolume(t *testing.T) {
	w, m := migrated(t)
	w.then("container cp holder-pgdata:/vol -", runner.Response{Stdout: string(volumeTar(t, root))}, runner.Response{Stdout: string(pgdataTar(t))})
	res := w.finish(m)
	if !w.ran("container cp --archive --quiet - holder-pgdata:/") || len(res.Problems) != 0 {
		t.Errorf("finish = %+v, want the empty volume restored", res)
	}
}

func TestFinishNeverOverwritesAVolumeHoldingOtherData(t *testing.T) {
	w, m := migrated(t)
	other := volumeTar(t, root, entry{name: "vol/PG_VERSION", typ: tar.TypeReg, mode: 0o600, uid: 70, content: "17\n"})
	w.respond("container cp holder-pgdata:/vol -", runner.Response{Stdout: string(other)})
	res := w.finish(m)
	p := find(res.Problems, "volume:acme_pgdata")
	if p.Severity != module.SeverityBlocker || p.Fix != "To replace it with the old Mac's data, run: docker container rm --force 'acme-db-1' && docker volume rm 'acme_pgdata' && mtm finish --only docker" {
		t.Errorf("problem = %+v", p)
	}
	if w.ran("container cp --archive --quiet - holder-pgdata:/") {
		t.Error("finish wrote into a volume that holds data")
	}
	if w.ran("container start dbcontainer") {
		t.Error("finish started containers while a volume is unsettled")
	}
	if settled, _ := loadSettled(w.st); settled["acme_pgdata"] != "" || settled["cache"] != intact {
		t.Errorf("settled = %v, want only cache", settled)
	}
}

func TestFinishRefusesAnArchiveThatChanged(t *testing.T) {
	w, m := migrated(t)
	w.respond("volume ls --quiet", runner.Response{Stdout: "cache\n"})
	os.WriteFile(w.exports.Path("docker/volumes/acme_pgdata.tar.gz"), []byte("tampered"), 0o600)
	res := w.finish(m)
	if p := find(res.Problems, "volume:acme_pgdata"); p.Severity != module.SeverityBlocker || !strings.Contains(p.Detail, "doesn't match the fingerprint") {
		t.Errorf("problem = %+v", p)
	}
	for _, c := range w.fake.Calls() {
		if strings.Contains(c.String(), "volume create") {
			t.Error("finish created a volume for a damaged archive")
		}
	}
}

func TestFinishLoadsMissingImagesAndListsPulls(t *testing.T) {
	w, m := migrated(t)
	w.respond("image ls --no-trunc --format json", runner.Response{})
	load := "image load --quiet --input " + w.exports.Path(webArchive)
	w.respond(load, runner.Response{Stdout: "Loaded image: web:latest\n"})
	res := w.finish(m)
	if !w.ran(load) || !slices.Contains(res.Done, "loaded 1 locally built image") {
		t.Errorf("finish = %+v, want web loaded", res)
	}
	if p := find(res.Problems, "pull"); p.Severity != module.SeverityWarn || p.Fix != "Run: docker pull 'postgres:16-alpine'" {
		t.Errorf("pull step = %+v", p)
	}
}

func TestFinishSkipsVolumesItSettledBefore(t *testing.T) {
	w, m := migrated(t)
	if err := saveSettled(w.st, map[string]string{"acme_pgdata": intact, "cache": restored}); err != nil {
		t.Fatal(err)
	}
	before := w.count("image import - " + EmptyImage)
	w.finish(m)
	if w.count("image import - "+EmptyImage) != before {
		t.Error("finish read volumes it settled on an earlier run")
	}
}

func TestFinishRecreatesComposeContainersThatAreGone(t *testing.T) {
	w, m := migrated(t)
	w.respond("container ls --all --quiet --no-trunc", runner.Response{})
	var inv Inventory
	m.Module(Name, &inv)
	inv.Stopped = append(inv.Stopped, Container{ID: "lone", Name: "scratch"})
	m = record(t, inv)
	dir := w.home + "/GitHub/acme"
	up := "compose --project-name acme --project-directory " + dir + " --file " + dir + "/docker-compose.yml --file /opt/shared/compose.yml up --detach --pull never --no-build"
	w.respond(up, runner.Response{})
	res := w.finish(m)
	if !slices.Contains(res.Done, "started Compose project acme") {
		t.Errorf("done = %q", res.Done)
	}
	for _, c := range w.fake.Calls() {
		if c.String() == w.mod.Docker+" "+up && c.Dir != dir {
			t.Errorf("compose ran in %q, want %q", c.Dir, dir)
		}
	}
	if p := find(res.Problems, "gone:scratch"); p.Severity != module.SeverityWarn {
		t.Errorf("problems = %+v, want scratch listed by hand", res.Problems)
	}
}

func TestFinishNeedsDockerRunning(t *testing.T) {
	w, m := migrated(t)
	os.Remove(socket(w.home))
	before := len(w.fake.Calls())
	res := w.finish(m)
	if !module.HasBlocker(res.Problems) || len(w.fake.Calls()) != before {
		t.Errorf("finish = %+v, want a blocker and no docker commands", res)
	}
}

func (w *world) verify(m *manifest.Manifest) map[string]module.Check {
	w.t.Helper()
	checks, err := w.mod.Verify(w.env(m), w.st)
	if err != nil {
		w.t.Fatal(err)
	}
	byID := map[string]module.Check{}
	for _, c := range checks {
		byID[c.ID] = c
	}
	return byID
}

func TestVerifyTrustsTheVolumesFinishSettled(t *testing.T) {
	w, m := migrated(t)
	w.finish(m)
	w.respond("container cp holder-pgdata:/vol -", runner.Response{Stdout: string(volumeTar(t, root, version))})
	before := w.count("image import - " + EmptyImage)
	checks := w.verify(m)
	if c := checks["docker:volume:acme_pgdata"]; c.Status != module.StatusPass || !c.Critical {
		t.Errorf("check = %+v, want a critical pass", c)
	}
	if w.count("image import - "+EmptyImage) != before {
		t.Error("verify re-read volumes finish already compared")
	}
}

func TestVerifyComparesVolumesFinishDidntSettle(t *testing.T) {
	w, m := migrated(t)
	w.respond("container cp holder-cache:/vol -", runner.Response{Stdout: string(volumeTar(t, root, version))})
	checks := w.verify(m)
	if c := checks["docker:volume:acme_pgdata"]; c.Status != module.StatusPass || c.Detail != "matches the old Mac's" {
		t.Errorf("pgdata = %+v", c)
	}
	if c := checks["docker:volume:cache"]; c.Status != module.StatusFail || !c.Critical {
		t.Errorf("cache = %+v, want a critical fail", c)
	}
	if !w.ran("image rm " + EmptyImage) {
		t.Error("verify left mtm's empty image behind")
	}
}

func TestVerifyFailsMissingAndUnexportedVolumes(t *testing.T) {
	w, m := migrated(t)
	var inv Inventory
	m.Module(Name, &inv)
	inv.Volumes = append(inv.Volumes, Volume{Name: "never"})
	w.respond("volume ls --quiet", runner.Response{Stdout: "cache\nnever\n"})
	checks := w.verify(record(t, inv))
	if c := checks["docker:volume:acme_pgdata"]; c.Status != module.StatusFail || !strings.Contains(c.Detail, "missing") {
		t.Errorf("pgdata = %+v", c)
	}
	if c := checks["docker:volume:never"]; c.Status != module.StatusFail || !strings.Contains(c.Detail, "couldn't export") {
		t.Errorf("never = %+v", c)
	}
}

func TestVerifyImages(t *testing.T) {
	w, m := migrated(t)
	w.respond("image ls --no-trunc --format json", runner.Response{Stdout: `{"ID": "sha256:0000", "Repository": "web", "Tag": "latest"}` + "\n"})
	checks := w.verify(m)
	if c := checks["docker:image:web:latest"]; c.Status != module.StatusInfo {
		t.Errorf("web = %+v, want info for a tag that now names another image", c)
	}
	if c := checks["docker:pulled"]; c.Status != module.StatusWarn || !slices.Equal(c.Paths, []string{"postgres:16-alpine"}) {
		t.Errorf("pulled = %+v", c)
	}
	w.respond("image ls --no-trunc --format json", runner.Response{})
	if c := w.verify(m)["docker:image:web:latest"]; c.Status != module.StatusFail {
		t.Errorf("web = %+v, want a fail when missing", c)
	}
}

func TestVerifyFailsWithoutDockerRunning(t *testing.T) {
	w, m := migrated(t)
	os.Remove(socket(w.home))
	if c := w.verify(m)["docker:running"]; c.Status != module.StatusFail || !c.Critical {
		t.Errorf("check = %+v, want a critical fail", c)
	}
}

func TestFinishAsksForTheStageDriveWhenItIsDetached(t *testing.T) {
	w, m := migrated(t)
	w.respond("volume ls --quiet", runner.Response{Stdout: "cache\n"})
	w.exports = &stage.Stage{Root: "/Volumes/Detached/mtm-exports"}
	res := w.finish(m)
	if p := find(res.Problems, "volume:acme_pgdata"); p.Severity != module.SeverityBlocker || !strings.Contains(p.Fix, "attach that drive") {
		t.Errorf("problem = %+v, want a blocker asking for the drive", p)
	}
}
