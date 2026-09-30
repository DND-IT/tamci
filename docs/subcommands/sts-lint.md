# `tamci sts-lint`

Checks the [octo-sts](https://github.com/octo-sts/app) trust policies in a
repository, `.github/chainguard/<identity>.sts.yaml`, against the rules a
policy must meet before a token broker such as one deployed with
[`terraform-aws-github-token-broker`](https://github.com/DND-IT/terraform-aws-github-token-broker)
hands out tokens on it. [`tamci token`](token.md) is the other side: the
workflow that asks for the token.

A trust policy lives in the repository the token is **for**, so each one is a
grant of access to that repository. Run this on every pull request that
touches `.github/chainguard/`, and put that directory under CODEOWNERS.

## Rules

| Rule | Why |
|------|-----|
| Fields are the ones octo-sts knows | A misspelt field is ignored by octo-sts, which loosens the policy silently |
| `issuer` is exactly `https://token.actions.githubusercontent.com` or one AWS account's issuer, `https://<id>.tokens.sts.global.api.aws`; no `issuer_pattern` | Only GitHub Actions tokens, or STS web identity tokens from one AWS account (IAM outbound identity federation) |
| Exactly one of `subject` or `subject_pattern` | octo-sts takes one |
| With a GitHub issuer, the subject starts with `repo:OWNER@OWNER-ID/REPO@REPO-ID:`, names literal, dots escaped | A repository deleted and recreated under the same name gets a new ID and inherits nothing. `gh api repos/OWNER/REPO/actions/oidc/customization/sub` prints the prefix |
| With an AWS issuer, `subject` is one role ARN, `arn:aws:iam::ACCOUNT-ID:role/NAME`, with no `subject_pattern` and no `claim_pattern` | The token's subject is the requesting principal's ARN, so the role is the whole identity; a pattern would admit roles nobody reviewed |
| The run context after the repository is a literal, or an alternation of literals such as `(pull_request\|ref:refs/heads/main)` | No `.*`: which runs qualify has to be readable |
| `claim_pattern.workflow_ref` (or `job_workflow_ref` for a reusable workflow) starts with `OWNER/REPO/\.github/workflows/FILE\.yaml@` | Only that workflow file can use the identity, not every workflow in the caller. `workflow_ref` must be in the subject's repository |
| `permissions` is not empty and each level is `read`, `write` or `admin` | The token gets exactly these |
| `write` or `admin` only when every allowed context is `ref:refs/heads/main` or `environment:<name>`, and never for an AWS issuer | A `pull_request` run executes the pull request's code, so anyone who can open one could use a write identity; an AWS role reads, and a write path stays a reviewed GitHub Actions run |
| Files end in `.sts.yaml` | octo-sts does not read `.sts.yml` |

## Flags

| Flag     | Default              | Description                           |
|----------|----------------------|---------------------------------------|
| `--path` | `.github/chainguard` | Directory holding the trust policies. |

Each finding is an `::error` annotation on the policy file, and any finding
fails the step. A repository with no policies passes.

## Example

```yaml
on:
  pull_request:
    paths: ['.github/chainguard/**']

permissions:
  contents: read

jobs:
  sts-lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7
      - uses: DND-IT/tamci/actions/sts-lint@v0
```

A policy that lets one IAM role in an AWS account read this repository, the way an
agent on AgentCore Runtime clones it. The issuer is the account's own, from
`aws iam get-outbound-web-identity-federation-info`, and the role requests its token with the
broker's domain as audience:

```yaml
issuer: https://a1d2b0fd-1177-4468-9351-2f0e723d1c44.tokens.sts.global.api.aws
subject: arn:aws:iam::017421875774:role/agent-harness
permissions:
  contents: read
  metadata: read
```

A policy that passes, granting a workflow in another repository write access
to this one from `main` only:

```yaml
issuer: https://token.actions.githubusercontent.com
subject: repo:DND-IT@19909911/my-service@123456789:ref:refs/heads/main
claim_pattern:
  workflow_ref: DND-IT/my-service/\.github/workflows/deploy\.yaml@refs/heads/main
permissions:
  contents: write
```
