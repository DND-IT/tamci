package gitutil

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrShallowClone indicates the repository is a shallow clone.
var ErrShallowClone = fmt.Errorf("shallow clone detected: add 'fetch-depth: 0' to your actions/checkout step")

func init() {
	// Docker containers don't trust the mounted workspace.
	workspace := os.Getenv("GITHUB_WORKSPACE")
	if workspace == "" {
		workspace = "/github/workspace"
	}
	exec.Command("git", "config", "--global", "--add", "safe.directory", workspace).Run() //nolint:errcheck
}

// ConfigureAuth sets up git to use the GitHub token for push operations.
// Must be called before CreateTag/PushTag in GitHub Actions.
func ConfigureAuth(token string) {
	if token == "" {
		return
	}
	server := os.Getenv("GITHUB_SERVER_URL")
	if server == "" {
		server = "https://github.com"
	}
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return
	}

	exec.Command("git", "config", "--local", "user.name", "github-actions[bot]").Run()                                    //nolint:errcheck
	exec.Command("git", "config", "--local", "user.email", "41898282+github-actions[bot]@users.noreply.github.com").Run() //nolint:errcheck

	// Set the remote URL with embedded token for push auth.
	// https://github.com → https://x-access-token:TOKEN@github.com/owner/repo.git
	host := strings.TrimPrefix(strings.TrimPrefix(server, "https://"), "http://")
	authURL := fmt.Sprintf("https://x-access-token:%s@%s/%s.git", token, host, repo)
	exec.Command("git", "remote", "set-url", "origin", authURL).Run() //nolint:errcheck
}

// CheckShallowClone fails if the repo is a shallow clone.
func CheckShallowClone() error {
	out, err := exec.Command("git", "rev-parse", "--is-shallow-repository").Output()
	if err != nil {
		return fmt.Errorf("check shallow clone: %w", err)
	}
	if strings.TrimSpace(string(out)) == "true" {
		return ErrShallowClone
	}
	return nil
}

// ListTags returns all tags matching the given prefix, sorted by version descending.
func ListTags(prefix string) ([]string, error) {
	args := []string{"tag", "-l", "--sort=-v:refname"}
	if prefix != "" {
		args = append(args, prefix+"*")
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

// LatestTag returns the most recent tag matching the prefix, or "" if none.
func LatestTag(prefix string) (string, error) {
	tags, err := ListTags(prefix)
	if err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", nil
	}
	return tags[0], nil
}

// HasConventionalCommits checks if there are conventional commits since the given ref.
// If ref is empty, checks all commits.
func HasConventionalCommits(since string) (bool, error) {
	args := []string{"log", "--format=%s"}
	if since != "" {
		args = append(args, since+"..HEAD")
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return false, fmt.Errorf("check commits: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Conventional commit prefixes.
		for _, prefix := range []string{"feat", "fix", "perf", "revert", "docs", "style", "chore", "refactor", "test", "build", "ci"} {
			if strings.HasPrefix(line, prefix+":") || strings.HasPrefix(line, prefix+"(") || strings.HasPrefix(line, prefix+"!:") {
				return true, nil
			}
		}
	}
	return false, nil
}

// CreateTag creates an annotated tag at HEAD.
func CreateTag(tag, message string) error {
	if err := exec.Command("git", "tag", "-a", tag, "-m", message).Run(); err != nil {
		return fmt.Errorf("create tag %s: %w", tag, err)
	}
	return nil
}

// PushTag pushes a single tag to origin.
func PushTag(tag string) error {
	if err := exec.Command("git", "push", "origin", tag).Run(); err != nil {
		return fmt.Errorf("push tag %s: %w", tag, err)
	}
	return nil
}

// TagExists checks if a tag already exists locally.
func TagExists(tag string) (bool, error) {
	out, err := exec.Command("git", "tag", "-l", tag).Output()
	if err != nil {
		return false, fmt.Errorf("check tag %s: %w", tag, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}
