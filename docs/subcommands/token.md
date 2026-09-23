# `tamci token`

Exchanges the job's GitHub Actions OIDC token for a GitHub App installation
token at an [octo-sts](https://github.com/octo-sts/app) broker, such as one
deployed with
[`terraform-aws-github-token-broker`](https://github.com/DND-IT/terraform-aws-github-token-broker).
The token is limited to the scope repository and the permissions in the
trust policy `.github/chainguard/<identity>.sts.yaml` on that repository's
default branch, and is valid for one hour.

As an action, the post step revokes the token when the job ends.

## Flags

| Flag         | Default                | Description                                                        |
|--------------|------------------------|--------------------------------------------------------------------|
| `--url`      | _(req)_                | Broker exchange URL (`https://<domain>/sts/exchange`).             |
| `--identity` | _(req)_                | Trust policy name.                                                 |
| `--scope`    | `$GITHUB_REPOSITORY`   | Repository (`owner/repo`) the token is issued for.                 |
| `--audience` | host of `--url`        | OIDC token audience; set only for a policy with its own audience.  |

The job needs `permissions: id-token: write`.

## Outputs

- `token` — the installation token, masked in logs

## Example

```yaml
permissions:
  id-token: write

steps:
  - id: broker
    uses: DND-IT/tamci/actions/token@v0
    with:
      url: https://github-sts.example.com/sts/exchange
      identity: release

  - env:
      GH_TOKEN: ${{ steps.broker.outputs.token }}
    run: gh release create ...
```

Transport errors and 5xx responses from the broker are retried three times.
A 4xx fails the step with the broker's message, which names the trust policy
check that failed.
