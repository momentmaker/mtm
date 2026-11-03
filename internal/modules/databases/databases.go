// Package databases saves a pg_dumpall dump of each running Homebrew
// Postgres cluster as a safety net, and checks every database arrived.
// The homebrew module stops and starts the servers; this module comes
// before it in the module order, so it dumps while they still run.
package databases

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/words"
)

const Name = "databases"

// Module finds clusters in Homebrew's var folder and their programs in its
// opt folder.
type Module struct {
	Var string
	Opt string
}

func New() Module { return Module{Var: "/opt/homebrew/var", Opt: "/opt/homebrew/opt"} }

func (Module) Name() string { return Name }

// Cluster is a Postgres data folder. A running one records where its server
// listened, its databases, and its dump.
type Cluster struct {
	Name      string          `json:"name"`
	Major     string          `json:"major"`
	Running   bool            `json:"running"`
	Socket    string          `json:"socket,omitempty"`
	Port      string          `json:"port,omitempty"`
	Databases []string        `json:"databases,omitempty"`
	Dump      *stage.Artifact `json:"dump,omitempty"`
}

type Inventory struct {
	Clusters []Cluster `json:"clusters,omitempty"`
}

// pgUnset keeps the user's connection settings from steering a Postgres
// command anywhere but the cluster's own Unix socket.
var pgUnset = []string{"PGHOST", "PGHOSTADDR", "PGPORT", "PGDATABASE", "PGUSER", "PGSERVICE", "PGSERVICEFILE", "PGPASSWORD"}

// pgEnv stops libpq reading saved passwords: Homebrew's clusters trust
// local connections, and mtm reads no secrets.
var pgEnv = []string{"PGPASSFILE=/dev/null", "PGCONNECT_TIMEOUT=10"}

const listQuery = "SELECT datname FROM pg_database WHERE NOT datistemplate ORDER BY 1"
const relationQuery = "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND n.nspname NOT LIKE 'pg_temp%'"
const sizeQuery = "SELECT sum(pg_database_size(datname)) FROM pg_database"

func (m Module) pg(c Cluster, tool string, args ...string) runner.Command {
	return runner.Command{
		Name:  filepath.Join(m.Opt, "postgresql@"+c.Major, "bin", tool),
		Args:  append([]string{"--host", c.Socket, "--port", c.Port}, args...),
		Unset: pgUnset,
		Env:   pgEnv,
	}
}

// psql runs one query. -X skips the user's .psqlrc, which could change
// the output format.
func (m Module) psql(env module.Env, c Cluster, query string) (string, error) {
	out, err := env.Run.Run(env.Ctx, m.pg(c, "psql", "-w", "-X", "--dbname", "postgres", "--tuples-only", "--no-align", "--command", query))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out.Stdout)), nil
}

// roleExists matches the one error every pg_dumpall replay produces: the
// dump creates the role that already owns the cluster.
var roleExists = regexp.MustCompile(`role ".*" already exists`)

// service is the Homebrew service that runs a cluster.
func service(c Cluster) string {
	if strings.HasPrefix(c.Name, "postgresql@") {
		return c.Name
	}
	return "postgresql@" + c.Major
}

func (m Module) dataDir(c Cluster) string { return filepath.Join(m.Var, c.Name) }

// clusters lists the folders in Homebrew's var folder that hold a
// PG_VERSION file.
func (m Module) clusters() ([]Cluster, error) {
	entries, err := os.ReadDir(m.Var)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", m.Var, err)
	}
	var out []Cluster
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "postgres") {
			continue
		}
		version, err := os.ReadFile(filepath.Join(m.Var, e.Name(), "PG_VERSION"))
		if err != nil {
			continue
		}
		out = append(out, Cluster{Name: e.Name(), Major: strings.TrimSpace(string(version))})
	}
	return out, nil
}

// listening reads the port and socket folder a server wrote to
// postmaster.pid, lines 4 and 5. A stopped server leaves no postmaster.pid.
func (m Module) listening(c Cluster) (port, socket string, up bool, err error) {
	data, err := os.ReadFile(filepath.Join(m.dataDir(c), "postmaster.pid"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read %s's postmaster.pid: %w", c.Name, err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 5 {
		return "", "", false, fmt.Errorf("read %s's postmaster.pid: only %d lines", c.Name, len(lines))
	}
	return strings.TrimSpace(lines[3]), strings.TrimSpace(lines[4]), true, nil
}

func (m Module) ready(env module.Env, c Cluster) bool {
	_, err := env.Run.Run(env.Ctx, m.pg(c, "pg_isready", "--quiet"))
	return err == nil
}

// live returns c with its server's socket and port, and Running when the
// server answers there. A problem is a server mtm can't reach.
func (m Module) live(env module.Env, c Cluster) (Cluster, *module.Finding) {
	port, socket, up, err := m.listening(c)
	switch {
	case err != nil:
		return c, &module.Finding{Module: Name, ID: "pid:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Could not tell whether " + c.Name + " is running", Detail: err.Error(),
			Fix: "mtm saved no dump of it. Migration Assistant still copies its data folder."}
	case !up:
		return c, nil
	case socket == "" || !filepath.IsAbs(socket):
		return c, &module.Finding{Module: Name, ID: "socket:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
			Title:  c.Name + " listens on no Unix socket",
			Detail: "mtm connects only through an absolute Unix socket folder, never the network, so it can't dump this cluster.",
			Fix:    "Set unix_socket_directories in its postgresql.conf, restart it, and run mtm prepare again."}
	}
	c.Port, c.Socket = port, socket
	if !m.ready(env, c) {
		return Cluster{Name: c.Name, Major: c.Major}, &module.Finding{Module: Name, ID: "answer:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
			Title: c.Name + " left a postmaster.pid but doesn't answer",
			Fix:   "If it should be running, check it with brew services list. mtm saved no dump of it."}
	}
	c.Running = true
	return c, nil
}

func (m Module) databases(env module.Env, c Cluster) ([]string, error) {
	out, err := m.psql(env, c, listQuery)
	if err != nil {
		return nil, fmt.Errorf("list %s's databases: %w", c.Name, err)
	}
	return strings.Fields(out), nil
}

// size is the cluster's size on disk, an upper bound for its compressed
// dump. A failure reads as 0: the check is only an estimate.
func (m Module) size(env module.Env, c Cluster) uint64 {
	out, err := m.psql(env, c, sizeQuery)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseUint(out, 10, 64)
	return n
}

func (m Module) dump(env module.Env, c Cluster) (stage.Artifact, error) {
	f, err := env.Exports.CreateArtifact(path.Join("postgres", c.Name+".sql.gz"))
	if err != nil {
		return stage.Artifact{}, err
	}
	gz := gzip.NewWriter(f)
	cmd := m.pg(c, "pg_dumpall", "-w")
	cmd.Stdout = gz
	_, err = env.Run.Run(env.Ctx, cmd)
	if cerr := gz.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		f.Abort()
		return stage.Artifact{}, fmt.Errorf("dump %s: %w", c.Name, err)
	}
	return f.Commit()
}

func (m Module) Scan(env module.Env) ([]module.Finding, error) {
	clusters, err := m.clusters()
	if err != nil {
		return nil, err
	}
	var findings []module.Finding
	for _, c := range clusters {
		live, problem := m.live(env, c)
		switch {
		case problem != nil:
			findings = append(findings, *problem)
		case live.Running:
			findings = append(findings, module.Finding{
				Module: Name, ID: "running:" + c.Name, Severity: module.SeverityInfo, Handler: module.ByMtm,
				Title:  "Postgres " + c.Major + " is running (" + c.Name + ")",
				Detail: "prepare saves a pg_dumpall dump as a safety net, and the homebrew module stops the server so its data folder copies whole.",
			})
		default:
			findings = append(findings, module.Finding{
				Module: Name, ID: "stopped:" + c.Name, Severity: module.SeverityInfo, Handler: module.ByMigrationAssistant,
				Title:  "Postgres " + c.Major + " data in " + m.dataDir(c),
				Detail: "It isn't running, so Migration Assistant copies its data folder as it is.",
			})
		}
	}
	return findings, nil
}

func (m Module) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	var previous Inventory
	if _, err := module.LoadInventory(env, Name, &previous); err != nil && !errors.Is(err, module.ErrNotPrepared) {
		return module.Result{}, err
	}
	clusters, err := m.clusters()
	if err != nil {
		return module.Result{}, err
	}
	var res module.Result
	var inv Inventory
	for _, c := range clusters {
		c, done, problem := m.prepareCluster(env, c, previous)
		inv.Clusters = append(inv.Clusters, c)
		if done != "" {
			res.Done = append(res.Done, done)
		}
		if problem != nil {
			res.Problems = append(res.Problems, *problem)
		}
	}
	res.Inventory = inv
	return res, nil
}

func (m Module) prepareCluster(env module.Env, c Cluster, previous Inventory) (Cluster, string, *module.Finding) {
	live, problem := m.live(env, c)
	earlier, wasRunning := earlierRunning(previous, c.Name)
	if problem != nil {
		// A stale postmaster.pid says nothing about what the cluster held, so
		// an earlier record of it still stands.
		if wasRunning {
			return keepIfDumpIntact(env, earlier), "", problem
		}
		return live, "", problem
	}
	if !live.Running {
		// A cluster that stopped since the earlier prepare dumped it, as the
		// homebrew module leaves it, hasn't changed since that dump.
		if wasRunning {
			p := keepIfDumpIntact(env, earlier)
			if p.Dump != nil {
				return p, "kept the dump of " + c.Name + " from the earlier prepare", nil
			}
			return p, "", &module.Finding{Module: Name, ID: "dump:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
				Title: c.Name + " has stopped, so mtm can't save a dump of it",
				Fix:   "To save one, run: " + startThenPrepare(c) + ". Migration Assistant still copies its data folder."}
		}
		return live, "", nil
	}
	c = live
	dbs, err := m.databases(env, c)
	if err != nil {
		return c, "", noDump(c, err)
	}
	c.Databases = dbs
	if err := stage.Room(env.Free, env.Exports.Root, m.size(env, c)); err != nil {
		return c, "", &module.Finding{Module: Name, ID: "space:" + c.Name, Severity: module.SeverityBlocker, Handler: module.Manual,
			Title: "The dump of " + c.Name + " doesn't fit", Detail: err.Error(),
			Fix: "Free space, or run mtm prepare --stage /Volumes/<drive> with an encrypted APFS drive. The server may already be stopped by then: start it with " + startThenPrepare(c) + "."}
	}
	a, err := m.dump(env, c)
	if err != nil {
		return c, "", noDump(c, err)
	}
	c.Dump = &a
	return c, fmt.Sprintf("saved a dump of %s (%s, %s)", c.Name, words.Count(len(dbs), "database", "databases"), words.Bytes(uint64(a.Size))), nil
}

func earlierRunning(previous Inventory, name string) (Cluster, bool) {
	i := slices.IndexFunc(previous.Clusters, func(p Cluster) bool { return p.Name == name })
	if i < 0 || !previous.Clusters[i].Running {
		return Cluster{}, false
	}
	return previous.Clusters[i], true
}

// keepIfDumpIntact returns the earlier record, without its dump unless the
// dump still matches its fingerprint.
func keepIfDumpIntact(env module.Env, p Cluster) Cluster {
	if p.Dump != nil && env.Exports.CheckArtifact(*p.Dump) != nil {
		p.Dump = nil
	}
	return p
}

// startThenPrepare is the fix for a dump that needs the server running:
// the homebrew module may already have stopped it by the next run.
func startThenPrepare(c Cluster) string {
	return "brew services start " + macos.ShellQuote(service(c)) + " && mtm prepare --only databases"
}

func noDump(c Cluster, err error) *module.Finding {
	return &module.Finding{
		Module: Name, ID: "dump:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
		Title: "Could not save a safety-net dump of " + c.Name, Detail: err.Error(),
		Fix: "Migration Assistant still copies its data folder. For a dump too, check that pg_dumpall works. The server may already be stopped by then: start it with " + startThenPrepare(c) + ".",
	}
}

// Finish does nothing unless asked to restore the dumps: the homebrew
// module starts the servers, and Migration Assistant copied their data.
func (m Module) Finish(env module.Env, _ *stage.Stage) (module.Result, error) {
	if !env.RestoreDump {
		return module.Result{}, nil
	}
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return module.Result{}, err
	}
	var res module.Result
	for _, c := range inv.Clusters {
		if c.Dump == nil {
			continue
		}
		done, warning, blocker := m.restore(env, c)
		if blocker != nil {
			res.Problems = append(res.Problems, *blocker)
			continue
		}
		res.Done = append(res.Done, done)
		if warning != nil {
			res.Problems = append(res.Problems, *warning)
		}
	}
	if len(res.Done) == 0 && len(res.Problems) == 0 {
		res.Problems = append(res.Problems, module.Finding{Module: Name, ID: "no-dump", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "prepare saved no Postgres dump to restore"})
	}
	return res, nil
}

// notRunningFix is the recovery for a cluster whose server won't answer.
// Homebrew's post_install skips initdb when PG_VERSION exists, so a data
// folder that is there but unusable has to be moved aside first. mtm only
// prints the commands.
func (m Module) notRunningFix(c Cluster) string {
	svc, dir := macos.ShellQuote(service(c)), macos.ShellQuote(m.dataDir(c))
	return "If its data folder didn't arrive or won't start, move it aside: mv " + dir + " " + dir + ".old, then create an empty one: brew postinstall " + svc +
		". Then run: brew services start " + svc + " && mtm finish --only databases --restore-dump"
}

func restoreProblem(c Cluster, title, detail, fix string) *module.Finding {
	return &module.Finding{Module: Name, ID: "restore:" + c.Name, Severity: module.SeverityBlocker, Handler: module.Manual,
		Title: title, Detail: detail, Fix: fix}
}

// restore streams a cluster's dump into its running server, reached through
// the socket the new Mac's postmaster.pid names. A cluster that already holds
// databases or tables could end up mixing two Macs' data, so it needs
// --force. It returns a blocker, or the done line and perhaps a warning.
func (m Module) restore(env module.Env, c Cluster) (string, *module.Finding, *module.Finding) {
	live, problem := m.live(env, Cluster{Name: c.Name, Major: c.Major})
	if problem != nil || !live.Running {
		return "", nil, restoreProblem(c, c.Name+" isn't running", "", m.notRunningFix(c))
	}
	again := "Fix the cause above, then run mtm finish --only databases --restore-dump again."
	dbs, err := m.databases(env, live)
	if err != nil {
		return "", nil, restoreProblem(c, "Could not list "+c.Name+"'s databases", err.Error(), again)
	}
	forceFix := "Check them first. To restore anyway, run: mtm finish --only databases --restore-dump --force"
	if extra := slices.DeleteFunc(dbs, func(d string) bool { return d == "postgres" }); len(extra) > 0 && !env.Force {
		return "", nil, restoreProblem(c, c.Name+" already has databases: "+strings.Join(extra, ", "),
			"Restoring the dump on top could mix the old Mac's data with this one's.", forceFix)
	}
	if !env.Force {
		out, err := m.psql(env, live, relationQuery)
		if err != nil {
			return "", nil, restoreProblem(c, "Could not look for tables in "+c.Name+"'s postgres database", err.Error(), again)
		}
		if n, _ := strconv.Atoi(out); n > 0 {
			return "", nil, restoreProblem(c, "The postgres database of "+c.Name+" already holds tables",
				"Restoring the dump on top could mix the old Mac's data with this one's.", forceFix)
		}
	}
	if err := env.Exports.CheckArtifact(*c.Dump); err != nil {
		return "", nil, restoreProblem(c, "Could not use the dump of "+c.Name, err.Error(),
			"If prepare used --stage, attach that drive; otherwise check that ~/.mtm/exports arrived. Then run mtm finish --only databases --restore-dump again.")
	}
	stderr, err := m.load(env, live, *c.Dump)
	if err != nil {
		return "", nil, restoreProblem(c, "Could not restore "+c.Name, loadFailure(err, stderr), again)
	}
	done := "restored " + c.Name + " from its dump"
	if n := countErrors(stderr); n > 0 {
		return done, &module.Finding{Module: Name, ID: "restore-errors:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "psql reported " + words.Count(n, "error", "errors") + " while restoring " + c.Name,
			Fix:   "Check that its databases hold what you expect; a missing extension is a common cause."}, nil
	}
	return done, nil, nil
}

// loadFailure describes a failed psql load without its output: stderr
// quotes the rows and statements it choked on, which are the user's data.
func loadFailure(err error, stderr []byte) string {
	code, ok := runner.ExitCode(err)
	if !ok {
		return err.Error()
	}
	detail := fmt.Sprintf("psql exited with status %d", code)
	if n := countErrors(stderr); n > 0 {
		detail += ", after " + words.Count(n, "error", "errors")
	}
	return detail
}

// countErrors counts psql's ERROR lines, leaving out the existing role.
// psql exits 0 after statement errors, so stderr is the only sign of them.
func countErrors(stderr []byte) int {
	n := 0
	for _, line := range strings.Split(string(stderr), "\n") {
		if strings.Contains(line, "ERROR:") && !roleExists.MatchString(line) {
			n++
		}
	}
	return n
}

func (m Module) load(env module.Env, c Cluster, dump stage.Artifact) ([]byte, error) {
	f, err := os.Open(env.Exports.Path(dump.Name))
	if err != nil {
		return nil, fmt.Errorf("open export: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("read the dump of %s: %w", c.Name, err)
	}
	cmd := m.pg(c, "psql", "-w", "-X", "--quiet", "--dbname", "postgres", "--file=-")
	cmd.Input = gz
	out, err := env.Run.Run(env.Ctx, cmd)
	return out.Stderr, err
}

func (m Module) Verify(env module.Env, _ *stage.Stage) ([]module.Check, error) {
	var inv Inventory
	if _, err := module.LoadInventory(env, Name, &inv); err != nil {
		return nil, err
	}
	var checks []module.Check
	for _, c := range inv.Clusters {
		checks = append(checks, m.check(env, c))
	}
	return checks, nil
}

func (m Module) check(env module.Env, c Cluster) module.Check {
	ch := module.Check{Module: Name, ID: Name + ":" + c.Name, Status: module.StatusPass, Critical: true, Title: c.Name}
	if !c.Running {
		if _, err := os.Stat(filepath.Join(m.dataDir(c), "PG_VERSION")); err != nil {
			ch.Status, ch.Detail = module.StatusFail, "its data folder "+m.dataDir(c)+" is missing"
		} else {
			ch.Detail = "its data folder arrived"
		}
		return ch
	}
	live, problem := m.live(env, Cluster{Name: c.Name, Major: c.Major})
	if problem != nil || !live.Running {
		svc := macos.ShellQuote(service(c))
		ch.Status, ch.Detail = module.StatusFail, "not running; run: brew services start "+svc+". "+m.notRunningFix(c)
		return ch
	}
	dbs, err := m.databases(env, live)
	if err != nil {
		ch.Status, ch.Detail = module.StatusFail, err.Error()
		return ch
	}
	if len(c.Databases) == 0 {
		ch.Detail = "running; prepare recorded no database names to compare"
		return ch
	}
	var missing []string
	for _, d := range c.Databases {
		if !slices.Contains(dbs, d) {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		ch.Status, ch.Paths = module.StatusFail, missing
		ch.Detail = "missing " + words.Count(len(missing), "database", "databases") + "; mtm finish --only databases --restore-dump can restore them from the old Mac's dump"
		return ch
	}
	ch.Detail = "running, with all " + words.Count(len(c.Databases), "database", "databases") + " from the old Mac"
	return ch
}
