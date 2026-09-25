package rollout

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	mustGit(t, "", "config", "--global", "user.name", "test")
	mustGit(t, "", "config", "--global", "user.email", "test@example.com")
	mustGit(t, "", "config", "--global", "init.defaultBranch", "main")
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

const valuesContent = "image:\n  repository: registry/acme/api\n  tag: \"1.0.0\"\n"

// initRemoteAndClone creates a bare remote whose main branch contains the
// given files, and returns the bare path plus a working clone.
func initRemoteAndClone(t *testing.T, files map[string]string) (bare, clone string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "remote.git")
	mustGit(t, "", "init", "--bare", bare)

	seed := filepath.Join(root, "seed")
	mustGit(t, "", "clone", bare, seed)
	for name, content := range files {
		path := filepath.Join(seed, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(t, seed, "add", ".")
	mustGit(t, seed, "commit", "-m", "initial")
	mustGit(t, seed, "push", "-u", "origin", "main")

	clone = filepath.Join(root, "clone")
	mustGit(t, "", "clone", bare, clone)
	return bare, clone
}

func TestRunDirect_Validation(t *testing.T) {
	cases := []struct {
		name string
		opts DirectOptions
	}{
		{"no files", DirectOptions{Value: "v"}},
		{"no value", DirectOptions{Files: []string{"a.yaml"}}},
		{"key mode without key", DirectOptions{Files: []string{"a.yaml"}, Value: "v", Mode: "key"}},
		{"pr without branch", DirectOptions{Files: []string{"a.yaml"}, Value: "v", Deploy: "pr", Token: "t"}},
		{"pr without token", DirectOptions{Files: []string{"a.yaml"}, Value: "v", Deploy: "pr", Branch: "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := RunDirect(tc.opts); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRunDirect_DryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(path, []byte(valuesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := RunDirect(DirectOptions{
		Files:   []string{"values.yaml"},
		Value:   "2.0.0",
		DryRun:  true,
		WorkDir: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.DiffSummary) != 1 {
		t.Fatalf("want 1 diff, got %d", len(result.DiffSummary))
	}
	d := result.DiffSummary[0]
	if d.OldValue != "1.0.0" || d.NewValue != "2.0.0" {
		t.Errorf("diff = %+v", d)
	}
	if result.EnvResults[0].Status != "dry-run" {
		t.Errorf("status = %q", result.EnvResults[0].Status)
	}
	data, _ := os.ReadFile(path)
	if string(data) != valuesContent {
		t.Error("dry-run must not modify files")
	}
}

func TestRunDirect_PreflightFails(t *testing.T) {
	setupHome(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain.yaml"), []byte("foo: bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := RunDirect(DirectOptions{
		Files:   []string{"plain.yaml"},
		Value:   "2.0.0",
		WorkDir: dir,
	})
	if err == nil || !strings.Contains(err.Error(), "pre-flight") {
		t.Fatalf("want pre-flight error, got: %v", err)
	}
}

func TestRunDirect_AutoDeployEndToEnd(t *testing.T) {
	setupHome(t)
	bare, clone := initRemoteAndClone(t, map[string]string{"values.yaml": valuesContent})
	t.Setenv("GITHUB_REF_NAME", "main")

	result, err := RunDirect(DirectOptions{
		Files:        []string{"values.yaml"},
		Value:        "2.0.0",
		Deploy:       "auto",
		WorkDir:      clone,
		GitUserName:  "bot",
		GitUserEmail: "bot@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Deployed {
		t.Error("expected Deployed=true")
	}
	if len(result.CommitSHA) != 40 {
		t.Errorf("commit sha = %q", result.CommitSHA)
	}

	data, _ := os.ReadFile(filepath.Join(clone, "values.yaml"))
	if !strings.Contains(string(data), `tag: "2.0.0"`) {
		t.Errorf("values.yaml not updated (quoting preserved):\n%s", data)
	}
	if got := mustGit(t, bare, "rev-parse", "main"); got != result.CommitSHA {
		t.Errorf("remote sha %q != result sha %q — commit was not pushed", got, result.CommitSHA)
	}
}

func TestRunDirect_AutoPushBranch(t *testing.T) {
	cases := []struct {
		name    string
		refType string
		refName string
		detach  bool
		wantErr string
	}{
		{name: "branch checked out on tag run", refType: "tag", refName: "v1.0.0"},
		{name: "branch run", refType: "branch", refName: "main"},
		{name: "detached HEAD on tag run", refType: "tag", refName: "v1.0.0", detach: true, wantErr: "HEAD is detached"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupHome(t)
			bare, clone := initRemoteAndClone(t, map[string]string{"values.yaml": valuesContent})
			if tc.detach {
				mustGit(t, clone, "checkout", "--detach")
			}
			t.Setenv("GITHUB_REF_TYPE", tc.refType)
			t.Setenv("GITHUB_REF_NAME", tc.refName)
			before := mustGit(t, bare, "rev-parse", "main")

			result, err := RunDirect(DirectOptions{
				Files:        []string{"values.yaml"},
				Value:        "2.0.0",
				WorkDir:      clone,
				GitUserName:  "bot",
				GitUserEmail: "bot@example.com",
			})

			if branches := mustGit(t, bare, "branch", "--list", tc.refName); tc.refType == "tag" && branches != "" {
				t.Errorf("pushed a branch named after the tag: %q", branches)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got: %v", tc.wantErr, err)
				}
				if got := mustGit(t, bare, "rev-parse", "main"); got != before {
					t.Error("remote main moved despite the error")
				}
				data, _ := os.ReadFile(filepath.Join(clone, "values.yaml"))
				if string(data) != valuesContent {
					t.Error("values.yaml modified despite the error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := mustGit(t, bare, "rev-parse", "main"); got != result.CommitSHA {
				t.Errorf("remote main %q != result sha %q", got, result.CommitSHA)
			}
		})
	}
}

func TestRunDirect_PRDeployEndToEnd(t *testing.T) {
	setupHome(t)
	bare, clone := initRemoteAndClone(t, map[string]string{"values.yaml": valuesContent})

	var prCreated, autoMergeEnabled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/o/r/pulls":
			_, _ = w.Write([]byte("[]"))
		case r.Method == "POST" && r.URL.Path == "/repos/o/r/pulls":
			prCreated = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 42, "html_url": "http://pr/42", "state": "open", "node_id": "n42",
			})
		case strings.Contains(r.URL.Path, "labels"):
			_, _ = w.Write([]byte("[]"))
		case r.URL.Path == "/graphql":
			autoMergeEnabled = true
			_, _ = w.Write([]byte(`{"data": {}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	result, err := RunDirect(DirectOptions{
		Files:         []string{"values.yaml"},
		Value:         "3.0.0",
		Deploy:        "pr",
		Branch:        "deploy/values",
		AutoMerge:     true,
		Token:         "tok",
		Owner:         "o",
		Repo:          "r",
		WorkDir:       clone,
		GitUserName:   "bot",
		GitUserEmail:  "bot@example.com",
		GitHubBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !prCreated {
		t.Error("expected PR to be created")
	}
	if !autoMergeEnabled {
		t.Error("expected auto-merge GraphQL call")
	}
	if len(result.PRURLs) != 1 || result.PRURLs[0] != "http://pr/42" {
		t.Errorf("pr urls = %v", result.PRURLs)
	}
	if got := mustGit(t, bare, "ls-tree", "--name-only", "deploy/values"); !strings.Contains(got, "values.yaml") {
		t.Errorf("deploy branch not pushed to remote, tree: %s", got)
	}
}

const matrixConfig = `
global:
  aws_region: eu-central-1
environment:
  dev:
    deploy: auto
    tag: version
  prod:
    deploy: pr
    tag: sha
service:
  api: {}
`

func writeMatrixFixture(t *testing.T, root string) (configPath, chartsDir string) {
	t.Helper()
	configPath = filepath.Join(root, "matrix.config.yaml")
	if err := os.WriteFile(configPath, []byte(matrixConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	chartsDir = filepath.Join(root, "charts")
	for _, env := range []string{"dev", "prod"} {
		dir := filepath.Join(chartsDir, "api", "envs", env)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte(valuesContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return configPath, chartsDir
}

func TestRun_Validation(t *testing.T) {
	root := t.TempDir()
	configPath, chartsDir := writeMatrixFixture(t, root)

	base := Options{ConfigPath: configPath, ChartsDir: chartsDir, WorkDir: root}

	t.Run("missing service", func(t *testing.T) {
		opts := base
		opts.Version = "1.2.3"
		if _, err := Run(opts); err == nil || !strings.Contains(err.Error(), "service is required") {
			t.Fatalf("got: %v", err)
		}
	})
	t.Run("missing version", func(t *testing.T) {
		opts := base
		opts.Service = "api"
		if _, err := Run(opts); err == nil || !strings.Contains(err.Error(), "version is required") {
			t.Fatalf("got: %v", err)
		}
	})
	t.Run("missing sha for tag:sha env", func(t *testing.T) {
		opts := base
		opts.Service, opts.Version, opts.Token = "api", "1.2.3", "tok"
		if _, err := Run(opts); err == nil || !strings.Contains(err.Error(), "tag: sha") {
			t.Fatalf("got: %v", err)
		}
	})
	t.Run("missing token for pr env", func(t *testing.T) {
		opts := base
		opts.Service, opts.Version, opts.SHA = "api", "1.2.3", "abc1234"
		if _, err := Run(opts); err == nil || !strings.Contains(err.Error(), "token is required") {
			t.Fatalf("got: %v", err)
		}
	})
}

func TestRun_MatrixDryRun(t *testing.T) {
	root := t.TempDir()
	configPath, chartsDir := writeMatrixFixture(t, root)

	result, err := Run(Options{
		Service:    "api",
		Version:    "1.2.3",
		SHA:        "abc1234",
		ConfigPath: configPath,
		ChartsDir:  chartsDir,
		WorkDir:    root,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.EnvResults) != 2 {
		t.Fatalf("want 2 env results, got %d", len(result.EnvResults))
	}
	byName := map[string]EnvResult{}
	for _, e := range result.EnvResults {
		if e.Status != "dry-run" {
			t.Errorf("env %s status = %q", e.Name, e.Status)
		}
		byName[e.Name] = e
	}
	if byName["dev"].Tag != "1.2.3" {
		t.Errorf("dev tag = %q, want version", byName["dev"].Tag)
	}
	if byName["prod"].Tag != "abc1234" {
		t.Errorf("prod tag = %q, want sha", byName["prod"].Tag)
	}

	data, _ := os.ReadFile(filepath.Join(chartsDir, "api", "envs", "dev", "values.yaml"))
	if string(data) != valuesContent {
		t.Error("dry-run must not modify values files")
	}
}

func TestRun_MatrixAutoDeployEndToEnd(t *testing.T) {
	setupHome(t)
	devValues := "charts/api/envs/dev/values.yaml"
	bare, clone := initRemoteAndClone(t, map[string]string{devValues: valuesContent})
	t.Setenv("GITHUB_REF_NAME", "main")

	cfg := `
environment:
  dev:
    deploy: auto
    tag: version
service:
  api: {}
`
	configPath := filepath.Join(clone, "matrix.config.yaml")
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Run(Options{
		Service:      "api",
		Version:      "1.2.3",
		ConfigPath:   configPath,
		ChartsDir:    filepath.Join(clone, "charts"),
		WorkDir:      clone,
		GitUserName:  "bot",
		GitUserEmail: "bot@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Deployed {
		t.Error("expected Deployed=true")
	}
	if got := mustGit(t, bare, "log", "-1", "--format=%s", "main"); got != "deploy(api/dev): 1.2.3" {
		t.Errorf("commit message = %q", got)
	}
	data, _ := os.ReadFile(filepath.Join(clone, devValues))
	if !strings.Contains(string(data), `tag: "1.2.3"`) {
		t.Errorf("values not updated:\n%s", data)
	}
}

func TestRun_MatrixAutoDeployTagRun(t *testing.T) {
	cfg := `
environment:
  dev:
    deploy: auto
    tag: version
service:
  api: {}
`
	devValues := "charts/api/envs/dev/values.yaml"
	for _, detach := range []bool{false, true} {
		name := "branch checked out"
		if detach {
			name = "detached HEAD"
		}
		t.Run(name, func(t *testing.T) {
			setupHome(t)
			bare, clone := initRemoteAndClone(t, map[string]string{devValues: valuesContent})
			if detach {
				mustGit(t, clone, "checkout", "--detach")
			}
			t.Setenv("GITHUB_REF_TYPE", "tag")
			t.Setenv("GITHUB_REF_NAME", "v1.2.3")
			configPath := filepath.Join(clone, "matrix.config.yaml")
			if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := Run(Options{
				Service:      "api",
				Version:      "1.2.3",
				ConfigPath:   configPath,
				ChartsDir:    filepath.Join(clone, "charts"),
				WorkDir:      clone,
				GitUserName:  "bot",
				GitUserEmail: "bot@example.com",
			})

			if branches := mustGit(t, bare, "branch", "--list", "v1.2.3"); branches != "" {
				t.Errorf("pushed a branch named after the tag: %q", branches)
			}
			if detach {
				if err == nil || !strings.Contains(err.Error(), "HEAD is detached") {
					t.Fatalf("want detached HEAD error, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := mustGit(t, bare, "log", "-1", "--format=%s", "main"); got != "deploy(api/dev): 1.2.3" {
				t.Errorf("commit message on main = %q", got)
			}
		})
	}
}

func TestValuesPath_EscapeGuard(t *testing.T) {
	charts := t.TempDir()
	opts := Options{ChartsDir: charts, Service: "../evil"}
	if _, err := valuesPath(opts, "dev"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}

	opts.Service = "api"
	p, err := valuesPath(opts, "dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join("api", "envs", "dev", "values.yaml")) {
		t.Errorf("path = %q", p)
	}
}

func TestResolveTag(t *testing.T) {
	opts := Options{Version: "1.2.3", SHA: "abc"}
	if got := resolveTag(Environment{Tag: "sha"}, opts); got != "abc" {
		t.Errorf("sha tag = %q", got)
	}
	if got := resolveTag(Environment{Tag: "version"}, opts); got != "1.2.3" {
		t.Errorf("version tag = %q", got)
	}
	if got := resolveTag(Environment{}, opts); got != "1.2.3" {
		t.Errorf("default tag = %q", got)
	}
}

func TestWriteOutputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	t.Setenv("GITHUB_OUTPUT", path)

	result := &Result{
		Deployed:     true,
		Environments: []string{"dev"},
		CommitSHA:    "abc123",
		PRURLs:       []string{"http://pr/1"},
		DiffSummary:  []FileDiff{{File: "a.yaml", OldValue: "", NewValue: "2.0.0"}},
	}
	if err := WriteOutputs(result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		"deployed=true",
		`environments=["dev"]`,
		"commit_sha=abc123",
		`pr_urls=["http://pr/1"]`,
		"changed_files<<__EOF__\na.yaml\n__EOF__",
		"a.yaml: (none) → 2.0.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestWriteOutputs_NoEnvIsNoop(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	if err := WriteOutputs(&Result{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := WriteOutputs(nil); err != nil {
		t.Fatalf("nil result: %v", err)
	}
}

func TestWriteStepSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary")
	t.Setenv("GITHUB_STEP_SUMMARY", path)

	result := &Result{
		CommitSHA: "abc123",
		EnvResults: []EnvResult{
			{Name: "dev", Tag: "1.2.3", Status: "deployed"},
			{Name: "prod", Tag: "1.2.3", Status: "pr-opened", PRURL: "http://pr/1"},
		},
	}
	if err := WriteStepSummary(result, "api", "1.2.3"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(path)
	out := string(data)
	for _, want := range []string{
		"## Deploy Summary: api @ 1.2.3",
		"| dev | `1.2.3` | deployed | — |",
		"| prod | `1.2.3` | pr-opened | http://pr/1 |",
		"Commit: `abc123`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}

func TestDirectCommitMessage(t *testing.T) {
	prod := "deploy/charts/console/envs/prod/values.yaml"
	dev := "deploy/charts/console/envs/dev/values.yaml"
	tests := []struct {
		name    string
		opts    DirectOptions
		files   []string
		oldTags map[string]string
		want    string
	}{
		{"env layout with old value", DirectOptions{Value: "0.19.0"}, []string{prod}, map[string]string{prod: "0.18.1"}, "deploy(console/prod): 0.18.1 → 0.19.0"},
		{"no old value", DirectOptions{Value: "0.19.0"}, []string{prod}, nil, "deploy(console/prod): 0.19.0"},
		{"unchanged value", DirectOptions{Value: "0.19.0"}, []string{prod}, map[string]string{prod: "0.19.0"}, "deploy(console/prod): 0.19.0"},
		{"multiple files", DirectOptions{Value: "0.19.0"}, []string{dev, prod}, map[string]string{dev: "0.18.1", prod: "0.18.1"}, "deploy(console/dev, console/prod): 0.19.0"},
		{"other layout", DirectOptions{Value: "v2"}, []string{"k8s/api/values.yaml"}, nil, "deploy(k8s/api): v2"},
		{"repo root", DirectOptions{Value: "v2"}, []string{"values.yaml"}, nil, "deploy(values.yaml): v2"},
		{"override", DirectOptions{Value: "v2", CommitMessage: "chore: bump"}, []string{prod}, nil, "chore: bump"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := directCommitMessage(tt.opts, tt.files, tt.oldTags); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// pushConcurrentTag advances the remote's values.yaml to tag from a second
// clone, as a rollout that pushed first would.
func pushConcurrentTag(t *testing.T, bare, tag string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	mustGit(t, "", "clone", bare, other)
	content := strings.Replace(valuesContent, `"1.0.0"`, `"`+tag+`"`, 1)
	if err := os.WriteFile(filepath.Join(other, "values.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, other, "commit", "-am", "deploy: "+tag)
	mustGit(t, other, "push", "origin", "main")
}

func TestRunDirect_AutoRetryReappliesAfterConcurrentDeploy(t *testing.T) {
	setupHome(t)
	bare, clone := initRemoteAndClone(t, map[string]string{"values.yaml": valuesContent})
	t.Setenv("GITHUB_REF_NAME", "main")
	pushConcurrentTag(t, bare, "1.1.0")

	result, err := RunDirect(DirectOptions{
		Files:        []string{"values.yaml"},
		Value:        "2.0.0",
		WorkDir:      clone,
		GitUserName:  "bot",
		GitUserEmail: "bot@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Deployed || result.EnvResults[0].Status != "deployed" {
		t.Errorf("result = %+v, want deployed", result)
	}
	if got := mustGit(t, bare, "show", "main:values.yaml"); !strings.Contains(got, `tag: "2.0.0"`) {
		t.Errorf("remote values.yaml not updated:\n%s", got)
	}
	if got := mustGit(t, bare, "rev-parse", "main"); got != result.CommitSHA {
		t.Errorf("remote main %q != result sha %q", got, result.CommitSHA)
	}
}

func TestRunDirect_AutoRetrySkipsWhenRemoteIsNewer(t *testing.T) {
	setupHome(t)
	bare, clone := initRemoteAndClone(t, map[string]string{"values.yaml": valuesContent})
	t.Setenv("GITHUB_REF_NAME", "main")
	pushConcurrentTag(t, bare, "3.0.0")
	remoteBefore := mustGit(t, bare, "rev-parse", "main")

	result, err := RunDirect(DirectOptions{
		Files:        []string{"values.yaml"},
		Value:        "2.0.0",
		WorkDir:      clone,
		GitUserName:  "bot",
		GitUserEmail: "bot@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Deployed || result.EnvResults[0].Status != "skipped" {
		t.Errorf("result = %+v, want skipped and not deployed", result)
	}
	if got := mustGit(t, bare, "rev-parse", "main"); got != remoteBefore {
		t.Error("remote main moved; an older version must not be pushed over a newer one")
	}
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		current, tag string
		want         bool
	}{
		{"0.10.0", "0.9.2", true},
		{"0.9.2", "0.10.0", false},
		{"1.0.0", "1.0.0", false},
		{"v2.0.0", "v1.9.9", true},
		{"abc1234", "1.0.0", false},
		{"1.0.0", "abc1234", false},
		{"1.0.0-rc.1", "0.9.0", false},
		{"", "1.0.0", false},
	}
	for _, tc := range cases {
		if got := newerVersion(tc.current, tc.tag); got != tc.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", tc.current, tc.tag, got, tc.want)
		}
	}
}
