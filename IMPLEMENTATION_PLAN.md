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
**Plan**: `docs/superpowers/plans/2026-11-02-mtm-stage2-machine-state.md`
**Status**: Complete

### Known gaps carried out of Stage 2
Found in the final review and its re-review.
- **Real-run validation pending.** Stopping and restarting services was proven with fakes only. The success criterion "prepare stops and --undo restarts brew services on the author's M3" still needs a run with a throwaway service, or in a VM.
- **Homebrew edge cases:**
  - a restarted `scheduled` service warns "not running" in verify;
  - `sudo brew services run X` is invisible;
  - `run` services restarted with `start` also launch at login, on both Macs;
  - an undo that only partly worked still drops the record;
  - a failed manifest save after prepare stopped services loses the stop record.
- **Pasting fix texts:** fix lines that end ", then run mtm prepare again" aren't one-line pasteable (harmless; brew rejects the extra words).
- **Shells:** tcsh and nushell can't print the PATH, so toolchains blocks prepare for those users. A version command that fails on both Macs reads "unknown" and passes.
- **Undo leaves the files, git, and system records trusted** while saying "back in use". Stage 4's `retire` covers changes made after prepare.
- **finish:** exits 1 whenever manual steps remain. `finish --only` is missing; Stage 3 needs it for Docker.
- **system:** Apple-managed `paths.d` files can be suggested for overwrite (suggest a diff instead). Apps with the same bundle id collapse across roots. An unreadable vendor folder skips its apps silently.
- **loginitems:** symlinked agent plists are skipped. Login item names containing ", " split wrongly. Its scan can't be tested inside `sandbox-exec`, because launchctl is unavailable there.
- **Root-only undo:** when the record holds only root services, `undo` and `finish` still check brew's package cache, though they only print sudo commands for those. A cold cache then blocks for no benefit.
- **Sandboxing brew isn't possible:** `launchctl list` returns nothing under `sandbox-exec`, so `brew services` would misreport running services.
- **Defense in depth:** consider running every external command mtm launches (brew, the login shell, tool `--version`) under the deny-network sandbox.

## Stage 3: Docker Desktop and Postgres
**Goal**: the `docker` module (volume and locally built image export without network or helper images, content digests, skipping the restore when `Docker.raw` arrived intact, `compose up --pull never`) and `databases` (safety-net `pg_dumpall`, clean stop, `--restore-dump`). Adds free-space checks with a 10 GB reserve, `prepare --stage` on an encrypted APFS volume, export fingerprint checks before restore, and skipping steps whose inputs are unchanged on re-run.
**Success Criteria**: integration tests against a real Docker Desktop export, delete, and restore a volume with a matching digest. A non-empty mismatched volume is never overwritten: `mtm` prints the commands that remove it, and `finish` then restores it.
**Ordering**: the `databases` module must come before `homebrew` in the module list. Its `pg_dumpall` needs Postgres running, and `homebrew` stops the service during `prepare`.
**Tests**: `integration`-tagged Docker and Postgres tests; unit tests for the content digest and the free-space and encrypted-volume checks.
**Plan**: `docs/superpowers/plans/2026-11-02-mtm-stage3-docker-postgres.md`
**Status**: Complete

### Known gaps carried out of Stage 3
Found in the final review.
- **Real-run validation pending.**
  - `prepare --stage` has not run on a real encrypted external drive.
  - The docker module's prepare and finish have run only against fakes. The real-engine tests cover `mtm-test` round trips.
  - The Postgres integration test has never run: there is no Homebrew Postgres on the author's Mac.
- **Docker:**
  - Data written inside containers, outside any volume, isn't exported or checked.
  - Volume archive names can collide on a case-insensitive volume (`Data` and `data`).
  - A `--stage` drive that mounts under another name can't be found by `finish`; there is no `finish --stage`.
  - Printed `mtm prepare --only …` fixes omit `--stage`.
  - `docker compose up` may fetch remote `include:` or `extends:` resources.
  - Stale archives from earlier runs aren't removed.
  - Volumes mtm can't export are reported only by scan.
  - A stream cut exactly at a tar block boundary digests cleanly; this relies on docker cp's exit status.
  - A settlement in `docker-finish.json` isn't tied to the record's identity.
  - verify fails without Docker Desktop even when nothing was recorded.
- **Postgres:**
  - `PGPASSFILE=/dev/null` makes libpq warn on every connection.
  - Database names with spaces are split.
  - A connection failure in verify (password auth, a renamed account) has no fix text.
  - Scan's space check ignores dump sizes.
  - A failed psql load's stderr still reaches the log.
- **Free space** comes from statfs `f_bavail`, which leaves out APFS purgeable space.

## Stage 4: Manual steps and `retire`
Stage 4 is planned and built in two parts, so each part ships working software and gets its own review.

### Stage 4, Part 1: Catalog, checklist, and permissions
**Goal**:
- the TOML app catalog, seeded from `tape` (BSD-2);
- the `checklist` engine and `mtm checklist` (a walk-through, plus done, skip with a reason, and open), persisted in `state.json`, with System Settings deep links;
- checklist items from `finish` (`permissions`, the catalog's license and note steps through `system`, and login items through `loginitems`);
- the `permissions` module;
- the catalog's volatile paths in the `files` module.

The verdict starts counting open checklist items.

**Success Criteria**: an open checklist item blocks SAFE TO WIPE, and skipping it with a reason unblocks it. A passing check can resolve items. Catalog files are validated in tests.
**Tests**:
- catalog validation tests over every built-in file;
- checklist merge and state tests;
- `mtm checklist` CLI tests, including the walk-through;
- `permissions` unit tests with sqlite3 output fixtures;
- the no-network test runs `permissions` too.

**Plan**: `docs/superpowers/plans/2026-11-02-mtm-stage4-part1-catalog-checklist-permissions.md`
**Status**: Complete

#### Known gaps carried out of Stage 4, Part 1
Found in the final review.
- **Real-run validation pending:**
  - `permissions` with Full Disk Access is tested only with fixtures of sqlite3 output;
  - the macOS 26 System Settings deep links are unchecked (spec 12.5);
  - the catalog's bundle ids and required permissions haven't been checked against real installs.
- **Links:** `mtm checklist` opens any `x-apple.systempreferences:` link, not only the exact set mtm generates.
- **Terminal detection:** a character device on stdin, such as `/dev/null`, counts as a terminal, so `mtm checklist </dev/null` starts the walk-through.
- **Note step ids are positional** (`system:note:<id>:<n>`). Editing an app's notes can attach an old status to new text.
- **The macOS version is read twice:** finish and verify run `sw_vers` again in `permissions` and `loginitems` instead of using the version preflight read.
- **Stale steps** are never removed when the record or the catalog changes. They stay open, which is NOT SAFE, until skipped.
- **Catalog edge cases:**
  - Automation permissions in the catalog can't resolve, because they have no target;
  - scan's permission count includes services System Settings doesn't show.
- **Smaller test gaps** are listed in the final review in `.superpowers` (or in the plan's review history).

### Stage 4, Part 2: Browsers, secrets, and `retire`
**Goal**:
- the `browsers` module (Chromium profiles and extensions, Safe Storage presence, Firefox, Safari);
- the `secrets` module (SSH and GPG identities, Secure Enclave keys, password managers from the catalog);
- `mtm retire` (changes on the old Mac since `prepare`, plus the retirement checklist, with the catalog's license deactivation steps).

**Success Criteria**: `retire` lists a file changed on the old Mac after `prepare`. SSH and GPG identities are critical checks.
**Tests**: module unit tests with fixtures; a `retire` integration test on a temp home folder.
**Plan**: `docs/superpowers/plans/2026-11-03-mtm-stage4-part2-browsers-secrets-retire.md`
**Status**: Complete

#### Known gaps carried out of Stage 4, Part 2
Found while planning.
- **Real-run validation pending:**
  - Arc's Safe Storage item name, and the Bitwarden and Secretive agent sockets in the catalog, haven't been checked against real installs;
  - Firefox is tested only with fixture files;
  - the gpg fixtures were written from GnuPG's documented colon format: a scratch GnuPG folder's path was too long for gpg-agent's socket;
  - whether prepare's gpg listing writes into `~/.gnupg` (trust database, keyring) after the files walk, so that retire lists mtm's own writes as changes.
- **Safe Storage is checked for presence only.** A browser that made a new key on the new Mac passes, though the old Mac's passwords and cookies can't be decrypted with it. Comparing the keys would mean reading them. Comparing the item's creation date (`cdat`), which `security` prints without `-g`, could close this without reading the key; it needs checking on a real Mac.
- **SSH agents:** `Include` files in `~/.ssh/config` aren't followed, and only the terminal's `SSH_AUTH_SOCK` is seen.
- **An `sk-` key can be on a security key, which moves, or in the Secure Enclave, which doesn't.** mtm can't tell which, so its step covers both.
- **retire:**
  - it lists only home-folder changes, not data elsewhere, such as Postgres in `/opt/homebrew/var` or Docker volumes, if services or containers ran again after `prepare`;
  - it can't tell a change Migration Assistant already copied from one made after it;
  - a file changed inside a folder `prepare` couldn't read isn't listed, since nothing in it was recorded;
  - a step a newer catalog adds lands after the erase step;
  - it exits 1 whenever shell history changed, which running it in a shell with shared history does.
- **Touch ID's count** comes from `bioutil`'s text output. If the format changes, the count is unknown and the step is listed anyway.

## Stage 5: Ship
Stage 5 is built in two parts. The first ships everything but the first release. The second runs on the new Mac after the author's own migration, which is mtm's first real-world run. This Mac has too little free disk for Tart's macOS VM images.

### Stage 5, Part 1: CI, release pipeline, and docs
**Goal**:
- `LICENSE` (MIT), `README.md`, `CONTRIBUTING.md` (including how to add a catalog app), and `SECURITY.md`;
- GitHub Actions CI on `macos-26` (`gofmt`, `go vet`, `go test -race`, `govulncheck`, catalog validation);
- the GoReleaser release pipeline: `darwin/arm64`, checksums, provenance attestations, and a formula in `momentmaker/homebrew-tap`;
- a snapshot release dry run in CI.

Nothing is tagged or released.

**Success Criteria**: CI passes on GitHub for a push. The snapshot dry run builds the `darwin_arm64` archive, `checksums.txt`, and a formula, and the built binary reports its version.
**Tests**: `TestReadmeShowsTheCurrentUsage`; the CI run on GitHub, including the dry run.
**Plan**: `docs/superpowers/plans/2026-11-03-mtm-stage5-part1-ci-release-docs.md`
**Status**: In Progress

#### Known gaps carried out of Stage 5, Part 1
Found while planning.
- **The CI and release files ran first on GitHub.** The author's Mac couldn't run actionlint or GoReleaser, so a branch's first push was their first run. The release workflow runs only on a tag, so its first run is `v0.1.0` (Part 2).
- **GoReleaser marks `brews` deprecated** in favor of Homebrew casks, so `goreleaser check` exits 2. CI's dry run builds a snapshot instead.
  - A cask for an un-notarized binary needs its quarantine flag removed, which mtm won't do.
  - If GoReleaser removes `brews`, the formula must be written another way.
- **The author's Go toolchain:** Go 1.26.1 has GO-2026-4869 (`archive/tar`), so `govulncheck` fails on it until Go is upgraded to 1.26.2 or later. CI and releases take the newest 1.26 patch.

### Stage 5, Part 2: Real migration, end-to-end suite, and first release
**Goal**:
- the author's own migration to the new Mac with mtm, which settles the "Real-run validation pending" items above;
- the Tart end-to-end suite: a cloned VM as a perfect migration, with injected faults;
- the `HOMEBREW_TAP_TOKEN` secret, the `v0.1.0` tag, and a working `brew install momentmaker/tap/mtm`.

**Success Criteria**: tagging a release produces checksummed, attested binaries and a working `brew install`. The Tart end-to-end suite passes locally.
**Tests**: the Tart suite; the release itself.
**Status**: Not Started
