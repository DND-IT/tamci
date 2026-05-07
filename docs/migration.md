# Migration from action-* repos

The six former GitHub Actions are migrated one repo at a time. Each
becomes a thin `action.yaml` shim that delegates to the unified
`ghcr.io/dnd-it/tamci` Docker image.

## Per-action mapping

| Old repo                     | New repo (if renamed)        | tamci subcommand   | Shim `args`     |
|------------------------------|------------------------------|--------------------|-----------------|
| `dnd-it/action-config`       | _(unchanged)_                | `tamci config`     | `[config]`      |
| `dnd-it/action-deployer`     | `dnd-it/action-rollout`      | `tamci rollout`    | `[rollout]`     |
| `dnd-it/action-releaser`     | `dnd-it/action-release`      | `tamci release`    | `[release]`     |
| `dnd-it/action-lock`         | _(unchanged)_                | `tamci lock`       | `[lock]`        |
| `dnd-it/action-summary`      | _(unchanged)_                | `tamci summary`    | `[summary]`     |
| `dnd-it/action-yaml-update`  | _(unchanged)_                | `tamci set`        | `[set]`         |

## What a shim looks like

After migration, each consumer repo collapses to roughly one file.
Worked example for `action-rollout` (renamed from `action-deployer`):

```yaml
name: Rollout
description: Atomically updates Helm values files (matrix or direct mode).
author: DND-IT

inputs:
  service:
    description: "Service name; must match a key under service: in matrix config"
    required: false
  # …all existing inputs, names UNCHANGED so consumers don't break…
  token:
    description: "GitHub token"
    required: true

outputs:
  deployed:
    description: "true if at least one environment was updated"
  # …all existing outputs, UNCHANGED…

runs:
  using: docker
  image: docker://ghcr.io/dnd-it/tamci:1.0.0
  args: [rollout]

branding:
  icon: upload-cloud
  color: blue
```

**Deleted from the shim repo:** `cmd/`, `internal/`, `go.mod`, `go.sum`,
`Dockerfile`, all `*_test.go`, build artifacts.

**Kept:** `action.yaml`, slimmed `README.md`, `CHANGELOG.md`,
`release-please-config.json`, `LICENSE`, `catalog-info.yaml`.

## Rollout order

1. Publish `tamci@v1.0.0` (build + push to ghcr.io).
2. For each consumer repo:
    1. Replace contents with the shim.
    2. Cut a final tag under the old repo name.
    3. Rename the GitHub repo (`action-deployer` → `action-rollout`,
       `action-releaser` → `action-release`); the four others keep their
       names.
    4. Cut `v1.0.0` under the new name.

GitHub repo renames preserve `uses:` references via permanent redirects,
so `dnd-it/action-deployer@v0.1.3` keeps resolving even after the repo
moves to `action-rollout`. Consumers migrate at their own pace.

## INPUT_* env-var passthrough

The runner sets `INPUT_<NAME>` for every input on Docker actions
automatically. Viper inside `tamci` reads via `SetEnvPrefix("INPUT")` +
a kebab-to-snake key replacer, so `inputs.service` →
`INPUT_SERVICE` → `--service` — the same code path, three entry points.
No `env:` block is needed in `action.yaml`.

## Tradeoff: per-action release independence

Before consolidation, you could ship `action-lock@0.1.1` without
touching `action-deployer@0.1.3`. Under tamci, every fix is one
binary release and every shim points at it. Each shim still pins a
specific `tamci` tag, so consumers can stay on an older CLI during a
regression — but you publish one binary, not six.
