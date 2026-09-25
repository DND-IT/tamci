# `tamci rollout`

Atomically updates Helm values files. Two modes:

- **Matrix mode** (default): `--service` + `--version` resolves which
  files to touch via `matrix-config.yaml`.
- **Direct mode** (when `--file` is set): edits one or more YAML files
  with a single value, optionally opening a PR.

Replaces `action-deployer` (which will be renamed to `action-rollout`).

## Matrix-mode flags

| Flag             | Default                          | Description                              |
|------------------|----------------------------------|------------------------------------------|
| `--service`      | _(required)_                     | Service name from matrix config.         |
| `--version`      | _(required)_                     | Release version (e.g. `1.4.0`).          |
| `--sha`          | _(required)_                     | Short commit SHA (used when env tag=sha).|
| `--config`       | `.github/matrix-config.yaml`     | Path to the matrix config file.          |
| `--charts-dir`   | `deploy/charts`                  | Root for per-service Helm chart values.  |

A service rolls out to every environment under `environment:` unless it lists
a subset in `environments:`; naming one that is not defined is an error.

```yaml
environment:
  dev:
    deploy: auto
  prod:
    deploy: pr
service:
  api: {}
  console:
    environments: [prod]
```

## Direct-mode flags (presence of `--file` triggers direct mode)

| Flag             | Default      | Description                                            |
|------------------|--------------|--------------------------------------------------------|
| `--file`         | _(required)_ | Newline-separated YAML file paths.                     |
| `--value`        | _(required)_ | New value to write — applied to every file.            |
| `--mode`         | `image`      | `image` \| `key` \| `marker`.                          |
| `--key`          | _(empty)_    | Dot-notation path (mode=key).                          |
| `--deploy`       | `auto`       | `auto` (commit) \| `pr` (open PR).                     |
| `--branch`       | _(empty)_    | PR branch name (deploy=pr).                            |
| `--auto-merge`   | `false`      | Enable auto-merge on the deploy PR.                    |
| `--merge-method` | `SQUASH`     | `MERGE` \| `SQUASH` \| `REBASE`.                       |

## Shared

| Flag             | Default                                                    |
|------------------|------------------------------------------------------------|
| `--token`        | _(required)_ — needs `contents:write` + `pull-requests:write` |
| `--git-user-name`| `github-actions[bot]`                                      |
| `--git-user-email`| `github-actions[bot]@users.noreply.github.com`            |
| `--dry-run`      | `false`                                                    |
| `--release-notes`| `true`                                                     |
| `--tag-prefix`   | _(empty)_                                                  |

## Which branch `deploy: auto` pushes to

Auto deploys (matrix environments with `deploy: auto`, and direct mode with
`--deploy auto`) commit and push to the branch checked out in the working
directory. If HEAD is detached, they fall back to `GITHUB_REF_NAME` when the
run was triggered by a branch (or `main` outside GitHub Actions). On a
tag-triggered run with a detached HEAD there is no branch to push to, so the
rollout fails before editing anything; check out the target branch first:

```yaml
- uses: actions/checkout@v4
  with:
    ref: main
```

## Release notes on deploy PRs

Every deploy PR lists the GitHub releases it ships: each published release
after the tag currently in the values file, up to and including the new one,
newest first. A release is matched by `<tag_prefix><version>`; the prefix
comes from the service's `tag_prefix` in the matrix config, falling back
to `--tag-prefix`, and without one both `1.4.0` and `v1.4.0` match. If the old tag has no release (such as the first deploy) only the
new release is shown; if the new one has none (such as `tag: sha`
environments), the section is left out. A failed lookup never blocks the deploy. Turn it off
with `--release-notes=false`.

## Outputs

`deployed`, `environments`, `commit_sha`, `pr_urls`, `changed_files`, `diff`.
