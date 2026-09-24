package git

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// MarkSafe adds the working directory to the global safe.directory list, which
// git needs inside a Docker action where the workspace has another owner.
func (c *Client) MarkSafe() error {
	absDir, err := filepath.Abs(c.Dir)
	if err != nil {
		return err
	}
	return c.runGlobal("config", "--global", "--add", "safe.directory", absDir)
}

// TopLevel returns the root of the working tree.
func (c *Client) TopLevel() (string, error) {
	out, err := c.cmdOutput("rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not inside a git repository: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// RemoteURL returns the URL of the origin remote.
func (c *Client) RemoteURL() (string, error) {
	out, err := c.cmdOutput("remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Fetch runs a quiet fetch from origin with the given extra arguments.
func (c *Client) Fetch(args ...string) error {
	return c.run(append([]string{"fetch", "--quiet", "origin"}, args...)...)
}

// IsAncestor reports whether commit is reachable from ref.
func (c *Client) IsAncestor(commit, ref string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", commit, ref)
	cmd.Dir = c.Dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s: %w\n%s", commit, ref, err, out)
}

// LatestTag returns the highest tag matching pattern by version sort, or "".
func (c *Client) LatestTag(pattern string) (string, error) {
	out, err := c.cmdOutput("tag", "-l", pattern, "--sort=-v:refname")
	if err != nil {
		return "", fmt.Errorf("git tag -l %s: %w", pattern, err)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return first, nil
}

// TagExists reports whether refs/tags/<tag> exists locally.
func (c *Client) TagExists(tag string) bool {
	_, err := c.cmdOutput("rev-parse", "-q", "--verify", "refs/tags/"+tag)
	return err == nil
}

// CommitMessages returns the full message of each commit in revRange that
// touches paths, newest first.
func (c *Client) CommitMessages(revRange string, paths ...string) ([]string, error) {
	args := append([]string{"log", "--format=%B%x1e", revRange, "--"}, paths...)
	out, err := c.cmdOutput(args...)
	if err != nil {
		return nil, fmt.Errorf("git log %s: %w", revRange, err)
	}
	var messages []string
	for m := range strings.SplitSeq(out, "\x1e") {
		if m = strings.TrimSpace(m); m != "" {
			messages = append(messages, m)
		}
	}
	return messages, nil
}

// OneLine returns "<short sha> <subject>" for commit.
func (c *Client) OneLine(commit string) (string, error) {
	out, err := c.cmdOutput("log", "-1", "--format=%h %s", commit)
	if err != nil {
		return "", fmt.Errorf("git log %s: %w", commit, err)
	}
	return strings.TrimSpace(out), nil
}

// Show returns the content of path at ref.
func (c *Client) Show(ref, path string) ([]byte, error) {
	out, err := c.cmdOutput("show", ref+":"+path)
	if err != nil {
		return nil, fmt.Errorf("git show %s:%s: %w", ref, path, err)
	}
	return []byte(out), nil
}

// CreateAnnotatedTag creates an annotated tag at commit.
func (c *Client) CreateAnnotatedTag(tag, commit, message string) error {
	return c.run("tag", "-a", tag, commit, "-m", message)
}

// PushTag pushes refs/tags/<tag> to origin.
func (c *Client) PushTag(tag string) error {
	return c.run("push", "origin", "refs/tags/"+tag)
}
