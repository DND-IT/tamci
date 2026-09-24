# `tamci promote`

Promote to prod by tag. The default branch deploys to dev; prod moves only
when someone pushes a tag `prod/<kind>/<target>/v<semver>` that points at a
commit on the default branch. Two subcommands cover the two sides:

- `tamci promote resolve` runs in CI and decides, from the GitHub event,
  what the run does.
- `tamci promote tag <target>` runs on a laptop with the person's own git
  credentials and creates and pushes the prod tag.

`promote` is experimental: both subcommands refuse to run unless the root
flag `--experimental` is passed (`tamci --experimental promote tag shared`)
or `INPUT_EXPERIMENTAL=true` is set, which is what the action's required
`experimental: true` input does. Its flags, outputs and tag format may still
change. No other subcommand is affected.

```yaml
- uses: DND-IT/tamci/actions/promote@v0
  id: target
  with:
    experimental: true
    target: shared
    kind: infra
```

## `tamci promote resolve`

Reads `GITHUB_EVENT_NAME`, the payload at `GITHUB_EVENT_PATH`,
`GITHUB_REF_TYPE`, `GITHUB_REF_NAME` and `GITHUB_SHA`.

| Event | Condition | `plan` | `apply` | `release` |
|-------|-----------|--------|---------|-----------|
| `pull_request` `opened`, `reopened`, `synchronize` | | `true` | dev if the pull request carries `label` | |
| `pull_request` `labeled` | the added label is `label` | | dev | |
| `pull_request` `unlabeled` | the removed label is `label` | | | `true` |
| `pull_request` `closed` | | | | `true` |
| `push` of a branch | the branch is `default_branch`, else error | | dev | |
| `push` of a tag | a prod tag on `default_branch`, else error | | prod | |
| `workflow_dispatch`, `inputs.environment` dev or unset | run from `default_branch`, else error | | dev | |
| `workflow_dispatch`, `inputs.environment` prod | run from a prod tag on `default_branch`, else error | | prod | |

A prod tag is one starting `<prod_environment>/<kind>/<target>/v`. The check
that its commit is on the default branch fetches `origin/<default_branch>`,
so check out with credentials (the `actions/checkout` default). Any other
event or pull request action gives `plan=false`, empty `apply` and
`release=false`. The step fails with an `::error::` when a run asks for an
environment it may not apply to.

### Flags

| Flag                 | Default      | Description |
|----------------------|--------------|-------------|
| `--target`           | _(req)_      | Stack or service name. |
| `--kind`             | _(req)_      | Tag kind, e.g. `infra` or `app`. |
| `--label`            | `deploy:dev` | Pull request label that applies the pull request to dev. |
| `--default-branch`   | _(event)_    | Defaults to `repository.default_branch` from the event, then `main`. |
| `--dev-environment`  | `dev`        | Name of the dev environment. |
| `--prod-environment` | `prod`       | Name of the prod environment; the first part of the tag. |

### Outputs

- `plan`: `true` / `false`
- `apply`: the dev or prod environment name, or empty
- `release`: `true` / `false`

A one-row table with the decision goes to the step summary.

### Example

```yaml
on:
  push:
    branches: [main]
    tags: ["prod/infra/shared/v*"]
  pull_request:
    types: [opened, synchronize, reopened, labeled, unlabeled, closed]
  workflow_dispatch:
    inputs:
      environment:
        type: choice
        options: [dev, prod]

jobs:
  setup:
    runs-on: ubuntu-latest
    outputs:
      plan: ${{ steps.target.outputs.plan }}
      apply: ${{ steps.target.outputs.apply }}
      release: ${{ steps.target.outputs.release }}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7
        with:
          fetch-depth: 0
      - id: target
        uses: DND-IT/tamci/actions/promote@v0
        with:
          target: shared
          kind: infra
```

## `tamci promote tag <target>`

Works out the prod tag for a stack or a service, shows it, asks, then
creates an annotated tag and pushes it to `origin`. It fetches the default
branch and the tags first, and refuses a tag that already exists.

- **infra** (a directory `<stacks-dir>/<target>`): the commit is the head of
  the latest successful `push` or `workflow_dispatch` run of
  `.github/workflows/<target>.yaml` on the default branch, which is what dev
  runs. The version bumps the last `prod/infra/<target>/v*` tag by the
  conventional commits since it under that workflow's `on.push.paths`: a
  `!` or a `BREAKING CHANGE:` footer is major, `feat` is minor, anything
  else is patch; the first tag starts from `0.0.0`. With nothing changed it
  fails unless `--version` is given. Needs a GitHub token: `GITHUB_TOKEN`,
  else `gh auth token`.
- **app** (a file `<charts-dir>/<target>/envs/prod/values.yaml`): the version
  is the latest `<target>/v*` release tag and the commit is that tag's. It
  also shows the tag prod runs now, read from the image block in the prod
  values file on the default branch whose `repository` ends in `/<target>`.

### Flags

| Flag           | Default         | Description |
|----------------|-----------------|-------------|
| `--kind`       | _(inferred)_    | `infra` or `app`. |
| `--version`    |                 | Tag this version instead of the computed one. |
| `--yes`        | `false`         | Push without asking. |
| `--dry-run`    | `false`         | Show the tag and commit, create nothing. |
| `--stacks-dir` | `stacks`        | Directory holding one Terraform stack per subdirectory. |
| `--charts-dir` | `deploy/charts` | Directory holding the Helm charts. |

### Example

```console
$ tamci promote tag console --dry-run
prod runs console 0.26.0; promoting 0.26.1

  tag:    prod/app/console/v0.26.1
  commit: 2db43e9 perf(console): cache record files by version so page views only list the bucket (#206)
dry run: not tagging
```
