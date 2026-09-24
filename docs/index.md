# tamci

Tamedia CI CLI — one Go binary, nine subcommands, runs identically locally
and inside GitHub Actions.

## Why it exists

Before tamci, six separate Go-based GitHub Actions covered our CI/CD
pipeline:

- `action-config` — read `matrix-config.yaml`, emit workflow matrix
- `action-deployer` — Helm values updates with PR or auto-commit
- `action-releaser` — versioned releases via git-cliff
- `action-lock` — distributed mutex on a git ref
- `action-summary` — write to `GITHUB_STEP_SUMMARY`
- `action-yaml-update` — generic YAML edits

Each shipped its own Go module, its own Docker image, its own GitHub API
client, its own `INPUT_*` parsing. Two of them — `action-deployer` and
`action-yaml-update` — contained near-duplicate format-preserving YAML
editors. Across the six repos, ~1,500 lines of overlapping code.

`tamci` consolidates them into a single binary with shared internals.
Each former action ships as a thin `action.yaml` under `actions/<name>`
in this repo that delegates to the unified Docker image.

## Subcommand surface

| Subcommand        | Replaces             | Notes                                       |
|-------------------|----------------------|---------------------------------------------|
| `tamci config`    | action-config        | Matrix expansion + change detection         |
| `tamci rollout`   | action-deployer      | Matrix or direct file mode                  |
| `tamci release`   | action-releaser      | semver/calver, direct or PR mode            |
| `tamci lock`      | action-lock          | git-ref mutex under `refs/locks/`           |
| `tamci promote`   | _(new)_              | Promote to prod by tag (experimental)       |
| `tamci set`       | action-yaml-update   | Format-preserving YAML editor               |
| `tamci summary`   | action-summary       | Append to GitHub step summary               |
| `tamci token`     | _(new)_              | GitHub App token from an octo-sts broker    |
| `tamci sts-lint`  | _(new)_              | Check octo-sts trust policies               |

See **[Subcommands](subcommands/config.md)** for per-command flags and
behavior.

## Three entry points, one code path

The same binary is invoked three ways with identical results:

```bash
# 1. CLI flags (local)
tamci rollout --service api --version 1.4.0

# 2. Environment variables (programmatic / scripts)
INPUT_SERVICE=api INPUT_VERSION=1.4.0 tamci rollout

# 3. Docker action (CI)
# inputs.service / inputs.version flow through automatically as INPUT_*
- uses: dnd-it/action-rollout@v1
  with:
    service: api
    version: 1.4.0
```

Cobra+Viper bind every flag to `INPUT_<NAME>`; the kebab-case-to-snake
replacer means `--charts-dir` reads `INPUT_CHARTS_DIR`.
