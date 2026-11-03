//go:build integration

package databases

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
)

// requirePostgres finds a Homebrew postgresql@N and returns its programs'
// folder and major version.
func requirePostgres(t *testing.T) (string, string) {
	t.Helper()
	matches, _ := filepath.Glob("/opt/homebrew/opt/postgresql@*/bin/initdb")
	if len(matches) == 0 {
		t.Skip("needs a Homebrew postgresql@N")
	}
	bin := filepath.Dir(matches[len(matches)-1])
	return bin, strings.TrimPrefix(filepath.Base(filepath.Dir(bin)), "postgresql@")
}

// pgCommand keeps the developer's PG settings from steering a command to
// another server, as the module does for its own.
func pgCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	for _, kv := range os.Environ() {
		if !slices.Contains(pgUnset, strings.SplitN(kv, "=", 2)[0]) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, pgEnv...)
	return cmd
}

func mustRun(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := pgCommand(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestPostgresDumpAndRestore runs a throwaway cluster in a temporary
// folder that listens only on a Unix socket there, never on the network
// and never on the developer's own clusters.
func TestPostgresDumpAndRestore(t *testing.T) {
	bin, major := requirePostgres(t)
	varDir := t.TempDir()
	data := filepath.Join(varDir, "postgresql@"+major)
	sock, err := os.MkdirTemp("", "mtmpg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sock) })
	mustRun(t, filepath.Join(bin, "initdb"), "--pgdata", data, "--auth", "trust", "--encoding", "UTF8", "--locale", "C")
	mustRun(t, filepath.Join(bin, "pg_ctl"), "--pgdata", data, "--log", filepath.Join(varDir, "server.log"),
		"--options", "-k "+sock+" -p 5432 -c listen_addresses=''", "--wait", "start")
	t.Cleanup(func() { pgCommand(filepath.Join(bin, "pg_ctl"), "--pgdata", data, "--mode", "fast", "stop").Run() })
	psql := func(db, query string) string {
		return mustRun(t, filepath.Join(bin, "psql"), "--host", sock, "--port", "5432", "-X", "-w", "--dbname", db, "--tuples-only", "--no-align", "--command", query)
	}
	psql("postgres", "CREATE DATABASE mtmtest")
	psql("mtmtest", "CREATE TABLE t (v text); INSERT INTO t VALUES ('kept')")

	st, err := stage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exports, err := stage.OpenDir(st.Path(stage.ExportsDirName))
	if err != nil {
		t.Fatal(err)
	}
	mod := Module{Var: varDir, Opt: filepath.Dir(filepath.Dir(bin))}
	env := module.Env{Ctx: context.Background(), Run: runner.Exec{}, Exports: exports,
		Free: func(string) (uint64, error) { return 1 << 40, nil }}
	res, err := mod.Prepare(env, st)
	if err != nil {
		t.Fatal(err)
	}
	inv := res.Inventory.(Inventory)
	if len(inv.Clusters) != 1 || inv.Clusters[0].Dump == nil || !slices.Contains(inv.Clusters[0].Databases, "mtmtest") {
		t.Fatalf("prepare = %+v", res)
	}

	psql("postgres", "DROP DATABASE mtmtest")
	env.Manifest, env.RestoreDump = record(t, inv), true
	if res, err := mod.Finish(env, st); err != nil || len(res.Problems) != 0 {
		t.Fatalf("finish --restore-dump = %+v, %v", res, err)
	}
	if got := psql("mtmtest", "SELECT v FROM t"); got != "kept" {
		t.Errorf("restored row = %q, want kept", got)
	}
	checks, err := mod.Verify(env, st)
	if err != nil || len(checks) != 1 || checks[0].Status != module.StatusPass {
		t.Errorf("verify = %+v, %v", checks, err)
	}
}
