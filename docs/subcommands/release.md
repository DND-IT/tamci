# `tamci release`

Calculate the next version (semver or calver) and create a tag/release
via git-cliff. Two release modes:

- **`direct`** (default): tag and release immediately.
- **`pr`**: open a release PR; on merge, create the tag and release.

Replaces `action-releaser` (which will be renamed to `action-release`).

## Flags

| Flag                | Default    | Description                                                         |
|---------------------|------------|---------------------------------------------------------------------|
| `--version-strategy`| `semver`   | `semver` \| `calver`.                                               |
| `--cliff-config`    | _(empty)_  | Path to custom `cliff.toml` (auto-detect if empty).                 |
| `--tag-prefix`      | _(empty)_  | Prefix for git tags (e.g. `v`).                                     |
| `--release-mode`    | `direct`   | `direct` \| `pr`.                                                   |
| `--draft`           | `false`    | Create release as draft.                                            |
| `--prerelease`      | `false`    | Mark release as prerelease.                                         |
| `--include-path`    | _(empty)_  | Glob to scope commits (e.g. `services/api/**`) for monorepos.       |
| `--dry-run`         | `false`    | Calculate version + changelog without creating tag/release.         |
| `--github-token`    | env        | Falls back to `GITHUB_TOKEN`.                                       |

## Outputs

`version`, `changelog`, `tag`, `release-url`, `previous-version`,
`skipped`, `dry-run`, `release-mode`, `release-published`,
`release-pr-url`, `release-pr-number`, `pr-created`.

## Configuration file

A `.release.yml` (or `.release.yaml`) at the repo root can set defaults:

```yaml
version-strategy: semver
tag-prefix: v
release-mode: pr
draft: false
prerelease: false

# Monorepo: separate scopes
packages:
  - name: api
    path: services/api
    tag-pattern: api-v
  - name: worker
    path: services/worker
    tag-pattern: worker-v
```

CLI flags / `INPUT_*` env vars override the file.
