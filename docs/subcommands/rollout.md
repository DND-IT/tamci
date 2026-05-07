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

## Outputs

`deployed`, `environments`, `commit_sha`, `pr_urls`, `changed_files`, `diff`.
