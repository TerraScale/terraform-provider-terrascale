# Dependency and compiler qualification

The provider's minimum Go version is **1.26.0**, required by the patched x/net,
x/sys and x/text modules. This minimum is a source compatibility floor, not a
release compiler selection. The published `terrascale.tech/sdk v0.0.6` dependency's
own minimum Go version is not the provider release compiler.
Both CI and release use the exact **1.27.3** compiler in `.go-version`.
This supersedes the older Go 1.25.0 preparation instructions in `release.md`.

On 2026-10-26 the official Go download catalog listed Go 1.27.3 as latest stable.
The module proxy listed gRPC **1.84.0**, x/net **0.61.0**, and x/sys **0.49.0**
as latest stable; these exceed the advisory patch floors. On 2026-10-27 x/net
advanced to **0.62.0** for GO-2026-6860 and GO-2026-6866. Go MVS also advances
x/text to **0.43.0** and the gRPC-required genproto RPC revision. `go.mod` and
`go.sum` record the selected graph; no advisory exceptions are configured.
An explicit OpenTelemetry SDK **1.47.0** graph pin fixes GO-2026-6505 in the
upstream dependency test graph, although provider binaries do not import it.
Retain that security pin when tidying until the upstream requirement advances.

## Advisory gates

The existing `test` job installs **govulncheck v1.9.0**, caching its executable
with the scanner and compiler versions in the key. Go dependency/build caching
continues through setup-go. Every scan queries the live Go vulnerability feed;
advisory results are not cached as a substitute for scanning.

Both test and release jobs run a provider module scan and a source
symbol scan including tests. The module gate fails on **every known advisory**,
including low/moderate/unrated findings and findings outside called symbols.
Scanner/feed/loading errors also fail the job. The source scan adds call-path
evidence; a static call path alone does not demonstrate an exploitable attack.

A full `go list -m all` OSV comparison additionally covers upstream dependency
test/tool constraints outside provider imports. Its remaining GO-2026-5932
match is specific to x/crypto's unmaintained `openpgp` packages, with no fixed
version. Provider package graphs and binary metadata must establish their
absence; a match against the containing module is not runtime reachability.
This is a recorded package-specific disposition, not a scanner exception.

GoReleaser's post-build hook checks each of the six actual release binaries:
Linux, macOS and Windows, each for amd64 and arm64. It records `go version -m`,
requires the exact `.go-version` compiler, and runs a binary module scan before
archiving, signing or publishing. Module scanning deliberately does not require
a recoverable symbol table in stripped binaries. Missing format/compiler support
is a scanner error and blocks the build; it is never reported as a clean scan.
The existing ZIP, SHA256 and protocol-6 manifest checks remain in `test`.

## Unsigned local qualification

Use the pinned compiler and scanner plus GoReleaser v2.19.0. Put `govulncheck`
on PATH (CI uses `scripts/install-govulncheck.sh` and `GITHUB_PATH`).

```sh
nice -n 19 govulncheck -scan=module -show=verbose
nice -n 19 govulncheck -test -show=verbose ./...
nice -n 19 go test ./...
nice -n 19 goreleaser check --config .goreleaser.yml
nice -n 19 goreleaser release --config .goreleaser.yml --snapshot --clean --skip=sign,publish
```

The snapshot command replaces only its generated `dist/` directory. It neither
signs nor publishes. Binary scans inspect embedded modules and compiler versions;
they do not execute other platforms' binaries or prove runtime exploitability.
No published provider release existed in the GitHub release listing checked on
2026-10-26, so no existing customer release binary was available to qualify.

User-only signing setup, Terraform Registry connection, tag authorization and
publication approval from `release.md` remain required. This qualification does
not authorize a tag, signing, publication, production mutation or deployment.
