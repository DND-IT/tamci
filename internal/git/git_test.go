package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigure_SetsIdentityAndRemote(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)
	t.Chdir(clone)

	err := Configure("bot", "bot@example.com", "secret-token", "acme/widgets", "https://github.example.com")
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if got := mustGit(t, clone, "config", "user.name"); got != "bot" {
		t.Errorf("user.name = %q", got)
	}
	remote := mustGit(t, clone, "remote", "get-url", "origin")
	want := "https://x-access-token:secret-token@github.example.com/acme/widgets.git"
	if remote != want {
		t.Errorf("remote = %q, want %q", remote, want)
	}
}

func TestConfigure_NoTokenKeepsRemote(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)
	t.Chdir(clone)

	before := mustGit(t, clone, "remote", "get-url", "origin")
	if err := Configure("bot", "bot@example.com", "", "", "https://github.com"); err != nil {
		t.Fatalf("configure: %v", err)
	}
	after := mustGit(t, clone, "remote", "get-url", "origin")
	if before != after {
		t.Errorf("remote changed without token: %q -> %q", before, after)
	}
}

func TestGetDefaultBranch(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)
	t.Chdir(clone)

	if got := GetDefaultBranch(); got != "main" {
		t.Errorf("default branch = %q, want main", got)
	}
}

func TestGetDefaultBranch_FallsBackToMain(t *testing.T) {
	setupHome(t)
	dir := t.TempDir()
	mustGit(t, "", "init", dir)
	t.Chdir(dir)

	if got := GetDefaultBranch(); got != "main" {
		t.Errorf("default branch = %q, want fallback main", got)
	}
}

func TestCreateBranch(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)
	t.Chdir(clone)

	if err := CreateBranch("yaml-update/abc", "main"); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if got := mustGit(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); got != "yaml-update/abc" {
		t.Errorf("current branch = %q", got)
	}

	// Recreating the same branch must not fail (it is deleted first).
	mustGit(t, clone, "checkout", "main")
	if err := CreateBranch("yaml-update/abc", "main"); err != nil {
		t.Fatalf("recreate branch: %v", err)
	}
}

func TestCommitAndPush(t *testing.T) {
	setupHome(t)
	bare, clone := initRemoteAndClone(t)
	t.Chdir(clone)

	if err := Configure("bot", "bot@example.com", "", "", "https://github.com"); err != nil {
		t.Fatal(err)
	}
	if err := CreateBranch("update/1", "main"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(clone, "values.yaml")
	if err := os.WriteFile(path, []byte("tag: v9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sha, err := CommitAndPush([]string{"values.yaml"}, "chore: update values", "update/1")
	if err != nil {
		t.Fatalf("commit and push: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("sha = %q, want full sha", sha)
	}
	if got := mustGit(t, bare, "log", "-1", "--format=%s", "update/1"); got != "chore: update values" {
		t.Errorf("remote message = %q", got)
	}
	if got := mustGit(t, bare, "rev-parse", "update/1"); got != sha {
		t.Errorf("remote sha %q != returned sha %q", got, sha)
	}
}

func TestCommitAndPush_NothingToCommit(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)
	t.Chdir(clone)

	if _, err := CommitAndPush([]string{"README.md"}, "no-op", "main"); err == nil {
		t.Fatal("expected error when there is nothing to commit")
	}
}

func TestConfigure_SafeDirectoryContained(t *testing.T) {
	// Guard: the --global safe.directory write must land in the test HOME,
	// not the developer's real config.
	setupHome(t)
	_, clone := initRemoteAndClone(t)
	t.Chdir(clone)

	if err := Configure("bot", "bot@example.com", "", "", "https://github.com"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".gitconfig"))
	if err != nil {
		t.Fatalf("read temp gitconfig: %v", err)
	}
	if !strings.Contains(string(data), "safe") {
		t.Errorf("expected safe.directory entry in temp HOME gitconfig, got:\n%s", data)
	}
}
