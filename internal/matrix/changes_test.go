package matrix

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func setupChangesRepo(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	dir := t.TempDir()
	gitRun(t, "", "init", "-b", "main", dir)
	gitRun(t, dir, "config", "user.name", "test")
	gitRun(t, dir, "config", "user.email", "test@example.com")

	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")

	if err := os.MkdirAll(filepath.Join(dir, "services", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "services", "api", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add api")

	t.Setenv("GITHUB_WORKSPACE", dir)
	return dir
}

func TestDetectChangedFiles_Push(t *testing.T) {
	setupChangesRepo(t)
	t.Setenv("GITHUB_EVENT_NAME", "push")

	files, err := DetectChangedFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || files[0] != "services/api/main.go" {
		t.Errorf("files = %v", files)
	}
}

func TestDetectChangedFiles_WorkflowDispatchReturnsNil(t *testing.T) {
	setupChangesRepo(t)
	t.Setenv("GITHUB_EVENT_NAME", "workflow_dispatch")

	files, err := DetectChangedFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if files != nil {
		t.Errorf("want nil (no filtering), got %v", files)
	}
}

func TestDetectChangedFiles_PullRequestWithoutBaseRef(t *testing.T) {
	setupChangesRepo(t)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_BASE_REF", "")

	if _, err := DetectChangedFiles(); err == nil {
		t.Fatal("expected error when GITHUB_BASE_REF is unset")
	}
}

func TestDetectChangedFiles_PullRequest(t *testing.T) {
	dir := setupChangesRepo(t)
	// Simulate origin/main pointing at the first commit.
	gitRun(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD~1")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_BASE_REF", "main")

	files, err := DetectChangedFiles()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "main.go") {
		t.Errorf("files = %v", files)
	}
}
