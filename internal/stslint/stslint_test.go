package stslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const smoke = `issuer: https://token.actions.githubusercontent.com
subject_pattern: repo:DND-IT@19909911/tamedia-ai-platform@1328624642:ref:refs/heads/main
claim_pattern:
  workflow_ref: DND-IT/tamedia-ai-platform/\.github/workflows/token-broker-smoke\.yaml@.*
permissions:
  metadata: read
`

func TestLint_Passes(t *testing.T) {
	cases := map[string]string{
		"read-only policy on main": smoke,
		"read-only policy reachable from pull requests": `issuer: https://token.actions.githubusercontent.com
subject_pattern: repo:DND-IT@19909911/terraform-aws-github-token-broker@1379168258:(pull_request|ref:refs/heads/main)
claim_pattern:
  workflow_ref: DND-IT/terraform-aws-github-token-broker/\.github/workflows/e2e\.yaml@.*
permissions:
  metadata: read
`,
		"cross-repo write from main": `issuer: https://token.actions.githubusercontent.com
subject: repo:DND-IT@19909911/my-service@123456:ref:refs/heads/main
claim_pattern:
  workflow_ref: DND-IT/my-service/\.github/workflows/deploy\.yaml@refs/heads/main
permissions:
  contents: write
`,
		"write from a protected environment": `issuer: https://token.actions.githubusercontent.com
subject_pattern: repo:DND-IT@19909911/my\.service@123456:environment:prod
claim_pattern:
  workflow_ref: DND-IT/my\.service/\.github/workflows/deploy\.yml@.*
permissions:
  contents: write
  pull_requests: write
`,
		"reusable workflow in another repo": `issuer: https://token.actions.githubusercontent.com
subject: repo:DND-IT@19909911/my-service@123456:ref:refs/heads/main
claim_pattern:
  job_workflow_ref: DND-IT/github-workflows/\.github/workflows/service-pipeline\.yaml@refs/tags/v3.*
permissions:
  contents: write
`,
	}
	for name, policy := range cases {
		t.Run(name, func(t *testing.T) {
			if msgs := Lint([]byte(policy)); len(msgs) != 0 {
				t.Errorf("want no findings, got %q", msgs)
			}
		})
	}
}

func TestLint_Fails(t *testing.T) {
	cases := map[string]struct {
		policy string
		want   string
	}{
		"unknown field": {
			strings.Replace(smoke, "claim_pattern:", "claim_patterns:", 1),
			"field claim_patterns not found",
		},
		"another issuer": {
			strings.Replace(smoke, "https://token.actions.githubusercontent.com", "https://accounts.google.com", 1),
			"issuer must be",
		},
		"issuer pattern": {
			"issuer_pattern: .*\n" + smoke,
			"issuer_pattern is not allowed",
		},
		"mutable subject": {
			strings.Replace(smoke, "DND-IT@19909911/tamedia-ai-platform@1328624642", "DND-IT/tamedia-ai-platform", 1),
			"must start with repo:OWNER@OWNER-ID/REPO@REPO-ID:",
		},
		"wildcard repository": {
			strings.Replace(smoke, "tamedia-ai-platform@1328624642", ".*@[0-9]+", 1),
			"must start with repo:OWNER@OWNER-ID/REPO@REPO-ID:",
		},
		"open context": {
			strings.Replace(smoke, ":ref:refs/heads/main", ":.*", 1),
			"must be a literal",
		},
		"both subject forms": {
			"subject: repo:DND-IT@19909911/x@1:ref:refs/heads/main\n" + smoke,
			"not both",
		},
		"no workflow pin": {
			`issuer: https://token.actions.githubusercontent.com
subject: repo:DND-IT@19909911/x@1:ref:refs/heads/main
permissions:
  metadata: read
`,
			"claim_pattern must pin workflow_ref",
		},
		"workflow pin open": {
			strings.Replace(smoke, `DND-IT/tamedia-ai-platform/\.github/workflows/token-broker-smoke\.yaml@.*`, ".*", 1),
			"claim_pattern.workflow_ref must start with",
		},
		"workflow pin in another repo": {
			strings.Replace(smoke, `DND-IT/tamedia-ai-platform/\.github`, `DND-IT/other/\.github`, 1),
			"names DND-IT/other but the subject is DND-IT/tamedia-ai-platform",
		},
		"no permissions": {
			strings.Replace(smoke, "permissions:\n  metadata: read\n", "", 1),
			"permissions is empty",
		},
		"bad permission level": {
			strings.Replace(smoke, "metadata: read", "metadata: all", 1),
			`"all" is not read, write or admin`,
		},
		"write reachable from pull requests": {
			strings.NewReplacer(":ref:refs/heads/main", ":(pull_request|ref:refs/heads/main)", "metadata: read", "contents: write").Replace(smoke),
			`granted to runs from "pull_request"`,
		},
		"write from another branch": {
			strings.NewReplacer("refs/heads/main", "refs/heads/dev", "metadata: read", "contents: write").Replace(smoke),
			`granted to runs from "ref:refs/heads/dev"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			msgs := Lint([]byte(tc.policy))
			for _, m := range msgs {
				if strings.Contains(m, tc.want) {
					return
				}
			}
			t.Errorf("want a finding containing %q, got %q", tc.want, msgs)
		})
	}
}

func TestLintDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("smoke.sts.yaml", smoke)
	write("bad.sts.yaml", "issuer: nope\n")
	write("old.sts.yml", smoke)
	write("README.md", "not a policy")

	findings, count, err := LintDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	files := map[string]bool{}
	for _, f := range findings {
		files[filepath.Base(f.File)] = true
	}
	if !files["bad.sts.yaml"] || !files["old.sts.yml"] || files["smoke.sts.yaml"] || files["README.md"] {
		t.Errorf("findings in %v", files)
	}
}

func TestLintDir_Missing(t *testing.T) {
	findings, count, err := LintDir(filepath.Join(t.TempDir(), "absent"))
	if err != nil || count != 0 || findings != nil {
		t.Errorf("got %v, %d, %v", findings, count, err)
	}
}
