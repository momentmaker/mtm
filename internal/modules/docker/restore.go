package docker

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

// settledFile lists, on the new Mac, each volume finish found intact or
// restored. verify trusts that comparison: containers change their volumes
// as soon as they start, so a later one would fail.
const settledFile = "docker-finish.json"

const (
	intact   = "intact"
	restored = "restored"
)

type settlement struct {
	How    string `json:"how"`
	Digest string `json:"digest"`
}

// settledAs answers only for the digest finish settled against: a record
// that changed since then has to be compared again.
func settledAs(settled map[string]settlement, v Volume) string {
	s := settled[v.Name]
	if s.Digest != v.Digest {
		return ""
	}
	return s.How
}

var errHoldsData = errors.New("the volume holds data")

const restoreFix = "If prepare used --stage, attach that drive; otherwise check that ~/.mtm/exports arrived. Then run mtm finish --only docker again."

func loadSettled(st *stage.Stage) (map[string]settlement, error) {
	path := st.Path(settledFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]settlement{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	settled := map[string]settlement{}
	if err := json.Unmarshal(data, &settled); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return settled, nil
}

func saveSettled(st *stage.Stage, settled map[string]settlement) error {
	data, err := json.MarshalIndent(settled, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", settledFile, err)
	}
	return st.WriteFile(settledFile, data)
}

func merge(a, b module.Result) module.Result {
	a.Done = append(a.Done, b.Done...)
	a.Problems = append(a.Problems, b.Problems...)
	return a
}

// Finish settles every volume before it starts anything: a container that
// starts first could change a volume before it is compared.
func (m Module) Finish(env module.Env, st *stage.Stage) (module.Result, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return module.Result{}, err
	}
	if !inv.Installed || len(inv.Volumes)+len(inv.Images)+len(inv.Stopped) == 0 {
		return module.Result{}, nil
	}
	if !m.installed() || m.engine(env) == "" {
		return module.Result{Problems: []module.Finding{{
			Module: Name, ID: "not-running", Severity: module.SeverityBlocker, Handler: module.Manual,
			Title:  "Docker Desktop isn't running on this Mac",
			Detail: "Don't start any containers before mtm finish: it checks their volumes before anything changes them.",
			Fix:    "Start Docker Desktop, then run mtm finish --only docker.",
		}}}, nil
	}
	if err := m.removeLeftovers(env); err != nil {
		return module.Result{}, err
	}
	res, err := m.settleVolumes(env, st, inv.Volumes)
	if err != nil {
		return res, err
	}
	loaded, err := m.loadImages(env, inv.Images)
	if err != nil {
		return res, err
	}
	res = merge(res, loaded)
	if module.HasBlocker(res.Problems) {
		return res, nil
	}
	started, err := m.startContainers(env, inv.Stopped)
	if err != nil {
		return res, err
	}
	return merge(res, started), nil
}

// settleVolumes makes each recorded volume match the old Mac's, except one
// that holds other data, and records each volume it settles.
func (m Module) settleVolumes(env module.Env, st *stage.Stage, vols []Volume) (module.Result, error) {
	var res module.Result
	settled, err := loadSettled(st)
	if err != nil {
		return res, err
	}
	pending := slices.DeleteFunc(slices.Clone(vols), func(v Volume) bool { return settledAs(settled, v) != "" })
	if len(pending) == 0 {
		return res, nil
	}
	present, err := m.volumeNames(env)
	if err != nil {
		return res, err
	}
	if err := m.importEmpty(env); err != nil {
		return res, err
	}
	counts := map[string]int{}
	for _, v := range pending {
		how, problem := m.settle(env, v, slices.Contains(present, v.Name))
		if problem != nil {
			res.Problems = append(res.Problems, *problem)
			continue
		}
		settled[v.Name] = settlement{How: how, Digest: v.Digest}
		counts[how]++
	}
	if err := m.removeEmpty(env); err != nil {
		res.Problems = append(res.Problems, module.Finding{Module: Name, ID: "empty-image", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Could not remove mtm's empty image", Detail: err.Error(), Fix: "Run: docker image rm " + EmptyImage})
	}
	if err := saveSettled(st, settled); err != nil {
		return res, err
	}
	if n := counts[intact]; n > 0 {
		res.Done = append(res.Done, "found "+words.Count(n, "volume", "volumes")+" intact")
	}
	if n := counts[restored]; n > 0 {
		res.Done = append(res.Done, "restored "+words.Count(n, "volume", "volumes")+" from the old Mac's exports")
	}
	return res, nil
}

func (m Module) settle(env module.Env, v Volume, exists bool) (string, *module.Finding) {
	if v.Digest == "" {
		return "", &module.Finding{
			Module: Name, ID: "volume:" + v.Name, Severity: module.SeverityBlocker, Handler: module.Manual,
			Title: "Volume " + v.Name + " wasn't exported by prepare",
			Fix:   "Check its data on the old Mac before you erase it. If you can, run mtm prepare --only docker there and migrate again.",
		}
	}
	if exists {
		sum, entries, err := m.volumeDigest(env, v.Name)
		switch {
		case err != nil:
			return "", &module.Finding{Module: Name, ID: "volume:" + v.Name, Severity: module.SeverityBlocker, Handler: module.Manual,
				Title: "Could not read volume " + v.Name, Detail: err.Error(), Fix: "Fix the cause above, then run mtm finish --only docker again."}
		case sum == v.Digest:
			return intact, nil
		case entries > 0:
			return "", m.differs(env, v)
		}
	}
	if err := m.restore(env, v, !exists); err != nil {
		if errors.Is(err, errHoldsData) {
			return "", m.differs(env, v)
		}
		return "", &module.Finding{Module: Name, ID: "volume:" + v.Name, Severity: module.SeverityBlocker, Handler: module.Manual,
			Title: "Could not restore volume " + v.Name, Detail: err.Error(), Fix: restoreFix}
	}
	return restored, nil
}

// differs explains a volume that holds other data than the old Mac's. mtm
// never overwrites it (spec 6.2, rule 4): the user removes it, and finish
// then restores it like any missing volume.
func (m Module) differs(env module.Env, v Volume) *module.Finding {
	var steps []string
	detail := "mtm never overwrites a volume that holds data. These commands delete it and the containers using it; finish then restores it from the old Mac's export and re-creates the Compose containers."
	if cs, err := m.containers(env); err != nil {
		detail += " mtm couldn't list the containers using the volume (" + err.Error() + "); remove them first, or docker volume rm will fail."
	} else {
		var users []string
		for _, c := range cs {
			if slices.Contains(c.Volumes, v.Name) {
				users = append(users, macos.ShellQuote(c.Name))
			}
		}
		if len(users) > 0 {
			steps = append(steps, "docker container rm --force "+strings.Join(users, " "))
		}
	}
	steps = append(steps, "docker volume rm "+macos.ShellQuote(v.Name), "mtm finish --only docker")
	return &module.Finding{
		Module: Name, ID: "volume:" + v.Name, Severity: module.SeverityBlocker, Handler: module.Manual,
		Title:  "Volume " + v.Name + " holds different data from the old Mac's",
		Detail: detail,
		Fix:    "To replace it with the old Mac's data, run: " + strings.Join(steps, " && "),
	}
}

// restore fills a missing or empty volume from its archive, once the
// archive matches its fingerprint, and checks the result against the
// record.
func (m Module) restore(env module.Env, v Volume, create bool) error {
	if v.Archive == nil {
		return errors.New("prepare saved no archive of it")
	}
	if err := env.Exports.CheckArtifact(*v.Archive); err != nil {
		return err
	}
	if create {
		args := []string{"volume", "create"}
		for _, k := range slices.Sorted(maps.Keys(v.Labels)) {
			args = append(args, "--label", k+"="+v.Labels[k])
		}
		if _, err := m.docker(env, append(args, v.Name)...); err != nil {
			return fmt.Errorf("create volume %s: %w", v.Name, err)
		}
	}
	if _, entries, err := m.volumeDigest(env, v.Name); err != nil {
		return err
	} else if entries > 0 {
		return errHoldsData
	}
	f, err := os.Open(env.Exports.Path(v.Archive.Name))
	if err != nil {
		return fmt.Errorf("open export: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read the archive of %s: %w", v.Name, err)
	}
	if err := m.writeVolume(env, v.Name, gz); err != nil {
		return err
	}
	sum, _, err := m.volumeDigest(env, v.Name)
	if err != nil {
		return err
	}
	if sum != v.Digest {
		return fmt.Errorf("after restoring, volume %s still doesn't match the old Mac's", v.Name)
	}
	return nil
}

// present lists the image ids and tags on this Mac.
func (m Module) present(env module.Env) (map[string]bool, map[string]bool, error) {
	out, err := m.docker(env, "image", "ls", "--no-trunc", "--format", "json")
	if err != nil {
		return nil, nil, fmt.Errorf("list images: %w", err)
	}
	ids, tags := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(string(out.Stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r struct {
			ID         string `json:"ID"`
			Repository string `json:"Repository"`
			Tag        string `json:"Tag"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, nil, fmt.Errorf("parse docker image ls: %w", err)
		}
		ids[r.ID] = true
		if r.Repository != "<none>" && r.Tag != "<none>" {
			tags[r.Repository+":"+r.Tag] = true
		}
	}
	return ids, tags, nil
}

func anyTag(img Image, tags map[string]bool) bool {
	return slices.ContainsFunc(img.Tags, func(t string) bool { return tags[t] })
}

// pulls lists the tags of pulled images this Mac has neither by id nor by
// tag. A pull goes by tag: reports mask every sha256: value, so a pull by
// digest couldn't be printed.
func pulls(images []Image, ids, tags map[string]bool) []string {
	var missing []string
	for _, img := range images {
		if img.Pulled && !ids[img.ID] && !anyTag(img, tags) {
			missing = append(missing, img.Tags...)
		}
	}
	return missing
}

func pullCommand(tags []string) string {
	cmds := make([]string, len(tags))
	for i, t := range tags {
		cmds[i] = "docker pull " + macos.ShellQuote(t)
	}
	return strings.Join(cmds, " && ")
}

// loadImages loads each locally built image this Mac lacks from its
// archive and lists the pulled images to pull again.
func (m Module) loadImages(env module.Env, images []Image) (module.Result, error) {
	var res module.Result
	if len(images) == 0 {
		return res, nil
	}
	ids, tags, err := m.present(env)
	if err != nil {
		return res, err
	}
	loaded := 0
	for _, img := range images {
		if img.Pulled || ids[img.ID] {
			continue
		}
		if err := m.loadImage(env, img); err != nil {
			res.Problems = append(res.Problems, module.Finding{Module: Name, ID: "image:" + img.Tags[0], Severity: module.SeverityBlocker, Handler: module.Manual,
				Title: "Could not load image " + img.Tags[0], Detail: err.Error(), Fix: restoreFix})
			continue
		}
		loaded++
	}
	if loaded > 0 {
		res.Done = append(res.Done, "loaded "+words.Count(loaded, "locally built image", "locally built images"))
	}
	if missing := pulls(images, ids, tags); len(missing) > 0 {
		res.Problems = append(res.Problems, module.Finding{
			Module: Name, ID: "pull", Severity: module.SeverityWarn, Handler: module.Manual,
			Title:  "Pull " + words.Count(len(missing), "image", "images") + " from their registries",
			Detail: "They were pulled on the old Mac and didn't arrive. Pulling uses the network, so mtm leaves it to you.",
			Fix:    "Run: " + pullCommand(missing),
		})
	}
	return res, nil
}

func (m Module) loadImage(env module.Env, img Image) error {
	if img.Archive == nil {
		return errors.New("prepare saved no archive of it")
	}
	if err := env.Exports.CheckArtifact(*img.Archive); err != nil {
		return err
	}
	if _, err := m.docker(env, "image", "load", "--quiet", "--input", env.Exports.Path(img.Archive.Name)); err != nil {
		return fmt.Errorf("load image %s: %w", img.Tags[0], err)
	}
	return nil
}

// startContainers starts the containers prepare stopped. Those that no
// longer exist come back through their Compose project, offline: no pulls,
// no builds.
func (m Module) startContainers(env module.Env, stopped []Container) (module.Result, error) {
	var res module.Result
	if len(stopped) == 0 {
		return res, nil
	}
	now, err := m.containers(env)
	if err != nil {
		return res, err
	}
	var existing []Container
	var projects []Container
	for _, c := range stopped {
		switch {
		case slices.ContainsFunc(now, func(n container) bool { return n.ID == c.ID }):
			existing = append(existing, c)
		case c.Project != "" && c.WorkingDir != "":
			if !slices.ContainsFunc(projects, func(p Container) bool { return p.Project == c.Project }) {
				projects = append(projects, c)
			}
		default:
			res.Problems = append(res.Problems, module.Finding{
				Module: Name, ID: "gone:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
				Title:  "Container " + c.Name + " no longer exists",
				Detail: "It wasn't part of a Compose project, so mtm can't re-create it.",
				Fix:    "Re-create it the way you did on the old Mac.",
			})
		}
	}
	res = merge(res, m.start(env, existing))
	for _, p := range projects {
		res = merge(res, m.composeUp(env, p))
	}
	return res, nil
}

func (m Module) composeUp(env module.Env, c Container) module.Result {
	dir := expandHome(env.Home, c.WorkingDir)
	args := []string{"compose", "--project-name", c.Project, "--project-directory", dir}
	for _, f := range c.ConfigFiles {
		args = append(args, "--file", expandHome(env.Home, f))
	}
	cmd := m.command(env, append(args, "up", "--detach", "--pull", "never", "--no-build")...)
	cmd.Dir = dir
	if _, err := env.Run.Run(env.Ctx, cmd); err != nil {
		return module.Result{Problems: []module.Finding{{
			Module: Name, ID: "compose:" + c.Project, Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Could not start Compose project " + c.Project, Detail: err.Error(),
			Fix: "Pull any missing images, then run: cd " + macos.ShellQuote(dir) + " && docker compose -p " + macos.ShellQuote(c.Project) + " up -d",
		}}}
	}
	return module.Result{Done: []string{"started Compose project " + c.Project}}
}

func (m Module) Verify(env module.Env, st *stage.Stage) ([]module.Check, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	if !inv.Installed {
		return nil, nil
	}
	if !m.installed() || m.engine(env) == "" {
		return []module.Check{{Module: Name, ID: Name + ":running", Status: module.StatusFail, Critical: true,
			Title: "Docker Desktop", Detail: "not running, so its volumes can't be checked; start it and run mtm verify again"}}, nil
	}
	checks, err := m.volumeChecks(env, st, inv.Volumes)
	if err != nil {
		return nil, err
	}
	if len(inv.Images) == 0 {
		return checks, nil
	}
	ids, tags, err := m.present(env)
	if err != nil {
		return nil, err
	}
	pulled := 0
	for _, img := range inv.Images {
		if img.Pulled {
			pulled++
			continue
		}
		checks = append(checks, imageCheck(img, ids, tags))
	}
	if pulled > 0 {
		checks = append(checks, pulledCheck(inv.Images, ids, tags, pulled))
	}
	return checks, nil
}

// volumeChecks trusts the volumes finish settled and compares the rest
// now, through holders that need mtm's empty image for the time it takes.
func (m Module) volumeChecks(env module.Env, st *stage.Stage, vols []Volume) ([]module.Check, error) {
	if len(vols) == 0 {
		return nil, nil
	}
	settled, err := loadSettled(st)
	if err != nil {
		return nil, err
	}
	present, err := m.volumeNames(env)
	if err != nil {
		return nil, err
	}
	var checks []module.Check
	imported := false
	for _, v := range vols {
		c := module.Check{Module: Name, ID: Name + ":volume:" + v.Name, Status: module.StatusPass, Critical: true, Title: "volume " + v.Name}
		switch {
		case v.Digest == "":
			c.Status, c.Detail = module.StatusFail, "prepare couldn't export it; check its data on the old Mac before you erase it"
		case !slices.Contains(present, v.Name):
			c.Status, c.Detail = module.StatusFail, "missing; run mtm finish --only docker to restore it"
		case settledAs(settled, v) == intact:
			c.Detail = "matched the old Mac's when mtm finish checked it"
		case settledAs(settled, v) == restored:
			c.Detail = "restored by mtm finish from the old Mac's export"
		default:
			if !imported {
				if err := m.importEmpty(env); err != nil {
					return nil, err
				}
				imported = true
			}
			sum, _, err := m.volumeDigest(env, v.Name)
			switch {
			case err != nil:
				c.Status, c.Detail = module.StatusFail, "could not read it: "+err.Error()
			case sum != v.Digest:
				c.Status, c.Detail = module.StatusFail, "differs from the old Mac's; run mtm finish --only docker"
			default:
				c.Detail = "matches the old Mac's"
			}
		}
		checks = append(checks, c)
	}
	if imported {
		if err := m.removeEmpty(env); err != nil {
			checks = append(checks, module.Check{Module: Name, ID: Name + ":empty-image", Status: module.StatusWarn, Title: "mtm's empty image",
				Detail: "could not remove it: " + err.Error() + "; run: docker image rm " + macos.ShellQuote(m.Empty)})
		}
	}
	return checks, nil
}

func imageCheck(img Image, ids, tags map[string]bool) module.Check {
	c := module.Check{Module: Name, ID: Name + ":image:" + img.Tags[0], Status: module.StatusPass, Title: "image " + img.Tags[0], Detail: "present"}
	switch {
	case ids[img.ID]:
	case anyTag(img, tags):
		c.Status, c.Detail = module.StatusInfo, "the tag now names a different image, such as one rebuilt on this Mac"
	default:
		c.Status, c.Detail = module.StatusFail, "missing; run mtm finish --only docker to load it"
	}
	return c
}

func pulledCheck(images []Image, ids, tags map[string]bool, pulled int) module.Check {
	c := module.Check{Module: Name, ID: Name + ":pulled", Status: module.StatusPass, Title: "pulled images",
		Detail: words.Count(pulled, "pulled image is", "pulled images are") + " here"}
	if missing := pulls(images, ids, tags); len(missing) > 0 {
		c.Status, c.Paths = module.StatusWarn, missing
		c.Detail = "not pulled again yet; run: " + pullCommand(missing)
	}
	return c
}
