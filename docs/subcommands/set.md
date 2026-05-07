# `tamci set`

Update YAML files with format-preserving edits; optionally open a PR
with the changes. Three location strategies via `--mode`:

- `key` — explicit dot-notation paths.
- `image` — find Helm `image:` blocks (or Kustomize `images:` lists)
  matching `--image-name` and update their tag.
- `marker` — find scalars whose inline comment contains a marker.

Replaces `action-yaml-update`. Repo name unchanged; the CLI verb is `set`.

## File selection

| Flag             | Description                                              |
|------------------|----------------------------------------------------------|
| `--files`        | Newline-separated YAML file paths.                       |
| `--files-from`   | Directory to recursively search.                         |
| `--files-filter` | Filename filter for `--files-from` (e.g. `values.yaml`). |

At least one of `--files` / `--files-from` must be set.

## Mode-specific flags

### `--mode key`

```bash
tamci set --files deploy/values.yaml --mode key \
          --keys image.tag --values 1.4.0
```

Multi-key: `--keys` and `--values` are newline-separated parallel lists,
or use `--value` to apply one value to every key.

### `--mode image`

```bash
tamci set --files deploy/values.yaml --mode image \
          --image-name api --image-tag 1.4.0
```

### `--mode marker`

```yaml
# deploy/values.yaml
image:
  tag: 1.0.0  # x-yaml-update
```

```bash
tamci set --files deploy/values.yaml --mode marker --value 1.4.0
```

Multiple markers: `--markers` (newline-separated) + `--values`.

## PR / commit options

| Flag              | Default                            |
|-------------------|------------------------------------|
| `--create-pr`     | `true`                             |
| `--target-branch` | repo default                       |
| `--pr-branch`     | auto-generated                     |
| `--pr-title`      | `chore: update YAML values`        |
| `--pr-body`       | auto-generated change list         |
| `--pr-labels`     | _(empty)_, comma-separated         |
| `--pr-reviewers`  | _(empty)_, comma-separated         |
| `--auto-merge`    | `false`                            |
| `--merge-method`  | `SQUASH`                           |
| `--commit-message`| `chore: update YAML values`        |
| `--dry-run`       | `false` — preview without writing  |

## Outputs

`changed`, `changed_files`, `pr_number`, `pr_url`, `commit_sha`, `diff`.
