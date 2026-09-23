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
| `issuer` is exactly `https://token.actions.githubusercontent.com`; no `issuer_pattern` | Only GitHub Actions tokens |
| Exactly one of `subject` or `subject_pattern` | octo-sts takes one |
| The subject starts with `repo:OWNER@OWNER-ID/REPO@REPO-ID:`, names literal, dots escaped | A repository deleted and recreated under the same name gets a new ID and inherits nothing. `gh api repos/OWNER/REPO/actions/oidc/customization/sub` prints the prefix |
| The run context after the repository is a literal, or an alternation of literals such as `(pull_request\|ref:refs/heads/main)` | No `.*`: which runs qualify has to be readable |
| `claim_pattern.workflow_ref` (or `job_workflow_ref` for a reusable workflow) starts with `OWNER/REPO/\.github/workflows/FILE\.yaml@` | Only that workflow file can use the identity, not every workflow in the caller. `workflow_ref` must be in the subject's repository |
| `permissions` is not empty and each level is `read`, `write` or `admin` | The token gets exactly these |
| `write` or `admin` only when every allowed context is `ref:refs/heads/main` or `environment:<name>` | A `pull_request` run executes the pull request's code, so anyone who can open one could use a write identity |
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
