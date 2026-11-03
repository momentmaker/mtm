package docker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

// world is a Mac with Docker Desktop running, faked: every docker command
// is answered from responses keyed by its arguments. A command given a
// sequence with then gets the next response each time it runs.
type world struct {
	t       *testing.T
	home    string
	mod     Module
	fake    *runner.Fake
	seq     map[string][]runner.Response
	st      *stage.Stage
	exports *stage.Stage
	free    uint64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	testutil.WriteFile(t, socket(home), "")
	cli := filepath.Join(root, "docker")
	testutil.WriteFile(t, cli, "")
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	exports, err := stage.OpenDir(st.Path(stage.ExportsDirName))
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, home: home, mod: Module{Docker: cli, Empty: EmptyImage}, fake: &runner.Fake{Responses: map[string]runner.Response{}},
		seq: map[string][]runner.Response{}, st: st, exports: exports, free: 100_000_000_000}
	w.respond("version --format {{.Server.Version}}", runner.Response{Stdout: "29.2.0\n"})
	w.respond("container ls --all --quiet --filter label=dev.mtm.temporary", runner.Response{})
	return w
}

func (w *world) respond(args string, r runner.Response) {
	w.fake.Responses[w.mod.Docker+" "+args] = r
}

func (w *world) then(args string, rs ...runner.Response) {
	w.seq[w.mod.Docker+" "+args] = rs
}

func (w *world) Run(ctx context.Context, cmd runner.Command) (runner.Output, error) {
	if q := w.seq[cmd.String()]; len(q) > 0 {
		w.fake.Responses[cmd.String()], w.seq[cmd.String()] = q[0], q[1:]
	}
	return w.fake.Run(ctx, cmd)
}

func (w *world) env(m *manifest.Manifest) module.Env {
	return module.Env{
		Ctx: context.Background(), Run: w, Home: w.home, Manifest: m, Exports: w.exports,
		Free: func(string) (uint64, error) { return w.free, nil },
	}
}

func (w *world) ran(args string) bool {
	for _, c := range w.fake.Calls() {
		if c.String() == w.mod.Docker+" "+args {
			return true
		}
	}
	return false
}

// stdin is what the command with args read from its standard input.
func (w *world) stdin(args string) string {
	for _, c := range w.fake.Calls() {
		if c.String() == w.mod.Docker+" "+args {
			return c.Stdin
		}
	}
	w.t.Fatalf("%s did not run", args)
	return ""
}

// The archives docker container cp writes for the two volumes.
func pgdataTar(t *testing.T) []byte { return volumeTar(t, root, version, sub, table) }
func cacheTar(t *testing.T) []byte  { return volumeTar(t, root) }

func digestFor(t *testing.T, archive []byte) string {
	sum, _ := digestOf(t, archive)
	return sum
}

// imageInspect describes two images under the containerd image store: web
// was built here, postgres pulled.
const imageInspect = `[
  {"Id": "sha256:97b7d22c446fad509c336466f3185b8b27d1f17866c845f762e3a62cdaf3942f",
   "RepoTags": ["web:latest"], "RepoDigests": ["web@sha256:97b7d22c446fad509c336466f3185b8b27d1f17866c845f762e3a62cdaf3942f"],
   "Size": 141949797,
   "Descriptor": {"mediaType": "application/vnd.oci.image.index.v1+json", "digest": "sha256:97b7d22c446fad509c336466f3185b8b27d1f17866c845f762e3a62cdaf3942f", "size": 856},
   "Identity": {"Build": [{"Ref": "dmuc03ygk19qwt4cjc44aigup", "CreatedAt": "2026-11-02T01:37:05.970614002Z"}]}},
  {"Id": "sha256:97ff59a4e30e08d1c11bdcd9455e7832368c0572b576c9092cde2df4ae5552a3",
   "RepoTags": ["postgres:16-alpine"], "RepoDigests": ["postgres@sha256:97ff59a4e30e08d1c11bdcd9455e7832368c0572b576c9092cde2df4ae5552a3"],
   "Size": 108190238,
   "Descriptor": {"mediaType": "application/vnd.oci.image.index.v1+json", "digest": "sha256:97ff59a4e30e08d1c11bdcd9455e7832368c0572b576c9092cde2df4ae5552a3", "size": 10301},
   "Identity": {"Pull": [{"Repository": "docker.io/library/postgres"}]}}
]`

const (
	webID      = "sha256:97b7d22c446fad509c336466f3185b8b27d1f17866c845f762e3a62cdaf3942f"
	postgresID = "sha256:97ff59a4e30e08d1c11bdcd9455e7832368c0572b576c9092cde2df4ae5552a3"
	webArchive = "docker/images/97b7d22c446fad509c336466f3185b8b27d1f17866c845f762e3a62cdaf3942f.tar"
)

// volumeInspect has a Compose volume, an anonymous one, and an NFS mount
// mtm can't export.
const volumeInspect = `[
  {"CreatedAt": "2026-03-27T22:54:32Z", "Driver": "local",
   "Labels": {"com.docker.compose.project": "acme", "com.docker.compose.volume": "pgdata"},
   "Mountpoint": "/var/lib/docker/volumes/acme_pgdata/_data", "Name": "acme_pgdata", "Options": null, "Scope": "local"},
  {"CreatedAt": "2026-03-27T22:54:32Z", "Driver": "local", "Labels": {"com.docker.volume.anonymous": ""},
   "Mountpoint": "/var/lib/docker/volumes/cache/_data", "Name": "cache", "Options": null, "Scope": "local"},
  {"CreatedAt": "2026-03-27T22:54:32Z", "Driver": "local", "Labels": null,
   "Mountpoint": "/var/lib/docker/volumes/nfs/_data", "Name": "nfs", "Options": {"type": "nfs", "o": "addr=10.0.0.2"}, "Scope": "local"}
]`

const systemDF = `{"Images": [], "Containers": [], "BuildCache": [], "Volumes": [
  {"Name": "acme_pgdata", "Size": "224.3MB", "Links": "1"},
  {"Name": "cache", "Size": "52.38MB", "Links": "0"},
  {"Name": "nfs", "Size": "0B", "Links": "0"}
]}`

// containerInspect has a running Compose database, an exited container
// outside Compose, and a volume holder an interrupted mtm run left behind.
const containerInspect = `[
  {"Id": "dbcontainer", "Name": "/acme-db-1", "State": {"Status": "running", "Running": true},
   "HostConfig": {"RestartPolicy": {"Name": "unless-stopped", "MaximumRetryCount": 0}},
   "Config": {"Labels": {"com.docker.compose.project": "acme", "com.docker.compose.service": "db",
     "com.docker.compose.project.working_dir": "HOME/GitHub/acme",
     "com.docker.compose.project.config_files": "HOME/GitHub/acme/docker-compose.yml,/opt/shared/compose.yml"}},
   "Mounts": [{"Type": "volume", "Name": "acme_pgdata", "Destination": "/var/lib/postgresql/data"}]},
  {"Id": "testcontainer", "Name": "/acme-yt-testdb", "State": {"Status": "exited", "Running": false},
   "HostConfig": {"RestartPolicy": {"Name": "no", "MaximumRetryCount": 0}},
   "Config": {"Labels": {}},
   "Mounts": [{"Type": "volume", "Name": "cache", "Destination": "/data"}, {"Type": "bind", "Name": "", "Destination": "/src"}]},
  {"Id": "holder", "Name": "/brave_turing", "State": {"Status": "created", "Running": false},
   "HostConfig": {"RestartPolicy": {"Name": "no", "MaximumRetryCount": 0}},
   "Config": {"Labels": {"dev.mtm.temporary": "1"}}, "Mounts": []}
]`

// oldMac answers the commands scan and prepare run on a Mac with the
// images, volumes, and containers above.
func (w *world) oldMac() {
	t := w.t
	w.respond("image ls --quiet", runner.Response{Stdout: "97b7d22c446f\n97ff59a4e30e\n97b7d22c446f\n"})
	w.respond("image inspect 97b7d22c446f 97ff59a4e30e", runner.Response{Stdout: imageInspect})
	w.respond("volume ls --quiet", runner.Response{Stdout: "acme_pgdata\ncache\nnfs\n"})
	w.respond("volume inspect acme_pgdata cache nfs", runner.Response{Stdout: volumeInspect})
	w.respond("system df --verbose --format json", runner.Response{Stdout: systemDF})
	w.respond("container ls --all --quiet --no-trunc", runner.Response{Stdout: "dbcontainer\ntestcontainer\nholder\n"})
	w.respond("container inspect dbcontainer testcontainer holder", runner.Response{Stdout: strings.ReplaceAll(containerInspect, "HOME", w.home)})
	w.respond("container stop --time 60 dbcontainer", runner.Response{Stdout: "dbcontainer\n"})
	w.respond("image import - "+EmptyImage, runner.Response{Stdout: "sha256:700c\n"})
	w.respond("image rm "+EmptyImage, runner.Response{})
	w.holder("acme_pgdata", "holder-pgdata", pgdataTar(t))
	w.holder("cache", "holder-cache", cacheTar(t))
	w.respond("image save web:latest", runner.Response{Stdout: "web image archive"})
}

// holder answers the commands that mount volume in a holder container,
// read it as archive, and remove the holder.
func (w *world) holder(volume, id string, archive []byte) {
	w.respond("container create --pull never --label dev.mtm.temporary=1 --volume "+volume+":/vol "+EmptyImage+" /mtm-holder", runner.Response{Stdout: id + "\n"})
	w.respond("container cp "+id+":/vol -", runner.Response{Stdout: string(archive)})
	w.respond("container cp --archive --quiet - "+id+":/", runner.Response{})
	w.respond("container rm "+id, runner.Response{Stdout: id + "\n"})
}
