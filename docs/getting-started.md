# Getting started

## Build from source

```bash
git clone git@github.com:DND-IT/tamci.git
cd tamci
go build -o /usr/local/bin/tamci ./cmd/tamci
tamci --help
```

## Use as a Docker action

Each subcommand has a thin `action.yaml` under `actions/<name>` in this
repo (`uses: DND-IT/tamci/actions/<name>@v0`) that delegates to the
unified `tamci` image. The runner sets `INPUT_*` for every input block
field automatically — no `env:` plumbing is needed in the workflow.

```yaml
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: dnd-it/action-rollout@v1
        with:
          service: api
          version: 1.4.0
          token: ${{ secrets.GITHUB_TOKEN }}
```

## Run any subcommand locally

The local UX matches CI exactly. Useful for debugging an action without
push-and-pray:

```bash
# Dry-run a deploy
tamci rollout --service api --version 1.4.0 --dry-run

# Read a matrix config and print the matrix
tamci config --config-path .github/matrix-config.yaml

# Write to a step-summary file (instead of $GITHUB_STEP_SUMMARY)
GITHUB_STEP_SUMMARY=/tmp/summary.md tamci summary --string 'hello'
cat /tmp/summary.md
```

When `GITHUB_STEP_SUMMARY` / `GITHUB_OUTPUT` are unset, output falls
back to stdout — convenient for local exploration.

## Environment expectations

Subcommands that talk to git or GitHub expect the same env vars the
runner sets:

- `GITHUB_REPOSITORY` — `owner/repo`
- `GITHUB_TOKEN` — fallback when `--token` is not set
- `GITHUB_REF_NAME`, `GITHUB_BASE_REF` — used by change detection

For local runs, set the ones you need or pass `--token` explicitly.
