package docker

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

// Inventory is what prepare records. Stopped lists the containers it
// stopped, which finish and undo start again.
type Inventory struct {
	Installed bool        `json:"installed"`
	Engine    string      `json:"engine,omitempty"`
	Images    []Image     `json:"images,omitempty"`
	Volumes   []Volume    `json:"volumes,omitempty"`
	Stopped   []Container `json:"stopped,omitempty"`
}

// stopTimeout gives a database in a container time to shut down cleanly
// before Docker kills it.
const stopTimeout = "60"

const spaceFix = "Free space, or run mtm prepare --stage /Volumes/<drive> with an encrypted APFS drive."

var quitDocker = module.Finding{
	Module: Name, ID: "quit", Severity: module.SeverityWarn, Handler: module.Manual,
	Title:  "Quit Docker Desktop before Migration Assistant",
	Detail: "Its disk image copies whole only while nothing writes to it. mtm finish checks every volume either way.",
	Fix:    "Click the whale in the menu bar and choose Quit Docker Desktop.",
}

type snapshot struct {
	images     []Image
	volumes    []Volume
	skipped    []string
	containers []container
}

func (m Module) snapshot(env module.Env) (snapshot, error) {
	var s snapshot
	var err error
	if s.images, err = m.images(env); err != nil {
		return s, err
	}
	if s.volumes, s.skipped, err = m.volumes(env); err != nil {
		return s, err
	}
	s.containers, err = m.containers(env)
	return s, err
}

// exportSize is what prepare writes: every volume, and each locally built
// image without an archive it can keep from an earlier run.
func exportSize(s snapshot, kept map[string]*stage.Artifact) uint64 {
	var n int64
	for _, v := range s.volumes {
		n += v.Size
	}
	for _, img := range s.images {
		if !img.Pulled && kept[img.ID] == nil {
			n += img.Size
		}
	}
	return uint64(n)
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	if !m.installed() {
		return nil, nil
	}
	if m.engine(env) == "" {
		return []module.Finding{{
			Module: Name, ID: "not-running", Severity: module.SeverityWarn, Handler: module.ByMtm,
			Title: "Docker Desktop isn't running",
			Fix:   "Start it, so mtm scan can list its volumes and images. mtm prepare needs it running.",
		}}, nil
	}
	s, err := m.snapshot(env)
	if err != nil {
		return nil, err
	}
	var built, pulled int
	for _, img := range s.images {
		if img.Pulled {
			pulled++
		} else {
			built++
		}
	}
	size := exportSize(s, nil)
	findings := []module.Finding{{
		Module: Name, ID: "exports", Severity: module.SeverityInfo, Handler: module.ByMtm,
		Title:  fmt.Sprintf("%s and %s to export (%s)", words.Count(len(s.volumes), "volume", "volumes"), words.Count(built, "locally built image", "locally built images"), words.Bytes(size)),
		Detail: "prepare exports them as a safety net; finish restores them only if Docker's disk image didn't arrive intact.",
	}}
	if pulled > 0 {
		findings = append(findings, module.Finding{
			Module: Name, ID: "pulled", Severity: module.SeverityInfo, Handler: module.ByMtm,
			Title:  words.Count(pulled, "pulled image", "pulled images"),
			Detail: "The new Mac can pull them again; finish lists the docker pull commands for any that are missing.",
		})
	}
	if running := runningNames(s.containers); len(running) > 0 {
		findings = append(findings, module.Finding{
			Module: Name, ID: "running", Severity: module.SeverityInfo, Handler: module.ByMtm,
			Title:  words.Count(len(running), "running container", "running containers") + ": " + strings.Join(running, ", "),
			Detail: "prepare stops them so their volumes copy whole; finish starts them on the new Mac.",
		})
	}
	if err := stage.Room(env.Free, env.Home, size); err != nil {
		findings = append(findings, module.Finding{
			Module: Name, ID: "space", Severity: module.SeverityBlocker, Handler: module.Manual,
			Title: "Docker's exports don't fit on this Mac", Detail: err.Error(), Fix: spaceFix,
		})
	}
	for _, c := range s.containers {
		if c.Running && c.AutoRemove {
			findings = append(findings, module.Finding{
				Module: Name, ID: "autoremove:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
				Title:  c.Name + " was started with --rm",
				Detail: "Stopping it deletes it and any anonymous volume it uses, so mtm prepare won't stop it.",
				Fix:    "Copy out any data you need, then stop it yourself before mtm prepare: docker container stop " + macos.ShellQuote(c.Name),
			})
		}
		if c.Restart == "always" && len(c.Volumes) > 0 {
			findings = append(findings, module.Finding{
				Module: Name, ID: "restart:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
				Title:  c.Name + " restarts whenever Docker starts",
				Detail: "On the new Mac it would change its volumes before mtm finish checks them.",
				Fix:    "Run: docker update --restart unless-stopped " + macos.ShellQuote(c.Name),
			})
		}
	}
	if len(s.skipped) > 0 {
		findings = append(findings, module.Finding{
			Module: Name, ID: "skipped", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "mtm can't export " + strings.Join(s.skipped, ", "),
			Fix:   "Back these volumes up yourself before you erase the old Mac.",
		})
	}
	return findings, nil
}

func runningNames(cs []container) []string {
	var names []string
	for _, c := range cs {
		if c.Running {
			names = append(names, c.Name)
		}
	}
	return names
}

func (m Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	if !m.installed() {
		return module.Result{Inventory: Inventory{}, Done: []string{"Docker Desktop is not installed"}}, nil
	}
	var previous Inventory
	_, err := module.LoadInventory(env, Name, &previous)
	if err != nil && !errors.Is(err, module.ErrNotPrepared) {
		return module.Result{}, err
	}
	recorded := err == nil && previous.Installed
	engine := m.engine(env)
	if engine == "" {
		return keepPrevious(previous, recorded, m.keptProblem(env, previous), "Docker Desktop isn't running. Start it and run: mtm prepare --only docker")
	}
	if err := m.removeLeftovers(env); err != nil {
		return keepPrevious(previous, recorded, blocker("list", "Could not list Docker's containers", err), "")
	}
	s, err := m.snapshot(env)
	if err != nil {
		return keepPrevious(previous, recorded, blocker("list", "Could not list Docker's images, volumes, and containers", err), "")
	}
	kept := m.keptArchives(env, previous, s.images)
	if err := stage.Room(env.Free, env.Exports.Root, exportSize(s, kept)); err != nil {
		problem := blocker("space", "Docker's exports don't fit", err)
		problem.Fix = spaceFix
		return keepPrevious(previous, recorded, problem, "")
	}
	if doomed := autoRemoving(s.containers); len(doomed) > 0 {
		pronoun := "it"
		if len(doomed) > 1 {
			pronoun = "them"
		}
		problem := blocker("autoremove", "Stopping "+strings.Join(doomed, ", ")+" would delete "+pronoun+" and any anonymous volumes", errors.New("They were started with --rm."))
		problem.Fix = "Copy out any data you need, then stop " + pronoun + " yourself and run mtm prepare again."
		return keepPrevious(previous, recorded, problem, "")
	}

	inv := Inventory{Installed: true, Engine: engine, Stopped: stillStopped(previous.Stopped, s.containers)}
	var res module.Result
	stopped, failed := m.stop(env, s.containers)
	inv.Stopped = append(inv.Stopped, stopped...)
	if len(stopped) > 0 {
		res.Done = append(res.Done, "stopped "+words.Count(len(stopped), "container", "containers")+": "+names(stopped))
	}
	if len(failed) > 0 {
		// Exporting volumes a running container writes to would record a
		// state that never existed. Undigested, verify fails them until a
		// prepare succeeds.
		inv.Volumes, inv.Images = s.volumes, s.images
		res.Inventory, res.Problems = inv, failed
		return res, nil
	}
	volumes, vanished, err := m.volumesAfterStop(env, s.volumes)
	if err != nil {
		inv.Volumes, inv.Images = s.volumes, s.images
		res.Inventory = inv
		res.Problems = append(res.Problems, blocker("list", "Could not list Docker's volumes", err))
		return res, nil
	}
	for _, v := range vanished {
		res.Problems = append(res.Problems, module.Finding{
			Module: Name, ID: "volume:" + v.Name, Severity: module.SeverityBlocker, Handler: module.Manual,
			Title: "Volume " + v.Name + " disappeared while prepare stopped containers",
			Fix:   "Check whether you still need its data, then run mtm prepare --only docker again.",
		})
	}
	if err := m.importEmpty(env); err != nil {
		inv.Volumes, inv.Images = s.volumes, s.images
		res.Inventory = inv
		res.Problems = append(res.Problems, blocker("empty-image", "Could not prepare to export volumes", err))
		return res, nil
	}
	var written int64
	for _, v := range volumes {
		exported, err := m.exportVolume(env, v)
		if err != nil {
			inv.Volumes = append(inv.Volumes, v)
			res.Problems = append(res.Problems, exportBlocker("volume:"+v.Name, "Could not export volume "+v.Name, err))
			continue
		}
		inv.Volumes = append(inv.Volumes, exported)
		written += exported.Archive.Size
	}
	inv.Volumes = append(inv.Volumes, vanished...)
	if len(volumes) > 0 {
		res.Done = append(res.Done, fmt.Sprintf("exported %s (%s)", words.Count(len(volumes), "volume", "volumes"), words.Bytes(uint64(written))))
	}
	images, problems, done := m.saveImages(env, s.images, kept)
	inv.Images = images
	res.Problems = append(res.Problems, problems...)
	res.Done = append(res.Done, done...)
	if err := m.removeEmpty(env); err != nil {
		res.Problems = append(res.Problems, module.Finding{Module: Name, ID: "empty-image", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Could not remove mtm's empty image", Detail: err.Error(), Fix: "Run: docker image rm " + EmptyImage})
	}
	res.Problems = append(res.Problems, quitDocker)
	res.Inventory = inv
	return res, nil
}

// keptProblem is what a prepare without Docker Desktop reports about
// keeping the earlier record. A kept archive that is not in this run's
// exports folder is a blocker: a full prepare that changed or dropped
// --stage would otherwise keep archives that restore can't find.
func (m Module) keptProblem(env module.Env, previous Inventory) module.Finding {
	if keptArchivesIntact(env, previous) {
		return module.Finding{
			Module: Name, ID: "kept", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Docker Desktop isn't running, so mtm kept the Docker export from the earlier prepare",
			Fix:   "If you used Docker since then, start Docker Desktop and run mtm prepare --only docker.",
		}
	}
	return module.Finding{
		Module: Name, ID: "kept", Severity: module.SeverityBlocker, Handler: module.Manual,
		Title: "Docker Desktop isn't running, and the earlier Docker exports aren't in this prepare's exports folder",
		Fix:   "Start Docker Desktop and run mtm prepare --only docker.",
	}
}

func keptArchivesIntact(env module.Env, previous Inventory) bool {
	for _, v := range previous.Volumes {
		if v.Archive != nil && env.Exports.CheckArtifact(*v.Archive) != nil {
			return false
		}
	}
	for _, img := range previous.Images {
		if img.Archive != nil && env.Exports.CheckArtifact(*img.Archive) != nil {
			return false
		}
	}
	return true
}

func autoRemoving(cs []container) []string {
	var out []string
	for _, c := range cs {
		if c.Running && c.AutoRemove {
			out = append(out, c.Name)
		}
	}
	return out
}

// volumesAfterStop splits the snapshot's volumes into those still there and
// those that vanished while containers stopped. Exporting a vanished volume
// would have a holder re-create it empty, and record that as verified.
func (m Module) volumesAfterStop(env module.Env, snap []Volume) (present, vanished []Volume, err error) {
	names, err := m.volumeNames(env)
	if err != nil {
		return nil, nil, err
	}
	for _, v := range snap {
		if slices.Contains(names, v.Name) {
			present = append(present, v)
		} else {
			vanished = append(vanished, v)
		}
	}
	return present, vanished, nil
}

func blocker(id, title string, err error) module.Finding {
	return module.Finding{
		Module: Name, ID: id, Severity: module.SeverityBlocker, Handler: module.Manual,
		Title: title, Detail: err.Error(), Fix: "Fix the cause above and run mtm prepare --only docker again.",
	}
}

// exportBlocker is blocker with the --stage advice when the archive didn't
// fit, so the user isn't told to simply run prepare again.
func exportBlocker(id, title string, err error) module.Finding {
	f := blocker(id, title, err)
	if errors.Is(err, stage.ErrNoRoom) {
		f.Fix = spaceFix
	}
	return f
}

// keepPrevious ends prepare with problem but keeps the earlier record, so
// finish still starts the containers an earlier run stopped. Without an
// earlier record it fails instead, with failure when given: an empty
// record of a running Docker would let verify pass with nothing checked.
func keepPrevious(previous Inventory, recorded bool, problem module.Finding, failure string) (module.Result, error) {
	if !recorded {
		if failure == "" {
			failure = problem.Title + ": " + problem.Detail + ". " + problem.Fix
		}
		return module.Result{}, errors.New(failure)
	}
	return module.Result{Inventory: previous, Problems: []module.Finding{problem}}, nil
}

// stillStopped keeps containers an earlier prepare stopped that still
// exist and aren't running, so finish starts them even after a re-run.
func stillStopped(earlier []Container, now []container) []Container {
	var out []Container
	for _, e := range earlier {
		i := slices.IndexFunc(now, func(c container) bool { return c.ID == e.ID })
		if i >= 0 && !now[i].Running {
			out = append(out, e)
		}
	}
	return out
}

func (m Module) stop(env module.Env, cs []container) ([]Container, []module.Finding) {
	var stopped []Container
	var failed []module.Finding
	for _, c := range cs {
		if !c.Running {
			continue
		}
		if _, err := m.docker(env, "container", "stop", "--time", stopTimeout, c.ID); err != nil {
			failed = append(failed, module.Finding{
				Module: Name, ID: "stop:" + c.Name, Severity: module.SeverityBlocker, Handler: module.Manual,
				Title: "Could not stop container " + c.Name, Detail: err.Error(),
				Fix: "Run: docker container stop " + macos.ShellQuote(c.Name) + ", then run mtm prepare again. A running container's volumes could be copied mid-write.",
			})
			continue
		}
		stopped = append(stopped, c.Container)
	}
	return stopped, failed
}

func names(cs []Container) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return strings.Join(out, ", ")
}

// exportVolume writes the volume's archive, gzip-compressed, and its
// content digest, computed from the same stream.
func (m Module) exportVolume(env module.Env, v Volume) (Volume, error) {
	if err := stage.Room(env.Free, env.Exports.Root, uint64(v.Size)); err != nil {
		return v, err
	}
	f, err := env.Exports.CreateArtifact(path.Join("docker", "volumes", v.Name+".tar.gz"))
	if err != nil {
		return v, err
	}
	gz, err := gzip.NewWriterLevel(f, gzip.BestSpeed)
	if err != nil {
		f.Abort()
		return v, err
	}
	d := NewDigest()
	err = m.readVolume(env, v.Name, io.MultiWriter(gz, d))
	sum, n, derr := d.Finish()
	if err == nil {
		err = derr
	}
	if cerr := gz.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		f.Abort()
		return v, err
	}
	a, err := f.Commit()
	if err != nil {
		return v, err
	}
	v.Digest, v.Entries, v.Archive = sum, n, &a
	return v, nil
}

// keptArchives maps each image whose archive an earlier prepare saved to
// that archive, when the image and its tags are unchanged and the archive
// still matches its fingerprint. Checking reads the archive once.
func (m Module) keptArchives(env module.Env, previous Inventory, images []Image) map[string]*stage.Artifact {
	kept := map[string]*stage.Artifact{}
	for _, img := range images {
		for _, p := range previous.Images {
			if p.ID == img.ID && slices.Equal(p.Tags, img.Tags) && p.Archive != nil && env.Exports.CheckArtifact(*p.Archive) == nil {
				kept[img.ID] = p.Archive
			}
		}
	}
	return kept
}

func (m Module) saveImages(env module.Env, images []Image, kept map[string]*stage.Artifact) ([]Image, []module.Finding, []string) {
	var out []Image
	var problems []module.Finding
	var saved, reused, pulled int
	var written int64
	for _, img := range images {
		switch {
		case img.Pulled:
			pulled++
		case kept[img.ID] != nil:
			img.Archive = kept[img.ID]
			reused++
		default:
			a, err := m.saveImage(env, img)
			if err != nil {
				problems = append(problems, exportBlocker("image:"+img.Tags[0], "Could not save image "+img.Tags[0], err))
				break
			}
			img.Archive = &a
			saved++
			written += a.Size
		}
		out = append(out, img)
	}
	var done []string
	if saved > 0 {
		done = append(done, fmt.Sprintf("saved %s (%s)", words.Count(saved, "locally built image", "locally built images"), words.Bytes(uint64(written))))
	}
	if reused > 0 {
		done = append(done, "kept "+words.Count(reused, "locally built image", "locally built images")+" the earlier prepare saved")
	}
	if pulled > 0 {
		done = append(done, "recorded "+words.Count(pulled, "pulled image", "pulled images")+" to pull again if the new Mac lacks them")
	}
	return out, problems, done
}

func (m Module) saveImage(env module.Env, img Image) (stage.Artifact, error) {
	if err := stage.Room(env.Free, env.Exports.Root, uint64(img.Size)); err != nil {
		return stage.Artifact{}, err
	}
	f, err := env.Exports.CreateArtifact(imageArchive(img.ID))
	if err != nil {
		return stage.Artifact{}, err
	}
	cmd := m.command(env, append([]string{"image", "save"}, img.Tags...)...)
	cmd.Stdout = f
	if _, err := env.Run.Run(env.Ctx, cmd); err != nil {
		f.Abort()
		return stage.Artifact{}, fmt.Errorf("save image %s: %w", img.Tags[0], err)
	}
	return f.Commit()
}

func imageArchive(id string) string {
	return path.Join("docker", "images", strings.TrimPrefix(id, "sha256:")+".tar")
}

// Undo fails while Docker Desktop isn't running, which keeps the record so
// prepare --undo can start the containers once it is.
func (m Module) Undo(env module.Env, _ *stage.Stage) (module.Result, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return module.Result{}, err
	}
	if !inv.Installed || len(inv.Stopped) == 0 {
		return module.Result{}, nil
	}
	if m.engine(env) == "" {
		return module.Result{}, errors.New("Docker Desktop isn't running. Start it and run: mtm prepare --undo")
	}
	return m.start(env, inv.Stopped), nil
}

func (m Module) start(env module.Env, cs []Container) module.Result {
	var res module.Result
	var ok []Container
	for _, c := range cs {
		if _, err := m.docker(env, "container", "start", c.ID); err != nil {
			res.Problems = append(res.Problems, module.Finding{
				Module: Name, ID: "start:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
				Title: "Could not start container " + c.Name, Detail: err.Error(),
				Fix: "Run: docker container start " + macos.ShellQuote(c.Name),
			})
			continue
		}
		ok = append(ok, c)
	}
	if len(ok) > 0 {
		res.Done = append(res.Done, "started "+words.Count(len(ok), "container", "containers")+": "+names(ok))
	}
	return res
}
