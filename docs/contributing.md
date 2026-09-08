# Contributing

## Local development

```bash
go test ./...                     # run the full suite (~2k LOC of tests)
go build -o /tmp/tamci ./cmd/tamci
go vet ./...
go mod tidy
```

Smoke-test a subcommand without GitHub Actions env:

```bash
/tmp/tamci summary --string '{"k":"v"}' --data-type json
/tmp/tamci config --config-path .github/matrix-config.yaml
/tmp/tamci rollout --service api --version 1.4.0 --dry-run
```

## Adding a subcommand

1. Create `internal/<name>/` with the domain logic. Keep cobra/viper
   out of this package — take plain function arguments. Tests live here.
2. Create `internal/cli/<name>.go` that wires the cobra command, defines
   flags, and calls into your domain package.
3. Register in `internal/cli/root.go` via `cmd.AddCommand(new<Name>Cmd())`.
4. Add a doc page under `docs/subcommands/<name>.md` and add it to the
   nav in `mkdocs.yml`.

The subcommand naming convention: imperative verb (`rollout`, `set`,
`release`), or noun for read-only commands (`config`, `summary`).

## Conventions

- **Read inputs via viper, not `os.Getenv("INPUT_…")`.** The viper
  bootstrap in `internal/cli/root.go` handles the env prefix, kebab-to-snake
  translation, and flag fallback.
- **Outputs go through `internal/gha`.** Don't print
  `::set-output::` directly — `gha.SetOutput` handles the modern
  `GITHUB_OUTPUT` file protocol with multi-line delimiter syntax.
- **No new YAML engines.** `internal/yamlx` is the one. If editing
  YAML, use `LoadYAML` → `Update*` → `DumpYAML`, or the file-level
  `SetTag/ReadTag/HasTarget` wrappers.
- **Preserve test corpora.** Each domain package carries the original
  action's test corpus. When refactoring, keep these green — they
  encode behavior contracts from the legacy actions.

## Releasing

`tamci` itself uses `tamci release` once the bootstrap loop is complete.
For now: tag manually, push, and the CI pipeline (TODO) builds the
multi-arch image to `ghcr.io/dnd-it/tamci:<tag>`.

Each `actions/<name>/action.yaml` pins a specific tamci tag via the
`# x-release-please-version` comment and is listed under `extra-files`
in `release-please-config.json`.
