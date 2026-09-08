package publish

import "testing"

func TestOwnerRepoFromEnv(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "acme/widgets")
	owner, repo, err := OwnerRepoFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "acme" || repo != "widgets" {
		t.Errorf("got %q/%q", owner, repo)
	}
}

func TestOwnerRepoFromEnv_Unset(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "")
	if _, _, err := OwnerRepoFromEnv(); err == nil {
		t.Fatal("expected error when GITHUB_REPOSITORY is unset")
	}
}

func TestOwnerRepoFromEnv_Malformed(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "just-a-name")
	if _, _, err := OwnerRepoFromEnv(); err == nil {
		t.Fatal("expected error for repository without owner")
	}
}

func TestOwnerRepoFromEnv_NestedSlash(t *testing.T) {
	// SplitN keeps everything after the first slash as the repo name.
	t.Setenv("GITHUB_REPOSITORY", "acme/group/widgets")
	owner, repo, err := OwnerRepoFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "acme" || repo != "group/widgets" {
		t.Errorf("got %q/%q", owner, repo)
	}
}
