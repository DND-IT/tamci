# `tamci lock`

Distributed mutex backed by GitHub git refs. Acquire creates a ref under
`refs/locks/<name>` (atomically; ref creation is unique); release deletes
it; status reads it without changing anything.

The ref points at a lock commit made on top of `--sha`: its tree is the
target commit's tree, its committer date is the acquisition time and its
message carries a `Lock-Holder: <holder>` trailer when `--holder` is set.
Stale locks are detected from that acquisition time, not from the age of
the commit being locked.

With `--holder`:

- acquire succeeds if the lock is free or already held by the same holder;
  a re-acquire moves the ref to a new lock commit on top of `--sha`, which
  also renews the acquisition time.
- release deletes the ref only if the recorded holder matches, and outputs
  `released=false` otherwise without failing the step.

Without `--holder`, a held lock is never re-acquired and release deletes the
ref unconditionally, as before.

Replaces `action-lock`.

## Flags

| Flag                | Default | Description                                                         |
|---------------------|---------|---------------------------------------------------------------------|
| `--action`          | _(req)_ | `acquire` \| `release` \| `status`.                                 |
| `--lock-name`       | _(req)_ | Used as the ref name under `refs/locks/`.                           |
| `--sha`             | `$GITHUB_SHA` | Commit the lock is taken on.                                  |
| `--holder`          | _(none)_ | Who holds the lock (e.g. `pr-123`).                                |
| `--timeout`         | `300`   | Max seconds to wait for acquisition. `0` tries once.                |
| `--poll-interval`   | `10`    | Seconds between attempts.                                           |
| `--stale-threshold` | `600`   | Seconds since acquisition after which a lock is force-acquirable. `0` disables. |
| `--fail-on-timeout` | `true`  | Fail the step on timeout (set `false` to skip gracefully).          |
| `--token`           | _(req)_ | GitHub token with `contents:write`.                                 |

## Outputs

- `acquired`: `true` / `false` (acquire, release)
- `lock_ref`: full ref path (e.g. `refs/locks/deploy-prod`)
- `holder`: the current holder, empty if free or taken without one (acquire, status)
- `locked`: `true` / `false` (status)
- `released`: `true` / `false` (release)

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

A pull request holding an environment until its label is removed:

```yaml
- id: lock
  uses: DND-IT/tamci/actions/lock@v0
  with:
    action: acquire
    lock_name: tf-infra-dev
    sha: ${{ github.event.pull_request.head.sha }}
    holder: pr-${{ github.event.number }}
    timeout: 0
    stale_threshold: 0
    fail_on_timeout: false
    token: ${{ secrets.GITHUB_TOKEN }}
- if: steps.lock.outputs.acquired != 'true'
  run: echo "held by ${{ steps.lock.outputs.holder }}"

# on main: skip while a pull request holds it
- id: status
  uses: DND-IT/tamci/actions/lock@v0
  with:
    action: status
    lock_name: tf-infra-dev
    token: ${{ secrets.GITHUB_TOKEN }}

# on unlabel or close: only the holder releases
- uses: DND-IT/tamci/actions/lock@v0
  with:
    action: release
    lock_name: tf-infra-dev
    holder: pr-${{ github.event.number }}
    token: ${{ secrets.GITHUB_TOKEN }}
```
