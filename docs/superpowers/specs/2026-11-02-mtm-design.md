# mtm: Design

- **Date:** 2026-11-02
- **Status:** Draft, awaiting review
- **Scope of this document:** version 1 of `mtm`, an open-source command-line tool for moving a developer's Apple Silicon Mac to a new Apple Silicon Mac.

## 1. Intent

**What the user asked for.** Move everything from the current Mac to a brand-new one so it feels like nothing changed except more compute and RAM: dev environment, every project, files, folders, apps, browsers. Open source, so anyone switching Macs can use it.

**Success looks like.**
- On the first day on the new Mac, the terminal, toolchains, projects, containers, apps, and browser sessions work the way they did on the old Mac.
- Before the old Mac is erased, the tool proves nothing important was lost and says **SAFE TO WIPE**, or lists exactly what is missing.
- The steps no tool can automate (privacy permissions, Touch ID, passkeys, licenses) are listed, explained, and tracked until done.

**Usage context (from the user).**
- Both Macs are on the desk together on switch day. The old Mac is wiped, sold, or traded in soon after, so the transfer happens once and must be verified before the wipe.
- Audience: developers comfortable in a terminal. Interface: a CLI installed through Homebrew.
- Security is a hard requirement (section 6).

## 2. Scope

**Version 1**
- Commands: `scan`, `prepare`, `finish`, `verify`, `checklist`, `retire`, `cleanup`.
- Modules: `files`, `git`, `homebrew`, `toolchains`, `docker` (Docker Desktop), `databases` (Homebrew Postgres), `loginitems`, `permissions`, `browsers` (Chrome, Brave, Arc, Safari, Firefox), `secrets`, `system`.
- An app catalog covering the apps on the author's Mac plus common developer apps.
- Supported systems: Apple Silicon on both Macs, macOS 26 or later on both Macs.

**Later**
- OrbStack and Colima.
- MySQL and Redis dumps.
- Export to chezmoi or nix-darwin.
- A native Mac app.
- macOS 15 support.

**Never**
- Intel Macs.
- Keeping two Macs in sync.
- Migrating from a Time Machine backup. `mtm` detects it and directs the user to a direct Mac-to-Mac migration (section 3).
- Copying the bulk of the disk itself. Migration Assistant does that.

## 3. Background: what Migration Assistant does and doesn't do

Apple publishes no list of what Migration Assistant skips. The findings below come from testing reports and vendor docs. Rows marked *verified* were checked against the cited source while writing this spec. The rest come from the research survey and will be confirmed when the author migrates their own Mac (section 12).

| Area | Behavior on a direct Apple Silicon → Apple Silicon migration | Source |
|---|---|---|
| Home folder, `/Applications`, `/opt/homebrew`, `/usr/local`, `/Library` additions | Copied. Toolchains keep working because the architecture and paths are the same. | eclecticlight.co (2024-11-15); Homebrew discussion #6290 |
| Login keychain | Copied by Migration Assistant. *Verified.* Copying `login.keychain-db` by hand **fails** on Secure Enclave Macs running macOS 26. *Verified.* | eclecticlight.co (2026-09-11); derflounder (2026-09-08) |
| Login keychain from a Time Machine backup | Not confirmed to work on 26.4+. | eclecticlight.co (2026-09-11) |
| Docker Desktop VM disk (`Docker.raw`) | Time Machine excludes it. Docker recommends `docker image save` plus volume backups, and also documents copying `Docker.raw` as a recovery method. *Verified.* Whether Migration Assistant copies it intact is unconfirmed. | docs.docker.com, backup-and-restore |
| Privacy permissions (Full Disk Access, Accessibility, Screen Recording) | Not migrated on macOS 14 and 15. macOS 26 adds a "Privacy & Security Settings" checkbox whose scope is undocumented. Plan to re-grant. | MacRumors forum thread 2408854; Apple Mac User Guide mh27921 |
| "Local Items" and device-only keychain items, Secure Enclave keys, passkeys | Never migrated, except through iCloud Keychain sync. | Apple Keychain Access guide kyca1121; Secretive README |
| Google sign-in in Chromium browsers | Device Bound Session Credentials tie Google sessions to the Secure Enclave, so the user signs in again. | helpnetsecurity.com (2026-04-10) |
| Touch ID, iCloud session | Not migrated. | Apple support 105095 |
| Login items, background-item approvals | LaunchAgent files are copied. The Login Items list and approvals are not restored. | MacRumors forum thread 2408854 |
| Hardware-bound licenses | JetBrains, Adobe (two activations), and Office need reactivation. | vendor support pages |
| Files stored only in the cloud (iCloud Drive with "Optimize Mac Storage", File Provider placeholders) | Not downloaded by Migration Assistant. They return through the cloud provider. | Apple behavior; `mtm` must not read these (section 8.3) |

**Conclusion.** For this scope, copying files is not the gap. The gaps are:
- container, VM, and database data;
- state bound to hardware or security (privacy permissions, Secure Enclave keys, passkeys, Google sessions, Touch ID);
- login items and licenses;
- the lack of any check that the copy is complete before the old Mac is erased.

## 4. Approach

**Chosen: `mtm` works alongside Migration Assistant.** Migration Assistant does the bulk copy, including the login keychain, which only it can move. `mtm` runs before it to export what Migration Assistant handles badly and to write a record of the old Mac. `mtm` runs after it to restore those exports, guide the manual steps, and check the new Mac against the record. The record travels inside the Migration Assistant copy, so verification needs no network connection between the Macs.

**Rejected**
- **Our own copy over Thunderbolt or SSH instead of Migration Assistant.** It cannot move the login keychain on macOS 26, so browser sessions and signing keys would be lost. It also means reimplementing Migration Assistant's handling of ownership, extended attributes, and sandbox containers, at higher risk.
- **Declarative rebuild of a fresh Mac** (Brewfile, dotfiles, app setting definitions). It produces a clean machine but loses app state, sessions, and anything without a definition, which contradicts "as is". It is a possible later export format.

## 5. User flow

| Step | Where | Command | What happens |
|---|---|---|---|
| 1 | Old Mac | `mtm scan` | Read-only report in three groups: handled by Migration Assistant, handled by `mtm`, manual with a guide. **Blockers** (prepare refuses until fixed): not Apple Silicon, macOS older than 26, FileVault off, export size larger than free space. **Warnings**: repos with unpushed commits, stashes, untracked or secret-like ignored files (Migration Assistant copies them, but nowhere else has them), Intel-only apps that need Rosetta, and no Full Disk Access. |
| 2 | Old Mac | `mtm prepare` | Exports Docker volumes and locally built images, dumps Postgres, stops services cleanly, writes the record to `~/.mtm/`. Records the prepare time and prints Migration Assistant instructions. Must be the last step on the old Mac before Migration Assistant. |
| 3 | Both | Migration Assistant | The user runs it **directly Mac to Mac** (Thunderbolt cable recommended), **during the new Mac's first-boot setup** so no second user account is created, with every category selected. |
| 4 | New Mac | `mtm finish` | `mtm` is already present because Migration Assistant copies `/opt/homebrew`. Restores Docker data only if Migration Assistant didn't bring it intact, restarts services, re-registers launch agents, then starts the checklist. |
| 5 | New Mac | `mtm checklist` | Walks through the manual steps, when run in a terminal. Each opens the right System Settings pane and waits for done, for a skip with a reason, or for later. Elsewhere it lists the open steps. `list [--all] [--json]`, `done <id>…`, `skip <id> --reason "…"`, and `open <id>` do the same one step at a time. |
| 6 | New Mac | `mtm verify` | Compares the new Mac with the record. Prints **SAFE TO WIPE** or **NOT SAFE TO WIPE** with the reasons. Can be run any number of times. |
| 7 | Old Mac | `mtm retire` | Lists anything on the old Mac that changed after `prepare`. Then walks through deactivating licenses, signing out of iCloud and turning off Find My, removing old Secure Enclave keys from services, and "Erase All Content and Settings". `mtm` never erases anything itself. |
| 8 | New Mac | `mtm cleanup` | After SAFE TO WIPE, deletes the exports and the record, including a `prepare --stage` folder when its drive is attached. Reminds the user to revoke the terminal's Full Disk Access. Refuses before SAFE TO WIPE unless `--force` is passed. |

Every command can be re-run. `scan` and `verify` accept `--json`. `prepare`, `finish`, and `verify` accept `--only <module>`.

## 6. Security model

### 6.1 Assets and threats
- **Assets:** database dumps and Docker volume exports (may contain production-like data and secrets), the file record (reveals file names), the user's secrets on disk (`.env`, `~/.ssh`, `~/.gnupg`, keychain), and the old Mac after it leaves the user's hands.
- **Threats:**
  - exports left behind on disk or in backups;
  - exports staged on an unencrypted external drive;
  - secrets leaking into logs or bug reports;
  - a tampered or corrupted export restored onto the new Mac;
  - a malicious or vulnerable `mtm` release;
  - an old Mac sold with recoverable data or Activation Lock;
  - `mtm` weakening the system's security settings.

### 6.2 Rules `mtm` follows
1. **It never reads secret contents.** No keychain secrets, private keys, cookies, or saved passwords. Presence checks only: for example, `security find-generic-password -s "Chrome Safe Storage"` without `-w` or `-g`, which does not reveal the value. Secret files are fingerprinted (SHA-256) and never copied or printed.
2. **It never makes network connections.** No telemetry, no update checks, no listening ports. Steps that need the network, such as pulling registry images, are printed for the user to run.
3. **It never weakens security.** It never turns off SIP or lowers the boot security policy, and it never stores the user's password. Steps that need root are printed as exact commands for the user to run with `sudo`.
4. **It never deletes or overwrites user data.** `prepare` only writes inside `~/.mtm/` (or the `--stage` folder). A Docker volume that holds data is never overwritten: `mtm` prints the commands that remove it, and once the user runs them, `finish` restores the volume. A database restore refuses on a non-empty cluster without `--force`. `cleanup` deletes only `~/.mtm/` and the `--stage` folder.
5. **It never reads cloud-only files** (section 8.3), which would trigger downloads.

### 6.3 Protecting what `mtm` creates
- `~/.mtm/` is created with mode `0700` and its files with `0600`. `mtm` refuses to run if the directory exists with looser permissions or a different owner.
- `~/.mtm/` is excluded from Time Machine with `tmutil addexclusion`.
- **FileVault must be on** on both Macs (`fdesetup status`). `prepare` and `finish` refuse to run without it. On a FileVault disk the exports are no more exposed than the original data beside them, so `mtm` adds no separate encryption there.
- **External staging** (`prepare --stage /Volumes/X`) is allowed only on an encrypted APFS volume that honors file ownership. `statfs` gives the file system type and the ignore-ownership flag; `diskutil info -plist` gives encryption. Exports go in `<folder>/mtm-exports`, created with mode `0700` and excluded from Time Machine, and the manifest records that path for `finish`. `mtm` uses Apple's encryption and implements no cryptography of its own.
- Every export gets a SHA-256 fingerprint in `manifest.json` when written. `finish` checks the fingerprint before restoring and refuses on a mismatch.
- Reports and logs contain paths, counts, and statuses only, never fingerprints, file contents, or environment values. Command arguments are logged with values of known secret flags and environment variables redacted. Reports are safe to paste into a public issue.

### 6.4 Permissions `mtm` needs
- It runs as the user.
- Full Disk Access for the terminal is optional. Without it, `scan` lists each location it could not read and what that means for verification. For example, privacy permissions are inferred from the catalog instead of read from the privacy permissions database.
- `cleanup` reminds the user to revoke Full Disk Access.

### 6.5 Retiring the old Mac (`mtm retire`)
The checklist covers:
- deactivating licenses;
- signing out of iCloud and turning off Find My, which removes Activation Lock;
- removing the old Mac from the Apple Account device list;
- removing the old Mac's Secure Enclave SSH keys and passkeys from GitHub and servers once their replacements work;
- "Erase All Content and Settings", which destroys the disk encryption key.

### 6.6 Supply chain
- Go with the standard library. The only third-party dependency is `github.com/BurntSushi/toml` (no transitive dependencies) for the catalog.
- Dependencies are pinned in `go.sum`.
- CI runs `govulncheck` on every change.
- Release binaries come with SHA-256 checksums and GitHub build provenance attestations.
- Distributed through a Homebrew tap under the project's GitHub organization.
- Apple notarization of release binaries is planned and depends on an Apple Developer Program membership (section 12).

## 7. Architecture

### 7.1 Language
Go. Reasons:
- One self-contained binary.
- The standard library covers almost everything: process execution, archives, gzip, SHA-256, JSON.
- The Go team ships an official vulnerability scanner.
- chezmoi shows the language works well for this kind of tool.

macOS-specific facts come from built-in command-line tools: `defaults`, `launchctl`, `tmutil`, `fdesetup`, `security`, `diskutil`, `sqlite3`, `lipo`, `dscl`, `mdls`, `brew`, `git`, `docker`.

### 7.2 Package layout
```
cmd/mtm/            entry point, subcommand parsing (standard library flag package)
internal/runner/    the only place system commands are executed; fakeable in tests
internal/stage/     ~/.mtm/ lifecycle: permissions, Time Machine exclusion, free-space checks, export fingerprints, external volume checks
internal/manifest/  manifest.json and files.jsonl.gz schemas, reading and writing, schema version
internal/catalog/   loads and checks the embedded TOML app definitions; the privacy-pane table with deep links
internal/checklist/ manual steps: their ids, status (pending, done, skipped with a reason), and merging on re-runs
internal/report/    terminal and JSON rendering, redaction
internal/verdict/   combines module checks and checklist state into SAFE / NOT SAFE
internal/modules/   one package per module (section 9)
catalog/            TOML app definitions and embed.go, which builds them into the binary; README.md documents the format and credits tape
```

### 7.3 Module contract
```go
type Module interface {
    Name() string
    Scan(env Env) ([]Finding, error)
    Prepare(env Env, st *stage.Stage) (Result, error)
    Finish(env Env, st *stage.Stage) (Result, error)
    Verify(env Env, st *stage.Stage) ([]Check, error)
}
```
- `Env` carries the runner, home directory, and the current mode (old Mac or new Mac). Logging wraps the runner. Modules that need the catalog get it through their constructors (`files.New`, `system.New`, `permissions.New`). `Env.Manifest` is the record: on the new Mac it is what `verify` and `finish` check against; during `prepare` it holds this Mac's previous record, if any, so a re-run can carry forward what the first run changed (for example, services it stopped).
- Modules whose `prepare` changes the old Mac also implement `Undo(env, st) (Result, error)`, which `prepare --undo` calls. After a successful undo, that module's record is removed, so `verify` reports it not prepared until `prepare` runs again.
- `Finding` has: module, id, severity (`info`, `warn`, `blocker`), handler (`migration-assistant`, `mtm`, `manual`), title, detail, fix.
- `Result` has: what was done, artifacts written (each with its fingerprint), and problems.
- Modules that leave steps for the user on the new Mac also implement `Steps(env) ([]checklist.Item, error)` (`Stepper`), which works from the record alone. On the new Mac, `finish` and `verify` both merge every recorded module's steps into `state.json`, so `verify` lists them even when `finish` didn't run, or ran with `--only`. A step already there keeps its status and gets the new text, so either command can run again. A `finish` that adds a step clears the last verify result, so `cleanup` waits for a new SAFE TO WIPE.
- A `Check` can name the checklist items it proves done (`Resolves`). When it passes, `verify` marks them done, as the `permissions` module does for each grant it finds.
- `Check` has: module, id, status (`pass`, `fail`, `warn`, `info`), critical flag, title, detail.
- Modules never print. All output goes through `report`.

A module that does not apply to the machine (for example, Docker not installed) returns empty results.

### 7.4 Catalog
One TOML file per app, `catalog/<id>.toml`. Example:
```toml
id = "raycast"
name = "Raycast"
bundle_id = "com.raycast.macos"

[[permissions]]
service = "Accessibility"

[license]
reactivate = false

[state]
volatile = ["~/Library/Application Support/com.raycast.macos/*.log"]
```
- Fields:
  - identity: `id` (lowercase words joined by hyphens, matching the file name), `name`, and `bundle_id`;
  - required privacy permissions: `[[permissions]]`, each with `service` and an optional `client`, for when macOS records the grant for a helper rather than the app;
  - license reactivation: `[license]`, with `reactivate` and a `note`. Deactivate steps for `retire` come with `retire`.
  - per-app checklist `notes`;
  - `[state]` `volatile` paths (`~/` patterns of files the app deletes on its own). The `files` module treats a missing one as info.
- The tests reject a file with an unknown key, an unknown service, a malformed id or bundle id, or a bundle id already in another file.
- Privacy panes live in one table in `internal/catalog`, with each pane's name, its privacy-database service id, and its System Settings deep link per macOS major version. They are not repeated in each app file. A macOS major without a checked link gets the pane's name and no link.
- Initial data is seeded from the `tape` project's definitions (BSD-2-Clause, credited). mackup's GPL-3 database is not copied.
- Adding an app means adding a file. The contributor guide documents the format, and CI validates every file.

## 8. Data

### 8.1 Layout
```
~/.mtm/
  manifest.json      schema version, mtm version, macOS version and build, hostname,
                     machine id (SHA-256 of the hardware UUID, never the raw UUID),
                     prepare time, per-module inventories, export fingerprints
  files.jsonl.gz     file record, one JSON object per line
  Brewfile           output of brew bundle dump, kept as a fallback
  exports/           or <stage folder>/mtm-exports with prepare --stage
    docker/volumes/  one <volume>.tar.gz per volume (docker cp output, gzip-compressed)
    docker/images/   one <image id>.tar per locally built image (docker image save output)
    postgres/        one <cluster>.sql.gz per running cluster (pg_dumpall output)
  docker-finish.json new Mac only: volumes finish found intact or restored
  state.json         new Mac only: checklist steps with their status and skip reasons, last verify result
  logs/              one log file per command run, redacted
```
All paths in the record are relative to the home folder, so verification works if the username differs on the new Mac.

Checklist steps aren't in `manifest.json`. They follow from the record through each module's `Steps` (section 7.3), and `state.json` keeps them with what the user did with each.

### 8.2 File record entry
```json
{"p":"GitHub/mtm/.env","t":"f","s":512,"m":1793500000123456789,"mode":384,"h":"sha256:9f2c…","area":"critical"}
```
- `p` path, `t` type (file, directory, symlink), `s` size, `m` modification time in Unix nanoseconds, `mode` permission bits.
- `h` fingerprint: only for important files, or for every file with `--deep`.
- `area`: `critical` or `appstate`.
- Symlinks store their target instead of a fingerprint.

### 8.3 What the record covers
- **Roots:** the home folder. `/Applications` is recorded as an app inventory (bundle id, version, architectures) by the `system` module, not file by file.
- **Areas:** everything in the home folder outside `~/Library` is `critical`. `~/Library` is `appstate`. Browser profiles inside `~/Library` are also checked by the `browsers` module, whose checks can be critical (section 9.9).
- **Fingerprinted by default:** dotfiles at the top of the home folder, `~/.ssh`, `~/.gnupg`, `~/.config`, untracked and important ignored files in repos (section 9.2), and all exports.
- **Excluded:** `~/Library/Caches`, sandboxed app caches (`~/Library/Containers/*/Data/Library/Caches`, `~/Library/Group Containers/*/Library/Caches`), `~/.Trash`, `~/Library/Logs`, `~/.mtm`, and the Docker Desktop VM folder (checked by the `docker` module instead). Time Machine exclusions are deliberately not used: a direct Migration Assistant transfer copies those paths anyway, and checking each one would cost a command per file.
- **Cloud-only files:** files with the `SF_DATALESS` flag are recorded as `cloud` with size and time but never opened. Opening them would start a download.
- **Unreadable locations** (privacy-protected without Full Disk Access) are recorded as `unreadable` and listed in the report.

### 8.4 How `verify` judges each recorded file
| Situation on new Mac | Outcome |
|---|---|
| Present, same size, time, and fingerprint (if recorded) | pass |
| Present, modification time later than prepare time | info: changed after `prepare`, by an app or by the user |
| Present, same modification time, but different size or fingerprint | **fail**: corruption |
| Missing, path matches a volatile pattern | info |
| Missing, `critical` area | **fail** |
| Missing, `appstate` area | warn |
| Recorded as `cloud` | info: expected to return through the cloud provider |

Files on the new Mac that are not in the record are ignored. Reports group results by top-level folder (by the first two levels inside `~/Library`), one check per group. A group's status is `fail` if any file in it failed, otherwise `warn` if any file warned, otherwise `pass`. Info outcomes are counted in the group's detail and leave the status at `pass`. `--details` lists every path.

### 8.5 Verdict
**SAFE TO WIPE** requires all of:
1. No check in any module has status `fail`. This includes the preflight checks: FileVault is on, and `verify` is running on a different Mac from the one that ran `prepare` (compared by machine id).
2. Every check marked critical has status `pass`, not `warn` or `info`. Critical checks are those from `git`, `secrets` identities, `docker` volumes, `databases`, and `critical`-area files.
3. Every checklist item is either done or skipped with a recorded reason (`mtm checklist skip <id> --reason "..."`).

Otherwise **NOT SAFE TO WIPE**, followed by the blocking items. Exit codes: `0` safe, `1` not safe, `2` `mtm` itself failed. The other commands use `0` success, `1` completed with problems (such as NOT READY), and `2` `mtm` failed.

## 9. Modules

### 9.1 `files`
- `prepare` walks the home folder and writes `files.jsonl.gz` (section 8).
- `verify` walks the same roots on the new Mac and applies section 8.4.
- Fingerprinting streams with a bounded worker pool.

### 9.2 `git`
- **Finding repos:** `.git` directories under the home folder, excluding `~/Library` and `node_modules`. Search depth is configurable.
- **Every git command** runs with `--no-optional-locks` and `-c core.fsmonitor=false`, so `mtm` never rewrites a repo's index or starts background daemons.
- **Recorded per repo:**
  - local refs with commit hashes (`git for-each-ref`). Remote-tracking refs are skipped, because their commits exist on the remote;
  - HEAD (symbolic ref and commit);
  - the stash list;
  - status (`git status --porcelain=v2 --branch -z --untracked-files=all --ignored=matching`; NUL-separated, so any file name parses);
  - the number of commits not on any remote (`git rev-list --count --branches --not --remotes`). This also covers local branches without an upstream;
  - fingerprints and modification times of untracked files, and of ignored files matching the important-file patterns (`.env*`, `*.pem`, `*.key`, `*.p12`, `*.local.*`).
- **`scan`:** unpushed commits, stashes, untracked files, and important ignored files are warnings. Migration Assistant copies them, but they exist nowhere else once the old Mac is wiped.
- **`verify`** (a critical check per repo) fails when:
  - the repo is missing or unreadable;
  - any recorded commit (refs, HEAD, stash) is missing from the object database (`git cat-file --batch-check`);
  - an important or untracked file is missing;
  - an important or untracked file's content differs and it was not modified after `prepare`.
- **Notes, not failures:** refs that moved and status changes since `prepare`, such as a fetch or a commit on the new Mac.
- **`--deep`** adds `git fsck --connectivity-only`.

### 9.3 `homebrew`
- Every `brew` command runs with `HOMEBREW_NO_AUTO_UPDATE=1` and `HOMEBREW_NO_ANALYTICS=1`, so Homebrew makes no network calls on `mtm`'s behalf (section 6.2, rule 2). A test runs each `brew` command `mtm` uses under a `sandbox-exec` profile that denies network access and asserts it still succeeds (section 11).
- `scan` and `prepare` record the output of `brew services list --json`. `prepare` also saves `brew bundle dump --file=- --formula --cask --tap --mas` into `~/.mtm/Brewfile` (written by `mtm` with mode 0600). The type flags matter: without them, `brew bundle dump` also asks cargo, go, npm, and VS Code for their packages, and cargo's rustup reaches the network. Homebrew has removed `--describe`, and the dump now includes descriptions by default.
- **Package-data cache.** Before any other brew command, `mtm` checks that Homebrew's API cache (`$(brew --cache)/api`) holds package data. Without it, brew would download from formulae.brew.sh on `mtm`'s behalf. In that case, `scan`, `finish`, and `verify` warn, and `prepare` blocks, keeping the earlier record. The message is: run `brew update` yourself (it uses the network), then run the command again.
  - Known gap: Homebrew 7 reads only the package file for the running macOS. A new Mac on a newer macOS than the old one can pass this check and still download. See IMPLEMENTATION_PLAN.md.
- **Who owns a service.** `brew services list` run as the user reports:
  - `brew services run` services with no user;
  - root services (in `/Library/LaunchDaemons`) as `none` or `stopped`, with user `root`, because the user can't see the system domain.
  - So a service with no user, or with this user, and any status other than `none` or `stopped`, is the user's.
  - A service with user `root` or another user is a root service. Whether it is loaded is checked with `launchctl print system/<label>`, which works without root.
- **`prepare`:**
  - Stops the user's active services with `brew services stop` and records them.
  - On a re-run, services the earlier run stopped stay recorded as stopped, so `finish` still restarts them.
  - A root service that is loaded is a **blocker**: stop it with `sudo brew services stop <name>`, then run `prepare` again. Its data would otherwise be copied mid-write. It is recorded with its label, so `finish` prints the restart command.
  - A user service that fails to stop is a blocker for the same reason.
  - A failed `brew services list` blocks and keeps the earlier record.
- **`prepare --undo`:** restarts what `prepare` stopped. Root services are printed as `sudo brew services start` commands. Names in every printed command are shell-quoted.
- **`finish`:** restarts the same services on the new Mac, with `brew services start` (so a `run` service now also starts at login). It prints the same kind of commands for root services.
- **`verify`:**
  - Homebrew is present, which is a fail otherwise.
  - `brew bundle check --file ~/.mtm/Brewfile --no-upgrade --verbose`: missing packages are a warn, with the `brew bundle install` command.
  - The user's services that were stopped are `started` again, and root services are loaded again (`launchctl print system/<label>`). Each is a warn otherwise.

### 9.4 `toolchains`
- Records path and version for: brew, git, node, npm, pnpm, bun, deno, python3, uv, go, rustc, cargo, ruby, java, asdf, mise.
- **Finding the PATH:**
  - The login shell comes from `dscl . -read /Users/<user> UserShell`. It may be fish, not `$SHELL`.
  - The PATH is read from that shell once:
    - fish uses `-l -c`;
    - zsh and bash use `-l -i -c`, because developers often set PATH in `.zshrc` or `.bashrc`, which only interactive shells read;
    - other shells use `-l -c`.
  - The shell prints the PATH after a marker, so stray output from startup files and a non-zero exit after printing don't matter. The shell has a 20-second timeout.
  - Each tool is then found in that PATH and run directly (`--version`; `go version`; `java -version` on stderr), with a 10-second timeout.
- Paths inside the home folder are recorded as `~/…`, so they compare across usernames.
- The global `~/.tool-versions` and `mise` config are dotfiles, which the `files` module already fingerprints.
- `verify` requires the same paths and versions on the new Mac. A difference, a missing tool, or a login shell that no longer prints a PATH is a fail, with both values shown.

### 9.5 `docker` (Docker Desktop)
- **Which Docker.** `mtm` runs Docker Desktop's own CLI (`/Applications/Docker.app/Contents/Resources/bin/docker`), not whichever `docker` is first on `PATH`, so the client matches the engine. Every command sets `DOCKER_HOST=unix://~/.docker/run/docker.sock` and unsets `DOCKER_CONTEXT`. A context or a `DOCKER_HOST` the user set could otherwise point `mtm` at another machine. The socket lives in the home folder, so a test run with a temporary `HOME` can't reach the developer's real Docker. Docker Desktop counts as running when that socket exists and `docker version` reaches the engine.
- **Pulled or built.** With Docker's containerd image store (the default in current Docker Desktop), every image has a repo digest, built ones too, so repo digests can't tell the two apart. Docker Engine 29 records where an image came from: `Identity.Pull` for a pulled image, `Identity.Build` for a built one, and neither for an imported or loaded one. With the containerd store (its `docker image inspect` has a `Descriptor`), an image counts as pulled only when `Identity.Pull` is set. With the classic store, an image counts as pulled when it has a repo digest. Everything else counts as locally built and is exported. Untagged images are skipped.
- **`scan`:**
  - reports counts and total sizes, not each item: volumes (sizes from `docker system df --verbose`), locally built images (sizes from `docker image inspect`, which match what `docker image save` writes) and pulled images, and running containers, plus the export size. It doesn't list each item;
  - estimates the export size from volumes plus locally built images. It is a **blocker** when that size doesn't fit beside the 10 GB reserve on the home volume, with the fix `mtm prepare --stage`.
  - warns about a container with restart policy `always` that mounts a volume. It restarts whenever Docker starts, so on the new Mac it would change its volume before `finish` checks it. The fix is `docker update --restart unless-stopped <name>`.
  - warns about a running container started with `--rm` (`HostConfig.AutoRemove`): stopping it deletes it and its anonymous volumes, so `prepare` won't stop it.
  - warns about volumes it can't export: a driver other than `local`, or driver options such as a bind to a folder.
- **`prepare`:**
  - Without Docker Desktop installed, it records "not installed". When Docker Desktop isn't running, a re-run keeps the earlier record with a warning; without one, the module fails with "start Docker Desktop and run `mtm prepare --only docker`".
  1. Removes leftover temporary containers from an interrupted run (label `dev.mtm.temporary`).
  2. Checks the whole export fits (section 10) **before** stopping anything. A running container started with `--rm` is a blocker, before anything stops: stopping it would delete it and its anonymous volumes.
  3. Stops running containers one at a time (`docker container stop --time 60`, so databases shut down cleanly) and records them. On a re-run, containers the earlier run stopped stay recorded while they still exist and aren't running. A stop failure is a blocker; the volumes are then recorded without content digests, so `verify` fails them until `prepare` succeeds.
     Then it lists the volumes again; one that disappeared is a blocker and is recorded without a digest, so `mtm` never vouches for a volume a holder would re-create empty.
  4. Exports each volume without network or a helper image:
     - `docker image import` a 1024-byte empty tar archive as `mtm/empty:1`;
     - `docker container create --pull never --label dev.mtm.temporary=1 --volume <name>:/vol mtm/empty:1 /mtm-holder`, a never-started container with the volume mounted (Docker needs a command, which never runs);
     - stream `docker container cp <id>:/vol -` into `docker/volumes/<name>.tar.gz`.
     - While streaming, it computes a **content digest**: SHA-256 over one line per entry, sorted by path, with the entry's type, permission bits, owner and group ids, size, link target, and content SHA-256. Sorting makes it independent of tar order. The owner is included because a database refuses a data folder it doesn't own. Modification times are left out. A failed volume export is a blocker, and the volume is recorded without a digest.
  5. Saves each locally built image with `docker image save <tags…>` into `docker/images/<id>.tar`. On a re-run, it keeps the earlier archive when the image id is the same and the archive still matches its fingerprint.
  6. Records pulled images by id and tags.
  7. Removes the temporary containers and image.
  8. Adds a by-hand step: quit Docker Desktop before Migration Assistant, so `Docker.raw` is copied while nothing writes to it.
- **`prepare --undo`:** starts the containers `prepare` stopped (`docker container start <id>`).
- **`finish`** requires Docker Desktop running. It refuses to start anything before every volume is settled:
  1. For each recorded volume, unless `~/.mtm/docker-finish.json` already lists it:
     - missing: creates it with its recorded labels, then restores it;
     - present with a matching content digest (Migration Assistant copied `Docker.raw` intact): nothing to do;
     - present and empty: restores it;
     - present, non-empty, and different: a **blocker**. `mtm` never overwrites it. It prints the commands that remove the volume and the containers mounting it; after the user runs them, `mtm finish --only docker` restores the volume (section 6.2, rule 4).
     - Restoring checks the archive's fingerprint first, then streams it with `docker container cp --archive --quiet - <id>:/`. `--archive` keeps owners and modes. Right before writing, it reads the volume again and writes only if it is still empty; otherwise it is the "holds different data" blocker. It then recomputes the digest, which must match.
     - Each settled volume is written to `~/.mtm/docker-finish.json` as `intact` or `restored`, with the digest it was settled against. An entry counts as settled only while that digest matches the record.
  2. Loads each missing locally built image from its archive (`docker image load --quiet --input`), after checking its fingerprint.
  3. Prints one by-hand step: `docker pull <tag>` for each pulled image this Mac lacks. Pulls go by tag: reports mask every `sha256:` value (section 6.3), so a pull by digest couldn't be printed.
  4. When no blocker remains, starts the containers `prepare` stopped. Those that still exist get `docker container start <id>`. Missing ones are re-created through their Compose project: `docker compose --project-name <p> --project-directory <dir> --file <f>… up --detach --pull never --no-build`. Containers outside Compose that no longer exist become by-hand steps.
- **`verify`** (a critical check per volume):
  - Docker Desktop must be installed and running, a critical fail otherwise.
  - A volume `finish` settled against the recorded digest passes, even if containers have changed it since: `finish` compared it before anything ran.
  - Otherwise the volume's digest is computed now and must match. A missing volume, a mismatch, or a volume recorded without a digest is a critical fail.
  - Each locally built image must be present by id: a fail otherwise. If only its tag now names a different image, it is info (rebuilt on the new Mac).
  - Pulled images missing by id and by tag are one warn, listing the `docker pull` commands.

### 9.6 `databases` (Homebrew Postgres)
- **Finding clusters.** A cluster is a folder in `/opt/homebrew/var` whose name starts with `postgres` and that holds a `PG_VERSION` file, such as `postgresql@16`. Its programs are in `/opt/homebrew/opt/postgresql@<major>/bin`. A running server writes its port and socket folder to `postmaster.pid` (lines 4 and 5). A socket folder that isn't an absolute path counts as none. It counts as running when `pg_isready` reaches it there.
- **Connecting.** Every Postgres command connects through that Unix socket (`--host <socket folder> --port <port>`), never over TCP. It passes `-w`, so it never prompts for a password. It unsets `PGHOST`, `PGHOSTADDR`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGSERVICE`, `PGSERVICEFILE`, and `PGPASSWORD`, and sets `PGPASSFILE=/dev/null`. `psql` also gets `--no-psqlrc`, so a user's `.psqlrc` can't change its output.
- **Who stops the service.** The `homebrew` module does. It stops the user's running services in `prepare` and starts them in `finish`, Postgres included. `databases` comes before `homebrew` in the module order, so the server is still running when it dumps.
- **`scan`:** each running cluster is mtm-handled (a safety-net dump), and each stopped one is copied by Migration Assistant. A running cluster without a Unix socket is a warn: `mtm` can't reach it without a network connection.
- **`prepare`:** for each running cluster:
  - records the database names (`SELECT datname FROM pg_database WHERE NOT datistemplate`);
  - checks free space against the sum of `pg_database_size`;
  - writes `pg_dumpall` output, gzip-compressed, to `postgres/<cluster>.sql.gz` as a safety net. A failed dump is a warn: Migration Assistant still copies the data folder.
  - On a re-run, a cluster that has stopped since the earlier run keeps that run's record, and its dump only while the dump still matches its fingerprint, because its data hasn't changed since. That is how the `homebrew` module leaves it. Without a matching dump it is a warn, with the fix `brew services start`, then `mtm prepare --only databases`.
- **`finish`:** nothing, unless `--restore-dump` is given (`mtm finish --only databases --restore-dump`). For each cluster with a dump, it then:
  - refuses unless the server is running. The fix is `brew postinstall postgresql@<major>` for a missing data folder, then `brew services start`.
  - reaches the server through the socket and port in the new Mac's `postmaster.pid`, not the ones recorded on the old Mac;
  - refuses on a cluster that has databases other than `postgres`, or whose `postgres` database holds tables, unless `--force` is also given;
  - checks the dump's fingerprint, then streams it into `psql --dbname postgres --file=-`;
  - warns with a count when psql reports errors other than the existing role. The warning never quotes psql's output.
- **`verify`** (a critical check per cluster): a cluster that ran at `prepare` must be running, found through the new Mac's `postmaster.pid`, and list every recorded database. Its fix is `brew services start`, then `--restore-dump` if it won't start. A cluster that was stopped must have its data folder (`PG_VERSION`).
- Postgres running inside Docker is covered by the `docker` module.

### 9.7 `loginitems`
- Records each launch agent in `~/Library/LaunchAgents` (`*.plist` only), with its `Label` and whether it is loaded (`launchctl list`).
- `prepare`, not `scan`, records login items through System Events (`osascript`). That triggers a one-time Automation permission prompt, which `scan` explains beforehand. If access is denied, `prepare` warns and asks the user to note their login items by hand.
- `finish` bootstraps the launch agents that were loaded and aren't now (`launchctl bootstrap gui/<uid> <plist>`). A failure becomes a by-hand step pointing at Login Items approvals. `finish` also adds a checklist step for each login item to add again, with a link to Login Items. If `prepare` couldn't list them, the step is to note them on the old Mac first.
- `verify` warns when an agent that was loaded isn't now, and lists the login items to add again (info).

### 9.8 `permissions`
- **Reading grants.** It reads the user's privacy database (`~/Library/Application Support/com.apple.TCC/TCC.db`) and the system one (`/Library/Application Support/com.apple.TCC/TCC.db`) with `/usr/bin/sqlite3 -init /dev/null -readonly -json`; `-init /dev/null` keeps a user's `.sqliterc` from changing the output.
  - It selects only granted rows (`auth_value = 2`), and only service, client, client type, and, for Automation, the target app.
  - macOS refuses to open either database without Full Disk Access ("authorization denied"). That refusal is how `mtm` tells Full Disk Access is missing.
  - A database whose file doesn't exist yet, as on a new account, holds no grants. `mtm` looks for the file itself (`lstat`), because sqlite3's "unable to open database file" also covers other failures to open. When macOS refuses even to look, sqlite3 decides.
  - Any other sqlite3 error is a failure.
- **`scan`:** with Full Disk Access, an info finding with the number of grants. Without it, a warn: `mtm` will use its catalog instead. The fix is to give the terminal Full Disk Access.
- **`prepare`:** records the grants (source `tcc`). Without Full Disk Access it records source `catalog` and warns the same way.
- **`finish`:** creates one checklist item per expected grant, with the pane's name and its deep link for this Mac's macOS.
  - The expected grants are the recorded ones. With a `catalog` record, they are what the catalog lists for the apps in the `system` module's record.
  - Grants for services System Settings doesn't show, which macOS uses internally, get no item.
  - Item ids are `permissions:<client>:<pane>`, plus `:<target>` for Automation.
- **`verify`:** with Full Disk Access on the new Mac, a passing check resolves the items whose grants are present, and a warn lists the rest. Without it, one info check asks the user to mark the items done in `mtm checklist`. Permissions are not critical checks; their checklist items count for the verdict.

### 9.9 `browsers`
- **Chromium family (Chrome, Brave, Arc):**
  - records profile names from `Local State` and extension ids per profile;
  - records whether the browser's "Safe Storage" keychain item exists (presence only, section 6.2).
- **Firefox:** profiles from `profiles.ini` and extension ids.
- **Safari:** relies on iCloud sync. Records only whether Safari data was readable.
- `finish` adds checklist items: sign in to Google again in each Chromium profile, and confirm two or three named sites are still logged in.
- `verify`: profiles and extensions present, and each Safe Storage item present.

### 9.10 `secrets`
- Records `~/.ssh` key names and types (`ssh-keygen -l` on public keys), and `gpg --list-secret-keys` identities (never key material).
- Detects Secure Enclave or hardware-backed SSH keys: Secretive installed, `SSH_AUTH_SOCK` pointing to a Secure Enclave agent, `sk-` key types.
- Detects password managers from the catalog (1Password, Bitwarden).
- **`finish` checklist:**
  - turn on iCloud Keychain and check passkeys are present;
  - sign in to the password manager and turn on its SSH agent if it was used;
  - create new Secure Enclave keys and add them to GitHub and servers;
  - enroll Touch ID fingerprints.
- **`verify`:** SSH and GPG identities match (critical). The keychain items recorded by other modules are present.

### 9.11 `system`
- **Recorded:**
  - FileVault status and macOS version, in the manifest by preflight;
  - login shell (`dscl . -read /Users/<user> UserShell`) and whether `/etc/shells` lists it;
  - fingerprints of `/etc/hosts`, `/etc/shells`, `/etc/paths.d/*`, `/etc/pam.d/sudo_local` (Touch ID for sudo). Copies go into `~/.mtm/exports/etc/` so `finish` can print an exact restore command. These files hold configuration, not secrets;
  - app inventory of `/Applications` and `~/Applications`, including one level of subfolders, skipping Apple's own `com.apple.*` apps and symlinks. Each app records its bundle id, version (`plutil -extract … raw`), and architectures (`lipo -archs`). Home apps are recorded as `~/Applications/…`.
- **`scan` and `prepare`:** warn about Intel-only apps. They need Rosetta on the new Mac, and Rosetta is limited after macOS 27.
- **`finish`:**
  - Adds checklist items from the catalog for the recorded apps: one to reactivate each license marked `reactivate`, and one per note.
  - Prints `sudo softwareupdate --install-rosetta --agree-to-license` if Rosetta is needed and missing.
  - For each `/etc` file that differs, prints `sudo install -m 0644 -o root -g wheel <copy> /etc/<file>`. `install` rather than `cp`, because the private copy is 0600 and `cp` would give a new `/etc` file that mode.
  - When the login shell changed, prints one line to paste: `echo <shell> | sudo tee -a /etc/shells >/dev/null && chsh -s <shell>`, without the `tee` part if the shell is already listed.
  - Paths are shell-quoted for sh, zsh, and fish.
- An `/etc` file that can't be read doesn't stop the module. `prepare` warns that it won't be restored, and `verify` treats it as changed.
- An app counts as Intel-only only when its architectures include neither `arm64` nor `arm64e`.
- **`verify`:**
  - The recorded login shell must exist: fail.
  - A changed shell, or one not listed in `/etc/shells`: warn.
  - A changed or missing `/etc` file: warn.
  - Every app must be present by bundle id: a missing app is a fail, because a discontinued or paid app may be impossible to get again. An older version is a warn.
  - Rosetta missing while Intel-only apps exist: warn.

## 10. Error handling
- **Fail fast, with context.** Every error names the module, the step, the command that failed, its exit code, and the next action. Example: `docker: export failed: Docker Desktop is not running. Start it and run: mtm prepare --only docker`.
- **Report everything.** `scan` and `verify` run every module and report every problem. One module's error becomes a failed check for that module, not an abort.
- **Clear readiness.** If any module fails in `prepare`, the run ends with **NOT READY FOR MIGRATION ASSISTANT** and exit code `1`. The modules that succeeded keep their results.
- **Re-runnable.** Each step records completion and the fingerprints of its inputs in the manifest. A re-run skips completed steps whose inputs are unchanged:
  - a locally built image archive whose image id and archive fingerprint are unchanged;
  - a Postgres dump of a cluster that has stopped since;
  - on the new Mac, a volume `finish` already settled.
  Volumes and running clusters are exported again, because reading them to check costs as much as exporting them.
- **Undo for the old Mac.** `prepare --undo` restarts the services and containers `prepare` stopped, for when the user calls off the migration. It runs only on the Mac that ran `prepare`. It removes the undone modules' records, so a later `verify` reports them not prepared rather than trusting a record of a state that no longer holds.
- **`finish` on the wrong Mac.** `finish` refuses to run on the Mac that ran `prepare`, because it would restart services there. Like `prepare`, it refuses without FileVault.
- **Free space.** Each export checks free space first and keeps at least 10 GB free (10,000,000,000 bytes, the units Finder shows). `docker` checks its whole export before it stops any container, then each archive again before writing it. If an export won't fit, `prepare` stops before writing it and suggests `--stage` on an encrypted external volume.
- **Migration Assistant problems.** `verify` lists what is missing. `mtm` explains the options: copy the specific missing paths from the old Mac, or erase the new Mac and run Migration Assistant again during first-boot setup.
- **Wrong Mac.** `prepare` refuses to overwrite a record made on a different Mac (for example, run by mistake on the new Mac after migration) and points to `mtm verify`. `verify` fails when run on the Mac that ran `prepare`. `cleanup` needs a SAFE TO WIPE result from a full `verify` (not one limited with `--only`) on the same Mac, or `--force`.
- **Version mismatch.** `mtm` refuses to read a manifest with a newer, unknown schema major version, and warns when the `mtm` version differs from the one that ran `prepare`.
- **Missing tools.** If Docker Desktop or a database is installed but stopped, `mtm` asks the user to start it, or starts it temporarily and stops it again afterward.
- **No silent failures.** Every error from a system command is logged with stderr (redacted) and surfaced in the report.

## 11. Testing
- **Unit tests per module.** A fake `runner` replays fixtures of real command output captured on macOS 26. The author's M3 is the first source. No unit test executes real system commands.
- **Table-driven tests** for the file classification in section 8.4 and the verdict rules in section 8.5.
- **Integration tests on a temporary home folder** (`HOME` overridden) with real files and real git repos created in a temp directory. They cover unpushed commits, stashes, untracked `.env` files, cloud-only files (simulated with flags where the test can set them), modes, and symlinks.
- **No-network test** behind the `integration` build tag: runs `mtm scan`, `prepare`, and `verify` on a temporary home folder under a `sandbox-exec` profile that denies network access, including every `brew` and `git` command they invoke, and asserts they succeed.
- **Docker integration tests** behind the `integration` build tag, run against a real Docker Desktop. They cover:
  - create a volume and an image, export, remove, and restore;
  - compare content digests, including owners and modes;
  - the skip-restore path when data is already intact;
  - a non-empty mismatched volume that stays untouched.

  They use only volumes, images, and containers named `mtm-test-…` and call the export and restore steps directly. They never run `prepare`, `finish`, or `undo`, which would stop or start the developer's own containers.
- **Postgres integration test** behind the same tag, skipped when no Homebrew `postgresql@N` is installed. It works on a throwaway cluster in a temporary folder: `initdb`, then `pg_ctl` with a Unix socket only. It covers dump, drop, `--restore-dump`, and verify.
- **End-to-end tests in macOS VMs** with Tart, run locally (hosted CI runners cannot run macOS VMs inside them):
  1. Provision a source VM and run `scan` and `prepare`.
  2. Clone the VM to simulate a perfect Migration Assistant copy.
  3. Inject faults: delete a critical file, corrupt a file without changing its time, change a repo's refs, remove a Docker volume, change a file on the source after `prepare`.
  4. Check that `verify` reports each fault with the right outcome and that `retire` flags the late change.
- **Real Migration Assistant run.** The author's own migration from the M3 is the first real-world test. Findings feed the catalog, fixtures, and the "unconfirmed" rows in section 3.
- **CI** (GitHub Actions, Apple Silicon runners): `go test ./...`, `go vet`, `gofmt` check, `govulncheck`, catalog validation, and reproducible release builds with checksums and provenance attestations.

## 12. Open questions and risks
1. **Notarization** needs an Apple Developer Program membership. If none is available, version 1 ships through a Homebrew formula and checksummed release binaries without notarization.
2. **The macOS 26 "Privacy & Security Settings" checkbox** in Migration Assistant has undocumented scope. If it does migrate privacy permissions, the `permissions` module's verify step marks those checklist items done automatically. The design works either way.
3. **Whether Migration Assistant copies `Docker.raw` intact** is unconfirmed. The design handles both outcomes (section 9.5). `Docker.raw` is also a sparse file: on the author's Mac it is 494 GB with 49 GB in use. If Migration Assistant doesn't keep it sparse, the new Mac needs the full size free.
4. **Google session binding (Device Bound Session Credentials)** comes from a single news source. The design treats re-sign-in as a checklist item, which costs little if it turns out unnecessary.
5. **System Settings deep links** change between macOS versions. Each link in the catalog table is checked manually on every supported macOS major version before release. The macOS 26 links (`com.apple.settings.PrivacySecurity.extension?Privacy_…` and `com.apple.LoginItems-Settings.extension`) have not been checked yet.
6. **The volatile-path list** will be incomplete at first. Expect the first real migrations to surface `appstate` warnings that become catalog entries.
