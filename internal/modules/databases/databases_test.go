package databases

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/manifest"
	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
	"github.com/momentmaker/mtm/internal/testutil"
)

// postmasterPID is what Postgres 16 writes to its data folder while it
// runs: pid, data folder, start time, port, socket folder, listen address,
// shared memory key, status.
const postmasterPID = "12345\n/opt/homebrew/var/postgresql@16\n1730500000\n5432\n/tmp\nlocalhost\n  5432001    65536\nready   \n"

const dumpSQL = "--\n-- PostgreSQL database cluster dump\n--\nCREATE ROLE dev;\n"

type fixture struct {
	t       *testing.T
	mod     Module
	fake    *runner.Fake
	st      *stage.Stage
	exports *stage.Stage
	free    uint64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	st, err := stage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	exports, err := stage.OpenDir(st.Path(stage.ExportsDirName))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, mod: Module{Var: filepath.Join(root, "var"), Opt: filepath.Join(root, "opt")},
		fake: &runner.Fake{Responses: map[string]runner.Response{}}, st: st, exports: exports, free: 100_000_000_000}
	if err := os.MkdirAll(f.mod.Var, 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

// cluster makes a data folder; a running one gets a postmaster.pid.
func (f *fixture) cluster(name, major string, running bool) {
	testutil.WriteFile(f.t, filepath.Join(f.mod.Var, name, "PG_VERSION"), major+"\n")
	if running {
		testutil.WriteFile(f.t, filepath.Join(f.mod.Var, name, "postmaster.pid"), postmasterPID)
	}
}

func (f *fixture) tool(major, name string) string {
	return filepath.Join(f.mod.Opt, "postgresql@"+major, "bin", name) + " --host /tmp --port 5432"
}

// serve answers as a running Postgres 16 server holding databases.
func (f *fixture) serve(databases ...string) {
	f.fake.Responses[f.tool("16", "pg_isready")+" --quiet"] = runner.Response{}
	f.fake.Responses[f.tool("16", "psql")+" -w -X --dbname postgres --tuples-only --no-align --command "+listQuery] = runner.Response{Stdout: strings.Join(databases, "\n") + "\n"}
	f.fake.Responses[f.tool("16", "psql")+" -w -X --dbname postgres --tuples-only --no-align --command "+sizeQuery] = runner.Response{Stdout: "8123456\n"}
	f.fake.Responses[f.tool("16", "psql")+" -w -X --dbname postgres --tuples-only --no-align --command "+relationQuery] = runner.Response{Stdout: "0\n"}
	f.fake.Responses[f.tool("16", "pg_dumpall")+" -w"] = runner.Response{Stdout: dumpSQL}
	f.fake.Responses[f.tool("16", "psql")+" -w -X --quiet --dbname postgres --file=-"] = runner.Response{}
}

func (f *fixture) env(m *manifest.Manifest) module.Env {
	return module.Env{Ctx: context.Background(), Run: f.fake, Manifest: m, Exports: f.exports,
		Free: func(string) (uint64, error) { return f.free, nil }}
}

func record(t *testing.T, inv Inventory) *manifest.Manifest {
	t.Helper()
	m := manifest.New("test")
	if err := m.SetModule(Name, time.Now(), inv); err != nil {
		t.Fatal(err)
	}
	return m
}

func (f *fixture) prepare(m *manifest.Manifest) (module.Result, Inventory) {
	f.t.Helper()
	res, err := f.mod.Prepare(f.env(m), f.st)
	if err != nil {
		f.t.Fatal(err)
	}
	inv, _ := res.Inventory.(Inventory)
	return res, inv
}

func (f *fixture) ran(line string) *runner.Command {
	for _, c := range f.fake.Calls() {
		if c.String() == line {
			return &c
		}
	}
	return nil
}

func ids(findings []module.Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.ID)
	}
	return out
}

func TestScanTellsRunningFromStoppedClusters(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@16", "16", true)
	f.cluster("postgresql@14", "14", false)
	testutil.WriteFile(t, filepath.Join(f.mod.Var, "postgres-notes", "README"), "not a cluster\n")
	f.serve("app", "postgres")
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"stopped:postgresql@14", "running:postgresql@16"}; !slices.Equal(ids(findings), want) {
		t.Errorf("findings = %q, want %q", ids(findings), want)
	}
}

func TestScanWarnsAboutAClusterWithoutASocket(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@16", "16", false)
	testutil.WriteFile(t, filepath.Join(f.mod.Var, "postgresql@16", "postmaster.pid"), "1\n/x\n1\n5432\n\n*\n")
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil || !slices.Equal(ids(findings), []string{"socket:postgresql@16"}) {
		t.Errorf("Scan = %+v, %v", findings, err)
	}
	if len(f.fake.Calls()) != 0 {
		t.Error("scan connected to a server without a socket")
	}
}

func TestPrepareDumpsARunningClusterThroughItsSocket(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@16", "16", true)
	f.serve("app", "postgres")
	res, inv := f.prepare(record(t, Inventory{}))
	if len(inv.Clusters) != 1 {
		t.Fatalf("clusters = %+v", inv.Clusters)
	}
	c := inv.Clusters[0]
	if !c.Running || c.Socket != "/tmp" || c.Port != "5432" || !slices.Equal(c.Databases, []string{"app", "postgres"}) || c.Dump == nil {
		t.Fatalf("cluster = %+v", c)
	}
	file, err := os.Open(f.exports.Path(c.Dump.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(gz); string(got) != dumpSQL {
		t.Errorf("dump = %q, want pg_dumpall's output", got)
	}
	dump := f.ran(f.tool("16", "pg_dumpall") + " -w")
	if dump == nil || !slices.Contains(dump.Unset, "PGHOST") || !slices.Contains(dump.Env, "PGPASSFILE=/dev/null") {
		t.Errorf("pg_dumpall = %+v, want PGHOST unset and no password file", dump)
	}
	if len(res.Problems) != 0 || len(res.Done) != 1 || !strings.HasPrefix(res.Done[0], "saved a dump of postgresql@16 (2 databases, ") {
		t.Errorf("result = %+v", res)
	}
}

func TestPrepareRecordsAStoppedClusterWithoutADump(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@14", "14", false)
	res, inv := f.prepare(record(t, Inventory{}))
	if len(inv.Clusters) != 1 || inv.Clusters[0].Running || inv.Clusters[0].Dump != nil || len(res.Problems) != 0 {
		t.Errorf("result = %+v", res)
	}
	if len(f.fake.Calls()) != 0 {
		t.Error("prepare ran Postgres commands for a stopped cluster")
	}
}

func TestPrepareAgainKeepsTheDumpOfAClusterThatStopped(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@16", "16", true)
	f.serve("app", "postgres")
	_, first := f.prepare(record(t, Inventory{}))
	os.Remove(filepath.Join(f.mod.Var, "postgresql@16", "postmaster.pid"))
	res, second := f.prepare(record(t, first))
	if second.Clusters[0].Dump == nil || *second.Clusters[0].Dump != *first.Clusters[0].Dump || !second.Clusters[0].Running {
		t.Errorf("clusters = %+v, want the earlier record kept", second.Clusters)
	}
	if !slices.Equal(res.Done, []string{"kept the dump of postgresql@16 from the earlier prepare"}) {
		t.Errorf("done = %q", res.Done)
	}
}

func TestPrepareDumpFailureWarns(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@16", "16", true)
	f.serve("app", "postgres")
	f.fake.Responses[f.tool("16", "pg_dumpall")+" -w"] = runner.Response{Code: 1, Stderr: "pg_dumpall: error: connection failed"}
	res, inv := f.prepare(record(t, Inventory{}))
	if !slices.Equal(ids(res.Problems), []string{"dump:postgresql@16"}) || res.Problems[0].Severity != module.SeverityWarn {
		t.Errorf("problems = %+v", res.Problems)
	}
	if inv.Clusters[0].Dump != nil {
		t.Error("a failed dump was recorded")
	}
	if _, err := os.Stat(f.exports.Path("postgres/postgresql@16.sql.gz")); err == nil {
		t.Error("a failed dump left a file behind")
	}
}

func TestPrepareBlocksWhenTheDumpDoesntFit(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@16", "16", true)
	f.serve("app", "postgres")
	f.free = 10_000_000_000
	res, _ := f.prepare(record(t, Inventory{}))
	if !module.HasBlocker(res.Problems) || f.ran(f.tool("16", "pg_dumpall")+" -w") != nil {
		t.Errorf("result = %+v, want a blocker before dumping", res)
	}
}

func TestPrepareWithoutHomebrewPostgres(t *testing.T) {
	f := newFixture(t)
	f.mod.Var = filepath.Join(t.TempDir(), "missing")
	res, inv := f.prepare(record(t, Inventory{}))
	if len(inv.Clusters) != 0 || len(res.Problems) != 0 {
		t.Errorf("result = %+v", res)
	}
}

// dumped returns a record of a running postgresql@16 whose dump prepare
// saved, with the server answering as holding databases.
func dumped(t *testing.T, databases ...string) (*fixture, *manifest.Manifest) {
	f := newFixture(t)
	f.cluster("postgresql@16", "16", true)
	f.serve("app", "postgres")
	_, inv := f.prepare(record(t, Inventory{}))
	f.serve(databases...)
	return f, record(t, inv)
}

func (f *fixture) finish(m *manifest.Manifest, restoreDump, force bool) module.Result {
	f.t.Helper()
	env := f.env(m)
	env.RestoreDump, env.Force = restoreDump, force
	res, err := f.mod.Finish(env, f.st)
	if err != nil {
		f.t.Fatal(err)
	}
	return res
}

const restoreLine = " -w -X --quiet --dbname postgres --file=-"

func TestFinishDoesNothingWithoutRestoreDump(t *testing.T) {
	f, m := dumped(t, "postgres")
	before := len(f.fake.Calls())
	if res := f.finish(m, false, false); len(res.Done)+len(res.Problems) != 0 || len(f.fake.Calls()) != before {
		t.Errorf("finish = %+v, want nothing done", res)
	}
}

func TestFinishRestoreDumpLoadsTheDump(t *testing.T) {
	f, m := dumped(t, "postgres")
	res := f.finish(m, true, false)
	if !slices.Equal(res.Done, []string{"restored postgresql@16 from its dump"}) {
		t.Errorf("finish = %+v", res)
	}
	if c := f.ran(f.tool("16", "psql") + restoreLine); c == nil || c.Stdin != dumpSQL {
		t.Errorf("psql = %+v, want the dump on its input", c)
	}
}

func TestFinishRestoreDumpNeedsForceForAClusterWithDatabases(t *testing.T) {
	f, m := dumped(t, "app", "postgres")
	res := f.finish(m, true, false)
	if !module.HasBlocker(res.Problems) || !strings.Contains(res.Problems[0].Fix, "--restore-dump --force") || f.ran(f.tool("16", "psql")+restoreLine) != nil {
		t.Errorf("finish = %+v, want a refusal", res)
	}
	if res := f.finish(m, true, true); len(res.Problems) != 0 || f.ran(f.tool("16", "psql")+restoreLine) == nil {
		t.Errorf("finish --force = %+v, want the dump restored", res)
	}
}

func TestFinishRestoreDumpNeedsTheServerRunning(t *testing.T) {
	f, m := dumped(t, "postgres")
	f.fake.Responses[f.tool("16", "pg_isready")+" --quiet"] = runner.Response{Code: 2}
	res := f.finish(m, true, false)
	if !module.HasBlocker(res.Problems) || !strings.Contains(res.Problems[0].Fix, "brew postinstall 'postgresql@16'") {
		t.Errorf("finish = %+v", res)
	}
	dir := f.mod.dataDir(Cluster{Name: "postgresql@16"})
	if want := "move it aside: mv '" + dir + "' '" + dir + "'.old, then create an empty one: brew postinstall"; !strings.Contains(res.Problems[0].Fix, want) {
		t.Errorf("fix = %q, want it to move the data folder aside first", res.Problems[0].Fix)
	}
}

func TestFinishRestoreDumpRefusesAChangedDump(t *testing.T) {
	f, m := dumped(t, "postgres")
	var inv Inventory
	m.Module(Name, &inv)
	os.WriteFile(f.exports.Path(inv.Clusters[0].Dump.Name), []byte("tampered"), 0o600)
	res := f.finish(m, true, false)
	if !module.HasBlocker(res.Problems) || f.ran(f.tool("16", "psql")+restoreLine) != nil {
		t.Errorf("finish = %+v, want a refusal", res)
	}
}

func (f *fixture) verify(m *manifest.Manifest) module.Check {
	f.t.Helper()
	checks, err := f.mod.Verify(f.env(m), f.st)
	if err != nil || len(checks) != 1 {
		f.t.Fatalf("Verify = %+v, %v", checks, err)
	}
	return checks[0]
}

func TestVerifyRunningClusterHasItsDatabases(t *testing.T) {
	f, m := dumped(t, "app", "postgres")
	if c := f.verify(m); c.Status != module.StatusPass || !c.Critical {
		t.Errorf("check = %+v, want a critical pass", c)
	}
	f.serve("postgres")
	if c := f.verify(m); c.Status != module.StatusFail || !slices.Equal(c.Paths, []string{"app"}) {
		t.Errorf("check = %+v, want app missing", c)
	}
	f.fake.Responses[f.tool("16", "pg_isready")+" --quiet"] = runner.Response{Code: 2}
	if c := f.verify(m); c.Status != module.StatusFail || !strings.Contains(c.Detail, "brew services start 'postgresql@16'") || !strings.Contains(c.Detail, "move it aside: mv ") {
		t.Errorf("check = %+v, want not running, with the move-aside advice", c)
	}
}

func TestVerifyStoppedClusterNeedsItsDataFolder(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@14", "14", false)
	_, inv := f.prepare(record(t, Inventory{}))
	m := record(t, inv)
	if c := f.verify(m); c.Status != module.StatusPass {
		t.Errorf("check = %+v", c)
	}
	os.RemoveAll(filepath.Join(f.mod.Var, "postgresql@14"))
	if c := f.verify(m); c.Status != module.StatusFail || !c.Critical {
		t.Errorf("check = %+v, want a critical fail", c)
	}
}

func TestPrepareAgainKeepsTheRecordOfAClusterWhoseDumpFailedBeforeItStopped(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@16", "16", true)
	f.serve("app", "postgres")
	f.fake.Responses[f.tool("16", "pg_dumpall")+" -w"] = runner.Response{Code: 1, Stderr: "pg_dumpall: error: connection failed"}
	_, first := f.prepare(record(t, Inventory{}))
	os.Remove(filepath.Join(f.mod.Var, "postgresql@16", "postmaster.pid"))
	res, second := f.prepare(record(t, first))
	c := second.Clusters[0]
	if !c.Running || c.Dump != nil || !slices.Equal(c.Databases, []string{"app", "postgres"}) {
		t.Errorf("cluster = %+v, want Running and Databases kept, no dump", c)
	}
	if !slices.Equal(ids(res.Problems), []string{"dump:postgresql@16"}) || res.Problems[0].Severity != module.SeverityWarn ||
		!strings.Contains(res.Problems[0].Fix, "brew services start 'postgresql@16' && mtm prepare --only databases") {
		t.Errorf("problems = %+v", res.Problems)
	}
}

func TestFinishRestoreDumpWarnsAboutPsqlErrorsOtherThanTheExistingRole(t *testing.T) {
	f, m := dumped(t, "postgres")
	f.fake.Responses[f.tool("16", "psql")+restoreLine] = runner.Response{Stderr: "psql:-:1: ERROR:  role \"dev\" already exists\npsql:-:5: ERROR:  extension \"x\" is not available\npsql:-:9: ERROR:  relation \"t\" does not exist\n"}
	res := f.finish(m, true, false)
	if !slices.Equal(res.Done, []string{"restored postgresql@16 from its dump"}) || !slices.Equal(ids(res.Problems), []string{"restore-errors:postgresql@16"}) {
		t.Fatalf("finish = %+v", res)
	}
	p := res.Problems[0]
	if p.Severity != module.SeverityWarn || p.Title != "psql reported 2 errors while restoring postgresql@16" || strings.Contains(p.Title+p.Detail+p.Fix, "is not available") {
		t.Errorf("problem = %+v", p)
	}
}

func TestFinishRestoreDumpNeedsForceForTablesInPostgres(t *testing.T) {
	f, m := dumped(t, "postgres")
	f.fake.Responses[f.tool("16", "psql")+" -w -X --dbname postgres --tuples-only --no-align --command "+relationQuery] = runner.Response{Stdout: "3\n"}
	res := f.finish(m, true, false)
	if !module.HasBlocker(res.Problems) || !strings.Contains(res.Problems[0].Fix, "--restore-dump --force") || f.ran(f.tool("16", "psql")+restoreLine) != nil {
		t.Errorf("finish = %+v, want a refusal", res)
	}
	if res := f.finish(m, true, true); len(res.Problems) != 0 || f.ran(f.tool("16", "psql")+restoreLine) == nil {
		t.Errorf("finish --force = %+v, want the dump restored", res)
	}
}

func TestFinishAndVerifyReachTheServerThroughThisMacsPostmasterPID(t *testing.T) {
	f, m := dumped(t, "postgres")
	os.Remove(filepath.Join(f.mod.Var, "postgresql@16", "postmaster.pid"))
	res := f.finish(m, true, false)
	if !module.HasBlocker(res.Problems) || f.ran(f.tool("16", "psql")+restoreLine) != nil {
		t.Errorf("finish = %+v, want a refusal", res)
	}
	if c := f.verify(m); c.Status != module.StatusFail || !strings.Contains(c.Detail, "brew services start 'postgresql@16'") {
		t.Errorf("check = %+v, want not running", c)
	}
}

func TestScanTreatsARelativeSocketFolderAsNone(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@16", "16", false)
	testutil.WriteFile(t, filepath.Join(f.mod.Var, "postgresql@16", "postmaster.pid"), "1\n/x\n1\n5432\nsockets\n*\n")
	findings, err := f.mod.Scan(f.env(nil))
	if err != nil || !slices.Equal(ids(findings), []string{"socket:postgresql@16"}) || len(f.fake.Calls()) != 0 {
		t.Errorf("Scan = %+v, %v, calls %d", findings, err, len(f.fake.Calls()))
	}
}

func TestPrepareAgainKeepsTheRecordWhenThePostmasterPIDIsStale(t *testing.T) {
	f, m := dumped(t, "app", "postgres")
	var earlier Inventory
	m.Module(Name, &earlier)
	f.fake.Responses[f.tool("16", "pg_isready")+" --quiet"] = runner.Response{Code: 2}
	res, inv := f.prepare(m)
	c := inv.Clusters[0]
	if !c.Running || c.Dump == nil || *c.Dump != *earlier.Clusters[0].Dump || !slices.Equal(c.Databases, []string{"app", "postgres"}) {
		t.Errorf("cluster = %+v, want the earlier record kept", c)
	}
	if !slices.Equal(ids(res.Problems), []string{"answer:postgresql@16"}) {
		t.Errorf("problems = %+v, want the answer warning", res.Problems)
	}
}

func TestPrepareAgainDropsAKeptDumpThatChanged(t *testing.T) {
	f, m := dumped(t, "app", "postgres")
	var earlier Inventory
	m.Module(Name, &earlier)
	os.WriteFile(f.exports.Path(earlier.Clusters[0].Dump.Name), []byte("tampered"), 0o600)
	f.fake.Responses[f.tool("16", "pg_isready")+" --quiet"] = runner.Response{Code: 2}
	_, inv := f.prepare(m)
	if c := inv.Clusters[0]; !c.Running || c.Dump != nil {
		t.Errorf("cluster = %+v, want Running kept and the damaged dump dropped", c)
	}
}

func TestFinishRestoreFailureDetailHasNoPsqlOutput(t *testing.T) {
	f, m := dumped(t, "postgres")
	f.fake.Responses[f.tool("16", "psql")+restoreLine] = runner.Response{Code: 3, Stderr: "psql:-:7: ERROR:  duplicate key value (email)=(ada@example.com)\n"}
	res := f.finish(m, true, false)
	p := res.Problems[0]
	if p.Detail != "psql exited with status 3, after 1 error" || strings.Contains(p.Title+p.Detail+p.Fix, "ada@example.com") {
		t.Errorf("problem = %+v, want only the status and a count", p)
	}
}

func TestVerifyPassesARunningClusterWithNoRecordedNamesHonestly(t *testing.T) {
	f := newFixture(t)
	f.cluster("postgresql@16", "16", true)
	f.serve("postgres")
	m := record(t, Inventory{Clusters: []Cluster{{Name: "postgresql@16", Major: "16", Running: true}}})
	c := f.verify(m)
	if c.Status != module.StatusPass || c.Detail != "running; prepare recorded no database names to compare" {
		t.Errorf("check = %+v", c)
	}
}
