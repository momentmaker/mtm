# mtm

Move a developer's Apple Silicon Mac to a new one, alongside Migration Assistant, and prove nothing was lost before you erase the old one.

> **Status: not released yet.** mtm hasn't been through a real migration yet. That run is planned, and it comes before the first release. Until then, read what each command does before you run it.

## Why

Migration Assistant copies files well. It doesn't cover:
- container, VM, and database data;
- state tied to the old Mac's hardware or security: privacy permissions, Secure Enclave keys, passkeys, Google sessions, and Touch ID;
- login items and licenses;
- any check that the copy is complete before the old Mac is erased.

mtm works around Migration Assistant.
- **Before it,** mtm records the old Mac and exports what Migration Assistant handles badly.
- **After it,** mtm restores those exports, lists the steps no tool can do for you, and checks the new Mac against the record. It ends with **SAFE TO WIPE**, or with the list of what's missing.

## Requirements

- Apple Silicon on both Macs, with macOS 26 or later.
- FileVault on, on both Macs.
- Migration Assistant run directly Mac to Mac, during the new Mac's first-boot setup. A Time Machine backup isn't supported.

## Install

Once released:

```sh
brew install momentmaker/tap/mtm
```

From source, with Go 1.26:

```sh
go install github.com/momentmaker/mtm/cmd/mtm@latest
```

## Use

| Step | Where | Command | What happens |
|---|---|---|---|
| 1 | Old Mac | `mtm scan` | Reports what Migration Assistant, mtm, and you will each handle. It changes nothing. |
| 2 | Old Mac | `mtm prepare` | Records this Mac and exports Docker volumes, locally built images, and Postgres. It stops services cleanly. Run it last, just before Migration Assistant. |
| 3 | Both | Migration Assistant | Run directly Mac to Mac, during the new Mac's setup, with every category selected. |
| 4 | New Mac | `mtm finish` | Restores what prepare set aside, and restarts services and launch agents. |
| 5 | New Mac | `mtm checklist` | Walks you through the steps no tool can do, such as privacy permissions, licenses, and signing in again. |
| 6 | New Mac | `mtm verify` | Checks the new Mac against the record, and prints **SAFE TO WIPE** or **NOT SAFE TO WIPE** with the reasons. |
| 7 | Old Mac | `mtm retire` | After SAFE TO WIPE: lists what changed since prepare, then walks through licenses, iCloud, and the erase. |
| 8 | New Mac | `mtm cleanup` | Deletes mtm's exports and record. |

```text
mtm moves a developer's Mac to a new Mac, alongside Migration Assistant.

Usage:
  mtm scan [--json]                         check this Mac before migrating (changes nothing)
  mtm prepare [--only a,b] [--deep] [--stage /Volumes/X]
                                            record this Mac before Migration Assistant;
                                            --stage puts exports on an encrypted drive
  mtm prepare --undo                        put this Mac back in use if you call off the migration
  mtm finish [--only a,b]                   on the new Mac: restore what prepare set aside
  mtm finish --only databases --restore-dump [--force]
                                            load the Postgres safety-net dump
  mtm checklist                             on the new Mac: go through the steps left to do by hand
  mtm checklist list [--all] [--json] | done <id>... | skip <id> --reason "..." | open <id>
  mtm verify [--only a,b] [--deep] [--details] [--json]
                                            check the new Mac against the record
  mtm retire [--details] [--json]           on the old Mac, after SAFE TO WIPE: what changed since
                                            prepare, then the steps before you erase it
  mtm retire list [--all] [--json] | done <id>... | skip <id> --reason "..." | open <id>
  mtm cleanup [--force]                     delete ~/.mtm, and the --stage exports, after SAFE TO WIPE
  mtm version
```

**Exit codes:**
- `verify`: `0` safe to wipe, `1` not safe, `2` mtm itself failed.
- Every other command: `0` success, `1` finished with problems or refused, `2` mtm failed or the command line was wrong.

## What it checks

- **Files:** your home folder, with a fingerprint for each dotfile at its top and everything in `~/.ssh`, `~/.gnupg`, and `~/.config`. Cloud-only files are never opened.
- **Git:** every repo's commits, stashes, and untracked and ignored secret-like files.
- **Homebrew and toolchains:** your Homebrew services, and the paths and versions of your tools.
- **Docker Desktop:** your volumes and locally built images.
- **Postgres:** Homebrew Postgres, with a safety-net dump.
- **Login items and privacy permissions:** your launch agents, login items, and privacy permissions.
- **Browsers:** Chrome, Brave, Arc, and Firefox profiles and extensions, and whether each Chromium browser's Safe Storage keychain item is there. For Safari, only whether its data could be read.
- **Identities:** your SSH and GPG identities.
- **Apps:** your apps, your login shell, and the `/etc` files developers commonly edit: `hosts`, `shells`, `paths.d`, and `pam.d/sudo_local`.

The app catalog in [`catalog/`](catalog/README.md) says what each app needs by hand on a new Mac.

## Safety

- **No network:** mtm never makes network connections. Steps that need the network are printed for you to run.
- **No secrets read:** it never reads secret contents. Keychain items are checked for presence only, private keys are only looked for, and secret files are fingerprinted, never copied or printed.
- **No security weakened:** it never turns off a security feature or stores your password. Steps that need root are printed for you to run with `sudo`.
- **No data deleted:** it never deletes or overwrites your data. `prepare` writes only inside `~/.mtm`, or the `--stage` folder, and `cleanup` deletes only those.
- **Its own folder:** `~/.mtm` is private (mode `0700`) and excluded from Time Machine.
- **Reports:** reports hold paths, counts, and statuses, never fingerprints or file contents, so they are safe to paste into an issue.
- **Full Disk Access is optional.** With it, mtm reads your privacy permissions directly instead of inferring them from its catalog. `cleanup` reminds you to revoke it.

The full design is in [`docs/superpowers/specs/2026-11-02-mtm-design.md`](docs/superpowers/specs/2026-11-02-mtm-design.md). To report a vulnerability, see [SECURITY.md](SECURITY.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). To add an app to the catalog, see [`catalog/README.md`](catalog/README.md).

## License

MIT; see [LICENSE](LICENSE). Some catalog entries started from [tape](https://github.com/vitorgalvao/tape)'s definitions, credited in [`catalog/README.md`](catalog/README.md).
