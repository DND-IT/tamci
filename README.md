# tamci

Tamedia CI CLI — one Go binary, six subcommands, runs identically locally and inside GitHub Actions.

`tamci` consolidates six previously separate Go-based GitHub Actions
(`action-config`, `action-deployer`, `action-releaser`, `action-lock`,
`action-summary`, `action-yaml-update`) into a single executable. Each
former action ships as a thin `action.yaml` under `actions/<name>` that
delegates to the unified Docker image.

## Subcommands

| Subcommand        | Purpose                                                              |
|-------------------|----------------------------------------------------------------------|
| `tamci config`    | Read `matrix-config.yaml` and emit a workflow matrix.                |
| `tamci rollout`   | Matrix-driven Helm values updates, or direct file mode.              |
| `tamci release`   | Calculate the next version and create a tag/release via git-cliff.   |
| `tamci lock`      | Distributed mutex via GitHub git refs (`refs/locks/<name>`).         |
| `tamci set`       | Update YAML files with format preservation; optionally open a PR.    |
| `tamci summary`   | Read input text or a file and append it to `GITHUB_STEP_SUMMARY`.    |

## Quick start

### Locally

```bash
go build -o tamci ./cmd/tamci

./tamci summary --string '{"service":"api"}' --data-type json
./tamci config --config-path .github/matrix-config.yaml
./tamci rollout --service api --version 1.4.0 --dry-run
```

Flags map 1:1 onto the GitHub Action inputs: `--service` ↔ `INPUT_SERVICE`.
Same code path, three entry points (CLI flag, env var, action input).

### In GitHub Actions

Each subcommand ships as an action inside this repo under `actions/<name>`,
so consumers reference `DND-IT/tamci/actions/<name>`:

```yaml
- uses: DND-IT/tamci/actions/rollout@v0
  with:
    service: api
    version: 1.4.0
    token: ${{ secrets.GITHUB_TOKEN }}
```

Each action runs `docker://ghcr.io/dnd-it/tamci:X.Y.Z` with
`args: [rollout]` and the GitHub runner sets `INPUT_*` env vars from the
inputs block automatically. The former `action-*` repos are deprecated;
the mapping is in [docs/migration.md](docs/migration.md).

## Repo layout

```
tamci/
├── cmd/tamci/main.go         # entry point
├── internal/
│   ├── cli/                  # cobra command definitions (one file per subcommand)
│   ├── gh/                   # GitHub REST/GraphQL client
│   ├── gha/                  # GitHub Actions I/O helpers (outputs, summary)
│   ├── git/                  # git CLI wrappers (stateless + stateful Client)
│   ├── lock/                 # git-ref mutex
│   ├── matrix/               # matrix-config parsing + change detection
│   ├── release/              # release subpackages (changelog, strategy, ...)
│   ├── rollout/              # rollout orchestration + service/env config
│   ├── summary/              # JSON deserialization + summary formatting
│   └── yamlx/                # format-preserving YAML edit engine
├── docs/                     # TechDocs source (mkdocs + techdocs-core)
├── actions/                  # one action.yaml per subcommand (uses: DND-IT/tamci/actions/<name>)
├── action.yaml               # top-level wrapper action
├── catalog-info.yaml         # Backstage catalog entry
├── mkdocs.yaml               # TechDocs config
├── Dockerfile                # multi-stage build, ENTRYPOINT /tamci
└── go.mod
```

## Development

```bash
go test ./...                 # full suite (~2k LOC of test corpora)
go build ./cmd/tamci          # build the binary
docker build -t tamci:dev .   # build the image
```

For deeper documentation see [/docs](docs/index.md) (rendered by Backstage TechDocs).

## License

See [LICENSE](LICENSE) when added.
