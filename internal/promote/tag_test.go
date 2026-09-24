package promote

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dnd-it/tamci/internal/gh"
	"github.com/dnd-it/tamci/internal/git"
)

func TestNextVersion(t *testing.T) {
	tests := []struct {
		name     string
		last     string
		messages []string
		want     string
	}{
		{"first release with a feat", "", []string{"feat(shared): add bucket"}, "0.1.0"},
		{"first release with only fixes", "", []string{"fix: typo"}, "0.0.1"},
		{"feat bumps minor", "1.2.3", []string{"fix: a", "feat: b"}, "1.3.0"},
		{"scoped feat bumps minor", "1.2.3", []string{"feat(infra): b"}, "1.3.0"},
		{"fix bumps patch", "1.2.3", []string{"fix(infra): a", "chore: b"}, "1.2.4"},
		{"non-conventional bumps patch", "1.2.3", []string{"Merge branch main"}, "1.2.4"},
		{"bang bumps major", "1.2.3", []string{"feat: a", "refactor(db)!: drop table"}, "2.0.0"},
		{"breaking footer bumps major", "1.2.3", []string{"fix: a\n\nBREAKING CHANGE: renamed output"}, "2.0.0"},
		{"feat mentioned outside the type does not bump minor", "1.2.3", []string{"fix: revert feat: x"}, "1.2.4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextVersion(tt.last, tt.messages)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("NextVersion(%q) = %s, want %s", tt.last, got, tt.want)
			}
		})
	}
}

func TestNextVersion_RejectsNonSemver(t *testing.T) {
	if _, err := NextVersion("2026.09", nil); err == nil {
		t.Fatal("want error for a non-semver version")
	}
}

func TestInferKind(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"stacks/shared", "deploy/charts/console/envs/prod", "deploy/charts/worker/envs/dev"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "deploy/charts/console/envs/prod/values.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		target  string
		want    string
		wantErr bool
	}{
		{"shared", KindInfra, false},
		{"console", KindApp, false},
		{"worker", "", true},
		{"missing", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got, err := InferKind(root, tt.target, "stacks", "deploy/charts")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCurrentProdTag(t *testing.T) {
	values := []byte(`global:
  awsRegion: eu-central-1
sidecar:
  image:
    repository: ghcr/dnd-it/otel
    tag: 9.9.9
web:
  image:
    registry: 123.dkr.ecr.eu-central-1.amazonaws.com
    repository: ghcr/dnd-it/console
    tag: 0.26.0
    pullPolicy: IfNotPresent
`)
	tests := []struct{ service, want string }{
		{"console", "0.26.0"},
		{"otel", "9.9.9"},
		{"sole", ""},
		{"platform-bot", ""},
	}
	for _, tt := range tests {
		got, err := CurrentProdTag(values, tt.service)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("CurrentProdTag(%s) = %q, want %q", tt.service, got, tt.want)
		}
	}
}

func TestPushPaths(t *testing.T) {
	wf := []byte(`name: shared
on:
  push:
    branches: [main]
    paths:
      - stacks/shared/**
      - .github/workflows/shared.yaml
  pull_request:
    paths: [ignored/**]
`)
	got, err := PushPaths(wf)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"stacks/shared/**", ".github/workflows/shared.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRepoFromRemote(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:DND-IT/tamedia-ai-platform.git":     "DND-IT/tamedia-ai-platform",
		"https://github.com/DND-IT/tamedia-ai-platform.git": "DND-IT/tamedia-ai-platform",
		"https://github.com/DND-IT/tamedia-ai-platform":     "DND-IT/tamedia-ai-platform",
		"ssh://git@github.com/DND-IT/tamci.git":             "DND-IT/tamci",
	} {
		got, err := RepoFromRemote(remote)
		if err != nil || got != want {
			t.Errorf("RepoFromRemote(%s) = %q, %v; want %q", remote, got, err, want)
		}
	}
}

func TestPrepare_App(t *testing.T) {
	dir, _, _ := repoWithOrigin(t)
	gitCmd(t, dir, "checkout", "-q", "main")
	values := "web:\n  image:\n    repository: ghcr/dnd-it/console\n    tag: 0.1.0\n"
	commitFile(t, dir, "deploy/charts/console/envs/prod/values.yaml", values)
	gitCmd(t, dir, "push", "-q", "origin", "main")
	gitCmd(t, dir, "fetch", "-q", "origin")
	release := commitFile(t, dir, "src", "feat(console): x")
	gitCmd(t, dir, "tag", "console/v0.2.0")
	gitCmd(t, dir, "tag", "console/v0.10.0", "HEAD~1")

	g := &git.Client{Dir: dir}
	opts := Options{Target: "console", Kind: KindApp, DefaultBranch: "main", ChartsDir: "deploy/charts"}

	var out bytes.Buffer
	plan, err := Prepare(g, nil, opts, &out)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tag != "prod/app/console/v0.10.0" || plan.Version != "0.10.0" {
		t.Errorf("plan = %+v, want the highest release tag", plan)
	}
	if want := "prod runs console 0.1.0; promoting 0.10.0\n"; out.String() != want {
		t.Errorf("out = %q, want %q", out.String(), want)
	}

	out.Reset()
	opts.Version = "0.2.0"
	plan, err = Prepare(g, nil, opts, &out)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Commit != release {
		t.Errorf("commit = %s, want %s", plan.Commit, release)
	}

	gitCmd(t, dir, "tag", "prod/app/console/v0.2.0", release)
	if _, err := Prepare(g, nil, opts, &out); err == nil {
		t.Error("want error when the prod tag exists")
	}
	opts.Version = "9.9.9"
	if _, err := Prepare(g, nil, opts, &out); err == nil {
		t.Error("want error when the release tag does not exist")
	}
}

type fakeRuns []gh.WorkflowRun

func (f fakeRuns) WorkflowRuns(_, _, _ string) ([]gh.WorkflowRun, error) { return f, nil }

func TestPrepare_Infra(t *testing.T) {
	dir, _, _ := repoWithOrigin(t)
	gitCmd(t, dir, "checkout", "-q", "main")
	commitFile(t, dir, ".github/workflows/shared.yaml", "on:\n  push:\n    paths:\n      - stacks/shared/**\n")
	first := commitFile(t, dir, "stacks/shared/main.tf", "fix(infra): first")
	gitCmd(t, dir, "tag", "-a", "prod/infra/shared/v0.1.0", "-m", "x")
	commitFile(t, dir, "elsewhere", "feat!: not under the stack")
	applied := commitFile(t, dir, "stacks/shared/vars.tf", "feat(infra): second")
	commitFile(t, dir, "stacks/shared/later.tf", "feat!: not applied to dev yet")

	g := &git.Client{Dir: dir}
	opts := Options{Target: "shared", Kind: KindInfra, DefaultBranch: "main"}
	runs := fakeRuns{{HeadSHA: "pr", Event: "pull_request"}, {HeadSHA: applied, Event: "push"}, {HeadSHA: first, Event: "push"}}

	var out bytes.Buffer
	plan, err := Prepare(g, runs, opts, &out)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tag != "prod/infra/shared/v0.2.0" || plan.Commit != applied {
		t.Errorf("plan = %+v, want v0.2.0 at %s", plan, applied)
	}
	if want := "Commits since prod/infra/shared/v0.1.0 that dev already has:\n  feat(infra): second\n"; out.String() != want {
		t.Errorf("out = %q, want %q", out.String(), want)
	}

	runs = fakeRuns{{HeadSHA: first, Event: "push"}}
	if _, err := Prepare(g, runs, opts, &out); err == nil {
		t.Error("want error when nothing changed since the last prod tag")
	}
	opts.Version = "0.1.1"
	if plan, err = Prepare(g, runs, opts, &out); err != nil || plan.Tag != "prod/infra/shared/v0.1.1" {
		t.Errorf("--version with no changes: plan = %+v, err = %v", plan, err)
	}

	if _, err := Prepare(g, fakeRuns{{HeadSHA: "pr", Event: "pull_request"}}, opts, &out); err == nil {
		t.Error("want error without a successful push or dispatch run")
	}
}
