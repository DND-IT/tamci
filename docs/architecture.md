# Architecture

## One binary, eight subcommands

`tamci` is a Go CLI built on cobra. Each subcommand lives in
`internal/cli/<name>.go` and delegates to a domain package under
`internal/<domain>/`. The domain packages don't know about cobra or
viper — they take plain function arguments and return values, which
keeps them testable and reusable.

```
cmd/tamci/main.go
  └─ internal/cli/root.go              (cobra root + viper bootstrap)
       ├─ config.go    → internal/matrix/      (action-config schema)
       ├─ rollout.go   → internal/rollout/     (action-deployer schema + orchestration)
       ├─ release.go   → internal/release/{changelog,strategy,…}
       ├─ lock.go      → internal/lock/
       ├─ set.go       → internal/yamlx + internal/git + internal/gh
       ├─ stslint.go   → internal/stslint/
       ├─ summary.go   → internal/summary/
       └─ token.go     → internal/token/
```

## Shared infrastructure

Three packages were extracted to kill the duplication that existed
across the six original action repos:

| Package          | Replaces                                                                      |
|------------------|-------------------------------------------------------------------------------|
| `internal/yamlx` | `action-yaml-update/internal/updater` + `action-deployer/internal/values`     |
| `internal/git`   | `action-yaml-update/internal/gitops` + `action-deployer/internal/git`         |
| `internal/gh`    | `action-yaml-update/internal/github` + `action-deployer/internal/github`      |

Two style of clients live side-by-side in `internal/git` and `internal/gh`:

- **Stateless functions** (used by `set`) — small, fixed surface, easier
  for one-shot calls.
- **Stateful `Client` struct** (used by `rollout`) — per-instance
  identity and working directory, retry-with-pull-rebase on push,
  separate force-push primitive. Needed by deploy flows where multiple
  environments may race.

They share the same git binary / GitHub API underneath; the split is
just API ergonomics.

## Input plumbing

Cobra defines flags in kebab-case:

```go
f.String("charts-dir", "deploy/charts", "...")
```

Viper is bootstrapped with prefix `INPUT` and a kebab-to-snake key
replacer. Reading `v.GetString("charts-dir")` resolves in order:

1. Cobra flag value, if set.
2. `INPUT_CHARTS_DIR` env var, if set.
3. Flag default.

Three entry points, one code path. Local CLI usage and CI Docker action
invocation hit the same `RunE`.

## Outputs

`internal/gha` writes to `GITHUB_OUTPUT` (with multi-line delimiter
syntax for newline-bearing values), `GITHUB_STEP_SUMMARY`, and emits
workflow `::notice::` / `::warning::` / `::error::` annotations.

When the relevant env vars are unset (local runs), output falls back to
stdout.

## Why this layout

- **Domain packages are reusable.** Nothing in `internal/yamlx` knows
  about cobra; it can be imported by future tools.
- **Test corpora are preserved.** `internal/yamlx`, `internal/matrix`,
  `internal/release/{strategy,config,…}`, and `internal/lock` carry the
  original tests verbatim from the legacy actions, with package renames.
  When refactoring, keep these green — they encode behavior contracts.
- **Subcommands are isolated.** A bug in `release` doesn't touch
  `summary` or `lock`. The cost is one binary release blocking all six,
  but consumers still pin a specific tamci tag.
