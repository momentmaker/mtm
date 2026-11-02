# mtm Implementation Plan

Spec: `docs/superpowers/specs/2026-11-02-mtm-design.md`. Each stage gets its own detailed plan in `docs/superpowers/plans/` when it starts. Every stage ends with a tool that builds, passes all tests, and works for everything built so far.

## Stage 1: Core, files, and git
**Goal**: `mtm scan`, `prepare`, `verify`, and `cleanup` working for home-folder files and git repos. Includes the private `~/.mtm` folder, the preflight gate (Apple Silicon, macOS 26+, FileVault), redacted logs and reports, and the SAFE TO WIPE verdict.
**Success Criteria**: a simulated migration (a `ditto` copy of a seeded home folder) verifies as SAFE TO WIPE. Each injected fault produces NOT SAFE TO WIPE naming the right check: a missing SSH key, a corrupted dotfile, a missing repo, a corrupted `.env`, or verifying on the same Mac. Changes made after `prepare` do not block.
**Tests**: unit tests per package (fake runner); table tests for file classification and the verdict; integration tests with real git and real files in temp home folders; the simulated-migration tests in `internal/app`.
**Plan**: `docs/superpowers/plans/2026-11-02-mtm-stage1-core-files-git.md`
**Status**: Complete

### Known gaps carried out of Stage 1
Found in the final review and deliberately deferred. None of them can produce a false SAFE TO WIPE unless marked otherwise. Fix the first two before the author's real migration.

**False NOT SAFE**
- **git activity on the new Mac blocks the verdict.** The files check also covers `.git` internals and tracked working-tree files, so `git gc` or a branch switch on the new Mac reads as missing critical files. This needs a ruling on the spec's 8.4 vs 9.2 conflict and a redesign: the files check hands `.git` to the git module; git verify checks that local-only objects are reachable (`rev-list --objects --missing=print <recorded tips> --not --remotes`); a tracked-clean file that goes missing becomes info. Until then, run `mtm verify` right after Migration Assistant, before working in any repo.
- **Links whose targets are legitimately absent read as corrupted.** Examples: Bazel output links into `/private/var/tmp`, Nix links into `/nix`, an external drive that isn't attached. Fix: give these their own "link target missing → <target>" outcome with a remedy, and exempt regenerable roots, while a link into an internal volume still fails.

**Needs confirming on a real Mac**
- In repos under iCloud Desktop & Documents or File Provider folders, `git status` may download cloud-only files after migration, because every index entry looks changed. Confirm, then skip and warn for those repos.

**Can produce a stale record**
- A `prepare` re-run that stops before saving (a preflight blocker, a Time Machine exclusion failure, a read error) keeps the previous record. Prepare does say NOT READY. Stage 4's `retire` covers this class: changes made after the last successful prepare.

**Smaller items**
- `--details` is capped at 200 paths per folder. Spec 8.4/§10 needs every path; lift the cap before release.
- File modes are recorded but never compared.
- One unreadable untracked file drops its whole repo from the git record.
- git search depth isn't configurable (spec 9.2).
- Case-only name collisions between a case-sensitive old volume and a case-insensitive new one aren't handled.
- Modified tracked files aren't fingerprinted (the files check covers them by size and timestamp).
- Following links at prepare and verify can hang on a stale network mount.
- Output escaping uses `strconv.IsPrint`, so ideographic and no-break spaces print escaped. Switch to `unicode.IsGraphic`. Bidi controls reach `--json` output raw. CLI errors on stderr aren't escaped.
- A damaged `~/.mtm` record exits 2 without suggesting `mtm cleanup --force`.

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
