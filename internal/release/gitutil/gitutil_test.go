package gitutil

import (
	"errors"
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

// setupRepo creates a repo with one commit and chdirs into it.
func setupRepo(t *testing.T) string {
	t.Helper()
	setupHome(t)
	dir := t.TempDir()
	mustGit(t, "", "init", dir)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "feat: initial")
	t.Chdir(dir)
	return dir
}

func TestCheckShallowClone_FullClone(t *testing.T) {
	setupRepo(t)
	if err := CheckShallowClone(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckShallowClone_Shallow(t *testing.T) {
	dir := setupRepo(t)
	// A second commit so a depth-1 clone is actually shallow.
	mustGit(t, dir, "commit", "--allow-empty", "-m", "feat: second")

	shallow := filepath.Join(t.TempDir(), "shallow")
	mustGit(t, "", "clone", "--depth", "1", "file://"+dir, shallow)
	t.Chdir(shallow)

	err := CheckShallowClone()
	if !errors.Is(err, ErrShallowClone) {
		t.Fatalf("want ErrShallowClone, got: %v", err)
	}
}

func TestListTags_SortedAndFiltered(t *testing.T) {
	dir := setupRepo(t)
	for _, tag := range []string{"v1.0.0", "v1.2.0", "v1.10.0", "svc-v2.0.0"} {
		mustGit(t, dir, "tag", tag)
	}

	tags, err := ListTags("v")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"v1.10.0", "v1.2.0", "v1.0.0"}
	if len(tags) != len(want) {
		t.Fatalf("got %v, want %v", tags, want)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Errorf("[%d] got %q, want %q (version sort)", i, tags[i], want[i])
		}
	}

	all, err := ListTags("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Errorf("unfiltered got %d tags, want 4", len(all))
	}
}

func TestListTags_Empty(t *testing.T) {
	setupRepo(t)
	tags, err := ListTags("v")
	if err != nil {
		t.Fatal(err)
	}
	if tags != nil {
		t.Errorf("want nil, got %v", tags)
	}
}

func TestLatestTag(t *testing.T) {
	dir := setupRepo(t)

	latest, err := LatestTag("v")
	if err != nil {
		t.Fatal(err)
	}
	if latest != "" {
		t.Errorf("want empty for no tags, got %q", latest)
	}

	mustGit(t, dir, "tag", "v1.0.0")
	mustGit(t, dir, "tag", "v1.1.0")
	latest, err = LatestTag("v")
	if err != nil {
		t.Fatal(err)
	}
	if latest != "v1.1.0" {
		t.Errorf("latest = %q, want v1.1.0", latest)
	}
}

func TestCreateTag_TagExists_PushTag(t *testing.T) {
	dir := setupRepo(t)

	exists, err := TagExists("v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("tag should not exist yet")
	}

	if err := CreateTag("v1.0.0", "Release v1.0.0"); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	exists, err = TagExists("v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("tag should exist after CreateTag")
	}

	// Annotated tag with the given message.
	if msg := mustGit(t, dir, "tag", "-l", "-n1", "v1.0.0"); !strings.Contains(msg, "Release v1.0.0") {
		t.Errorf("tag message = %q", msg)
	}

	// Push to a bare remote.
	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, "", "init", "--bare", bare)
	mustGit(t, dir, "remote", "add", "origin", bare)
	if err := PushTag("v1.0.0"); err != nil {
		t.Fatalf("push tag: %v", err)
	}
	if got := mustGit(t, bare, "tag", "-l"); got != "v1.0.0" {
		t.Errorf("remote tags = %q", got)
	}
}

func TestHasConventionalCommits(t *testing.T) {
	dir := setupRepo(t) // has "feat: initial"

	has, err := HasConventionalCommits("")
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("expected conventional commit to be detected")
	}

	mustGit(t, dir, "tag", "v1.0.0")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "random message")
	has, err = HasConventionalCommits("v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("non-conventional commit since tag should not count")
	}

	mustGit(t, dir, "commit", "--allow-empty", "-m", "fix(api): a bug")
	has, err = HasConventionalCommits("v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("fix(api) commit since tag should count")
	}
}

func TestConfigureAuth(t *testing.T) {
	dir := setupRepo(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, "", "init", "--bare", bare)
	mustGit(t, dir, "remote", "add", "origin", bare)

	t.Setenv("GITHUB_REPOSITORY", "acme/widgets")
	t.Setenv("GITHUB_SERVER_URL", "https://github.example.com")

	ConfigureAuth("secret")

	if got := mustGit(t, dir, "config", "--local", "user.name"); got != "github-actions[bot]" {
		t.Errorf("user.name = %q", got)
	}
	remote := mustGit(t, dir, "remote", "get-url", "origin")
	want := "https://x-access-token:secret@github.example.com/acme/widgets.git"
	if remote != want {
		t.Errorf("remote = %q, want %q", remote, want)
	}
}

func TestConfigureAuth_NoTokenIsNoop(t *testing.T) {
	dir := setupRepo(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, "", "init", "--bare", bare)
	mustGit(t, dir, "remote", "add", "origin", bare)

	ConfigureAuth("")
	if got := mustGit(t, dir, "remote", "get-url", "origin"); got != bare {
		t.Errorf("remote changed without token: %q", got)
	}
}
