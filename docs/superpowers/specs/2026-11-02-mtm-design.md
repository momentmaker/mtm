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
| 5 | New Mac | `mtm checklist` | Walks through the manual steps. Each opens the right System Settings pane or app and waits for done, or for a skip with a reason. |
| 6 | New Mac | `mtm verify` | Compares the new Mac with the record. Prints **SAFE TO WIPE** or **NOT SAFE TO WIPE** with the reasons. Can be run any number of times. |
| 7 | Old Mac | `mtm retire` | Lists anything on the old Mac that changed after `prepare`. Then walks through deactivating licenses, signing out of iCloud and turning off Find My, removing old Secure Enclave keys from services, and "Erase All Content and Settings". `mtm` never erases anything itself. |
| 8 | New Mac | `mtm cleanup` | After SAFE TO WIPE, deletes the exports and the record. Reminds the user to revoke the terminal's Full Disk Access. Refuses before SAFE TO WIPE unless `--force` is passed. |

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
4. **It never deletes or overwrites user data.** `prepare` only writes inside `~/.mtm/`. Restores refuse to overwrite a non-empty Docker volume or database without `--force`. `cleanup` deletes only `~/.mtm/`.
5. **It never reads cloud-only files** (section 8.3), which would trigger downloads.

### 6.3 Protecting what `mtm` creates
- `~/.mtm/` is created with mode `0700` and its files with `0600`. `mtm` refuses to run if the directory exists with looser permissions or a different owner.
- `~/.mtm/` is excluded from Time Machine with `tmutil addexclusion`.
- **FileVault must be on** on both Macs (`fdesetup status`). `prepare` and `finish` refuse to run without it. On a FileVault disk the exports are no more exposed than the original data beside them, so `mtm` adds no separate encryption there.
- **External staging** (`prepare --stage /Volumes/X`) is allowed only on an encrypted APFS volume, checked with `diskutil info`. `mtm` uses Apple's encryption and implements no cryptography of its own.
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
internal/catalog/   loads embedded TOML app definitions
internal/checklist/ manual steps: generation, persisted state, interactive walk-through
internal/report/    terminal and JSON rendering, redaction
internal/verdict/   combines module checks and checklist state into SAFE / NOT SAFE
internal/modules/   one package per module (section 9)
catalog/            TOML app definitions, embedded into the binary at build time
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
- `Env` carries the runner, home directory, catalog, logger, and the current mode (old Mac or new Mac).
- `Finding` has: module, id, severity (`info`, `warn`, `blocker`), handler (`migration-assistant`, `mtm`, `manual`), title, detail, fix.
- `Result` has: what was done, artifacts written (each with its fingerprint), checklist items to add, and problems.
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
- Fields: identity (`id`, `name`, `bundle_id`), required privacy permissions, license reactivation (with optional deactivate steps for `retire`), state locations, volatile paths (expected to change or disappear), and per-app checklist notes.
- System Settings deep links live in one table keyed by permission service and macOS major version. They are not repeated in each app file.
- Initial data is seeded from the `tape` project's definitions (BSD-2-Clause, credited). mackup's GPL-3 database is not copied.
- Adding an app means adding a file. The contributor guide documents the format, and CI validates every file.

## 8. Data

### 8.1 Layout
```
~/.mtm/
  manifest.json      schema version, mtm version, macOS version and build, hostname,
                     machine id (SHA-256 of the hardware UUID, never the raw UUID),
                     prepare time, per-module inventories, export fingerprints, checklist items
  files.jsonl.gz     file record, one JSON object per line
  Brewfile           output of brew bundle dump, kept as a fallback
  exports/
    docker/          volume and image archives
    postgres/        pg_dumpall output, gzip-compressed
  state.json         new Mac only: checklist progress, last verify result
  logs/              one log file per command run, redacted
```
All paths in the record are relative to the home folder, so verification works if the username differs on the new Mac.

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
- `scan` and `prepare` record the output of `brew services list --json`, plus `brew bundle dump --file ~/.mtm/Brewfile --describe` as a fallback.
- **`prepare`:** stops running user services with `brew services stop` and records which ones were running. Services running as root are printed as `sudo brew services stop <name>` for the user to run.
- **`finish`:** restarts the services that were running, printing the same kind of commands for root services.
- **`verify`:** `brew bundle check --file ~/.mtm/Brewfile`, and services that were running are running.

### 9.4 `toolchains`
- Records path and version for: brew, git, node, npm, pnpm, bun, deno, python3, uv, go, rustc, cargo, ruby, java, asdf, mise. Each is resolved through the user's login shell (`$SHELL -l -c 'command -v X; X --version'`), so dotfile-configured PATH is respected.
- Also records the global `~/.tool-versions` and `mise` config.
- `verify` requires the same paths and versions on the new Mac. A difference is a fail with both values shown.

### 9.5 `docker` (Docker Desktop)
- **`scan`:**
  - lists images, with registry digests separating pullable images from locally built ones;
  - lists volumes with sizes, containers, and Compose projects (from `com.docker.compose.project` and working-directory labels);
  - estimates the export size from volumes plus locally built images only.
- **`prepare`** requires Docker Desktop running:
  1. Stops running containers and records them.
  2. Saves locally built images with `docker image save`.
  3. Records registry images by name and digest.
  4. Exports each volume without network or a helper image: `docker import` an empty archive as an image `mtm/empty`; `docker create` a never-started container with the volume mounted; stream `docker cp <container>:/vol -` into the archive. While streaming, computes a content digest: SHA-256 over the sorted (path, mode, size, file SHA-256) entries, so tar ordering does not matter.
  5. Removes the temporary container and image.
- **`finish`:**
  1. If Docker Desktop on the new Mac already has every volume with a matching content digest (Migration Assistant copied `Docker.raw` intact), skips the restore.
  2. Otherwise loads the image archives and restores each missing or empty volume through the same empty-image container with `docker cp` into the mount. A non-empty volume whose digest does not match is reported as a critical fail and replaced only with `mtm finish --only docker --force` (section 6.2, rule 4).
  3. Prints `docker pull` commands for registry images for the user to run.
  4. Starts recorded Compose projects with `docker compose up -d --pull never`.
- **`verify`:** every volume's content digest matches, every locally built image is present, and every registry image is present or listed as a pending pull. Volume mismatches are critical fails.

### 9.6 `databases` (Homebrew Postgres)
- **`prepare`:** if a `postgresql@N` service is running, writes `pg_dumpall` output (gzip-compressed) to `exports/postgres/` as a safety net, then stops the service cleanly so the copied data directory is consistent.
- **`finish`:** starts the service. If it fails to start, offers `mtm finish --only databases --restore-dump`, which refuses on a non-empty cluster without `--force`.
- **`verify`:** the server is running and lists the same databases.
- Postgres running inside Docker is covered by the `docker` module.

### 9.7 `loginitems`
- Records launch agents in `~/Library/LaunchAgents` and whether each is loaded (`launchctl print gui/<uid>`).
- Records login items through System Events, which triggers a one-time Automation permission prompt explained to the user beforehand.
- `finish` bootstraps the launch agents that were loaded (`launchctl bootstrap gui/<uid> <plist>`) and adds checklist items for app-registered login items and background-item approvals.
- `verify` checks the same agents are loaded.

### 9.8 `permissions`
- **With Full Disk Access:** reads grants from the user and system privacy permissions databases with `/usr/bin/sqlite3` in read-only mode. Records service and client only.
- **Without it:** infers expected grants from catalog entries for installed apps.
- **`finish`:** creates one checklist item per app and permission, with a System Settings deep link.
- **`verify`:** with Full Disk Access, checks the grants exist and marks items done. Without it, relies on user confirmation.

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
  - FileVault status and macOS version;
  - login shell (`dscl . -read /Users/<user> UserShell`) and whether `/etc/shells` lists it;
  - fingerprints of `/etc/hosts`, `/etc/shells`, `/etc/paths.d/*`, `/etc/pam.d/sudo_local` (Touch ID for sudo);
  - app inventory of `/Applications` (bundle id, version, architectures via `lipo -archs`).
- **`scan`:** flags Intel-only apps. They need Rosetta on the new Mac, and Rosetta is limited after macOS 27.
- **`finish`:** prints `sudo softwareupdate --install-rosetta --agree-to-license` if needed, and fixes for any `/etc` file that differs.
- **`verify`:** same shell and `/etc` fingerprints (warn), and every app present at the same version or newer.

## 10. Error handling
- **Fail fast, with context.** Every error names the module, the step, the command that failed, its exit code, and the next action. Example: `docker: export failed: Docker Desktop is not running. Start it and run: mtm prepare --only docker`.
- **Report everything.** `scan` and `verify` run every module and report every problem. One module's error becomes a failed check for that module, not an abort.
- **Clear readiness.** If any module fails in `prepare`, the run ends with **NOT READY FOR MIGRATION ASSISTANT** and exit code `1`. The modules that succeeded keep their results.
- **Re-runnable.** Each step records completion and the fingerprints of its inputs in the manifest. A re-run skips completed steps whose inputs are unchanged.
- **Undo for the old Mac.** `prepare --undo` restarts the services and containers `prepare` stopped, for when the user calls off the migration.
- **Free space.** Each export checks free space first and keeps at least 10 GB free. If an export will not fit, `prepare` stops before writing it and suggests `--stage` on an encrypted external volume.
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
- **Docker integration tests** behind the `integration` build tag, run against a real Docker Desktop. They cover: create a volume and an image, export, remove, restore, compare content digests, and the skip-restore path when data is already intact.
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
3. **Whether Migration Assistant copies `Docker.raw` intact** is unconfirmed. The design handles both outcomes (section 9.5).
4. **Google session binding (Device Bound Session Credentials)** comes from a single news source. The design treats re-sign-in as a checklist item, which costs little if it turns out unnecessary.
5. **System Settings deep links** change between macOS versions. Each link in the catalog table is checked manually on every supported macOS major version before release.
6. **The volatile-path list** will be incomplete at first. Expect the first real migrations to surface `appstate` warnings that become catalog entries.
