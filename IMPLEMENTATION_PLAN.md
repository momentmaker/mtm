# mtm Implementation Plan

Spec: `docs/superpowers/specs/2026-11-02-mtm-design.md`. Each stage gets its own detailed plan in `docs/superpowers/plans/` when it starts. Every stage ends with a tool that builds, passes all tests, and works for everything built so far.

## Stage 1: Core, files, and git
**Goal**: `mtm scan`, `prepare`, `verify`, and `cleanup` working for home-folder files and git repos. Includes the private `~/.mtm` folder, the preflight gate (Apple Silicon, macOS 26+, FileVault), redacted logs and reports, and the SAFE TO WIPE verdict.
**Success Criteria**: a simulated migration (a `ditto` copy of a seeded home folder) verifies as SAFE TO WIPE. Each injected fault produces NOT SAFE TO WIPE naming the right check: a missing SSH key, a corrupted dotfile, a missing repo, a corrupted `.env`, or verifying on the same Mac. Changes made after `prepare` do not block.
**Tests**: unit tests per package (fake runner); table tests for file classification and the verdict; integration tests with real git and real files in temp home folders; the simulated-migration tests in `internal/app`.
**Plan**: `docs/superpowers/plans/2026-11-02-mtm-stage1-core-files-git.md`
**Status**: Not Started

## Stage 2: Machine state and `finish`
**Goal**: the `system` module (app inventory, Intel-only apps and Rosetta, login shell, `/etc` files), `homebrew` (Brewfile, stopping and restarting services, root services printed), `toolchains` (versions resolved through the login shell), and `loginitems`. Adds the `mtm finish` command and `prepare --undo`, and switches the post-prepare instructions to `mtm finish`.
**Success Criteria**: on the author's M3, `scan` lists every toolchain and brew service correctly. `prepare` stops and `--undo` restarts brew services. `verify` reports matching versions on a copy.
**Tests**: fixture-driven unit tests from real macOS 26 command output; a no-network test that runs every `brew` command `mtm` uses under a `sandbox-exec` profile that denies network access.
**Status**: Not Started

## Stage 3: Docker Desktop and Postgres
**Goal**: the `docker` module (volume and locally built image export without network or helper images, content digests, skipping the restore when `Docker.raw` arrived intact, `compose up --pull never`) and `databases` (safety-net `pg_dumpall`, clean stop, `--restore-dump`). Adds free-space checks with a 10 GB reserve, `prepare --stage` on an encrypted APFS volume, export fingerprint checks before restore, and skipping steps whose inputs are unchanged on re-run.
**Success Criteria**: integration tests against a real Docker Desktop export, delete, and restore a volume with a matching digest. A non-empty mismatched volume is never overwritten without `--force`.
**Tests**: `integration`-tagged Docker and Postgres tests; unit tests for the content digest and the free-space and encrypted-volume checks.
**Status**: Not Started

## Stage 4: Manual steps and `retire`
**Goal**: the TOML app catalog (seeded from `tape`, BSD-2), the `checklist` engine and `mtm checklist` (done, skip with reason, persisted in `state.json`, System Settings deep links), the `permissions`, `browsers`, and `secrets` modules, and `mtm retire` (changes on the old Mac since `prepare`, plus the retirement checklist). The verdict starts counting open checklist items.
**Success Criteria**: an open checklist item blocks SAFE TO WIPE, and skipping it with a reason unblocks it. `retire` lists a file changed on the old Mac after `prepare`. Catalog files are validated in tests.
**Tests**: catalog validation tests; checklist state tests; module unit tests with fixtures; `retire` integration test on a temp home folder.
**Status**: Not Started

## Stage 5: Ship
**Goal**: GitHub Actions CI on Apple Silicon (`go test`, `go vet`, `gofmt`, `govulncheck`, catalog validation); end-to-end tests in Tart VMs (cloned VM as a perfect migration, injected faults); release builds with checksums and provenance attestations; a Homebrew tap; README and contributor guide (including how to add a catalog app).
**Success Criteria**: tagging a release produces checksummed binaries and a working `brew install`. The Tart end-to-end suite passes locally.
**Tests**: the Tart suite; a release dry run.
**Status**: Not Started
