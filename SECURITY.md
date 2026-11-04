# Security policy

## Scope

mtm runs as you, on the Mac you're about to erase and on its replacement.
- **What it reads:** your home folder; a fingerprint of important files; the presence of keychain items (never their contents); and, with Full Disk Access, the privacy permissions databases.
- **What it runs:** your Homebrew, Docker Desktop, git, Postgres, and gpg tools; your login shell, once, as an interactive login shell to read your `PATH`, so your shell's startup files run; each developer tool on that `PATH` with `--version`; and macOS's own command-line tools (section 7.1 of the design lists them), including `osascript` to list your login items.
- **What it writes:** `prepare` writes only inside `~/.mtm`, or the `--stage` folder you give it; those hold your Docker volume exports and Postgres dumps, which can contain sensitive data. `finish` restores those exports into Docker on the new Mac, and loads the Postgres dump only if you ask with `--restore-dump`.

The rules it follows are in section 6 of the [design](docs/superpowers/specs/2026-11-02-mtm-design.md):
- no network connections;
- no secret contents read;
- no security setting weakened;
- no user data deleted or overwritten, unless you pass `--force` to `finish --restore-dump` or `cleanup`;
- `~/.mtm` private and excluded from Time Machine.

A way to break any of these is a vulnerability.

## Reporting a vulnerability

Report it privately with [GitHub's vulnerability reporting](https://github.com/momentmaker/mtm/security/advisories/new) (the **Security** tab, then **Report a vulnerability**). Please include:
- the mtm version (`mtm version`) and your macOS version;
- steps to reproduce;
- what an attacker gains.

Don't attach your real exports, dumps, `~/.mtm` contents, or logs. Describe them instead. Please don't open a public issue for an unfixed vulnerability.

We aim to acknowledge a report within 7 days.

## Supported versions

Fixes go into the latest release.

## Verifying a release

Each release archive has a SHA-256 checksum in `checksums.txt` and a GitHub build provenance attestation. To check where an archive came from:

```sh
gh attestation verify mtm_<version>_darwin_arm64.tar.gz --repo momentmaker/mtm
```
