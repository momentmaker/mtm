// Package docker exports Docker Desktop's volumes and locally built images
// before Migration Assistant, and on the new Mac restores whatever didn't
// arrive intact inside Docker's disk image.
package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
)

const (
	Name = "docker"
	// DesktopCLI is the docker command Docker Desktop ships. mtm runs it,
	// not whichever docker comes first on PATH, so client and engine match.
	DesktopCLI = "/Applications/Docker.app/Contents/Resources/bin/docker"
	// EmptyImage is the image mtm imports from an empty archive, so it can
	// mount a volume in a container without pulling anything.
	EmptyImage = "mtm/empty:1"
	mountPoint = "/vol"
	// holderCommand satisfies docker container create, which wants a
	// command. It never runs: mtm never starts a container that holds a
	// volume.
	holderCommand = "/mtm-holder"
	tempLabel     = "dev.mtm.temporary"
)

type Module struct {
	Docker string
	Empty  string
}

func New() Module { return Module{Docker: DesktopCLI, Empty: EmptyImage} }

func (Module) Name() string { return Name }

func (m Module) installed() bool {
	_, err := os.Stat(m.Docker)
	return err == nil
}

func socket(home string) string { return filepath.Join(home, ".docker", "run", "docker.sock") }

// command pins docker to this account's Docker Desktop: a DOCKER_HOST or
// context the user set could point it at another machine. The socket is in
// the home folder, so a run with a temporary HOME reaches no engine at all.
func (m Module) command(env module.Env, args ...string) runner.Command {
	return runner.Command{
		Name:  m.Docker,
		Args:  args,
		Unset: []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"},
		Env:   []string{"DOCKER_HOST=unix://" + socket(env.Home), "DOCKER_CLI_HINTS=false"},
	}
}

func (m Module) docker(env module.Env, args ...string) (runner.Output, error) {
	return env.Run.Run(env.Ctx, m.command(env, args...))
}

// engine returns the Docker Engine version, or "" when Docker Desktop
// isn't running.
func (m Module) engine(env module.Env) string {
	if _, err := os.Stat(socket(env.Home)); err != nil {
		return ""
	}
	out, err := m.docker(env, "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out.Stdout))
}

// Image is a tagged image. A pulled image came from a registry the new Mac
// can pull it from again; the rest were built or loaded here, so prepare
// saves them.
type Image struct {
	ID      string          `json:"id"`
	Tags    []string        `json:"tags"`
	Size    int64           `json:"size"`
	Pulled  bool            `json:"pulled"`
	Archive *stage.Artifact `json:"archive,omitempty"`
}

// Volume is a volume and, once exported, its content digest, the number
// of entries below its root, and its archive. Digest is empty when prepare
// couldn't export it.
type Volume struct {
	Name    string            `json:"name"`
	Labels  map[string]string `json:"labels,omitempty"`
	Size    int64             `json:"size"`
	Digest  string            `json:"digest,omitempty"`
	Entries int               `json:"entries"`
	Archive *stage.Artifact   `json:"archive,omitempty"`
}

// Container is a container prepare stopped. WorkingDir and ConfigFiles
// come from its Compose labels, under ~/ when inside the home folder, so
// finish can re-create it from its project if it no longer exists.
type Container struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Project     string   `json:"project,omitempty"`
	WorkingDir  string   `json:"working_dir,omitempty"`
	ConfigFiles []string `json:"config_files,omitempty"`
}

// container is a container as docker container inspect describes it now.
type container struct {
	Container
	Running bool
	Restart string
	Volumes []string
	// AutoRemove is docker run --rm: stopping the container deletes it and
	// its anonymous volumes.
	AutoRemove bool
}

type inspectedImage struct {
	ID          string          `json:"Id"`
	RepoTags    []string        `json:"RepoTags"`
	RepoDigests []string        `json:"RepoDigests"`
	Size        int64           `json:"Size"`
	Descriptor  json.RawMessage `json:"Descriptor"`
	Identity    *struct {
		Pull []json.RawMessage `json:"Pull"`
	} `json:"Identity"`
}

// pulled reports an image the new Mac can pull again. Docker's containerd
// image store, which has a Descriptor in docker image inspect, gives every
// image a repo digest, built ones too, so there only Identity.Pull marks a
// pulled image. The classic store gives repo digests to pulled and pushed
// images only.
func (i inspectedImage) pulled() bool {
	if len(i.Descriptor) > 0 {
		return i.Identity != nil && len(i.Identity.Pull) > 0
	}
	return len(i.RepoDigests) > 0
}

func (m Module) images(env module.Env) ([]Image, error) {
	out, err := m.docker(env, "image", "ls", "--quiet")
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	ids := unique(strings.Fields(string(out.Stdout)))
	if len(ids) == 0 {
		return nil, nil
	}
	out, err = m.docker(env, append([]string{"image", "inspect"}, ids...)...)
	if err != nil {
		return nil, fmt.Errorf("inspect images: %w", err)
	}
	return parseImages(out.Stdout)
}

// parseImages skips untagged images: nothing refers to them by name.
func parseImages(data []byte) ([]Image, error) {
	var raw []inspectedImage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse docker image inspect: %w", err)
	}
	var images []Image
	for _, r := range raw {
		if len(r.RepoTags) == 0 {
			continue
		}
		images = append(images, Image{ID: r.ID, Tags: r.RepoTags, Size: r.Size, Pulled: r.pulled()})
	}
	return images, nil
}

// volumes lists the volumes mtm can export, with their sizes, and the
// others with the reason it can't.
func (m Module) volumes(env module.Env) ([]Volume, []string, error) {
	out, err := m.docker(env, "volume", "ls", "--quiet")
	if err != nil {
		return nil, nil, fmt.Errorf("list volumes: %w", err)
	}
	names := strings.Fields(string(out.Stdout))
	if len(names) == 0 {
		return nil, nil, nil
	}
	out, err = m.docker(env, append([]string{"volume", "inspect"}, names...)...)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect volumes: %w", err)
	}
	var raw []struct {
		Name    string            `json:"Name"`
		Driver  string            `json:"Driver"`
		Labels  map[string]string `json:"Labels"`
		Options map[string]string `json:"Options"`
	}
	if err := json.Unmarshal(out.Stdout, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse docker volume inspect: %w", err)
	}
	sizes, err := m.volumeSizes(env)
	if err != nil {
		return nil, nil, err
	}
	var vols []Volume
	var skipped []string
	for _, r := range raw {
		switch {
		case r.Driver != "local":
			skipped = append(skipped, fmt.Sprintf("%s (driver %s)", r.Name, r.Driver))
		case len(r.Options) > 0:
			skipped = append(skipped, r.Name+" (driver options, such as a bind to a folder)")
		default:
			vols = append(vols, Volume{Name: r.Name, Labels: r.Labels, Size: sizes[r.Name]})
		}
	}
	return vols, skipped, nil
}

func (m Module) volumeNames(env module.Env) ([]string, error) {
	out, err := m.docker(env, "volume", "ls", "--quiet")
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	return strings.Fields(string(out.Stdout)), nil
}

func (m Module) volumeSizes(env module.Env) (map[string]int64, error) {
	out, err := m.docker(env, "system", "df", "--verbose", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("measure volumes: %w", err)
	}
	var df struct {
		Volumes []struct {
			Name string `json:"Name"`
			Size string `json:"Size"`
		} `json:"Volumes"`
	}
	if err := json.Unmarshal(out.Stdout, &df); err != nil {
		return nil, fmt.Errorf("parse docker system df: %w", err)
	}
	sizes := map[string]int64{}
	for _, v := range df.Volumes {
		sizes[v.Name] = parseSize(v.Size)
	}
	return sizes, nil
}

// parseSize reads sizes as docker system df prints them, "73.85MB", in
// decimal units. Anything else reads as 0: sizes only feed the free-space
// estimate, and each archive's real size is checked as it is written.
func parseSize(s string) int64 {
	units := []struct {
		suffix string
		mult   float64
	}{{"kB", 1e3}, {"KB", 1e3}, {"MB", 1e6}, {"GB", 1e9}, {"TB", 1e12}, {"PB", 1e15}, {"B", 1}}
	for _, u := range units {
		if num, ok := strings.CutSuffix(s, u.suffix); ok {
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0
			}
			return int64(f * u.mult)
		}
	}
	return 0
}

func (m Module) containers(env module.Env) ([]container, error) {
	out, err := m.docker(env, "container", "ls", "--all", "--quiet", "--no-trunc")
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	ids := strings.Fields(string(out.Stdout))
	if len(ids) == 0 {
		return nil, nil
	}
	out, err = m.docker(env, append([]string{"container", "inspect"}, ids...)...)
	if err != nil {
		return nil, fmt.Errorf("inspect containers: %w", err)
	}
	return parseContainers(env.Home, out.Stdout)
}

// parseContainers leaves out mtm's own volume holders.
func parseContainers(home string, data []byte) ([]container, error) {
	var raw []struct {
		ID    string `json:"Id"`
		Name  string `json:"Name"`
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
		HostConfig struct {
			AutoRemove    bool `json:"AutoRemove"`
			RestartPolicy struct {
				Name string `json:"Name"`
			} `json:"RestartPolicy"`
		} `json:"HostConfig"`
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		Mounts []struct {
			Type string `json:"Type"`
			Name string `json:"Name"`
		} `json:"Mounts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse docker container inspect: %w", err)
	}
	var out []container
	for _, r := range raw {
		labels := r.Config.Labels
		if _, ours := labels[tempLabel]; ours {
			continue
		}
		c := container{
			Container: Container{
				ID: r.ID, Name: strings.TrimPrefix(r.Name, "/"),
				Project:    labels["com.docker.compose.project"],
				WorkingDir: homeRelative(home, labels["com.docker.compose.project.working_dir"]),
			},
			Running:    r.State.Running,
			Restart:    r.HostConfig.RestartPolicy.Name,
			AutoRemove: r.HostConfig.AutoRemove,
		}
		if files := labels["com.docker.compose.project.config_files"]; files != "" {
			for _, f := range strings.Split(files, ",") {
				c.ConfigFiles = append(c.ConfigFiles, homeRelative(home, f))
			}
		}
		for _, mount := range r.Mounts {
			if mount.Type == "volume" {
				c.Volumes = append(c.Volumes, mount.Name)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// emptyArchive is an empty tar archive: two zero blocks.
var emptyArchive = make([]byte, 1024)

func (m Module) importEmpty(env module.Env) error {
	cmd := m.command(env, "image", "import", "-", m.Empty)
	cmd.Input = bytes.NewReader(emptyArchive)
	if _, err := env.Run.Run(env.Ctx, cmd); err != nil {
		return fmt.Errorf("create the empty image mtm mounts volumes with: %w", err)
	}
	return nil
}

func (m Module) removeEmpty(env module.Env) error {
	if _, err := m.docker(env, "image", "rm", m.Empty); err != nil {
		return fmt.Errorf("remove mtm's empty image: %w", err)
	}
	return nil
}

// hold creates a never-started container with volume mounted at /vol and
// returns its id. Docker creates the volume if it doesn't exist.
func (m Module) hold(env module.Env, volume string) (string, error) {
	out, err := m.docker(env, "container", "create", "--pull", "never", "--label", tempLabel+"=1",
		"--volume", volume+":"+mountPoint, m.Empty, holderCommand)
	if err != nil {
		return "", fmt.Errorf("mount volume %s: %w", volume, err)
	}
	id := strings.TrimSpace(string(out.Stdout))
	if id == "" {
		return "", fmt.Errorf("mount volume %s: docker printed no container id", volume)
	}
	return id, nil
}

// release removes a holder. If that fails, the next prepare or finish
// removes it as a leftover.
func (m Module) release(env module.Env, id string) {
	m.docker(env, "container", "rm", id)
}

// removeLeftovers removes holders an interrupted run left behind.
func (m Module) removeLeftovers(env module.Env) error {
	out, err := m.docker(env, "container", "ls", "--all", "--quiet", "--filter", "label="+tempLabel)
	if err != nil {
		return fmt.Errorf("list mtm's leftover containers: %w", err)
	}
	ids := strings.Fields(string(out.Stdout))
	if len(ids) == 0 {
		return nil
	}
	if _, err := m.docker(env, append([]string{"container", "rm"}, ids...)...); err != nil {
		return fmt.Errorf("remove mtm's leftover containers: %w", err)
	}
	return nil
}

// readVolume streams the volume's content to w as a tar archive.
func (m Module) readVolume(env module.Env, volume string, w io.Writer) error {
	id, err := m.hold(env, volume)
	if err != nil {
		return err
	}
	defer m.release(env, id)
	cmd := m.command(env, "container", "cp", id+":"+mountPoint, "-")
	cmd.Stdout = w
	if _, err := env.Run.Run(env.Ctx, cmd); err != nil {
		return fmt.Errorf("read volume %s: %w", volume, err)
	}
	return nil
}

// writeVolume extracts an archive readVolume wrote into the volume.
// --archive keeps owners and modes; without it every file would belong to
// root.
func (m Module) writeVolume(env module.Env, volume string, archive io.Reader) error {
	id, err := m.hold(env, volume)
	if err != nil {
		return err
	}
	defer m.release(env, id)
	cmd := m.command(env, "container", "cp", "--archive", "--quiet", "-", id+":/")
	cmd.Input = archive
	if _, err := env.Run.Run(env.Ctx, cmd); err != nil {
		return fmt.Errorf("restore volume %s: %w", volume, err)
	}
	return nil
}

// volumeDigest reads a volume and returns its content digest and the
// number of entries below its root.
func (m Module) volumeDigest(env module.Env, volume string) (string, int, error) {
	d := NewDigest()
	if err := m.readVolume(env, volume, d); err != nil {
		d.Finish()
		return "", 0, err
	}
	return d.Finish()
}

func homeRelative(home, p string) string {
	switch {
	case p == home:
		return "~"
	case strings.HasPrefix(p, home+"/"):
		return "~/" + strings.TrimPrefix(p, home+"/")
	}
	return p
}

func expandHome(home, p string) string {
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	}
	return p
}

func unique(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if !seen[it] {
			seen[it] = true
			out = append(out, it)
		}
	}
	return out
}
