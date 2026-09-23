// Package stslint checks octo-sts trust policies (.github/chainguard/*.sts.yaml)
// against the rules a policy must meet before a broker hands out tokens on it:
// GitHub Actions as the only issuer, a caller pinned by immutable repository ID
// and by workflow file, and write permissions only from main or a GitHub
// environment.
package stslint

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// GitHubActionsIssuer is the only issuer a policy may trust.
const GitHubActionsIssuer = "https://token.actions.githubusercontent.com"

// Policy mirrors octo-sts's TrustPolicy. Decoding is strict, so a misspelt
// field fails the lint instead of silently loosening the policy.
type Policy struct {
	Issuer          string            `yaml:"issuer"`
	IssuerPattern   string            `yaml:"issuer_pattern"`
	Subject         string            `yaml:"subject"`
	SubjectPattern  string            `yaml:"subject_pattern"`
	Audience        string            `yaml:"audience"`
	AudiencePattern string            `yaml:"audience_pattern"`
	ClaimPattern    map[string]string `yaml:"claim_pattern"`
	Permissions     map[string]string `yaml:"permissions"`
	App             string            `yaml:"app"`
	AppPattern      string            `yaml:"app_pattern"`
}

// Finding is one rule a policy file breaks.
type Finding struct {
	File    string
	Message string
}

func (f Finding) String() string { return f.File + ": " + f.Message }

var (
	// A literal regex fragment: name characters, with dots escaped.
	literalFrag = `(?:[A-Za-z0-9_/:@-]|\\\.)+`

	subjectExact   = regexp.MustCompile(`^repo:([A-Za-z0-9-]+)@[0-9]+/([A-Za-z0-9_.-]+)@[0-9]+:(.+)$`)
	subjectPattern = regexp.MustCompile(`^repo:([A-Za-z0-9-]+)@[0-9]+/((?:[A-Za-z0-9_-]|\\\.)+)@[0-9]+:(.+)$`)

	literal     = regexp.MustCompile(`^` + literalFrag + `$`)
	alternation = regexp.MustCompile(`^\((` + literalFrag + `(?:\|` + literalFrag + `)+)\)$`)

	workflowPin = regexp.MustCompile(`^([A-Za-z0-9-]+)/((?:[A-Za-z0-9_-]|\\\.)+)/\\\.github/workflows/(?:[A-Za-z0-9_-]|\\\.)+\\\.ya?ml@`)

	writeContext = regexp.MustCompile(`^(?:ref:refs/heads/main|environment:[A-Za-z0-9_.-]+)$`)
)

// LintDir lints every *.sts.yaml in dir. A missing directory has no policies
// and so no findings.
func LintDir(dir string) ([]Finding, int, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}

	var findings []Finding
	count := 0
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(name, ".sts.yml") {
			findings = append(findings, Finding{path, "octo-sts only reads <identity>.sts.yaml; rename to .sts.yaml"})
			continue
		}
		if !strings.HasSuffix(name, ".sts.yaml") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, 0, err
		}
		count++
		for _, msg := range Lint(data) {
			findings = append(findings, Finding{path, msg})
		}
	}
	return findings, count, nil
}

// Lint returns every rule the policy in data breaks, or nil if it passes.
func Lint(data []byte) []string {
	var p Policy
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return []string{fmt.Sprintf("not a valid trust policy: %v", err)}
	}

	var msgs []string
	add := func(format string, a ...any) { msgs = append(msgs, fmt.Sprintf(format, a...)) }

	if p.IssuerPattern != "" {
		add("issuer_pattern is not allowed; set issuer: %s", GitHubActionsIssuer)
	}
	if p.Issuer != GitHubActionsIssuer {
		add("issuer must be %s", GitHubActionsIssuer)
	}

	owner, repo, contexts, subjectMsgs := checkSubject(p)
	msgs = append(msgs, subjectMsgs...)

	msgs = append(msgs, checkWorkflowPin(p, owner, repo)...)

	if len(p.Permissions) == 0 {
		add("permissions is empty; list what the token needs")
	}
	var writes []string
	for _, k := range sortedKeys(p.Permissions) {
		switch v := p.Permissions[k]; v {
		case "read":
		case "write", "admin":
			writes = append(writes, k+": "+v)
		default:
			add("permissions.%s: %q is not read, write or admin", k, v)
		}
	}
	if len(writes) > 0 && contexts != nil {
		for _, c := range contexts {
			if !writeContext.MatchString(c) {
				add("%s is granted to runs from %q; write permissions are only for ref:refs/heads/main or environment:<name>", strings.Join(writes, ", "), c)
			}
		}
	}

	return msgs
}

// checkSubject returns the caller's owner and repository names and the run
// contexts (the part of sub after the repository) the subject admits. contexts
// is nil when the subject is invalid.
func checkSubject(p Policy) (owner, repo string, contexts []string, msgs []string) {
	switch {
	case p.Subject != "" && p.SubjectPattern != "":
		return "", "", nil, []string{"set subject or subject_pattern, not both"}
	case p.Subject == "" && p.SubjectPattern == "":
		return "", "", nil, []string{"subject or subject_pattern is required"}
	}

	immutable := "must start with repo:OWNER@OWNER-ID/REPO@REPO-ID: (gh api repos/OWNER/REPO/actions/oidc/customization/sub prints the prefix)"

	if p.Subject != "" {
		m := subjectExact.FindStringSubmatch(p.Subject)
		if m == nil {
			return "", "", nil, []string{"subject " + immutable}
		}
		return m[1], m[2], []string{m[3]}, nil
	}

	m := subjectPattern.FindStringSubmatch(p.SubjectPattern)
	if m == nil {
		return "", "", nil, []string{"subject_pattern " + immutable + ", with the names as literals and dots escaped"}
	}
	owner, repo, rest := m[1], unescape(m[2]), m[3]

	switch {
	case literal.MatchString(rest):
		return owner, repo, []string{unescape(rest)}, nil
	case alternation.MatchString(rest):
		for _, c := range strings.Split(alternation.FindStringSubmatch(rest)[1], "|") {
			contexts = append(contexts, unescape(c))
		}
		return owner, repo, contexts, nil
	default:
		return owner, repo, nil, []string{fmt.Sprintf("subject_pattern context %q must be a literal, such as ref:refs/heads/main, or an alternation of literals, such as (pull_request|ref:refs/heads/main)", rest)}
	}
}

// checkWorkflowPin requires workflow_ref or job_workflow_ref to name one
// workflow file. workflow_ref must be in the caller's own repository;
// job_workflow_ref may name a reusable workflow elsewhere.
func checkWorkflowPin(p Policy, owner, repo string) []string {
	wf, hasWF := p.ClaimPattern["workflow_ref"]
	jwf, hasJWF := p.ClaimPattern["job_workflow_ref"]
	if !hasWF && !hasJWF {
		return []string{"claim_pattern must pin workflow_ref (or job_workflow_ref for a reusable workflow) to one workflow file"}
	}

	pinMsg := "must start with OWNER/REPO/\\.github/workflows/FILE\\.yaml@, names literal and dots escaped"
	var msgs []string
	if hasWF {
		m := workflowPin.FindStringSubmatch(wf)
		switch {
		case m == nil:
			msgs = append(msgs, "claim_pattern.workflow_ref "+pinMsg)
		case owner != "" && (m[1] != owner || unescape(m[2]) != repo):
			msgs = append(msgs, fmt.Sprintf("claim_pattern.workflow_ref names %s/%s but the subject is %s/%s", m[1], unescape(m[2]), owner, repo))
		}
	}
	if hasJWF && workflowPin.FindStringSubmatch(jwf) == nil {
		msgs = append(msgs, "claim_pattern.job_workflow_ref "+pinMsg)
	}
	return msgs
}

func unescape(s string) string { return strings.ReplaceAll(s, `\.`, ".") }

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
