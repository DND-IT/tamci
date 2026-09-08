# Migration from action-* repos

The six former GitHub Actions ship as actions inside this repo, one per
subcommand under `actions/<name>`. Each is a thin `action.yaml` that
runs the unified `ghcr.io/dnd-it/tamci` Docker image. The dedicated
`action-*` repos are deprecated and receive no further changes.

## Per-action mapping

| Old `uses:`                     | New `uses:`                              | tamci subcommand |
|---------------------------------|------------------------------------------|------------------|
| `DND-IT/action-config@v3`       | `DND-IT/tamci/actions/config@v0`         | `tamci config`   |
| `DND-IT/action-deployer@v0`     | `DND-IT/tamci/actions/rollout@v0`        | `tamci rollout`  |
| `DND-IT/action-releaser@v0`     | `DND-IT/tamci/actions/release@v0`        | `tamci release`  |
| `DND-IT/action-lock@v0`         | `DND-IT/tamci/actions/lock@v0`           | `tamci lock`     |
| `DND-IT/action-summary@v2`      | `DND-IT/tamci/actions/summary@v0`        | `tamci summary`  |
| `DND-IT/action-yaml-update@v0`  | `DND-IT/tamci/actions/set@v0`            | `tamci set`      |

Inputs and outputs are unchanged, so a migration is a one-line edit of
the `uses:` reference. The legacy tags keep working until the old repos
are archived.

## What an action looks like

```yaml
runs:
  using: docker
  image: 'docker://ghcr.io/dnd-it/tamci:0.1.1' # x-release-please-version
  args: [rollout]
```

The image tag carries a release-please marker, and every
`actions/*/action.yaml` is listed under `extra-files` in
`release-please-config.json`, so each tamci release bumps all six
actions in the same commit that creates the tag.

## Prerequisites for consumers

- The `tamci` repo is private. Other repos in the organization can use
  its actions because the repo's Actions access level is set to
  "organization".
- The `ghcr.io/dnd-it/tamci` package must be public. The runner pulls
  `docker://` images before any step can log in, so a private package
  fails with `unauthorized`.

## INPUT_* env-var passthrough

The runner sets `INPUT_<NAME>` for every input on Docker actions
automatically. Viper inside `tamci` reads via `SetEnvPrefix("INPUT")` +
a kebab-to-snake key replacer, so `inputs.service` →
`INPUT_SERVICE` → `--service` — the same code path, three entry points.
No `env:` block is needed in `action.yaml`.

## Tradeoff: per-action release independence

Before consolidation, you could ship `action-lock@0.1.1` without
touching `action-deployer@0.1.3`. Under tamci, every fix is one
binary release and every action points at it. Consumers still pin a
specific tag, so they can stay on an older CLI during a regression, but
you publish one binary, not six.
