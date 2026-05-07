# Shim action.yaml proposals

These are the proposed thin `action.yaml` files that each `action-*` repo will
contain after the migration. Each one delegates to a single `tamci` subcommand
inside the unified `ghcr.io/dnd-it/tamci` Docker image.

## Migration order

1. Publish `tamci@v1.0.0` (build, push to ghcr.io).
2. For each shim:
   1. In the existing repo (e.g. `dnd-it/action-config`), replace the contents
      with the corresponding shim file plus a slimmed `README.md`,
      `CHANGELOG.md`, `release-please-config.json`, `LICENSE`,
      `catalog-info.yaml`. Delete `cmd/`, `internal/`, `Dockerfile`,
      `go.mod`, `go.sum`, tests.
   2. Cut a final tag under the old name.
   3. Rename the GitHub repo:
      - `action-deployer` → `action-rollout`
      - `action-releaser` → `action-release`
      The other four keep their names.
   4. Cut `v1.0.0` under the new name.

GitHub repo renames preserve `uses:` redirects, so existing consumers keep
working unchanged.

## Mapping

| Shim                | CLI subcommand   | Notes                                    |
|---------------------|------------------|------------------------------------------|
| action-config       | `tamci config`   | unchanged                                |
| action-rollout      | `tamci rollout`  | renamed from action-deployer             |
| action-release      | `tamci release`  | renamed from action-releaser             |
| action-lock         | `tamci lock`     | input `action: acquire\|release` mapped via INPUT_ACTION |
| action-summary      | `tamci summary`  | unchanged                                |
| action-yaml-update  | `tamci set`      | repo name kept; CLI verb is `set`        |
