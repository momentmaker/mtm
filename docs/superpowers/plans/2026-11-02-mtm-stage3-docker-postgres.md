# mtm Stage 3: Docker Desktop and Postgres Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Export Docker Desktop's volumes and locally built images, and dump running Homebrew Postgres clusters, before Migration Assistant. On the new Mac, restore only what didn't arrive intact, never overwrite data, and verify it all. The stage also adds:
- free-space checks with a 10 GB reserve;
- `prepare --stage` on an encrypted APFS volume;
- fingerprint checks before every restore;
- `finish --only`, `--force`, and `--restore-dump`.

**Architecture:**
- **Runner.** It can now stream: `Command.Stdout` takes a writer and `Command.Input` takes a reader, because exports can be larger than memory.
- **`stage` package.** It gains:
  - the exports folder (`OpenDir`);
  - fingerprinted export files (`CreateArtifact`, `CheckArtifact`);
  - the free-space check (`Free`, `Room`, `Reserve`);
  - the external-volume check behind `--stage` (`VolumeOf`, `CheckExternal`).
- **`app` package.** It opens the exports folder and hands it to modules as `Env.Exports`. It records an external folder in `manifest.ExportsDir`, and `cleanup` deletes that folder.
- **`docker` module.** It drives Docker Desktop's own CLI through the socket in the home folder. It exports volumes through never-started holder containers of an empty image, so it needs no network and no helper image. A content digest covers each volume: path, type, mode, owner, size, link target, and content.
- **`databases` module.** It finds Homebrew Postgres clusters from their data folders and talks to them only over their Unix socket. The existing `homebrew` module keeps stopping and starting the servers.

**Tech Stack:**
- Go 1.26, standard library only: `archive/tar`, `compress/gzip`, and `crypto/sha256`.
- Docker Desktop's CLI (`/Applications/Docker.app/Contents/Resources/bin/docker`, Engine 29 on the author's Mac) and its Compose plugin.
- Homebrew `postgresql@N` (`pg_isready`, `psql`, `pg_dumpall`).
- `/usr/sbin/diskutil`.

**Spec:** `docs/superpowers/specs/2026-11-02-mtm-design.md`. Stage 3 covers:
- sections 5 (cleanup and the `--stage` folder) and 6.2 rule 4 (volumes are never overwritten);
- 6.3 (external staging, export fingerprints) and 8.1 (the exports layout);
- 9.5 `docker` and 9.6 `databases`;
- 10 (re-runs, free space) and 11 (the Docker and Postgres integration tests).

These sections were rewritten while planning, from probes of the author's Mac. The probes found:
- With Docker's containerd image store, every image has a repo digest, so pulled images are told apart by `Identity.Pull`.
- `docker container create` needs a command even for a container that never starts.
- `docker container cp --archive` keeps owners and modes.
- `docker image save` and `load` keep the image id.

The stage list and the known gaps from Stages 1 and 2 are in `IMPLEMENTATION_PLAN.md`.

**How this plan was checked:**
- Every task's code was written and run in a scratch copy of the repository before the plan was written.
- Replayed in order on a clean copy, each task leaves `gofmt -l .`, `go vet ./...`, and `go test ./...` clean.
- The Docker integration tests (Task 9) passed against the author's running Docker Desktop.
- `TestNoNetwork` (Task 11) passed.
- The Postgres integration test (Task 10) skips on the author's Mac, which has no Homebrew Postgres; it was compiled and vetted but never run.

**Diffs:** a modified file's change is a unified diff against the file as the previous task left it. Apply it with `git apply`, or by hand. A new file is given in full.

## Global Constraints

- **Module and dependencies:** module path `github.com/momentmaker/mtm`, `go 1.26`, and no third-party dependencies.
- **Network and secrets (spec 6.2):** `mtm` makes no network connections and never reads secret contents.
- **What reports and logs hold (spec 6.3):** never fingerprints, file contents, or environment values. Volume digests and archive fingerprints live only in `manifest.json`.
- **Apple tools by absolute path:** this stage adds `/usr/sbin/diskutil`. The earlier ones stay as they are: `/usr/bin/plutil`, `/usr/bin/tmutil`, `/usr/bin/fdesetup`, `/usr/bin/sw_vers`, `/usr/sbin/sysctl`, `/usr/sbin/ioreg`, `/usr/bin/dscl`, `/usr/bin/lipo`, and `/bin/launchctl`.
- **Docker rules:**
  - Every Docker command goes through `docker.Module.command`, which runs `Module.Docker`: Docker Desktop's CLI, never whatever `docker` comes first on `PATH`.
  - That command sets `DOCKER_HOST=unix://<home>/.docker/run/docker.sock` and `DOCKER_CLI_HINTS=false`, and unsets `DOCKER_HOST`, `DOCKER_CONTEXT`, `DOCKER_TLS_VERIFY`, and `DOCKER_CERT_PATH`.
  - Every `docker container create` passes `--pull never`.
  - `docker compose up` passes `--pull never --no-build`.
  - The only image `mtm` creates is its empty image, imported from a 1024-byte empty archive.
- **Postgres rules:**
  - Every Postgres command connects through the cluster's Unix socket (`--host <socket folder> --port <port>`).
  - `psql` and `pg_dumpall` pass `-w`; `psql` also passes `-X`.
  - Each command unsets `PGHOST`, `PGHOSTADDR`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGSERVICE`, `PGSERVICEFILE`, and `PGPASSWORD`, and sets `PGPASSFILE=/dev/null`.
- **Data safety:** `mtm` never overwrites a Docker volume that holds data, and never deletes anything outside `~/.mtm` and a folder named `mtm-exports` that the record names.
- **Exports:**
  - Every export is written through `stage.Stage.CreateArtifact` (0600, atomic, SHA-256 recorded).
  - Every restore calls `stage.Stage.CheckArtifact` first.
  - Each export calls `stage.Room` first, which keeps 10 GB free (`stage.Reserve = 10_000_000_000`).
- **The developer's Mac is never changed by tests:**
  - No test may stop, start, or load anything on the developer's Mac.
  - Unit tests use `runner.Fake`.
  - The Docker integration tests touch only volumes, images, and containers named `mtm-test-…`. They never call `Scan`, `Prepare`, `Finish`, `Undo`, or `Verify` of the docker module against the real engine.
  - Never run `mtm prepare`, `finish`, `verify`, or `cleanup` against the real home folder.
  - Never run `brew services start` or `stop`, `launchctl bootstrap`, `osascript`, or `sudo` on the real Mac.
- **Code style:** modules never print; all output goes through `internal/report`. Comments explain why, never restate code. No commented-out code.
- **Exit codes:**
  - `verify`: `0` safe, `1` not safe, `2` mtm failed.
  - Other commands: `0` success, `1` completed with problems or refused, `2` mtm failed.
- **Before every commit:** `gofmt -l .` prints nothing, and `go vet ./...`, `go vet -tags integration ./...`, and `go test ./...` pass.
- **Commit messages** explain why and end with a `Co-Authored-By:` trailer naming the model that wrote the commit, as your harness instructs.

## Review Focus

These are the five conditions most likely to bite a real user. Each has a pinned test in the task that owns the code.

1. **A volume that changed on the new Mac before `finish` checked it.** This happens when a container with restart policy `always` starts with Docker, or when the user runs `docker compose up` first. `finish` must never overwrite it. It must print the exact commands that remove the volume, and start nothing.
   - Task 7 `TestScanWarnsAboutAContainerThatRestartsWithDocker`
   - Task 8 `TestFinishNeverOverwritesAVolumeHoldingOtherData`
   - Task 9 `TestDockerNeverOverwritesAVolumeHoldingOtherData`
2. **Too little free space.** The author's Mac has 13 GB free against about 7.6 GB of Docker exports. `prepare` must block before it stops any container, and point to `--stage`.
   - Task 2 `TestRoomKeepsTheReserve`
   - Task 7 `TestScanBlocksWhenTheExportsDontFit`, `TestPrepareChecksSpaceBeforeStoppingAnything`
   - Task 10 `TestPrepareBlocksWhenTheDumpDoesntFit`
3. **The `--stage` drive.** It is refused when it is exFAT, unencrypted, or ignores ownership. When it is detached at `finish`, the restore blocks and asks for it. When it is detached at `cleanup`, the report names the folder still to delete.
   - Task 3 `TestCheckExternal`
   - Task 4 `TestPrepareStageRefusesAnUnencryptedVolume`, `TestCleanupWithTheStageDriveDetached`
   - Task 8 `TestFinishAsksForTheStageDriveWhenItIsDetached`
4. **Re-running `prepare`** after the user quit Docker Desktop, or after the first run stopped containers and Postgres. The earlier record and stops are kept. An unchanged image archive isn't saved again.
   - Task 7 `TestPrepareKeepsTheRecordWhileDockerIsNotRunning`, `TestPrepareAgainKeepsContainersStoppedEarlier`, `TestPrepareAgainKeepsAnUnchangedImageArchive`
   - Task 10 `TestPrepareAgainKeepsTheDumpOfAClusterThatStopped`
5. **Running `finish` again after a partial failure.** Volumes it already settled are not read or restored again. `verify` trusts that comparison even after containers have changed the volumes.
   - Task 8 `TestFinishSkipsVolumesItSettledBefore`, `TestVerifyTrustsTheVolumesFinishSettled`

## File Structure

```
internal/runner/{runner,exec,fake}.go        (modify) Command.Stdout and Command.Input stream
internal/words/words.go                      (modify) Bytes: "13.1 GB"
internal/stage/stage.go                      (modify) OpenDir, ExistingDir, ExportsDirName, ExternalDirName
internal/stage/space.go                      Free, Room, Reserve, ErrNoRoom
internal/stage/artifact.go                   Artifact, CreateArtifact, CheckArtifact
internal/stage/volume.go                     VolumeOf, CheckExternal (prepare --stage)
internal/manifest/manifest.go                (modify) ExportsDir
internal/module/module.go                    (modify) Env.Exports, Env.Free, Env.Force, Env.RestoreDump
internal/app/app.go                          (modify) --stage, exports folder, FinishOptions, CleanupReport
cmd/mtm/main.go                              (modify) prepare --stage; finish --only/--force/--restore-dump; cleanup text; module wiring
internal/modules/docker/digest.go            content digest of a volume's tar stream
internal/modules/docker/client.go            Module, Docker CLI commands, inspect parsing, holder containers
internal/modules/docker/docker.go            Scan, Prepare, Undo
internal/modules/docker/restore.go           Finish, Verify, docker-finish.json
internal/modules/docker/integration_test.go  real Docker Desktop round trips (build tag integration)
internal/modules/databases/databases.go      Homebrew Postgres: Scan, Prepare, Finish (--restore-dump), Verify
internal/modules/databases/integration_test.go  throwaway cluster round trip (build tag integration)
cmd/mtm/nonetwork_integration_test.go        (modify) also run databases and docker offline
```

---
### Task 1: The runner streams standard input and output

**Files:**
- Modify: `internal/runner/runner.go`, `internal/runner/exec.go`, `internal/runner/fake.go`
- Test: `internal/runner/runner_test.go`

**Interfaces:**
- Consumes: `runner.Command`, `runner.Exec`, `runner.Fake` (Stage 1).
- Produces:
  - `runner.Command.Input io.Reader`, streamed to standard input.
  - `runner.Command.Stdout io.Writer`, which receives standard output as it arrives, while `Output.Stdout` stays empty.
  - A command sets `Stdin` or `Input`, never both. `Exec` refuses both.
  - With `Stdout` set, a command whose pipes stay open past the 2-second wait delay fails. Stage 2 lets that case succeed for captured output, which suits a login shell that leaves `ssh-agent` behind. For a streamed export it would save a short archive.
  - `runner.Fake` reads `Input` in full and records it as the call's `Stdin` in `Calls()`. It writes the canned `Response.Stdout` to the command's `Stdout` writer when one is set.

- [ ] **Step 1: Write the failing tests**

Add these four tests to `internal/runner/runner_test.go` (the file already imports `bytes` and `strings`):
```diff
--- a/internal/runner/runner_test.go
+++ b/internal/runner/runner_test.go
@@ -133,3 +133,49 @@
 		t.Errorf("log missing command or exit status:\n%s", got)
 	}
 }
+
+func TestExecStreamsInputAndOutput(t *testing.T) {
+	var got bytes.Buffer
+	out, err := Exec{}.Run(context.Background(), Command{
+		Name:   "/bin/cat",
+		Input:  strings.NewReader("streamed\n"),
+		Stdout: &got,
+	})
+	if err != nil {
+		t.Fatal(err)
+	}
+	if got.String() != "streamed\n" || len(out.Stdout) != 0 {
+		t.Errorf("streamed %q, Output.Stdout %q; want %q and nothing", got.String(), out.Stdout, "streamed\n")
+	}
+}
+
+func TestExecRefusesStdinAndInputTogether(t *testing.T) {
+	_, err := Exec{}.Run(context.Background(), Command{Name: "/bin/cat", Stdin: "a", Input: strings.NewReader("b")})
+	if err == nil || !strings.Contains(err.Error(), "both Stdin and Input") {
+		t.Errorf("err = %v, want both Stdin and Input", err)
+	}
+}
+
+func TestExecFailsAStreamCutOffByABackgroundChild(t *testing.T) {
+	var got bytes.Buffer
+	_, err := Exec{}.Run(context.Background(), Command{Name: "/bin/sh", Args: []string{"-c", "sleep 5 & echo hi"}, Stdout: &got})
+	if err == nil {
+		t.Error("a streamed command whose output was cut off reported success")
+	}
+}
+
+func TestFakeStreamsOutputAndRecordsInput(t *testing.T) {
+	f := &Fake{Responses: map[string]Response{"/bin/cat": {Stdout: "canned"}}}
+	var got bytes.Buffer
+	out, err := f.Run(context.Background(), Command{Name: "/bin/cat", Input: strings.NewReader("sent"), Stdout: &got})
+	if err != nil {
+		t.Fatal(err)
+	}
+	if got.String() != "canned" || len(out.Stdout) != 0 {
+		t.Errorf("streamed %q, Output.Stdout %q; want %q and nothing", got.String(), out.Stdout, "canned")
+	}
+	calls := f.Calls()
+	if len(calls) != 1 || calls[0].Stdin != "sent" || calls[0].Input != nil {
+		t.Errorf("calls = %+v, want one with Stdin %q and no Input", calls, "sent")
+	}
+}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/runner/`
Expected: the build fails with `unknown field Input in struct literal of type Command`.

- [ ] **Step 3: Implement streaming**

`internal/runner/runner.go`:
```diff
--- a/internal/runner/runner.go
+++ b/internal/runner/runner.go
@@ -6,6 +6,7 @@
 	"context"
 	"errors"
 	"fmt"
+	"io"
 	"strings"
 
 	"github.com/momentmaker/mtm/internal/redact"
@@ -13,14 +14,19 @@
 
 // Command is one system command. Unset names variables removed from the
 // current environment, and Env entries are then appended to it. Stdin,
-// when set, is written to the command's standard input.
+// when set, is written to the command's standard input. Input streams
+// standard input instead, and Stdout, when set, receives standard output
+// as it arrives rather than in Output.Stdout: exports can be larger than
+// memory. A command sets Stdin or Input, not both.
 type Command struct {
-	Name  string
-	Args  []string
-	Dir   string
-	Unset []string
-	Env   []string
-	Stdin string
+	Name   string
+	Args   []string
+	Dir    string
+	Unset  []string
+	Env    []string
+	Stdin  string
+	Input  io.Reader
+	Stdout io.Writer
 }
 
 // String is the unredacted command line. Fake uses it as the lookup key.
```

`internal/runner/exec.go`:
```diff
--- a/internal/runner/exec.go
+++ b/internal/runner/exec.go
@@ -23,23 +23,34 @@
 type Exec struct{}
 
 func (Exec) Run(ctx context.Context, cmd Command) (Output, error) {
+	if cmd.Stdin != "" && cmd.Input != nil {
+		return Output{}, fmt.Errorf("%s: both Stdin and Input are set", Display(cmd))
+	}
 	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
 	c.WaitDelay = waitDelay
 	c.Dir = cmd.Dir
 	if len(cmd.Env) > 0 || len(cmd.Unset) > 0 {
 		c.Env = append(without(os.Environ(), cmd.Unset), cmd.Env...)
 	}
-	if cmd.Stdin != "" {
+	switch {
+	case cmd.Input != nil:
+		c.Stdin = cmd.Input
+	case cmd.Stdin != "":
 		c.Stdin = strings.NewReader(cmd.Stdin)
 	}
 	var stdout, stderr bytes.Buffer
 	c.Stdout = &stdout
+	if cmd.Stdout != nil {
+		c.Stdout = cmd.Stdout
+	}
 	c.Stderr = &stderr
 	err := c.Run()
 	out := Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
-	if errors.Is(err, exec.ErrWaitDelay) && c.ProcessState != nil && c.ProcessState.Success() {
+	if errors.Is(err, exec.ErrWaitDelay) && c.ProcessState != nil && c.ProcessState.Success() && cmd.Stdout == nil {
 		// The command exited 0 and only a child it left behind held the
 		// pipes; anything that child writes later is dropped, not waited for.
+		// A streamed export gets no such pass: output still in flight would
+		// be cut off, and the archive saved short.
 		return out, nil
 	}
 	if err == nil {
```

`internal/runner/fake.go`:
```diff
--- a/internal/runner/fake.go
+++ b/internal/runner/fake.go
@@ -3,6 +3,7 @@
 import (
 	"context"
 	"fmt"
+	"io"
 	"sync"
 )
 
@@ -15,7 +16,9 @@
 }
 
 // Fake answers commands from Responses, keyed by Command.String(). Commands
-// without a response go to Fallback when it is set, and fail otherwise.
+// without a response go to Fallback when it is set, and fail otherwise. A
+// command's Input is read in full and recorded in Calls as Stdin, and a
+// response's Stdout is written to the command's Stdout when it has one.
 type Fake struct {
 	Responses map[string]Response
 	Fallback  Runner
@@ -25,6 +28,13 @@
 }
 
 func (f *Fake) Run(ctx context.Context, cmd Command) (Output, error) {
+	if cmd.Input != nil {
+		data, err := io.ReadAll(cmd.Input)
+		if err != nil {
+			return Output{}, fmt.Errorf("fake runner: read input of %s: %w", cmd.String(), err)
+		}
+		cmd.Stdin, cmd.Input = string(data), nil
+	}
 	f.mu.Lock()
 	f.calls = append(f.calls, cmd)
 	resp, ok := f.Responses[cmd.String()]
@@ -36,6 +46,12 @@
 		return Output{}, fmt.Errorf("fake runner: unexpected command: %s", cmd.String())
 	}
 	out := Output{Stdout: []byte(resp.Stdout), Stderr: []byte(resp.Stderr), Code: resp.Code}
+	if cmd.Stdout != nil {
+		if _, err := io.WriteString(cmd.Stdout, resp.Stdout); err != nil {
+			return Output{}, fmt.Errorf("fake runner: write output of %s: %w", cmd.String(), err)
+		}
+		out.Stdout = nil
+	}
 	switch {
 	case resp.Err != nil:
 		return out, resp.Err
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/runner/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go test ./...
git add internal/runner
git commit -m "Let commands stream input and output

Docker volume archives, image archives, and Postgres dumps can be larger
than memory, so the runner hands them to a writer or reads them from a
reader instead of buffering them."
```

---

### Task 2: Free space, the exports folder, and fingerprinted exports

**Files:**
- Modify: `internal/words/words.go`, `internal/words/words_test.go`, `internal/stage/stage.go`
- Create: `internal/stage/space.go`, `internal/stage/artifact.go`
- Test: `internal/stage/space_test.go`, `internal/stage/artifact_test.go`

**Interfaces:**
- Consumes: `stage.Stage`, `Stage.Create`, `File.Commit`, `File.Abort`, `checkSafe` (Stage 1).
- Produces:
  - `words.Bytes(n uint64) string`, in decimal units: `"13.1 GB"`, `"999 B"`.
  - `stage.OpenDir(root string) (*Stage, error)`, which creates `root` with mode 0700 and checks it like `~/.mtm`. `stage.Open(home)` now calls it.
  - `stage.Reserve uint64 = 10_000_000_000`, `stage.ErrNoRoom`.
  - `stage.Free(path string) (uint64, error)`, the bytes available to the user, from `statfs`.
  - `stage.Room(free func(string) (uint64, error), path string, need uint64) error`, which wraps `ErrNoRoom` unless `need` fits with `Reserve` to spare.
  - `stage.Artifact{Name string; Size int64; SHA256 string}`, with JSON tags `name`, `size`, and `sha256`.
  - `(*Stage).CreateArtifact(name string) (*ArtifactFile, error)`. `*ArtifactFile` is an `io.Writer` with `Commit() (Artifact, error)` and `Abort()`. The name must be local (`filepath.IsLocal`).
  - `(*Stage).CheckArtifact(a Artifact) error`. It checks the folder is private first, then re-reads the file. It wraps `stage.ErrArtifactChanged` on a size or SHA-256 mismatch.

- [ ] **Step 1: Write the failing tests**

`internal/words/words_test.go`:
```diff
--- a/internal/words/words_test.go
+++ b/internal/words/words_test.go
@@ -13,3 +13,15 @@
 		}
 	}
 }
+
+func TestBytes(t *testing.T) {
+	tests := []struct {
+		n    uint64
+		want string
+	}{{0, "0 B"}, {999, "999 B"}, {1000, "1.0 kB"}, {224_300_000, "224.3 MB"}, {13_136_968_000, "13.1 GB"}, {10_000_000_000, "10.0 GB"}}
+	for _, tt := range tests {
+		if got := Bytes(tt.n); got != tt.want {
+			t.Errorf("Bytes(%d) = %q, want %q", tt.n, got, tt.want)
+		}
+	}
+}
```

`internal/stage/space_test.go`:
```go
package stage

import (
	"errors"
	"strings"
	"testing"
)

func TestFreeReportsSpace(t *testing.T) {
	free, err := Free(t.TempDir())
	if err != nil || free == 0 {
		t.Errorf("Free = %d, %v; want some space", free, err)
	}
}

func freeSpace(n uint64) func(string) (uint64, error) {
	return func(string) (uint64, error) { return n, nil }
}

func TestRoomKeepsTheReserve(t *testing.T) {
	tests := []struct {
		name       string
		free, need uint64
		ok         bool
	}{
		{"fits beside the reserve", 25_000_000_000, 10_000_000_000, true},
		{"exactly fits", 20_000_000_000, 10_000_000_000, true},
		{"would eat into the reserve", 15_000_000_000, 10_000_000_000, false},
		{"already inside the reserve", 5_000_000_000, 0, false},
	}
	for _, tt := range tests {
		err := Room(freeSpace(tt.free), "/Volumes/X", tt.need)
		if (err == nil) != tt.ok {
			t.Errorf("%s: Room = %v, want ok %v", tt.name, err, tt.ok)
		}
		if err != nil && (!errors.Is(err, ErrNoRoom) || !strings.Contains(err.Error(), "mtm keeps 10.0 GB free")) {
			t.Errorf("%s: err = %v, want ErrNoRoom naming the reserve", tt.name, err)
		}
	}
}

func TestRoomPassesOnAFreeSpaceError(t *testing.T) {
	boom := errors.New("boom")
	if err := Room(func(string) (uint64, error) { return 0, boom }, "/x", 1); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}
```

`internal/stage/artifact_test.go`:
```go
package stage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newExports(t *testing.T) *Stage {
	t.Helper()
	s, err := OpenDir(filepath.Join(t.TempDir(), "exports"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func writeArtifact(t *testing.T, s *Stage, name, content string) Artifact {
	t.Helper()
	f, err := s.CreateArtifact(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	a, err := f.Commit()
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestArtifactRecordsSizeAndFingerprint(t *testing.T) {
	s := newExports(t)
	a := writeArtifact(t, s, "docker/volumes/db.tar.gz", "hello")
	want := Artifact{Name: "docker/volumes/db.tar.gz", Size: 5, SHA256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"}
	if a != want {
		t.Errorf("artifact = %+v, want %+v", a, want)
	}
	info, err := os.Stat(s.Path(a.Name))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("stat = %v, %v; want mode 0600", info, err)
	}
	if err := s.CheckArtifact(a); err != nil {
		t.Errorf("CheckArtifact = %v", err)
	}
}

func TestCheckArtifactCatchesAChangedFile(t *testing.T) {
	s := newExports(t)
	a := writeArtifact(t, s, "postgres/main.sql.gz", "hello")
	if err := os.WriteFile(s.Path(a.Name), []byte("jello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckArtifact(a); !errors.Is(err, ErrArtifactChanged) {
		t.Errorf("err = %v, want ErrArtifactChanged", err)
	}
}

func TestCheckArtifactFailsForAMissingFile(t *testing.T) {
	s := newExports(t)
	if err := s.CheckArtifact(Artifact{Name: "docker/images/gone.tar", Size: 1, SHA256: "00"}); err == nil {
		t.Error("CheckArtifact passed for a missing file")
	}
}

func TestArtifactNamesStayInsideTheFolder(t *testing.T) {
	s := newExports(t)
	if _, err := s.CreateArtifact("../escape"); err == nil {
		t.Error("CreateArtifact accepted ../escape")
	}
	if err := s.CheckArtifact(Artifact{Name: "/etc/hosts"}); err == nil {
		t.Error("CheckArtifact accepted /etc/hosts")
	}
}

func TestCheckArtifactRefusesAnOpenFolder(t *testing.T) {
	s := newExports(t)
	a := writeArtifact(t, s, "x", "hello")
	if err := os.Chmod(s.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckArtifact(a); err == nil {
		t.Error("CheckArtifact trusted a folder others can read")
	}
}

func TestOpenDirCreatesAPrivateFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mtm-exports")
	if _, err := OpenDir(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("stat = %v, %v; want mode 0700", info, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/words/ ./internal/stage/`
Expected: the build fails with `undefined: Bytes`, `undefined: OpenDir`, `undefined: Room`, and `undefined: Artifact`.

- [ ] **Step 3: Implement**

`internal/words/words.go`:
```diff
--- a/internal/words/words.go
+++ b/internal/words/words.go
@@ -10,3 +10,17 @@
 	}
 	return fmt.Sprintf("%d %s", n, many)
 }
+
+// Bytes formats n in the decimal units Finder shows: "13.1 GB".
+func Bytes(n uint64) string {
+	const unit = 1000
+	if n < unit {
+		return fmt.Sprintf("%d B", n)
+	}
+	div, exp := uint64(unit), 0
+	for rest := n / unit; rest >= unit; rest /= unit {
+		div *= unit
+		exp++
+	}
+	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
+}
```

`internal/stage/stage.go`:
```diff
--- a/internal/stage/stage.go
+++ b/internal/stage/stage.go
@@ -26,7 +26,13 @@
 // Open returns the stage folder under home, creating it with mode 0700
 // when it does not exist.
 func Open(home string) (*Stage, error) {
-	root := filepath.Join(home, DirName)
+	return OpenDir(filepath.Join(home, DirName))
+}
+
+// OpenDir returns the private folder root, creating it with mode 0700 when
+// it does not exist. Exports live in one: ~/.mtm/exports, or the folder
+// prepare --stage makes on an external volume.
+func OpenDir(root string) (*Stage, error) {
 	if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
 		return nil, fmt.Errorf("create %s: %w", root, err)
 	}
```

`internal/stage/space.go`:
```go
package stage

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/momentmaker/mtm/internal/words"
)

// Reserve is the free space every export leaves on its volume, so a
// migration never fills the disk that macOS and the user's apps need.
const Reserve uint64 = 10_000_000_000

var ErrNoRoom = errors.New("not enough free space")

// Free returns the bytes available to this user on the volume holding path.
func Free(path string) (uint64, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, fmt.Errorf("check free space on %s: %w", path, err)
	}
	return fs.Bavail * uint64(fs.Bsize), nil
}

// Room fails with ErrNoRoom unless need bytes fit on path's volume with
// Reserve to spare. free is Free, or a stand-in in tests.
func Room(free func(string) (uint64, error), path string, need uint64) error {
	avail, err := free(path)
	if err != nil {
		return err
	}
	if avail < Reserve || avail-Reserve < need {
		return fmt.Errorf("%w: the exports need %s, %s has %s free, and mtm keeps %s free", ErrNoRoom, words.Bytes(need), path, words.Bytes(avail), words.Bytes(Reserve))
	}
	return nil
}
```

`internal/stage/artifact.go`:
```go
package stage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

// Artifact is an export: its path inside the exports folder, its size, and
// its SHA-256, so finish can refuse a damaged or swapped file.
type Artifact struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

var ErrArtifactChanged = errors.New("the export doesn't match the fingerprint prepare recorded")

// ArtifactFile is a private file that fingerprints what is written to it.
type ArtifactFile struct {
	file *File
	hash hash.Hash
	size int64
	name string
}

func (s *Stage) CreateArtifact(name string) (*ArtifactFile, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("export name %q leaves %s", name, s.Root)
	}
	f, err := s.Create(name)
	if err != nil {
		return nil, err
	}
	return &ArtifactFile{file: f, hash: sha256.New(), name: name}, nil
}

func (a *ArtifactFile) Write(p []byte) (int, error) {
	n, err := a.file.Write(p)
	a.hash.Write(p[:n])
	a.size += int64(n)
	return n, err
}

// Commit saves the file under its name and returns its record.
func (a *ArtifactFile) Commit() (Artifact, error) {
	if err := a.file.Commit(); err != nil {
		return Artifact{}, err
	}
	return Artifact{Name: a.name, Size: a.size, SHA256: hex.EncodeToString(a.hash.Sum(nil))}, nil
}

func (a *ArtifactFile) Abort() { a.file.Abort() }

// CheckArtifact reads a's file in full and compares it with its record. It
// first checks the exports folder is private and this user's, as it is
// before mtm reads anything from ~/.mtm.
func (s *Stage) CheckArtifact(a Artifact) error {
	if !filepath.IsLocal(a.Name) {
		return fmt.Errorf("export name %q leaves %s", a.Name, s.Root)
	}
	if err := checkSafe(s.Root); err != nil {
		return err
	}
	path := s.Path(a.Name)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open export: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return fmt.Errorf("read export %s: %w", path, err)
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return fmt.Errorf("%w: %s", ErrArtifactChanged, path)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/words/ ./internal/stage/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go test ./...
git add internal/words internal/stage
git commit -m "Add fingerprinted exports and a 10 GB free-space reserve

Every export is written privately and atomically with its SHA-256, so
finish can refuse a damaged or swapped archive, and no export may leave
less than 10 GB free on its volume."
```

---

### Task 3: The encrypted-volume check behind `prepare --stage`

**Files:**
- Create: `internal/stage/volume.go`
- Test: `internal/stage/volume_test.go`

**Interfaces:**
- Consumes: `runner.Runner`, `runner.ExitCode`, `macos.ShellQuote`.
- Produces:
  - `stage.Volume{MountPoint, Type string; IgnoresOwners bool}`.
  - `stage.VolumeOf(path string) (Volume, error)`, from `statfs`.
  - `stage.ErrUnsafeVolume`.
  - `stage.CheckExternal(ctx, runner.Runner, Volume) error`.
    - It refuses a volume that isn't APFS, or that ignores ownership; that fix is `sudo diskutil enableOwnership '<mount>'`.
    - It runs `/usr/sbin/diskutil info -plist <mount>` and pipes the report into `/usr/bin/plutil -extract Encryption raw -o - -`.
    - A missing `Encryption` key (plutil exits 1) counts as not encrypted.

- [ ] **Step 1: Write the failing tests**

`internal/stage/volume_test.go`:
```go
package stage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/momentmaker/mtm/internal/runner"
)

func TestVolumeOfATempFolder(t *testing.T) {
	vol, err := VolumeOf(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if vol.Type != "apfs" || vol.MountPoint == "" || vol.IgnoresOwners {
		t.Errorf("volume = %+v, want an APFS volume that honors owners", vol)
	}
}

const diskutilInfo = "/usr/sbin/diskutil info -plist /Volumes/Stage"
const plutilEncryption = "/usr/bin/plutil -extract Encryption raw -o - -"

func diskutilSays(encryption runner.Response) *runner.Fake {
	return &runner.Fake{Responses: map[string]runner.Response{
		diskutilInfo:     {Stdout: "<plist>report</plist>"},
		plutilEncryption: encryption,
	}}
}

func TestCheckExternal(t *testing.T) {
	apfs := Volume{MountPoint: "/Volumes/Stage", Type: "apfs"}
	tests := []struct {
		name string
		vol  Volume
		r    runner.Runner
		want string
	}{
		{"encrypted APFS", apfs, diskutilSays(runner.Response{Stdout: "true\n"}), ""},
		{"not APFS", Volume{MountPoint: "/Volumes/Stage", Type: "exfat"}, &runner.Fake{}, "is exfat"},
		{"ignores owners", Volume{MountPoint: "/Volumes/Stage", Type: "apfs", IgnoresOwners: true}, &runner.Fake{}, "sudo diskutil enableOwnership '/Volumes/Stage'"},
		{"not encrypted", apfs, diskutilSays(runner.Response{Stdout: "false\n"}), "is not encrypted"},
		{"no Encryption key", apfs, diskutilSays(runner.Response{Code: 1}), "is not encrypted"},
	}
	for _, tt := range tests {
		err := CheckExternal(context.Background(), tt.r, tt.vol)
		if tt.want == "" {
			if err != nil {
				t.Errorf("%s: err = %v, want nil", tt.name, err)
			}
			continue
		}
		if !errors.Is(err, ErrUnsafeVolume) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want ErrUnsafeVolume with %q", tt.name, err, tt.want)
		}
	}
}

func TestCheckExternalHandsDiskutilsReportToPlutil(t *testing.T) {
	f := diskutilSays(runner.Response{Stdout: "true\n"})
	if err := CheckExternal(context.Background(), f, Volume{MountPoint: "/Volumes/Stage", Type: "apfs"}); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[1].Stdin != "<plist>report</plist>" {
		t.Errorf("calls = %+v, want plutil to read diskutil's report", calls)
	}
}

func TestCheckExternalFailsWhenDiskutilFails(t *testing.T) {
	f := &runner.Fake{Responses: map[string]runner.Response{diskutilInfo: {Code: 1, Stderr: "Could not find disk"}}}
	err := CheckExternal(context.Background(), f, Volume{MountPoint: "/Volumes/Stage", Type: "apfs"})
	if err == nil || errors.Is(err, ErrUnsafeVolume) {
		t.Errorf("err = %v, want diskutil's failure", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/stage/ -run 'Volume|CheckExternal'`
Expected: the build fails with `undefined: VolumeOf` and `undefined: CheckExternal`.

- [ ] **Step 3: Implement**

`internal/stage/volume.go`:
```go
package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"

	"github.com/momentmaker/mtm/internal/macos"
	"github.com/momentmaker/mtm/internal/runner"
)

// Volume is the file system holding a folder, as statfs reports it.
type Volume struct {
	MountPoint    string
	Type          string
	IgnoresOwners bool
}

// mntIgnoreOwnership is MNT_IGNORE_OWNERSHIP from <sys/mount.h>, which Go's
// syscall package doesn't define.
const mntIgnoreOwnership = 0x00200000

var ErrUnsafeVolume = errors.New("prepare --stage needs an encrypted APFS volume that honors file ownership")

func VolumeOf(path string) (Volume, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return Volume{}, fmt.Errorf("inspect the volume holding %s: %w", path, err)
	}
	return Volume{
		MountPoint:    cString(fs.Mntonname[:]),
		Type:          cString(fs.Fstypename[:]),
		IgnoresOwners: fs.Flags&mntIgnoreOwnership != 0,
	}, nil
}

func cString(b []int8) string {
	var sb strings.Builder
	for _, c := range b {
		if c == 0 {
			break
		}
		sb.WriteByte(byte(c))
	}
	return sb.String()
}

// CheckExternal refuses a volume that would leave exports readable: one
// that isn't APFS, isn't encrypted, or ignores file ownership, which makes
// a 0700 folder open to every account. statfs can't tell encryption;
// diskutil can.
func CheckExternal(ctx context.Context, r runner.Runner, vol Volume) error {
	if vol.Type != "apfs" {
		return fmt.Errorf("%w: %s is %s. Erase a drive as APFS (Encrypted) in Disk Utility, or leave out --stage", ErrUnsafeVolume, vol.MountPoint, vol.Type)
	}
	if vol.IgnoresOwners {
		return fmt.Errorf("%w: %s ignores file ownership, so other accounts could read the exports. Run: sudo diskutil enableOwnership %s", ErrUnsafeVolume, vol.MountPoint, macos.ShellQuote(vol.MountPoint))
	}
	encrypted, err := encrypted(ctx, r, vol.MountPoint)
	if err != nil {
		return err
	}
	if !encrypted {
		return fmt.Errorf("%w: %s is not encrypted. Encrypt it (Finder: right-click the drive > Encrypt), or leave out --stage", ErrUnsafeVolume, vol.MountPoint)
	}
	return nil
}

// encrypted reads the Encryption key of diskutil's report. plutil exits 1
// when the key is absent, which diskutil does for file systems that can't
// be encrypted.
func encrypted(ctx context.Context, r runner.Runner, mount string) (bool, error) {
	info, err := r.Run(ctx, runner.Command{Name: "/usr/sbin/diskutil", Args: []string{"info", "-plist", mount}})
	if err != nil {
		return false, fmt.Errorf("ask diskutil about %s: %w", mount, err)
	}
	out, err := r.Run(ctx, runner.Command{Name: "/usr/bin/plutil", Args: []string{"-extract", "Encryption", "raw", "-o", "-", "-"}, Stdin: string(info.Stdout)})
	if code, ok := runner.ExitCode(err); ok && code == 1 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read diskutil's report on %s: %w", mount, err)
	}
	return strings.TrimSpace(string(out.Stdout)) == "true", nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/stage/`
Expected: PASS. `TestVolumeOfATempFolder` reads the real temp folder's volume, which is APFS on every supported Mac.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go test ./...
git add internal/stage
git commit -m "Check that a --stage folder is on an encrypted APFS volume

Exports can hold production-like data and secrets. Off the FileVault disk
they are only as safe as the drive, so mtm refuses one that isn't APFS,
isn't encrypted, or ignores file ownership."
```

---

### Task 4: `prepare --stage`, the exports folder for modules, `finish` flags, and cleanup

**Files:**
- Modify: `internal/manifest/manifest.go`, `internal/module/module.go`, `internal/stage/stage.go`, `internal/app/app.go`, `cmd/mtm/main.go`
- Test: `internal/app/app_test.go` (modify), `internal/app/exports_test.go` (create), `cmd/mtm/main_test.go` (modify)

**Interfaces:**
- Consumes: Task 2 (`OpenDir`, `CreateArtifact`, `CheckArtifact`, `Free`) and Task 3 (`VolumeOf`, `CheckExternal`).
- Produces:
  - `manifest.Manifest.ExportsDir string` (`exports_dir`): the `--stage` exports folder, or empty for `~/.mtm/exports`.
  - New `module.Env` fields:
    - `Exports *stage.Stage`, the exports folder; nil during scan;
    - `Free func(path string) (uint64, error)`;
    - `Force bool` and `RestoreDump bool`, which carry finish's flags of those names.
  - New `stage` names:
    - constants `stage.ExportsDirName = "exports"` and `stage.ExternalDirName = "mtm-exports"`;
    - `stage.ExistingDir(root string) (*Stage, error)`.
  - New `app` names:
    - `app.App.Free`, which defaults to `stage.Free`;
    - `app.PrepareOptions.StageDir`;
    - `app.FinishOptions{Only []string; Force, RestoreDump bool}`;
    - `(*App).Finish(ctx, FinishOptions)`, which now takes options;
    - `app.CleanupReport{Removed bool; External string; ExternalRemoved bool}`;
    - `(*App).Cleanup(force bool) (CleanupReport, error)`.
  - **Rules:**
    - `prepare --stage <abs dir>` writes exports to `<dir>/mtm-exports`, after `CheckExternal` and a Time Machine exclusion.
    - `prepare --only` refuses to move the exports to a different folder from the earlier prepare's, so one record never spans two folders.
    - `finish` and `verify` give modules the recorded folder without checking it. A module checks it when it reads an export.
    - `cleanup` deletes the recorded folder only if it is named `mtm-exports` and is private to the user.
  - **CLI:**
    - `mtm prepare --stage DIR`;
    - `mtm finish --only a,b`;
    - `--restore-dump`, only with `--only databases`;
    - `--force`, only with `--restore-dump`;
    - `--undo` rejects `--stage`.

- [ ] **Step 1: Write the failing tests**

Update the existing app tests for the new `Finish` and `Cleanup` signatures. In `internal/app/app_test.go`:
```diff
--- a/internal/app/app_test.go
+++ b/internal/app/app_test.go
@@ -282,24 +282,24 @@
 	if rep := verifyOn(t, newHome, newUUID, VerifyOptions{}); !rep.Verdict.Safe {
 		t.Fatalf("full verify not safe: %v", blockingIDs(rep))
 	}
-	removed, err := a.Cleanup(false)
-	if err != nil || !removed {
-		t.Fatalf("cleanup after SAFE: %v, %v", removed, err)
+	rep, err := a.Cleanup(false)
+	if err != nil || !rep.Removed {
+		t.Fatalf("cleanup after SAFE: %+v, %v", rep, err)
 	}
 	if _, err := os.Stat(filepath.Join(newHome, stage.DirName)); !errors.Is(err, fs.ErrNotExist) {
 		t.Error("~/.mtm still exists")
 	}
-	if removed, err := a.Cleanup(false); err != nil || removed {
-		t.Errorf("second cleanup = %v, %v; want nothing to remove", removed, err)
+	if rep, err := a.Cleanup(false); err != nil || rep.Removed {
+		t.Errorf("second cleanup = %+v, %v; want nothing to remove", rep, err)
 	}
 }
 
 func TestCleanupForce(t *testing.T) {
 	home := seedHome(t)
 	prepareOld(t, home)
-	removed, err := newTestApp(home, fakeMac(home, oldUUID)).Cleanup(true)
-	if err != nil || !removed {
-		t.Errorf("forced cleanup = %v, %v", removed, err)
+	rep, err := newTestApp(home, fakeMac(home, oldUUID)).Cleanup(true)
+	if err != nil || !rep.Removed {
+		t.Errorf("forced cleanup = %+v, %v", rep, err)
 	}
 }
 
@@ -505,7 +505,7 @@
 	mustPrepare(t, stepApp(old, oldUUID, step))
 	newHome := migrate(t, old)
 	unrecorded := stepModule{name: "extra", calls: &calls}
-	rep, err := stepApp(newHome, newUUID, step, unrecorded).Finish(ctx)
+	rep, err := stepApp(newHome, newUUID, step, unrecorded).Finish(ctx, FinishOptions{})
 	if err != nil {
 		t.Fatal(err)
 	}
@@ -522,7 +522,7 @@
 	var calls []string
 	a := stepApp(home, oldUUID, stepModule{name: "svc", calls: &calls})
 	mustPrepare(t, a)
-	if _, err := a.Finish(ctx); !errors.Is(err, ErrFinishOnOldMac) {
+	if _, err := a.Finish(ctx, FinishOptions{}); !errors.Is(err, ErrFinishOnOldMac) {
 		t.Errorf("err = %v, want ErrFinishOnOldMac", err)
 	}
 	if slices.Contains(calls, "finish") {
@@ -538,7 +538,7 @@
 	newHome := migrate(t, old)
 	a := stepApp(newHome, newUUID, step)
 	a.Runner.(*runner.Fake).Responses["/usr/bin/fdesetup status"] = runner.Response{Stdout: "FileVault is Off.\n"}
-	rep, err := a.Finish(ctx)
+	rep, err := a.Finish(ctx, FinishOptions{})
 	if err != nil || !module.HasBlocker(rep.Problems) {
 		t.Errorf("Finish = %+v, %v; want a FileVault blocker", rep, err)
 	}
```

`internal/app/exports_test.go`:
```go
package app

import (
	"errors"
	"io/fs"
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
)

// exportModule writes an export in prepare and checks it in finish, as the
// docker and databases modules do. It keeps the Env finish gave it.
type exportModule struct{ seen *module.Env }

func (exportModule) Name() string                              { return "exp" }
func (exportModule) Scan(module.Env) ([]module.Finding, error) { return nil, nil }

func (exportModule) Prepare(env module.Env, _ *stage.Stage) (module.Result, error) {
	f, err := env.Exports.CreateArtifact("exp/data")
	if err != nil {
		return module.Result{}, err
	}
	if _, err := f.Write([]byte("data")); err != nil {
		f.Abort()
		return module.Result{}, err
	}
	a, err := f.Commit()
	if err != nil {
		return module.Result{}, err
	}
	return module.Result{Inventory: a}, nil
}

func (m exportModule) Finish(env module.Env, _ *stage.Stage) (module.Result, error) {
	*m.seen = env
	var a stage.Artifact
	if _, err := module.LoadInventory(env, "exp", &a); err != nil {
		return module.Result{}, err
	}
	if err := env.Exports.CheckArtifact(a); err != nil {
		return module.Result{}, err
	}
	return module.Result{Done: []string{"checked the export"}}, nil
}

func (exportModule) Verify(module.Env, *stage.Stage) ([]module.Check, error) { return nil, nil }

// stagingMac is fakeMac with diskutil answering for the volume holding dir
// and Time Machine accepting the exclusion of its exports folder.
func stagingMac(t *testing.T, home, dir, encrypted string) *runner.Fake {
	t.Helper()
	vol, err := stage.VolumeOf(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := fakeMac(home, oldUUID)
	f.Responses["/usr/sbin/diskutil info -plist "+vol.MountPoint] = runner.Response{Stdout: "<plist/>"}
	f.Responses["/usr/bin/plutil -extract Encryption raw -o - -"] = runner.Response{Stdout: encrypted + "\n"}
	f.Responses["/usr/bin/tmutil addexclusion "+filepath.Join(dir, stage.ExternalDirName)] = runner.Response{}
	return f
}

func loadManifest(t *testing.T, home string) *manifest.Manifest {
	t.Helper()
	st, err := stage.Existing(home)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(st)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPrepareStageRefusesAnUnencryptedVolume(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	var seen module.Env
	a := &App{Runner: stagingMac(t, home, dir, "false"), Home: home, Version: "test", Now: time.Now, Modules: []module.Module{exportModule{&seen}}}
	rep, err := a.Prepare(ctx, PrepareOptions{StageDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ready || !slices.ContainsFunc(rep.Problems, func(f module.Finding) bool {
		return f.ID == "exports" && strings.Contains(f.Detail, "not encrypted")
	}) {
		t.Errorf("report = %+v, want NOT READY naming the unencrypted volume", rep)
	}
	if _, err := os.Stat(filepath.Join(dir, stage.ExternalDirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("prepare made an exports folder on an unencrypted volume")
	}
}

func TestPrepareStageWritesExportsToThePrivateFolderOnTheVolume(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	var seen module.Env
	a := &App{Runner: stagingMac(t, home, dir, "true"), Home: home, Version: "test", Now: time.Now, Modules: []module.Module{exportModule{&seen}}}
	rep, err := a.Prepare(ctx, PrepareOptions{StageDir: dir})
	if err != nil || !rep.Ready {
		t.Fatalf("prepare = %+v, %v", rep, err)
	}
	want := filepath.Join(dir, stage.ExternalDirName)
	if got := loadManifest(t, home).ExportsDir; got != want {
		t.Errorf("ExportsDir = %q, want %q", got, want)
	}
	info, err := os.Stat(filepath.Join(want, "exp", "data"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("export = %v, %v; want a 0600 file in %s", info, err, want)
	}
}

func TestPrepareStageNeedsAFullPath(t *testing.T) {
	home := t.TempDir()
	var seen module.Env
	a := &App{Runner: fakeMac(home, oldUUID), Home: home, Version: "test", Now: time.Now, Modules: []module.Module{exportModule{&seen}}}
	rep, err := a.Prepare(ctx, PrepareOptions{StageDir: "Volumes/X"})
	if err != nil || rep.Ready || !strings.Contains(rep.Problems[0].Detail, "full path") {
		t.Errorf("prepare = %+v, %v; want a blocker asking for a full path", rep, err)
	}
}

func TestPrepareOnlyKeepsExportsInOneFolder(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	var seen module.Env
	a := &App{Runner: stagingMac(t, home, dir, "true"), Home: home, Version: "test", Now: time.Now, Modules: []module.Module{exportModule{&seen}}}
	if rep, err := a.Prepare(ctx, PrepareOptions{StageDir: dir}); err != nil || !rep.Ready {
		t.Fatalf("prepare --stage = %+v, %v", rep, err)
	}
	rep, err := a.Prepare(ctx, PrepareOptions{Only: []string{"exp"}, StageDir: dir})
	if err != nil || !rep.Ready {
		t.Fatalf("prepare --only with the same --stage = %+v, %v", rep, err)
	}
	rep, err = a.Prepare(ctx, PrepareOptions{Only: []string{"exp"}})
	if err != nil || rep.Ready || rep.Problems[0].ID != "exports" {
		t.Errorf("prepare --only without --stage = %+v, %v; want a blocker", rep, err)
	}
}

func TestFinishFindsTheExportsPrepareWrote(t *testing.T) {
	old := t.TempDir()
	var seen module.Env
	mod := exportModule{&seen}
	mustPrepare(t, stepApp(old, oldUUID, mod))
	newHome := migrate(t, old)
	rep, err := stepApp(newHome, newUUID, mod).Finish(ctx, FinishOptions{})
	if err != nil || !slices.Equal(rep.Done, []string{"exp: checked the export"}) {
		t.Errorf("finish = %+v, %v", rep, err)
	}
	if want := filepath.Join(newHome, stage.DirName, stage.ExportsDirName); seen.Exports == nil || seen.Exports.Root != want {
		t.Errorf("Exports = %+v, want %s", seen.Exports, want)
	}
}

func TestFinishOnlyRunsTheNamedModulesWithTheirFlags(t *testing.T) {
	old := t.TempDir()
	var seen module.Env
	var calls []string
	exp, step := exportModule{&seen}, stepModule{name: "svc", calls: &calls}
	mustPrepare(t, stepApp(old, oldUUID, exp, step))
	newHome := migrate(t, old)
	_, err := stepApp(newHome, newUUID, exp, step).Finish(ctx, FinishOptions{Only: []string{"exp"}, Force: true, RestoreDump: true})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(calls, "finish") {
		t.Error("finish --only exp also finished svc")
	}
	if !seen.Force || !seen.RestoreDump {
		t.Errorf("env = %+v, want Force and RestoreDump", seen)
	}
}

func TestFinishRejectsAnUnknownModule(t *testing.T) {
	if _, err := stepApp(t.TempDir(), newUUID).Finish(ctx, FinishOptions{Only: []string{"nope"}}); err == nil {
		t.Error("finish --only nope succeeded")
	}
}

func stagedRecord(t *testing.T, home, exportsDir string) {
	t.Helper()
	st, err := stage.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest.New("test")
	m.MachineID, m.ExportsDir = "x", exportsDir
	if err := manifest.Save(st, m); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupDeletesTheStageFolder(t *testing.T) {
	home := t.TempDir()
	external, err := stage.OpenDir(filepath.Join(t.TempDir(), stage.ExternalDirName))
	if err != nil {
		t.Fatal(err)
	}
	stagedRecord(t, home, external.Root)
	rep, err := newTestApp(home, fakeMac(home, newUUID)).Cleanup(true)
	if err != nil || !rep.Removed || !rep.ExternalRemoved || rep.External != external.Root {
		t.Fatalf("cleanup = %+v, %v", rep, err)
	}
	if _, err := os.Stat(external.Root); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the --stage folder still exists")
	}
}

func TestCleanupWithTheStageDriveDetached(t *testing.T) {
	home := t.TempDir()
	gone := filepath.Join(t.TempDir(), "detached", stage.ExternalDirName)
	stagedRecord(t, home, gone)
	rep, err := newTestApp(home, fakeMac(home, newUUID)).Cleanup(true)
	if err != nil || !rep.Removed || rep.ExternalRemoved || rep.External != gone {
		t.Errorf("cleanup = %+v, %v; want ~/.mtm deleted and the detached folder named", rep, err)
	}
}

func TestCleanupDeletesOnlyAFolderMtmMade(t *testing.T) {
	home := t.TempDir()
	other, err := stage.OpenDir(filepath.Join(t.TempDir(), "Documents"))
	if err != nil {
		t.Fatal(err)
	}
	stagedRecord(t, home, other.Root)
	if _, err := newTestApp(home, fakeMac(home, newUUID)).Cleanup(true); err == nil {
		t.Error("cleanup accepted a folder not named mtm-exports")
	}
	if _, err := os.Stat(other.Root); err != nil {
		t.Errorf("cleanup deleted %s: %v", other.Root, err)
	}
}
```

In `cmd/mtm/main_test.go`, add the `manifest` import and two tests:
```diff
--- a/cmd/mtm/main_test.go
+++ b/cmd/mtm/main_test.go
@@ -12,6 +12,7 @@
 	"time"
 
 	"github.com/momentmaker/mtm/internal/app"
+	"github.com/momentmaker/mtm/internal/manifest"
 	"github.com/momentmaker/mtm/internal/module"
 	"github.com/momentmaker/mtm/internal/modules/files"
 	"github.com/momentmaker/mtm/internal/modules/git"
@@ -250,3 +251,43 @@
 		t.Errorf("modules = %q, want %q", names, want)
 	}
 }
+
+func TestFinishFlagRules(t *testing.T) {
+	tests := []struct {
+		args []string
+		want string
+	}{
+		{[]string{"finish", "--restore-dump"}, "--restore-dump works only with --only databases"},
+		{[]string{"finish", "--only", "docker", "--restore-dump"}, "--restore-dump works only with --only databases"},
+		{[]string{"finish", "--only", "databases,docker", "--restore-dump"}, "--restore-dump works only with --only databases"},
+		{[]string{"finish", "--force"}, "--force works only with --restore-dump"},
+		{[]string{"prepare", "--undo", "--stage", "/Volumes/X"}, "can't be combined"},
+	}
+	for _, tt := range tests {
+		t.Setenv("HOME", t.TempDir())
+		var out, errOut bytes.Buffer
+		code := run(ctx, tt.args, &out, &errOut, fileVaultOnApp)
+		if code != exitFailed || !strings.Contains(errOut.String(), tt.want) {
+			t.Errorf("%q: code %d stderr %q, want %d and %q", tt.args, code, errOut.String(), exitFailed, tt.want)
+		}
+	}
+}
+
+func TestCleanupNamesADetachedStageFolder(t *testing.T) {
+	home := t.TempDir()
+	t.Setenv("HOME", home)
+	st, err := stage.Open(home)
+	if err != nil {
+		t.Fatal(err)
+	}
+	m := manifest.New("test")
+	m.MachineID, m.ExportsDir = "x", "/Volumes/Gone/"+stage.ExternalDirName
+	if err := manifest.Save(st, m); err != nil {
+		t.Fatal(err)
+	}
+	var out, errOut bytes.Buffer
+	code := run(ctx, []string{"cleanup", "--force"}, &out, &errOut, fileVaultOnApp)
+	if code != exitOK || !strings.Contains(out.String(), "Also delete /Volumes/Gone/mtm-exports") {
+		t.Errorf("code %d out %q", code, out.String())
+	}
+}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/ ./cmd/mtm/`
Expected: the build fails with `undefined: FinishOptions`, `undefined: stage.ExternalDirName`, and `m.ExportsDir undefined`.

- [ ] **Step 3: Implement**

`internal/manifest/manifest.go`:
```diff
--- a/internal/manifest/manifest.go
+++ b/internal/manifest/manifest.go
@@ -25,14 +25,17 @@
 )
 
 type Manifest struct {
-	Schema       int                     `json:"schema"`
-	MtmVersion   string                  `json:"mtm_version"`
-	MacOSVersion string                  `json:"macos_version"`
-	MacOSBuild   string                  `json:"macos_build"`
-	Hostname     string                  `json:"hostname"`
-	MachineID    string                  `json:"machine_id"`
-	PreparedAt   time.Time               `json:"prepared_at"`
-	Modules      map[string]ModuleRecord `json:"modules"`
+	Schema       int       `json:"schema"`
+	MtmVersion   string    `json:"mtm_version"`
+	MacOSVersion string    `json:"macos_version"`
+	MacOSBuild   string    `json:"macos_build"`
+	Hostname     string    `json:"hostname"`
+	MachineID    string    `json:"machine_id"`
+	PreparedAt   time.Time `json:"prepared_at"`
+	// ExportsDir is the folder prepare --stage wrote exports to. Empty
+	// means ~/.mtm/exports.
+	ExportsDir string                  `json:"exports_dir,omitempty"`
+	Modules    map[string]ModuleRecord `json:"modules"`
 }
 
 type ModuleRecord struct {
```

`internal/module/module.go`:
```diff
--- a/internal/module/module.go
+++ b/internal/module/module.go
@@ -22,13 +22,20 @@
 // Env is what a module gets to work with. Manifest is the record: on the
 // new Mac, the one verify and finish check against; during prepare, this
 // Mac's previous record (empty on the first run); nil during scan.
+// Exports is the folder export archives go in, nil during scan. Free
+// reports a volume's free space. Force and RestoreDump carry finish's
+// flags of those names.
 type Env struct {
-	Ctx      context.Context
-	Run      runner.Runner
-	Home     string
-	Machine  Machine
-	Deep     bool
-	Manifest *manifest.Manifest
+	Ctx         context.Context
+	Run         runner.Runner
+	Home        string
+	Machine     Machine
+	Deep        bool
+	Manifest    *manifest.Manifest
+	Exports     *stage.Stage
+	Free        func(path string) (uint64, error)
+	Force       bool
+	RestoreDump bool
 }
 
 type Severity string
```

`internal/stage/stage.go`:
```diff
--- a/internal/stage/stage.go
+++ b/internal/stage/stage.go
@@ -15,7 +15,14 @@
 	"github.com/momentmaker/mtm/internal/runner"
 )
 
-const DirName = ".mtm"
+const (
+	DirName = ".mtm"
+	// ExportsDirName is the exports folder inside ~/.mtm.
+	ExportsDirName = "exports"
+	// ExternalDirName is the exports folder prepare --stage makes on an
+	// external volume. cleanup deletes only a folder of this name.
+	ExternalDirName = "mtm-exports"
+)
 
 var ErrNotFound = errors.New("no ~/.mtm folder")
 
@@ -48,6 +55,12 @@
 	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
 		return nil, fmt.Errorf("%s: %w; run mtm prepare on the old Mac first", root, ErrNotFound)
 	}
+	return ExistingDir(root)
+}
+
+// ExistingDir returns the folder root if it exists and is private to this
+// user.
+func ExistingDir(root string) (*Stage, error) {
 	if err := checkSafe(root); err != nil {
 		return nil, err
 	}
```

`internal/app/app.go`:
```diff
--- a/internal/app/app.go
+++ b/internal/app/app.go
@@ -9,6 +9,7 @@
 	"io/fs"
 	"maps"
 	"os"
+	"path/filepath"
 	"slices"
 	"strings"
 	"time"
@@ -29,21 +30,27 @@
 	ErrNotVerified     = errors.New("mtm verify has not reported SAFE TO WIPE on this Mac; run mtm verify first, or pass --force to delete ~/.mtm anyway")
 )
 
+// App runs mtm's commands. Free reports a volume's free space; nil means
+// stage.Free.
 type App struct {
 	Runner  runner.Runner
 	Home    string
 	Version string
 	Now     func() time.Time
 	Modules []module.Module
+	Free    func(path string) (uint64, error)
 }
 
 type ScanReport struct {
 	Findings []module.Finding `json:"findings"`
 }
 
+// PrepareOptions are prepare's flags. StageDir is the --stage folder on an
+// external volume; empty keeps exports in ~/.mtm/exports.
 type PrepareOptions struct {
-	Only []string
-	Deep bool
+	Only     []string
+	Deep     bool
+	StageDir string
 }
 
 type PrepareReport struct {
@@ -62,6 +69,21 @@
 	Verdict verdict.Verdict `json:"verdict"`
 }
 
+type FinishOptions struct {
+	Only        []string
+	Force       bool
+	RestoreDump bool
+}
+
+// CleanupReport says what cleanup deleted. External is the prepare --stage
+// folder, if prepare used one; ExternalRemoved is false when its drive
+// isn't attached.
+type CleanupReport struct {
+	Removed         bool
+	External        string
+	ExternalRemoved bool
+}
+
 // StepReport is what finish or prepare --undo did, and what is left to do
 // by hand.
 type StepReport struct {
@@ -80,7 +102,7 @@
 // handle. It changes nothing on disk.
 func (a *App) Scan(ctx context.Context) ScanReport {
 	findings := preflight.Check(ctx, a.Runner)
-	env := a.env(ctx, module.OldMac, false, nil, a.Runner)
+	env := a.env(ctx, module.OldMac, false, nil, a.Runner, nil)
 	for _, m := range a.Modules {
 		found, err := m.Scan(env)
 		if err != nil {
@@ -132,12 +154,21 @@
 	case m.MachineID != info.MachineID:
 		return rep, fmt.Errorf("%w (recorded %s). If this is the new Mac, run mtm verify instead", ErrOtherMacsRecord, m.PreparedAt.Local().Format(time.RFC1123))
 	}
+	exports, problem := a.prepareExports(ctx, run, st, m, opts)
+	if problem != nil {
+		rep.Problems = append(rep.Problems, *problem)
+		return rep, nil
+	}
 	m.MtmVersion = a.Version
 	m.MacOSVersion, m.MacOSBuild, m.MachineID = info.MacOSVersion, info.MacOSBuild, info.MachineID
 	m.Hostname, _ = os.Hostname()
+	m.ExportsDir = ""
+	if opts.StageDir != "" {
+		m.ExportsDir = exports.Root
+	}
 
 	startedAt := a.Now()
-	env := a.env(ctx, module.OldMac, opts.Deep, m, run)
+	env := a.env(ctx, module.OldMac, opts.Deep, m, run, exports)
 	for _, mod := range mods {
 		res, err := mod.Prepare(env, st)
 		if err != nil {
@@ -204,7 +235,7 @@
 		})
 	}
 
-	env := a.env(ctx, module.NewMac, opts.Deep, m, run)
+	env := a.env(ctx, module.NewMac, opts.Deep, m, run, exportsFor(st, m))
 	for _, mod := range mods {
 		if _, ok := m.Modules[mod.Name()]; !ok {
 			checks = append(checks, module.Check{
@@ -242,7 +273,11 @@
 
 // Finish restores, on the new Mac, what prepare set aside on the old one.
 // It refuses on the old Mac, where it would restart what prepare stopped.
-func (a *App) Finish(ctx context.Context) (StepReport, error) {
+func (a *App) Finish(ctx context.Context, opts FinishOptions) (StepReport, error) {
+	mods, err := a.selectModules(opts.Only)
+	if err != nil {
+		return StepReport{}, err
+	}
 	if problems := preflight.Check(ctx, a.Runner); module.HasBlocker(problems) {
 		return StepReport{Problems: problems}, nil
 	}
@@ -266,9 +301,10 @@
 	if info.MachineID == m.MachineID {
 		return StepReport{}, ErrFinishOnOldMac
 	}
-	env := a.env(ctx, module.NewMac, false, m, run)
+	env := a.env(ctx, module.NewMac, false, m, run, exportsFor(st, m))
+	env.Force, env.RestoreDump = opts.Force, opts.RestoreDump
 	var rep StepReport
-	for _, mod := range a.Modules {
+	for _, mod := range mods {
 		if _, ok := m.Modules[mod.Name()]; !ok {
 			continue
 		}
@@ -306,7 +342,7 @@
 	if info.MachineID != m.MachineID {
 		return StepReport{}, ErrUndoOnNewMac
 	}
-	env := a.env(ctx, module.OldMac, false, m, run)
+	env := a.env(ctx, module.OldMac, false, m, run, exportsFor(st, m))
 	var rep StepReport
 	for _, mod := range a.Modules {
 		u, ok := mod.(module.Undoer)
@@ -339,31 +375,131 @@
 	return m, err
 }
 
-// Cleanup deletes ~/.mtm. Without force, it requires a SAFE TO WIPE result
-// from a full verify on this Mac.
-func (a *App) Cleanup(force bool) (bool, error) {
+// Cleanup deletes ~/.mtm, and the prepare --stage folder when its drive is
+// attached. Without force, it requires a SAFE TO WIPE result from a full
+// verify on this Mac.
+func (a *App) Cleanup(force bool) (CleanupReport, error) {
 	st, err := stage.Existing(a.Home)
 	if errors.Is(err, stage.ErrNotFound) {
-		return false, nil
+		return CleanupReport{}, nil
 	}
 	if err != nil {
-		return false, err
+		return CleanupReport{}, err
 	}
 	if !force {
 		s, err := state.Load(st)
 		if err != nil {
-			return false, err
+			return CleanupReport{}, err
 		}
 		if s.LastVerify == nil || !s.LastVerify.Safe {
-			return false, ErrNotVerified
+			return CleanupReport{}, ErrNotVerified
 		}
 	}
+	var rep CleanupReport
+	// A damaged record can't name a --stage folder; with --force, ~/.mtm
+	// still goes.
+	if m, err := manifest.Load(st); err == nil && m.ExportsDir != "" {
+		rep.External = m.ExportsDir
+		if rep.ExternalRemoved, err = removeExternal(m.ExportsDir); err != nil {
+			return rep, err
+		}
+	}
 	if err := st.Remove(); err != nil {
+		return rep, err
+	}
+	rep.Removed = true
+	return rep, nil
+}
+
+// removeExternal deletes the prepare --stage folder if its drive is
+// attached. It deletes only a private folder named mtm-exports, so a
+// damaged record can't point cleanup at other data.
+func removeExternal(root string) (bool, error) {
+	if filepath.Base(root) != stage.ExternalDirName {
+		return false, fmt.Errorf("the record names %s as the exports folder, which mtm didn't make; delete it yourself if it holds only exports", root)
+	}
+	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
+		return false, nil
+	}
+	exports, err := stage.ExistingDir(root)
+	if err != nil {
 		return false, err
 	}
+	if err := exports.Remove(); err != nil {
+		return false, err
+	}
 	return true, nil
 }
 
+// prepareExports opens the folder this run's exports go in. A problem ends
+// prepare before any module runs: an unsafe --stage volume, or a partial
+// run that would scatter one record's exports across two folders.
+func (a *App) prepareExports(ctx context.Context, run runner.Runner, st *stage.Stage, m *manifest.Manifest, opts PrepareOptions) (*stage.Stage, *module.Finding) {
+	want := ""
+	if opts.StageDir != "" {
+		want = filepath.Join(opts.StageDir, stage.ExternalDirName)
+	}
+	if len(opts.Only) > 0 && len(m.Modules) > 0 && m.ExportsDir != want {
+		return nil, &module.Finding{
+			Module: "stage", ID: "exports", Severity: module.SeverityBlocker, Handler: module.Manual,
+			Title:  "prepare --only must keep exports where the earlier prepare put them",
+			Detail: fmt.Sprintf("the earlier prepare used %s; this one would use %s", exportsName(m.ExportsDir), exportsName(want)),
+			Fix:    "Pass the same --stage as before, or run mtm prepare without --only.",
+		}
+	}
+	exports, err := a.openExports(ctx, run, st, opts.StageDir)
+	if err != nil {
+		return nil, &module.Finding{
+			Module: "stage", ID: "exports", Severity: module.SeverityBlocker, Handler: module.Manual,
+			Title:  "Could not use the folder for exports",
+			Detail: err.Error(),
+			Fix:    "Fix the cause above and run mtm prepare again.",
+		}
+	}
+	return exports, nil
+}
+
+func (a *App) openExports(ctx context.Context, run runner.Runner, st *stage.Stage, stageDir string) (*stage.Stage, error) {
+	if stageDir == "" {
+		return stage.OpenDir(st.Path(stage.ExportsDirName))
+	}
+	if !filepath.IsAbs(stageDir) {
+		return nil, fmt.Errorf("--stage needs a full path, such as /Volumes/Backup, not %s", stageDir)
+	}
+	vol, err := stage.VolumeOf(stageDir)
+	if err != nil {
+		return nil, err
+	}
+	if err := stage.CheckExternal(ctx, run, vol); err != nil {
+		return nil, err
+	}
+	exports, err := stage.OpenDir(filepath.Join(stageDir, stage.ExternalDirName))
+	if err != nil {
+		return nil, err
+	}
+	if err := exports.ExcludeFromBackups(ctx, run); err != nil {
+		return nil, err
+	}
+	return exports, nil
+}
+
+func exportsName(dir string) string {
+	if dir == "" {
+		return "~/.mtm/exports"
+	}
+	return dir
+}
+
+// exportsFor is the folder prepare wrote exports to. Nothing is checked
+// here: a module checks the folder when it reads an export, so a detached
+// --stage drive fails only the restores that need it.
+func exportsFor(st *stage.Stage, m *manifest.Manifest) *stage.Stage {
+	if m.ExportsDir != "" {
+		return &stage.Stage{Root: m.ExportsDir}
+	}
+	return &stage.Stage{Root: st.Path(stage.ExportsDirName)}
+}
+
 // unknownModules fails each module in the record that this mtm can't
 // verify. A newer mtm recorded data this one would otherwise skip silently.
 func (a *App) unknownModules(m *manifest.Manifest) []module.Check {
@@ -381,8 +517,12 @@
 	return checks
 }
 
-func (a *App) env(ctx context.Context, machine module.Machine, deep bool, m *manifest.Manifest, run runner.Runner) module.Env {
-	return module.Env{Ctx: ctx, Run: run, Home: a.Home, Machine: machine, Deep: deep, Manifest: m}
+func (a *App) env(ctx context.Context, machine module.Machine, deep bool, m *manifest.Manifest, run runner.Runner, exports *stage.Stage) module.Env {
+	free := a.Free
+	if free == nil {
+		free = stage.Free
+	}
+	return module.Env{Ctx: ctx, Run: run, Home: a.Home, Machine: machine, Deep: deep, Manifest: m, Exports: exports, Free: free}
 }
 
 func (a *App) selectModules(only []string) ([]module.Module, error) {
```

`cmd/mtm/main.go`:
```diff
--- a/cmd/mtm/main.go
+++ b/cmd/mtm/main.go
@@ -37,9 +37,13 @@
 
 Usage:
   mtm scan [--json]                         check this Mac before migrating (changes nothing)
-  mtm prepare [--only a,b] [--deep]         record this Mac before Migration Assistant
+  mtm prepare [--only a,b] [--deep] [--stage /Volumes/X]
+                                            record this Mac before Migration Assistant;
+                                            --stage puts exports on an encrypted drive
   mtm prepare --undo                        put this Mac back in use if you call off the migration
-  mtm finish                                on the new Mac: restore what prepare set aside
+  mtm finish [--only a,b]                   on the new Mac: restore what prepare set aside
+  mtm finish --only databases --restore-dump [--force]
+                                            load the Postgres safety-net dump
   mtm verify [--only a,b] [--deep] [--details] [--json]
                                             check the new Mac against the record
   mtm cleanup [--force]                     delete ~/.mtm after SAFE TO WIPE
@@ -138,17 +142,18 @@
 	only := flags.String("only", "", "comma-separated modules to prepare")
 	deep := flags.Bool("deep", false, "fingerprint every file (slow)")
 	undo := flags.Bool("undo", false, "put this Mac back in use: restart what prepare stopped")
+	stageDir := flags.String("stage", "", "write exports to this folder on an encrypted APFS drive")
 	if err := flags.Parse(args); err != nil {
 		return exitFailed
 	}
 	if *undo {
-		if *only != "" || *deep {
-			fmt.Fprintln(stderr, "mtm prepare: --undo can't be combined with --only or --deep")
+		if *only != "" || *deep || *stageDir != "" {
+			fmt.Fprintln(stderr, "mtm prepare: --undo can't be combined with --only, --deep, or --stage")
 			return exitFailed
 		}
 		return runUndo(ctx, a, stdout, stderr)
 	}
-	rep, err := a.Prepare(ctx, app.PrepareOptions{Only: splitList(*only), Deep: *deep})
+	rep, err := a.Prepare(ctx, app.PrepareOptions{Only: splitList(*only), Deep: *deep, StageDir: *stageDir})
 	if err != nil {
 		fmt.Fprintln(stderr, "mtm prepare:", err)
 		return exitFailed
@@ -185,10 +190,22 @@
 func cmdFinish(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
 	flags := flag.NewFlagSet("finish", flag.ContinueOnError)
 	flags.SetOutput(stderr)
+	only := flags.String("only", "", "comma-separated modules to finish")
+	restoreDump := flags.Bool("restore-dump", false, "load the Postgres dump prepare saved (with --only databases)")
+	force := flags.Bool("force", false, "with --restore-dump: restore even into a cluster that has databases")
 	if err := flags.Parse(args); err != nil {
 		return exitFailed
 	}
-	rep, err := a.Finish(ctx)
+	mods := splitList(*only)
+	if *restoreDump && (len(mods) != 1 || mods[0] != "databases") {
+		fmt.Fprintln(stderr, "mtm finish: --restore-dump works only with --only databases")
+		return exitFailed
+	}
+	if *force && !*restoreDump {
+		fmt.Fprintln(stderr, "mtm finish: --force works only with --restore-dump")
+		return exitFailed
+	}
+	rep, err := a.Finish(ctx, app.FinishOptions{Only: mods, Force: *force, RestoreDump: *restoreDump})
 	switch {
 	case errors.Is(err, app.ErrFinishOnOldMac):
 		fmt.Fprintln(stderr, "mtm finish:", err)
@@ -240,7 +257,7 @@
 	if err := flags.Parse(args); err != nil {
 		return exitFailed
 	}
-	removed, err := a.Cleanup(*force)
+	rep, err := a.Cleanup(*force)
 	switch {
 	case errors.Is(err, app.ErrNotVerified):
 		fmt.Fprintln(stderr, "mtm cleanup:", err)
@@ -248,10 +265,16 @@
 	case err != nil:
 		fmt.Fprintln(stderr, "mtm cleanup:", err)
 		return exitFailed
-	case !removed:
+	case !rep.Removed:
 		fmt.Fprintln(stdout, "Nothing to clean up: ~/.mtm does not exist.")
 		return exitOK
 	}
+	switch {
+	case rep.ExternalRemoved:
+		fmt.Fprintf(stdout, "Deleted %s.\n", rep.External)
+	case rep.External != "":
+		fmt.Fprintf(stdout, "Also delete %s, the exports prepare --stage wrote, when that drive is attached.\n", rep.External)
+	}
 	fmt.Fprintln(stdout, "Deleted ~/.mtm.")
 	fmt.Fprintln(stdout, "If you gave your terminal Full Disk Access for mtm, remove it now:")
 	fmt.Fprintln(stdout, "System Settings > Privacy & Security > Full Disk Access.")
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/app/ ./cmd/mtm/ ./internal/stage/`
Expected: PASS. The `--stage` tests answer `diskutil`, `plutil`, and `tmutil` from the fake. Their real temp folder only supplies its mount point through `statfs`.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go test ./...
git add internal/manifest internal/module internal/stage internal/app cmd/mtm
git commit -m "Add prepare --stage, an exports folder for modules, and finish flags

Modules get one exports folder: ~/.mtm/exports, or <drive>/mtm-exports
with --stage. The record names it, so finish finds it on the new Mac, and
cleanup deletes it when that drive is attached. finish gains --only, and
--restore-dump with --force for the databases module."
```

---

### Task 5: The volume content digest

**Files:**
- Create: `internal/modules/docker/digest.go`
- Test: `internal/modules/docker/digest_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `docker.NewDigest() *Digest`. `*Digest` is an `io.Writer`, fed the tar stream `docker container cp <id>:/vol -` writes.
  - `(*Digest).Finish() (sum string, entries int, err error)`, where `sum` is `sha256:<hex>`.
    - `entries` counts entries below the root `vol`; 0 means the volume is empty.
  - The digest covers one line per entry, sorted by path: quoted relative path, type flag, permission bits (octal), uid, gid, size, quoted link target, and content SHA-256. Modification times are left out.
  - Entries outside `vol/` are an error. Docker pads the stream past its end marker, and that padding is drained.
  - Test helpers used by later tasks:
    - `entry`;
    - `volumeTar(t, entries...) []byte`;
    - the fixtures `root`, `version`, `sub`, `table`, and `link`;
    - `digestOf(t, archive) (string, int)`.

- [ ] **Step 1: Write the failing tests**

`internal/modules/docker/digest_test.go`:
```go
package docker

import (
	"archive/tar"
	"bytes"
	"strings"
	"testing"
)

type entry struct {
	name    string
	typ     byte
	mode    int64
	uid     int
	content string
	link    string
}

// volumeTar builds an archive shaped like docker container cp's output for
// /vol: a "vol" folder entry, then entries under "vol/".
func volumeTar(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: e.mode, Uid: e.uid, Gid: e.uid, Linkname: e.link}
		if e.typ == tar.TypeReg {
			hdr.Size = int64(len(e.content))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var (
	root    = entry{name: "vol", typ: tar.TypeDir, mode: 0o700, uid: 70}
	version = entry{name: "vol/PG_VERSION", typ: tar.TypeReg, mode: 0o600, uid: 70, content: "16\n"}
	sub     = entry{name: "vol/base", typ: tar.TypeDir, mode: 0o700, uid: 70}
	table   = entry{name: "vol/base/1", typ: tar.TypeReg, mode: 0o600, uid: 70, content: "rows"}
	link    = entry{name: "vol/latest", typ: tar.TypeSymlink, mode: 0o777, uid: 70, link: "PG_VERSION"}
)

func digestOf(t *testing.T, archive []byte) (string, int) {
	t.Helper()
	d := NewDigest()
	if _, err := d.Write(archive); err != nil {
		t.Fatal(err)
	}
	sum, n, err := d.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return sum, n
}

func TestDigestIgnoresArchiveOrder(t *testing.T) {
	a, n := digestOf(t, volumeTar(t, root, version, sub, table, link))
	b, _ := digestOf(t, volumeTar(t, root, link, sub, table, version))
	if a != b || !strings.HasPrefix(a, "sha256:") || n != 4 {
		t.Errorf("digests %s and %s with %d entries, want equal sha256 values and 4 entries", a, b, n)
	}
}

func TestDigestChangesWithContentModeOwnerAndLink(t *testing.T) {
	base, _ := digestOf(t, volumeTar(t, root, version, link))
	changed := []struct {
		name    string
		entries []entry
	}{
		{"content of the same size", []entry{root, {name: "vol/PG_VERSION", typ: tar.TypeReg, mode: 0o600, uid: 70, content: "17\n"}, link}},
		{"mode", []entry{root, {name: "vol/PG_VERSION", typ: tar.TypeReg, mode: 0o644, uid: 70, content: "16\n"}, link}},
		{"owner", []entry{root, {name: "vol/PG_VERSION", typ: tar.TypeReg, mode: 0o600, uid: 0, content: "16\n"}, link}},
		{"root folder owner", []entry{{name: "vol", typ: tar.TypeDir, mode: 0o700, uid: 0}, version, link}},
		{"link target", []entry{root, version, {name: "vol/latest", typ: tar.TypeSymlink, mode: 0o777, uid: 70, link: "base"}}},
	}
	for _, c := range changed {
		if got, _ := digestOf(t, volumeTar(t, c.entries...)); got == base {
			t.Errorf("a change of %s left the digest the same", c.name)
		}
	}
}

func TestDigestCountsAnEmptyVolume(t *testing.T) {
	if _, n := digestOf(t, volumeTar(t, root)); n != 0 {
		t.Errorf("entries = %d, want 0 for a volume with only its root", n)
	}
}

func TestDigestReadsPastDockersPadding(t *testing.T) {
	padded := append(volumeTar(t, root, version), make([]byte, 20*1024)...)
	want, _ := digestOf(t, volumeTar(t, root, version))
	if got, _ := digestOf(t, padded); got != want {
		t.Errorf("padding changed the digest")
	}
}

func TestDigestRejectsEntriesOutsideTheVolume(t *testing.T) {
	d := NewDigest()
	d.Write(volumeTar(t, root, entry{name: "etc/passwd", typ: tar.TypeReg, mode: 0o644, content: "x"}))
	if _, _, err := d.Finish(); err == nil || !strings.Contains(err.Error(), "unexpected entry") {
		t.Errorf("err = %v, want unexpected entry", err)
	}
}

func TestDigestFailsOnATruncatedArchive(t *testing.T) {
	archive := volumeTar(t, root, version, table)
	d := NewDigest()
	d.Write(archive[:700])
	if _, _, err := d.Finish(); err == nil {
		t.Error("a truncated archive produced a digest")
	}
}

func TestDigestWriterFailsAfterABadArchive(t *testing.T) {
	d := NewDigest()
	junk := bytes.Repeat([]byte("not a tar archive "), 100)
	var err error
	for i := 0; i < 10 && err == nil; i++ {
		_, err = d.Write(junk)
	}
	if err == nil {
		t.Error("writes kept succeeding after the archive turned out bad")
	}
	if _, _, ferr := d.Finish(); ferr == nil {
		t.Error("Finish reported no error")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/docker/`
Expected: the build fails with `undefined: NewDigest`.

- [ ] **Step 3: Implement**

`internal/modules/docker/digest.go`:
```go
package docker

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// archiveRoot is the first path element of every entry docker container cp
// writes for /vol, the folder mtm mounts each volume at.
const archiveRoot = "vol"

// Digest computes a volume's content digest from the tar stream docker
// container cp writes, as the stream passes through: SHA-256 over one line
// per entry, sorted by path, holding the entry's type, permission bits,
// owner and group ids, size, link target, and content SHA-256. Sorting
// makes it independent of tar order. Owners count because a database
// refuses a data folder it doesn't own. Modification times don't.
type Digest struct {
	pw   *io.PipeWriter
	done chan struct{}

	sum     string
	entries int
	err     error
}

func NewDigest() *Digest {
	pr, pw := io.Pipe()
	d := &Digest{pw: pw, done: make(chan struct{})}
	go func() {
		defer close(d.done)
		d.sum, d.entries, d.err = digestTar(pr)
		// Docker pads the archive past its end marker; reading the rest
		// keeps the writer from blocking. After an error, closing makes
		// the writer fail instead.
		if d.err == nil {
			_, d.err = io.Copy(io.Discard, pr)
		}
		pr.CloseWithError(d.err)
	}()
	return d
}

func (d *Digest) Write(p []byte) (int, error) { return d.pw.Write(p) }

// Finish ends the stream and returns the digest and the number of entries
// below the volume's root folder; 0 means the volume is empty.
func (d *Digest) Finish() (string, int, error) {
	d.pw.Close()
	<-d.done
	return d.sum, d.entries, d.err
}

func digestTar(r io.Reader) (string, int, error) {
	tr := tar.NewReader(r)
	var lines []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", 0, fmt.Errorf("read the volume's archive: %w", err)
		}
		line, err := entryLine(hdr, tr)
		if err != nil {
			return "", 0, err
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "", 0, errors.New("read the volume's archive: it has no entries")
	}
	slices.Sort(lines)
	h := sha256.New()
	for _, l := range lines {
		io.WriteString(h, l)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), len(lines) - 1, nil
}

func entryLine(hdr *tar.Header, content io.Reader) (string, error) {
	rel, err := relative(hdr.Name)
	if err != nil {
		return "", err
	}
	var size int64
	sum := ""
	if hdr.Typeflag == tar.TypeReg {
		h := sha256.New()
		n, err := io.Copy(h, content)
		if err != nil {
			return "", fmt.Errorf("read %s in the volume's archive: %w", rel, err)
		}
		size, sum = n, hex.EncodeToString(h.Sum(nil))
	}
	fields := []string{
		strconv.Quote(rel), string(hdr.Typeflag), strconv.FormatInt(hdr.Mode&0o7777, 8),
		strconv.Itoa(hdr.Uid), strconv.Itoa(hdr.Gid), strconv.FormatInt(size, 10),
		strconv.Quote(hdr.Linkname), sum,
	}
	return strings.Join(fields, " ") + "\n", nil
}

// relative turns "vol/a/b" into "a/b" and "vol" into ".".
func relative(name string) (string, error) {
	name = strings.TrimSuffix(name, "/")
	if name == archiveRoot {
		return ".", nil
	}
	rel, ok := strings.CutPrefix(name, archiveRoot+"/")
	if !ok || rel == "" {
		return "", fmt.Errorf("read the volume's archive: unexpected entry %q", name)
	}
	return rel, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/modules/docker/`
Expected: PASS. `-race` matters here: the digest reads the stream on its own goroutine.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go test ./...
git add internal/modules/docker
git commit -m "Add a content digest for Docker volumes

The digest is computed from docker cp's tar stream as it passes, sorted so
tar order doesn't matter. It covers owners and modes: a database refuses a
data folder it doesn't own, so a restore that loses them must not match."
```

---

### Task 6: The Docker client: commands, inspect output, and holder containers

**Files:**
- Create: `internal/modules/docker/client.go`
- Test: `internal/modules/docker/fixture_test.go`, `internal/modules/docker/client_test.go`

**Interfaces:**
- Consumes:
  - Task 1 (`Command.Stdout`, `Command.Input`, and `Fake` streaming);
  - Task 2 (`stage.Artifact`, `OpenDir`, `ExportsDirName`, from Task 4);
  - Task 4 (`module.Env.Exports`, `module.Env.Free`);
  - Task 5 (`NewDigest`, and the test helpers `volumeTar`, `digestOf`, `root`, `version`, `sub`, `table`).
- Produces:
  - Constants:
    - `docker.Name = "docker"`;
    - `docker.DesktopCLI = "/Applications/Docker.app/Contents/Resources/bin/docker"`;
    - `docker.EmptyImage = "mtm/empty:1"`.
  - `docker.Module{Docker, Empty string}`, with `docker.New()` and `Name()`.
  - Types:
    - `Image{ID; Tags; Size; Pulled; Archive *stage.Artifact}`;
    - `Volume{Name; Labels; Size; Digest; Entries; Archive *stage.Artifact}`;
    - `Container{ID; Name; Project; WorkingDir; ConfigFiles}`;
    - the unexported `container`, which adds `Running`, `Restart`, and `Volumes`.
  - Unexported methods later tasks use:
    - `installed()`, `command(env, args...)`, `docker(env, args...)`, and `engine(env)`, which returns `""` when Docker Desktop isn't running;
    - `images(env)`, `volumes(env)` (which returns the skipped volumes too), and `containers(env)`;
    - `importEmpty`, `removeEmpty`, `hold`, `release`, `removeLeftovers`, `readVolume`, `writeVolume`, and `volumeDigest`;
    - `homeRelative`, `expandHome`, `parseSize`, and `unique`.
  - Test fixture in `fixture_test.go`:
    - `world`, with `newWorld`, `respond`, `then`, `Run`, `env`, `ran`, `stdin`, `oldMac`, and `holder`;
    - the fixtures `imageInspect`, `volumeInspect`, `systemDF`, and `containerInspect`;
    - the constants `webID`, `postgresID`, and `webArchive`;
    - `pgdataTar`, `cacheTar`, and `digestFor`.
  - The fixture JSON follows `docker image inspect`, `volume inspect`, `container inspect`, and `system df --verbose --format json` output captured from Docker Desktop with Engine 29.2.0 on the author's Mac, trimmed.

- [ ] **Step 1: Write the failing tests**

`internal/modules/docker/fixture_test.go`:
```go
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
```

`internal/modules/docker/client_test.go`:
```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/docker/`
Expected: the build fails with `undefined: Module`, `undefined: socket`, and `undefined: EmptyImage`.

- [ ] **Step 3: Implement**

`internal/modules/docker/client.go`:
```go
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
			Running: r.State.Running,
			Restart: r.HostConfig.RestartPolicy.Name,
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/modules/docker/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go test ./...
git add internal/modules/docker
git commit -m "Add the Docker client the docker module drives

It always runs Docker Desktop's own CLI through the socket in the home
folder, so a context or DOCKER_HOST can't point it at another machine.
It tells pulled images from built ones the way the containerd image store
allows, and it reads and writes volumes through never-started holders of
an empty image, without a pull or a helper image."
```

---
### Task 7: docker `scan`, `prepare`, and `prepare --undo`

**Files:**
- Create: `internal/modules/docker/docker.go`
- Test: `internal/modules/docker/docker_test.go`

**Interfaces:**
- Consumes: Task 6 (the client and the `world` fixture); Task 2 (`stage.Room`, `CreateArtifact`, `CheckArtifact`); `words.Count`, `words.Bytes`; `macos.ShellQuote`; `module.LoadInventory`, `module.ErrNotPrepared`.
- Produces:
  - `docker.Inventory{Installed bool; Engine string; Images []Image; Volumes []Volume; Stopped []Container}`.
  - `(Module).Scan`, `(Module).Prepare`, and `(Module).Undo`.
  - Unexported helpers Task 8 uses: `start(env, []Container) module.Result`, `names`, and `imageArchive(id)`.
  - Test helpers Task 8 uses: `record`, `ids`, `find`, `(*world).count`, and `(*world).prepare`.
  - **Prepare rules, in order** (spec 9.5):
    - Not installed: the record says not installed.
    - Not running: keep the earlier record with a warn, or fail with `Docker Desktop isn't running. Start it and run: mtm prepare --only docker`.
    - Remove leftover holders.
    - Snapshot the images, volumes, and containers.
    - Check the whole export fits, before stopping anything.
    - Stop running containers one at a time, with `--time 60`.
    - On a stop failure: block, and record the volumes without digests.
    - Import the empty image.
    - Export each volume to `docker/volumes/<name>.tar.gz`, with gzip at `BestSpeed`, plus its digest.
    - Save each built image to `docker/images/<hex id>.tar`, or keep the earlier archive when the id and tags are unchanged and its fingerprint still matches.
    - Remove the empty image.
    - Add the by-hand step "Quit Docker Desktop before Migration Assistant" (`quit`).

- [ ] **Step 1: Write the failing tests**

`internal/modules/docker/docker_test.go`:
```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/docker/`
Expected: the build fails with `undefined: Inventory` and `w.mod.Scan undefined`.

- [ ] **Step 3: Implement**

`internal/modules/docker/docker.go`:
```go
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
		return keepPrevious(previous, recorded, module.Finding{
			Module: Name, ID: "kept", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "Docker Desktop isn't running, so mtm kept the Docker export from the earlier prepare",
			Fix:   "If you used Docker since then, start Docker Desktop and run mtm prepare --only docker.",
		}, "Docker Desktop isn't running. Start it and run: mtm prepare --only docker")
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
	if err := m.importEmpty(env); err != nil {
		inv.Volumes, inv.Images = s.volumes, s.images
		res.Inventory = inv
		res.Problems = append(res.Problems, blocker("empty-image", "Could not prepare to export volumes", err))
		return res, nil
	}
	var written int64
	for _, v := range s.volumes {
		exported, err := m.exportVolume(env, v)
		if err != nil {
			inv.Volumes = append(inv.Volumes, v)
			res.Problems = append(res.Problems, blocker("volume:"+v.Name, "Could not export volume "+v.Name, err))
			continue
		}
		inv.Volumes = append(inv.Volumes, exported)
		written += exported.Archive.Size
	}
	if len(s.volumes) > 0 {
		res.Done = append(res.Done, fmt.Sprintf("exported %s (%s)", words.Count(len(s.volumes), "volume", "volumes"), words.Bytes(uint64(written))))
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

func blocker(id, title string, err error) module.Finding {
	return module.Finding{
		Module: Name, ID: id, Severity: module.SeverityBlocker, Handler: module.Manual,
		Title: title, Detail: err.Error(), Fix: "Fix the cause above and run mtm prepare --only docker again.",
	}
}

// keepPrevious ends prepare with problem but keeps the earlier record, so
// finish still starts the containers an earlier run stopped. Without an
// earlier record it fails instead, with failure when given: an empty
// record of a running Docker would let verify pass with nothing checked.
func keepPrevious(previous Inventory, recorded bool, problem module.Finding, failure string) (module.Result, error) {
	if !recorded {
		if failure == "" {
			failure = problem.Title + ": " + problem.Detail
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
				problems = append(problems, blocker("image:"+img.Tags[0], "Could not save image "+img.Tags[0], err))
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/modules/docker/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go test ./...
git add internal/modules/docker
git commit -m "Export Docker volumes and built images in prepare

prepare checks the export fits before it stops a container, stops the
running ones so their volumes copy whole, and records each volume's
content digest beside its archive. A re-run keeps image archives that
haven't changed, and the containers an earlier run stopped."
```

---

### Task 8: docker `finish` and `verify`

**Files:**
- Create: `internal/modules/docker/restore.go`
- Test: `internal/modules/docker/restore_test.go`

**Interfaces:**
- Consumes: Tasks 6 and 7.
- Produces:
  - `(Module).Finish` and `(Module).Verify`. With them, `docker.Module` satisfies `module.Module` and `module.Undoer`.
  - `~/.mtm/docker-finish.json`, mapping volume name to `"intact"` or `"restored"` (`loadSettled`, `saveSettled`).
  - **Finish rules** (spec 9.5):
    - It needs Docker Desktop running; otherwise a blocker, with nothing run.
    - It settles every unsettled volume before starting anything:
      - missing: `docker volume create --label k=v… <name>`, then restore;
      - matching digest: intact;
      - present and empty: restore;
      - present, non-empty, and different: a blocker. Its fix is `docker container rm --force <users> && docker volume rm <name> && mtm finish --only docker`. Nothing is written.
    - Restoring checks the fingerprint first, streams the archive in with `docker container cp --archive --quiet - <holder>:/`, and re-checks the digest.
    - It loads missing built images with `docker image load --quiet --input`.
    - Missing pulled images become one warn: `Run: docker pull '<tag>' && …`.
    - Only without a blocker does it start containers: `docker container start <id>` when the container exists. A missing Compose container comes back with `docker compose … up --detach --pull never --no-build`, run in the project folder. A missing non-Compose container is a warn.
  - **Verify rules:**
    - Docker Desktop not running: one critical fail.
    - A volume settled by finish: a critical pass.
    - An unsettled volume: compared now (critical).
    - Missing, or recorded without a digest: a critical fail.
    - A built image: pass when present by id, info when only its tag remains, fail otherwise.
    - Pulled images: one warn listing the missing tags.

- [ ] **Step 1: Write the failing tests**

`internal/modules/docker/restore_test.go`:
```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/docker/`
Expected: the build fails with `w.mod.Finish undefined`, `undefined: loadSettled`, and `undefined: intact`.

- [ ] **Step 3: Implement**

`internal/modules/docker/restore.go`:
```go
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

const restoreFix = "If prepare used --stage, attach that drive; otherwise check that ~/.mtm/exports arrived. Then run mtm finish --only docker again."

func loadSettled(st *stage.Stage) (map[string]string, error) {
	path := st.Path(settledFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	settled := map[string]string{}
	if err := json.Unmarshal(data, &settled); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return settled, nil
}

func saveSettled(st *stage.Stage, settled map[string]string) error {
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

func (m Module) volumeNames(env module.Env) ([]string, error) {
	out, err := m.docker(env, "volume", "ls", "--quiet")
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	return strings.Fields(string(out.Stdout)), nil
}

// settleVolumes makes each recorded volume match the old Mac's, except one
// that holds other data, and records each volume it settles.
func (m Module) settleVolumes(env module.Env, st *stage.Stage, vols []Volume) (module.Result, error) {
	var res module.Result
	settled, err := loadSettled(st)
	if err != nil {
		return res, err
	}
	pending := slices.DeleteFunc(slices.Clone(vols), func(v Volume) bool { return settled[v.Name] != "" })
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
		settled[v.Name] = how
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
	if cs, err := m.containers(env); err == nil {
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
		Detail: "mtm never overwrites a volume that holds data. These commands delete it and the containers using it; finish then restores it from the old Mac's export and re-creates the Compose containers.",
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
		case settled[v.Name] == intact:
			c.Detail = "matched the old Mac's when mtm finish checked it"
		case settled[v.Name] == restored:
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
		m.removeEmpty(env)
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/modules/docker/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./... && go test ./...
git add internal/modules/docker
git commit -m "Restore Docker data in finish only where it didn't arrive intact

finish compares every volume before any container starts. It restores
only missing or empty ones, never overwrites a volume that holds data,
and records what it settled, so verify can trust the comparison after
containers have changed the volumes."
```

---

### Task 9: Docker integration tests against Docker Desktop

**Files:**
- Create: `internal/modules/docker/integration_test.go` (build tag `integration`)

**Interfaces:**
- Consumes: Tasks 5–8 (`writeVolume`, `volumeDigest`, `exportVolume`, `settle`, `saveImage`, `loadImage`, `present`, `images`), plus the test helpers `volumeTar`, `digestFor`, `entry`, `root`, `version`, `sub`, `table`, and `link`.
- Produces:
  - `TestDockerVolumeRoundTrip`: seed with owners and modes, digest, export, intact, delete and restore with its label, then an empty volume restored.
  - `TestDockerNeverOverwritesAVolumeHoldingOtherData`.
  - `TestDockerImageRoundTrip`: an imported image counts as built, and save, delete, and load keep its id.
  - These are the Stage 3 success criterion (IMPLEMENTATION_PLAN.md): a real export, delete, and restore with a matching digest; a non-empty mismatched volume left untouched.

These tests run against the developer's own Docker Desktop. Read the file's opening comment and keep to it: only `mtm-test-…` names, and no call to `Scan`, `Prepare`, `Finish`, `Undo`, or `Verify`. They skip when Docker Desktop isn't running.

- [ ] **Step 1: Write the test**

`internal/modules/docker/integration_test.go`:
```go
//go:build integration

package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/momentmaker/mtm/internal/module"
	"github.com/momentmaker/mtm/internal/runner"
	"github.com/momentmaker/mtm/internal/stage"
)

// These tests run against the developer's own Docker Desktop. They touch
// only volumes, images, and containers named mtm-test-…, and call the
// export and restore steps directly. They never call Prepare, Finish, or
// Undo, which stop and start the developer's containers.

func requireDocker(t *testing.T) (module.Env, Module) {
	t.Helper()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("needs an Apple Silicon Mac")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	exports, err := stage.OpenDir(filepath.Join(t.TempDir(), "exports"))
	if err != nil {
		t.Fatal(err)
	}
	env := module.Env{
		Ctx: context.Background(), Run: runner.Exec{}, Home: home, Exports: exports,
		Free: func(string) (uint64, error) { return 1 << 40, nil },
	}
	m := Module{Docker: DesktopCLI, Empty: fmt.Sprintf("mtm-test/empty:%d", time.Now().UnixNano())}
	if !m.installed() || m.engine(env) == "" {
		t.Skip("needs Docker Desktop running")
	}
	if err := m.importEmpty(env); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.docker(env, "image", "rm", m.Empty) })
	return env, m
}

// testVolume names a volume for this test and removes it, and any holder
// left mounting it, when the test ends.
func testVolume(t *testing.T, env module.Env, m Module) string {
	t.Helper()
	name := fmt.Sprintf("mtm-test-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		out, _ := m.docker(env, "container", "ls", "--all", "--quiet", "--filter", "volume="+name)
		for _, id := range strings.Fields(string(out.Stdout)) {
			m.docker(env, "container", "rm", "--force", id)
		}
		m.docker(env, "volume", "rm", "--force", name)
	})
	return name
}

func mustDocker(t *testing.T, env module.Env, m Module, args ...string) string {
	t.Helper()
	out, err := m.docker(env, args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out.Stdout))
}

func TestDockerVolumeRoundTrip(t *testing.T) {
	env, m := requireDocker(t)
	name := testVolume(t, env, m)
	seed := volumeTar(t, root, version, sub, table, link)
	want := digestFor(t, seed)

	if err := m.writeVolume(env, name, bytes.NewReader(seed)); err != nil {
		t.Fatal(err)
	}
	if sum, n, err := m.volumeDigest(env, name); err != nil || sum != want || n != 4 {
		t.Fatalf("seeded volume = %s with %d entries, %v; want the seed's digest: owners and modes must survive Docker", sum, n, err)
	}
	v, err := m.exportVolume(env, Volume{Name: name, Labels: map[string]string{"dev.mtm.test": "1"}})
	if err != nil || v.Digest != want || v.Archive == nil {
		t.Fatalf("export = %+v, %v", v, err)
	}

	if how, p := m.settle(env, v, true); p != nil || how != intact {
		t.Errorf("intact volume settled as %q, %+v", how, p)
	}

	mustDocker(t, env, m, "volume", "rm", name)
	if how, p := m.settle(env, v, false); p != nil || how != restored {
		t.Fatalf("missing volume settled as %q, %+v", how, p)
	}
	if label := mustDocker(t, env, m, "volume", "inspect", "--format", `{{index .Labels "dev.mtm.test"}}`, name); label != "1" {
		t.Errorf("restored volume label = %q, want 1", label)
	}

	mustDocker(t, env, m, "volume", "rm", name)
	mustDocker(t, env, m, "volume", "create", name)
	if how, p := m.settle(env, v, true); p != nil || how != restored {
		t.Errorf("empty volume settled as %q, %+v", how, p)
	}
}

func TestDockerNeverOverwritesAVolumeHoldingOtherData(t *testing.T) {
	env, m := requireDocker(t)
	name := testVolume(t, env, m)
	if err := m.writeVolume(env, name, bytes.NewReader(volumeTar(t, root, version))); err != nil {
		t.Fatal(err)
	}
	v, err := m.exportVolume(env, Volume{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	extra := volumeTar(t, root, entry{name: "vol/new-on-this-mac", typ: tar.TypeReg, mode: 0o644, content: "new"})
	if err := m.writeVolume(env, name, bytes.NewReader(extra)); err != nil {
		t.Fatal(err)
	}
	before, _, err := m.volumeDigest(env, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, p := m.settle(env, v, true); p == nil || p.Severity != module.SeverityBlocker {
		t.Fatalf("settle = %+v, want a blocker", p)
	}
	if after, _, _ := m.volumeDigest(env, name); after != before {
		t.Error("settle changed a volume that holds other data")
	}
}

func TestDockerImageRoundTrip(t *testing.T) {
	env, m := requireDocker(t)
	tag := fmt.Sprintf("mtm-test/image:%d", time.Now().UnixNano())
	cmd := m.command(env, "image", "import", "-", tag)
	cmd.Input = bytes.NewReader(volumeTar(t, root, version))
	if _, err := env.Run.Run(env.Ctx, cmd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.docker(env, "image", "rm", "--force", tag) })
	images, err := m.images(env)
	if err != nil {
		t.Fatal(err)
	}
	var img Image
	for _, i := range images {
		if len(i.Tags) > 0 && i.Tags[0] == tag {
			img = i
		}
	}
	if img.ID == "" || img.Pulled {
		t.Fatalf("image = %+v, want an imported image counted as locally built", img)
	}
	a, err := m.saveImage(env, img)
	if err != nil {
		t.Fatal(err)
	}
	img.Archive = &a
	mustDocker(t, env, m, "image", "rm", tag)
	if err := m.loadImage(env, img); err != nil {
		t.Fatal(err)
	}
	ids, _, err := m.present(env)
	if err != nil || !ids[img.ID] {
		t.Errorf("after load, image %s present = %v, %v", img.ID, ids[img.ID], err)
	}
}
```

- [ ] **Step 2: Vet it**

Run: `go vet -tags integration ./internal/modules/docker/`
Expected: no output.

- [ ] **Step 3: Run it**

Run: `go test -tags integration -run 'TestDocker(VolumeRoundTrip|NeverOverwrites|ImageRoundTrip)' -v ./internal/modules/docker/`
Expected: three PASSes with Docker Desktop running, or three SKIPs ("needs Docker Desktop running") without it. Report which. On the author's Mac they passed in about 10 seconds.

Then check that nothing was left behind:
```bash
D=/Applications/Docker.app/Contents/Resources/bin/docker
$D volume ls --filter name=mtm-test -q | wc -l                 # 0
$D image ls 'mtm-test/*' -q | wc -l                            # 0
$D container ls --all --filter label=dev.mtm.temporary -q | wc -l   # 0
```

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go vet ./... && go vet -tags integration ./... && go test ./...
git add internal/modules/docker/integration_test.go
git commit -m "Test volume and image round trips against Docker Desktop

Fakes can't prove docker cp keeps owners and modes, or that save and load
keep an image's id. These tests do, on mtm-test volumes and images only,
without touching the developer's containers."
```

---

### Task 10: The `databases` module (Homebrew Postgres)

**Files:**
- Create: `internal/modules/databases/databases.go`
- Test: `internal/modules/databases/databases_test.go`, `internal/modules/databases/integration_test.go` (build tag `integration`)

**Interfaces:**
- Consumes: Task 1 (streaming), Task 2 (`Room`, `CreateArtifact`, `CheckArtifact`), Task 4 (`Env.Exports`, `Env.Free`, `Env.RestoreDump`, `Env.Force`); `words`, `macos.ShellQuote`, `testutil.WriteFile`.
- Produces:
  - `databases.Name = "databases"`; `databases.Module{Var, Opt string}`; `databases.New()`, which uses `/opt/homebrew/var` and `/opt/homebrew/opt`.
  - `databases.Cluster{Name, Major string; Running bool; Socket, Port string; Databases []string; Dump *stage.Artifact}`; `databases.Inventory{Clusters []Cluster}`.
  - `Scan`, `Prepare`, `Finish`, and `Verify`, per spec 9.6. It is not an `Undoer`: the homebrew module restarts the servers.
  - The dump goes to `postgres/<cluster>.sql.gz`.
  - A re-run keeps the earlier record of a cluster that has stopped since, if its dump still matches.

- [ ] **Step 1: Write the failing tests**

`internal/modules/databases/databases_test.go`:
```go
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
	if c := f.verify(m); c.Status != module.StatusFail || !strings.Contains(c.Detail, "brew services start 'postgresql@16'") {
		t.Errorf("check = %+v, want not running", c)
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/modules/databases/`
Expected: the build fails with `undefined: Module`.

- [ ] **Step 3: Implement**

`internal/modules/databases/databases.go`:
```go
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
	case socket == "":
		return c, &module.Finding{Module: Name, ID: "socket:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
			Title:  c.Name + " listens on no Unix socket",
			Detail: "mtm connects only through the socket, never the network, so it can't dump this cluster.",
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
	if problem != nil {
		return live, "", problem
	}
	if !live.Running {
		// A cluster that stopped since the earlier prepare dumped it, as the
		// homebrew module leaves it, hasn't changed since that dump.
		if i := slices.IndexFunc(previous.Clusters, func(p Cluster) bool { return p.Name == c.Name }); i >= 0 {
			p := previous.Clusters[i]
			if p.Running && p.Dump != nil && env.Exports.CheckArtifact(*p.Dump) == nil {
				return p, "kept the dump of " + c.Name + " from the earlier prepare", nil
			}
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
			Fix: "Free space, or run mtm prepare --stage /Volumes/<drive> with an encrypted APFS drive."}
	}
	a, err := m.dump(env, c)
	if err != nil {
		return c, "", noDump(c, err)
	}
	c.Dump = &a
	return c, fmt.Sprintf("saved a dump of %s (%s, %s)", c.Name, words.Count(len(dbs), "database", "databases"), words.Bytes(uint64(a.Size))), nil
}

func noDump(c Cluster, err error) *module.Finding {
	return &module.Finding{
		Module: Name, ID: "dump:" + c.Name, Severity: module.SeverityWarn, Handler: module.Manual,
		Title: "Could not save a safety-net dump of " + c.Name, Detail: err.Error(),
		Fix: "Migration Assistant still copies its data folder. For a dump too, check that pg_dumpall works, then run mtm prepare --only databases.",
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
		done, problem := m.restore(env, c)
		if problem != nil {
			res.Problems = append(res.Problems, *problem)
			continue
		}
		res.Done = append(res.Done, done)
	}
	if len(res.Done) == 0 && len(res.Problems) == 0 {
		res.Problems = append(res.Problems, module.Finding{Module: Name, ID: "no-dump", Severity: module.SeverityWarn, Handler: module.Manual,
			Title: "prepare saved no Postgres dump to restore"})
	}
	return res, nil
}

func restoreProblem(c Cluster, title, detail, fix string) *module.Finding {
	return &module.Finding{Module: Name, ID: "restore:" + c.Name, Severity: module.SeverityBlocker, Handler: module.Manual,
		Title: title, Detail: detail, Fix: fix}
}

// restore streams a cluster's dump into its running server. A cluster that
// already holds databases could end up mixing two Macs' data, so it needs
// --force.
func (m Module) restore(env module.Env, c Cluster) (string, *module.Finding) {
	svc := macos.ShellQuote(service(c))
	if !m.ready(env, c) {
		return "", restoreProblem(c, c.Name+" isn't running", "",
			"If its data folder didn't arrive, create an empty one with: brew postinstall "+svc+". Then run: brew services start "+svc+" && mtm finish --only databases --restore-dump")
	}
	dbs, err := m.databases(env, c)
	if err != nil {
		return "", restoreProblem(c, "Could not list "+c.Name+"'s databases", err.Error(), "Fix the cause above, then run mtm finish --only databases --restore-dump again.")
	}
	if extra := slices.DeleteFunc(dbs, func(d string) bool { return d == "postgres" }); len(extra) > 0 && !env.Force {
		return "", restoreProblem(c, c.Name+" already has databases: "+strings.Join(extra, ", "),
			"Restoring the dump on top could mix the old Mac's data with this one's.",
			"Check them first. To restore anyway, run: mtm finish --only databases --restore-dump --force")
	}
	if err := env.Exports.CheckArtifact(*c.Dump); err != nil {
		return "", restoreProblem(c, "Could not use the dump of "+c.Name, err.Error(),
			"If prepare used --stage, attach that drive; otherwise check that ~/.mtm/exports arrived. Then run mtm finish --only databases --restore-dump again.")
	}
	if err := m.load(env, c); err != nil {
		return "", restoreProblem(c, "Could not restore "+c.Name, err.Error(), "Fix the cause above, then run mtm finish --only databases --restore-dump again.")
	}
	return "restored " + c.Name + " from its dump", nil
}

func (m Module) load(env module.Env, c Cluster) error {
	f, err := os.Open(env.Exports.Path(c.Dump.Name))
	if err != nil {
		return fmt.Errorf("open export: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read the dump of %s: %w", c.Name, err)
	}
	cmd := m.pg(c, "psql", "-w", "-X", "--quiet", "--dbname", "postgres", "--file=-")
	cmd.Input = gz
	_, err = env.Run.Run(env.Ctx, cmd)
	return err
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
	svc := macos.ShellQuote(service(c))
	if !m.ready(env, c) {
		ch.Status, ch.Detail = module.StatusFail, "not running; run: brew services start "+svc+". If it won't start: mtm finish --only databases --restore-dump"
		return ch
	}
	dbs, err := m.databases(env, c)
	if err != nil {
		ch.Status, ch.Detail = module.StatusFail, err.Error()
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/modules/databases/`
Expected: PASS.

- [ ] **Step 5: Add the integration test**

`internal/modules/databases/integration_test.go`:
```go
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

func mustRun(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
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
	t.Cleanup(func() { exec.Command(filepath.Join(bin, "pg_ctl"), "--pgdata", data, "--mode", "fast", "stop").Run() })
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
```

Run: `go vet -tags integration ./internal/modules/databases/ && go test -tags integration -run TestPostgresDumpAndRestore -v ./internal/modules/databases/`
Expected: PASS where a Homebrew `postgresql@N` is installed, SKIP ("needs a Homebrew postgresql@N") otherwise. It skips on the author's Mac. Don't install Postgres to make it run. Report whether it passed or skipped.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./... && go vet -tags integration ./... && go test ./...
git add internal/modules/databases
git commit -m "Add the databases module for Homebrew Postgres

Before the homebrew module stops each running cluster, this saves a
pg_dumpall dump as a safety net, over the cluster's Unix socket only, and
records its databases. verify then checks they all arrived, and finish
--restore-dump loads the dump into a server that won't start with its
copied data."
```

---

### Task 11: Wire the modules and run them in the no-network test

**Files:**
- Modify: `cmd/mtm/main.go`, `cmd/mtm/main_test.go`, `cmd/mtm/nonetwork_integration_test.go`

**Interfaces:**
- Consumes: `databases.New` (Task 10) and `docker.New` (Task 6).
- Produces: `defaultApp` registers modules in this order: `files`, `git`, `system`, `databases`, `docker`, `homebrew`, `toolchains`, `loginitems`. `databases` must come before `homebrew` (IMPLEMENTATION_PLAN.md, Stage 3 ordering).

- [ ] **Step 1: Write the failing test**

In `cmd/mtm/main_test.go`:
```diff
--- a/cmd/mtm/main_test.go
+++ b/cmd/mtm/main_test.go
@@ -247,7 +247,7 @@
 	for _, m := range defaultApp(t.TempDir()).Modules {
 		names = append(names, m.Name())
 	}
-	if want := []string{"files", "git", "system", "homebrew", "toolchains", "loginitems"}; !slices.Equal(names, want) {
+	if want := []string{"files", "git", "system", "databases", "docker", "homebrew", "toolchains", "loginitems"}; !slices.Equal(names, want) {
 		t.Errorf("modules = %q, want %q", names, want)
 	}
 }
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/mtm/ -run TestDefaultAppModules`
Expected: FAIL. The modules list has no `databases` or `docker`.

- [ ] **Step 3: Wire the modules**

`cmd/mtm/main.go`:
```diff
--- a/cmd/mtm/main.go
+++ b/cmd/mtm/main.go
@@ -15,6 +15,8 @@
 
 	"github.com/momentmaker/mtm/internal/app"
 	"github.com/momentmaker/mtm/internal/module"
+	"github.com/momentmaker/mtm/internal/modules/databases"
+	"github.com/momentmaker/mtm/internal/modules/docker"
 	"github.com/momentmaker/mtm/internal/modules/files"
 	"github.com/momentmaker/mtm/internal/modules/git"
 	"github.com/momentmaker/mtm/internal/modules/homebrew"
@@ -67,6 +69,8 @@
 	os.Exit(code)
 }
 
+// defaultApp lists databases before homebrew: pg_dumpall needs Postgres
+// running, and homebrew's prepare stops it.
 func defaultApp(home string) *app.App {
 	name := currentUser()
 	return &app.App{
@@ -75,7 +79,8 @@
 		Version: version,
 		Now:     time.Now,
 		Modules: []module.Module{
-			files.New(), git.New(), system.New(name), homebrew.New(name), toolchains.New(name), loginitems.New(os.Getuid()),
+			files.New(), git.New(), system.New(name), databases.New(), docker.New(),
+			homebrew.New(name), toolchains.New(name), loginitems.New(os.Getuid()),
 		},
 	}
 }
```

`cmd/mtm/nonetwork_integration_test.go`. Its prepare steps now include `databases` and `docker`, and the new comment explains why docker reaches no engine there:
```diff
--- a/cmd/mtm/nonetwork_integration_test.go
+++ b/cmd/mtm/nonetwork_integration_test.go
@@ -110,6 +110,11 @@
 // loginitems is not covered. prepare leaves it out because it asks System
 // Events, which would show a permission prompt, and its scan and verify
 // fail here because launchctl list doesn't work inside sandbox-exec.
+//
+// docker runs but reaches no engine: it talks to Docker Desktop through
+// the socket in the home folder, and this home folder has none, so the
+// developer's containers are never stopped. Its prepare fails, asking for
+// Docker Desktop, which leaves prepare NOT READY (exit 1).
 func TestNoNetwork(t *testing.T) {
 	cache := requireIntegrationHost(t)
 	bin := filepath.Join(t.TempDir(), "mtm")
@@ -124,9 +129,9 @@
 		brewfile bool
 	}{
 		{[]string{"scan"}, []int{0, 1}, false},
-		{[]string{"prepare", "--only", "files,git,system,homebrew,toolchains"}, []int{0, 1}, true},
+		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains"}, []int{0, 1}, true},
 		{[]string{"prepare", "--undo"}, []int{0, 1}, false},
-		{[]string{"prepare", "--only", "files,git,system,homebrew,toolchains"}, []int{0, 1}, true},
+		{[]string{"prepare", "--only", "files,git,system,databases,docker,homebrew,toolchains"}, []int{0, 1}, true},
 		{[]string{"verify"}, []int{1}, false},
 	}
 	for _, s := range steps {
```

- [ ] **Step 4: Run every check**

```bash
gofmt -l .
go vet ./... && go vet -tags integration ./...
go test -race ./...
go test -tags integration -run TestNoNetwork -v ./cmd/mtm/
```
Expected:
- `gofmt` prints nothing, and `go vet` and `go test` pass.
- `TestNoNetwork` passes, or skips with its stated reason (a running Homebrew service, or a loaded root service). Report which.
- It passed on the author's Mac in about 75 seconds.
- In it, docker's prepare fails asking for Docker Desktop, because the temporary home folder has no Docker socket. That is expected, and the test allows exit 1.

- [ ] **Step 5: Commit**

```bash
git add cmd/mtm
git commit -m "Wire the docker and databases modules into mtm

databases runs before homebrew, because pg_dumpall needs the servers
homebrew's prepare stops. The no-network test now runs both modules too."
```

Don't edit `IMPLEMENTATION_PLAN.md`. The controller records Stage 3's status and known gaps after the final review.
