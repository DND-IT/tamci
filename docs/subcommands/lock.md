# `tamci lock`

Distributed mutex backed by GitHub git refs. Acquire creates a ref under
`refs/locks/<name>` (atomically; ref creation is unique); release deletes
it. Detects and clears stale locks based on commit age.

Replaces `action-lock`.

## Flags

| Flag                | Default | Description                                                         |
|---------------------|---------|---------------------------------------------------------------------|
| `--action`          | _(req)_ | `acquire` \| `release`.                                             |
| `--lock-name`       | _(req)_ | Used as the ref name under `refs/locks/`.                           |
| `--timeout`         | `300`   | Max seconds to wait for acquisition.                                |
| `--poll-interval`   | `10`    | Seconds between attempts.                                           |
| `--stale-threshold` | `600`   | Seconds after which a lock is force-acquirable. `0` disables.       |
| `--fail-on-timeout` | `true`  | Fail the step on timeout (set `false` to skip gracefully).          |
| `--token`           | _(req)_ | GitHub token with `contents:write`.                                 |

## Outputs

- `acquired` — `true` / `false`
- `lock_ref` — full ref path (e.g. `refs/locks/deploy-prod`)

## Example

```yaml
- uses: dnd-it/action-lock@v1
  with:
    action: acquire
    lock_name: deploy-prod
    timeout: 600
    token: ${{ secrets.GITHUB_TOKEN }}

# ...protected work here...

- uses: dnd-it/action-lock@v1
  if: always()
  with:
    action: release
    lock_name: deploy-prod
    token: ${{ secrets.GITHUB_TOKEN }}
```
