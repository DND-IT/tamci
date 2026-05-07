# CLAUDE.md

Repo context for Claude Code.

## What this is

`tamci` is a single Go CLI that consolidates six previously separate
GitHub Actions: `action-config`, `action-deployer`, `action-releaser`,
`action-lock`, `action-summary`, `action-yaml-update`. Each former action
became a subcommand: `config`, `rollout`, `release`, `lock`, `summary`,
`set` (respectively).

The CLI runs identically locally and inside GitHub Actions. CI invokes
the same binary via Docker action shims that pass GitHub-Action inputs
through as `INPUT_*` env vars.

## Layout

- `cmd/tamci/main.go` — entry point, calls `cli.NewRootCmd().Execute()`.
- `internal/cli/` — one file per subcommand (`config.go`, `lock.go`, …)
  plus `root.go`. All commands use cobra+viper; viper has prefix `INPUT`
  so `INPUT_SERVICE` ↔ `--service`.
- `internal/<domain>/` — domain logic for each subcommand. The
  subcommand file in `internal/cli/` is the cobra wiring; the actual
  work lives in the domain package.
- `internal/gh/` — GitHub API. Two clients live here: `gh.go` is the
  go-github-based client (used by `set`), `rest.go` is a lighter raw-HTTP
  `RESTClient` (used by `rollout`). Don't unify them yet — they serve
  different test patterns.
- `internal/git/` — git CLI wrappers. `git.go` exposes stateless
  helpers (`Configure`, `CommitAndPush`); `client.go` exposes a stateful
  `Client` struct used by `rollout` (per-instance Dir/UserName, retry
  logic, force-push).
- `internal/yamlx/` — the merged YAML editing engine. `yamlx.go` is the
  base port from action-yaml-update; `file.go` adds `SetTag/ReadTag/
  HasTarget` wrappers used by `rollout`; `scan.go` has the deeper image-tag
  walker.
- `internal/matrix/` — `matrix-config.yaml` parsing for `tamci config`
  (action-config schema). NOT the same schema as `internal/rollout/config.go`,
  which uses action-deployer's per-service environment schema.
- `internal/release/{changelog,config,gitutil,publish,releasepr,strategy}/`
  — preserved subpackage structure from action-releaser to keep the
  test corpus intact.
- `shims/` — proposed `action.yaml` files for the six consumer repos.
  Not consumed by the build; just a planning artifact.

## Conventions

- **Module path is `github.com/dnd-it/tamci`** (the binary, not the org).
  All internal imports use this prefix.
- **No new YAML engines.** `internal/yamlx` is the one. If editing YAML,
  use `LoadYAML` → `Update*` → `DumpYAML`, or the file-level
  `SetTag/ReadTag/HasTarget` wrappers.
- **Read inputs via viper, not `os.Getenv("INPUT_…")`.** Each cobra
  command builds its own viper in PreRunE via `bindFlags(cmd, v)`. Reads
  go through `v.GetString("flag-name")`. The release subcommand is the
  exception — `release/config/config.go` reads env vars directly because
  the legacy `config.Load()` was kept intact; flags bridge into env vars
  in `cli/release.go` before calling Load.
- **Outputs go through `internal/gha`** (`SetOutput`, `AppendStepSummary`,
  `Notice`, `Warning`, `Error`). Don't print `::set-output` directly —
  `gha.SetOutput` already handles the modern `GITHUB_OUTPUT` file
  protocol with multi-line delimiter syntax.
- **Tests are load-bearing.** Each ported subpackage carries the
  original action's test corpus (~3,300 lines total). When refactoring,
  keep these green — they encode behavior contracts from the legacy
  actions.
- **The Dockerfile builds for `linux/amd64` by default** (alpine base,
  CGO_ENABLED=0). Static binary, ENTRYPOINT `/tamci`.

## Common commands

```bash
go test ./...                 # full suite
go build -o /tmp/tamci ./cmd/tamci
go vet ./...
go mod tidy
```

## Migration status (as of 2026-05-05)

The six consumer action repos (`dnd-it/action-config` etc.) still ship
their own Go binaries. They will be converted to thin shims pointing at
`ghcr.io/dnd-it/tamci:X.Y.Z` once tamci v1.0.0 is published. Two repos
get renamed: `action-deployer` → `action-rollout`, `action-releaser` →
`action-release`. GitHub redirects `uses:` references through repo
renames, so existing consumers don't break.

## Things NOT to do

- Don't reintroduce a second YAML editor under `internal/`. The whole
  point of the consolidation was killing the duplication between
  action-deployer and action-yaml-update.
- Don't change the `INPUT_` env prefix or the kebab-to-snake replacer in
  `internal/cli/root.go` — every shim's input name depends on it.
- Don't merge `internal/gh/gh.go` and `rest.go` without porting the
  deployer's existing tests over to go-github first; they assume the raw
  REST client's call patterns.
- Don't write planning docs/markdown unless the user asks. Use the
  conversation, plan files, or `docs/` if it's user-facing documentation.
