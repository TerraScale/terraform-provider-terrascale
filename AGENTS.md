# terraform-provider-terrascale

Terraform provider for TerraScale: manages projects, databases, collections and project API keys.

## Layout and checks

- `main.go` starts the provider. `internal/provider/` holds the project, database, collection and API key resources with their tests. `docs/` and `examples/` hold the registry docs and examples; `scripts/` holds the release-binary check and the govulncheck installer.
- The Go version is pinned in `.go-version`; release config is `.goreleaser.yml`.
- Build: `go build ./...`
- Fast tests: `go test ./...` (CI also runs govulncheck and a GoReleaser snapshot).

Workspace rules (hosting, secrets, models, Cloudflare, GitHub and shipping) are in the **Shipping code** section of the TerraScale workspace AGENTS.md.
