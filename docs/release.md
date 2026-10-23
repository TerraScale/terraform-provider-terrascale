# Release preparation: v0.0.1

These files prepare `github.com/TerraScale/terraform-provider-terrascale` for its
initial v0.0.1 release. They do not create a repository, tag, key, or release.
The source includes the Go 1.25.0 root main, `main.version`, Framework protocol 6
provider, and project/database/collection/api_key resources, tests and docs.

## Validate without credentials or a Git repository

Install Go 1.25.0, Git (the executable only), and GoReleaser v2.19.0 separately.
From the complete provider source directory, run:

```sh
nice -n 19 goreleaser check --config .goreleaser.yml
nice -n 19 goreleaser release --config .goreleaser.yml --snapshot --clean --skip=sign,publish
```

`check` validates configuration without Git, Go source, credentials or output.
The snapshot command builds all six target binaries and ZIP archives into
`dist/`; `--clean` replaces that generated directory. The versioned manifest is
checksummed from the source `terraform-registry-manifest.json`; GoReleaser uploads
it with the versioned release asset name, without copying it into `dist/`. It needs source and Go
module dependencies but no `.git`, GPG key, Registry token or GitHub token.
GoReleaser v2.19.0 explicitly accepts non-repositories in snapshot mode; no
custom Git environment or `git init` is required. Outside Git, its fallback
version is `0.0.0-SNAPSHOT` rather than the eventual release version.
Do not treat a successful configuration check as a successful build.

CI runs `go test ./...`, `go build ./...`, configuration validation and the
unsigned snapshot under the check named `test`, on GitHub-hosted Ubuntu.
It verifies six ZIPs, binary naming, protocol-6 manifest and every SHA256 digest.
No local test run is required before pushing. The release workflow independently
runs tests/build/config validation before importing the signing key.

## User-only signing setup

1. Register the public signing key in the Terraform Registry for TerraScale.
   Generate or supply the signing key yourself; agents must not execute signing setup. Never paste private keys or passphrases into chat.
2. Run each command below yourself. Each opens its own hidden prompt; enter the
   existing secret directly there. Do not add `--body`, shell variables, pipes,
   file redirects, or secret values to the command line.

   ```sh
   gh secret set GPG_PRIVATE_KEY --repo TerraScale/terraform-provider-terrascale
   gh secret set GPG_PASSPHRASE --repo TerraScale/terraform-provider-terrascale
   ```

   Use an ASCII-armored private signing key. The import action unlocks the key
   using the passphrase and provides its fingerprint to GoReleaser. GitHub
   automatically supplies `GITHUB_TOKEN`; no PAT or Registry token belongs in
   this workflow. Key availability, validity and Registry trust remain user
   prerequisites, not something this preparation verified.
3. After the public repository and green `test` check exist, use the Registry's
   provider publishing flow to connect `TerraScale/terraform-provider-terrascale`.
   Confirm namespace access and public signing-key registration there. This
   manual Registry setup is separate from uploading GitHub release assets.

## Release contract

Only stable tags matching `^v0\.0\.(0|[1-9][0-9]*)$` are accepted. Initial tag:
`v0.0.1`. Other tags fail before checkout or signing-key import. Tag creation,
push and publication require separate authorization and are not part of this
preparation. After a full-history checkout, the release job also requires
`git merge-base --is-ancestor "$GITHUB_SHA" origin/main` before signing-key import
or publication. A tag on a commit outside `origin/main`, or missing ancestry,
fails the job without using signing credentials.

A release contains six `terraform-provider-terrascale_0.0.1_<os>_<arch>.zip`
archives (linux/darwin/windows x amd64/arm64), each containing only
`terraform-provider-terrascale_v0.0.1` (plus `.exe` on Windows), and:

- `terraform-provider-terrascale_0.0.1_manifest.json`: manifest version 1,
  protocol `6.0`, uploaded separately and included in the checksums.
- `terraform-provider-terrascale_0.0.1_SHA256SUMS`: SHA-256 of the six ZIPs
  and versioned manifest.
- `terraform-provider-terrascale_0.0.1_SHA256SUMS.asc`: ASCII-armored detached
  GPG signature of that checksum file.

For later v0.0.X tags, asset names use that tag's version without `v`.
The release job has `contents: write` solely to upload the GitHub release.
Snapshot validation deliberately skips both signing and publication; actual
signing, GitHub upload and Registry ingestion remain unverified until an
authorized release is made.

## Pins and upstream references

Action commit SHAs were resolved from actual upstream GitHub tag references:
`actions/checkout@v4`, `actions/setup-go@v5`,
`goreleaser/goreleaser-action@v6`, and `crazy-max/ghaction-import-gpg@v6`.
Both workflows pin the full resolved commit IDs and GoReleaser v2.19.0.

- [GoReleaser no-repository snapshot implementation](https://github.com/goreleaser/goreleaser/blob/v2.19.0/internal/pipe/git/git.go)
- [HashiCorp release config reference](https://github.com/hashicorp/terraform-provider-scaffolding-framework/blob/main/.goreleaser.yml)
- [GPG import action](https://github.com/crazy-max/ghaction-import-gpg/tree/e89d40939c28e39f97cf32126055eeae86ba74ec)

