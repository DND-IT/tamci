package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupHome points HOME at a temp dir so --global git config writes are
// contained, and seeds the identity/init defaults tests need.
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

// initRemoteAndClone creates a bare remote with one commit on main and
// returns the bare path plus a working clone.
func initRemoteAndClone(t *testing.T) (bare, clone string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "remote.git")
	mustGit(t, "", "init", "--bare", bare)

	seed := filepath.Join(root, "seed")
	mustGit(t, "", "clone", bare, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, seed, "add", "README.md")
	mustGit(t, seed, "commit", "-m", "initial")
	mustGit(t, seed, "push", "-u", "origin", "main")

	clone = filepath.Join(root, "clone")
	mustGit(t, "", "clone", bare, clone)
	return bare, clone
}

func TestClient_ConfigureSetsIdentity(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)

	c := &Client{Dir: clone, UserName: "deploy-bot", UserEmail: "bot@example.com"}
	if err := c.Configure(); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if got := mustGit(t, clone, "config", "user.name"); got != "deploy-bot" {
		t.Errorf("user.name = %q", got)
	}
	if got := mustGit(t, clone, "config", "user.email"); got != "bot@example.com" {
		t.Errorf("user.email = %q", got)
	}
}

func TestClient_AddCommitPush(t *testing.T) {
	setupHome(t)
	bare, clone := initRemoteAndClone(t)

	if err := os.WriteFile(filepath.Join(clone, "values.yaml"), []byte("tag: v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Client{Dir: clone, UserName: "bot", UserEmail: "bot@example.com"}
	if err := c.Configure(); err != nil {
		t.Fatal(err)
	}
	if err := c.Add("values.yaml"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := c.Commit("deploy: v2"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := c.Push("main", 1, nil); err != nil {
		t.Fatalf("push: %v", err)
	}

	remoteMsg := mustGit(t, bare, "log", "-1", "--format=%s", "main")
	if remoteMsg != "deploy: v2" {
		t.Errorf("remote head message = %q", remoteMsg)
	}
}

func TestClient_CommitNothingStagedIsNoop(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)

	c := &Client{Dir: clone, UserName: "bot", UserEmail: "bot@example.com"}
	if err := c.Configure(); err != nil {
		t.Fatal(err)
	}
	before := mustGit(t, clone, "rev-parse", "HEAD")
	if err := c.Commit("empty"); err != nil {
		t.Fatalf("commit with nothing staged should be a no-op, got: %v", err)
	}
	after := mustGit(t, clone, "rev-parse", "HEAD")
	if before != after {
		t.Error("no-op commit created a new commit")
	}
}

func TestClient_PushReappliesOntoRemoteAdvance(t *testing.T) {
	setupHome(t)
	bare, clone := initRemoteAndClone(t)

	// A concurrent run edits the same line first, so rebasing this run's
	// commit would conflict.
	other := filepath.Join(t.TempDir(), "other")
	mustGit(t, "", "clone", bare, other)
	if err := os.WriteFile(filepath.Join(other, "README.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, other, "commit", "-am", "concurrent")
	mustGit(t, other, "push", "origin", "main")

	c := &Client{Dir: clone, UserName: "bot", UserEmail: "bot@example.com"}
	if err := c.Configure(); err != nil {
		t.Fatal(err)
	}
	var sawTheirs bool
	apply := func() error {
		data, err := os.ReadFile(filepath.Join(clone, "README.md"))
		if err != nil {
			return err
		}
		sawTheirs = sawTheirs || string(data) == "theirs\n"
		if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("mine\n"), 0o644); err != nil {
			return err
		}
		if err := c.Add("README.md"); err != nil {
			return err
		}
		return c.Commit("mine")
	}
	if err := apply(); err != nil {
		t.Fatal(err)
	}

	if err := c.Push("main", 2, apply); err != nil {
		t.Fatalf("push should succeed after reapplying onto origin/main, got: %v", err)
	}
	if !sawTheirs {
		t.Error("reapply did not run on the fresh origin/main tree")
	}
	if got := mustGit(t, bare, "show", "main:README.md"); got != "mine" {
		t.Errorf("remote README.md = %q, want mine", got)
	}
	if got := mustGit(t, bare, "log", "-2", "--format=%s", "main"); got != "mine\nconcurrent" {
		t.Errorf("remote history = %q, want mine on top of concurrent", got)
	}
}

func TestClient_PushFailsAfterMaxAttempts(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)

	c := &Client{Dir: clone, UserName: "bot", UserEmail: "bot@example.com"}
	if err := c.Push("no-such-branch", 1, nil); err == nil {
		t.Fatal("expected error pushing a branch that does not exist")
	}
}

func TestClient_CheckoutBranchAndForcePush(t *testing.T) {
	setupHome(t)
	bare, clone := initRemoteAndClone(t)

	c := &Client{Dir: clone, UserName: "bot", UserEmail: "bot@example.com"}
	if err := c.Configure(); err != nil {
		t.Fatal(err)
	}
	if err := c.CheckoutBranch("deploy/api/prod", "main"); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if got := mustGit(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); got != "deploy/api/prod" {
		t.Errorf("current branch = %q", got)
	}

	if err := os.WriteFile(filepath.Join(clone, "v.yaml"), []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Add("v.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := c.Commit("deploy"); err != nil {
		t.Fatal(err)
	}
	if err := c.ForcePush("deploy/api/prod"); err != nil {
		t.Fatalf("force push: %v", err)
	}
	if got := mustGit(t, bare, "log", "-1", "--format=%s", "deploy/api/prod"); got != "deploy" {
		t.Errorf("remote branch head = %q", got)
	}

	// A second checkout resets the branch back onto origin/main.
	if err := c.CheckoutBranch("deploy/api/prod", "main"); err != nil {
		t.Fatalf("re-checkout: %v", err)
	}
	local := mustGit(t, clone, "rev-parse", "HEAD")
	originMain := mustGit(t, clone, "rev-parse", "origin/main")
	if local != originMain {
		t.Error("checkout -B did not reset branch to origin/main")
	}
}

func TestClient_RevParse(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)

	c := &Client{Dir: clone}
	sha, err := c.RevParse("HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("sha = %q, want full 40-char sha", sha)
	}
	if _, err := c.RevParse("no-such-ref"); err == nil {
		t.Error("expected error for unknown ref")
	}
}

func TestClient_DefaultBranch(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)

	c := &Client{Dir: clone}
	// Fresh clones have origin/HEAD set by git clone.
	if got := c.DefaultBranch(); got != "main" {
		t.Errorf("default branch = %q, want main", got)
	}

	// Without a resolvable origin/HEAD it returns "".
	noRemote := t.TempDir()
	mustGit(t, "", "init", noRemote)
	c2 := &Client{Dir: noRemote}
	if got := c2.DefaultBranch(); got != "" {
		t.Errorf("default branch = %q, want empty", got)
	}
}

func TestClient_CurrentBranch(t *testing.T) {
	setupHome(t)
	_, clone := initRemoteAndClone(t)

	c := &Client{Dir: clone}
	if got := c.CurrentBranch(); got != "main" {
		t.Errorf("current branch = %q, want main", got)
	}

	mustGit(t, clone, "checkout", "--detach")
	if got := c.CurrentBranch(); got != "" {
		t.Errorf("current branch on detached HEAD = %q, want empty", got)
	}
}

func TestIsAuthFailure(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("remote: Permission denied"), true},
		{errors.New("The requested URL returned error: 403"), true},
		{errors.New("could not read Username for 'https://github.com'"), true},
		{errors.New("Authentication failed for repo"), true},
		{errors.New("unable to access 'https://github.com/o/r.git'"), true},
		{errors.New("non-fast-forward"), false},
	}
	for _, tc := range cases {
		if got := isAuthFailure(tc.err); got != tc.want {
			t.Errorf("isAuthFailure(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
