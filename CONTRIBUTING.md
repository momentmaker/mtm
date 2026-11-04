# Contributing to mtm

Thanks for helping. mtm runs on the Mac someone is about to erase, so the bar is careful code with tests that prove it.

## Build and test

You need Go 1.26 on an Apple Silicon Mac.

```sh
go build ./cmd/mtm
gofmt -l .                       # prints nothing when the code is formatted
go vet ./...
go vet -tags integration ./...
go test -race ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
```

CI runs the same commands on every push and pull request. It also builds a release snapshot with GoReleaser and checks the binary reports its version.

### Integration tests

These run with `-tags integration`. Each skips itself when its host is missing:
- `TestNoNetwork` (`cmd/mtm`) runs `scan`, `prepare`, `verify`, and `retire` under a `sandbox-exec` profile that denies network access. It uses a temporary home folder.
  - It needs FileVault on and Homebrew in `/opt/homebrew`.
  - It skips when a Homebrew service is running, because `prepare` would stop it.
- The Docker tests (`internal/modules/docker`) need Docker Desktop running. They create and remove only volumes, images, and containers named `mtm-test-…`.
- The Postgres test (`internal/modules/databases`) needs a Homebrew `postgresql@N`. It works on a throwaway cluster in a temporary folder.

```sh
go test -tags integration -run TestNoNetwork -v ./cmd/mtm/
```

## Tests must never change the Mac they run on

Most contributors run the tests on the Mac they work on every day. So:
- **Fake every system command.** Unit tests replace system commands with `runner.Fake` (`internal/runner`) and its canned output. A fake that could reach a command touching real data (the keychain, privacy databases, gpg, bioutil, Docker, launchctl) has no `Fallback`.
- **Use temporary folders.** Fixture home folders, keys, and browser folders live in `t.TempDir()`. No test reads the real `~/.ssh`, `~/.gnupg`, browser data, keychain, or privacy databases.
- **Stop and start nothing.** No test stops, starts, loads, or opens anything: no Homebrew services, launch agents, containers, or `/usr/bin/open`.
- **Docker tests touch only `mtm-test-…` resources.** They never call the docker module's `Prepare`, `Finish`, or `Undo` against the real engine.
- **Never run mtm on your real home folder** while you develop. Use a temporary `HOME`.

## Code

- **Modules never print.** All output goes through `internal/report`, which masks fingerprints and credentials.
- **System commands run only through `internal/runner`.** Apple's tools are called by absolute path.
- **Comments explain why.** They don't restate the code, and there's no commented-out code.
- **Write the failing test first,** then the code that makes it pass.
- **Commit messages say why** the change is needed.

The design, including the security rules every change must keep, is in [`docs/superpowers/specs/2026-11-02-mtm-design.md`](docs/superpowers/specs/2026-11-02-mtm-design.md). Known gaps are in [`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md).

## Adding an app to the catalog

Add one TOML file to [`catalog/`](catalog/). The format and the steps are in [`catalog/README.md`](catalog/README.md). Then run `go test ./internal/catalog/`; CI rejects a file those tests reject.

## Reporting security problems

Please don't open a public issue. See [SECURITY.md](SECURITY.md).
