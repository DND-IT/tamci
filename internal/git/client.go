package git

// Stateful Client used by tamedia rollout. Operates on a working directory
// and per-instance identity. Methods provide retry on push, separate
// force-push, pull-rebase reconciliation, and auth-failure detection — needed
// by deploy flows where multiple environments may race on the same branch.

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Client runs git commands in a working directory with a fixed identity.
type Client struct {
	Dir       string
	UserName  string
	UserEmail string
}

// Configure marks the working directory as safe and sets the local git identity.
func (c *Client) Configure() error {
	absDir, _ := filepath.Abs(c.Dir)
	if err := c.runGlobal("config", "--global", "--add", "safe.directory", absDir); err != nil {
		slog.Warn("failed to set safe.directory", "dir", absDir, "error", err)
	}
	if err := c.run("config", "user.name", c.UserName); err != nil {
		return err
	}
	return c.run("config", "user.email", c.UserEmail)
}

// Add stages a file.
func (c *Client) Add(path string) error { return c.run("add", path) }

// Commit creates a commit; returns nil without error when there is nothing to commit.
func (c *Client) Commit(message string) error {
	if _, err := c.cmdOutput("diff", "--cached", "--quiet"); err == nil {
		slog.Info("nothing to commit")
		return nil
	}
	return c.run("commit", "-m", message)
}

// Push pushes to origin/branch with exponential-backoff retry on conflict.
// Pull-rebases between attempts. Returns early on auth failures.
func (c *Client) Push(branch string, maxAttempts int) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := c.run("push", "origin", branch)
		if err == nil {
			return nil
		}
		if isAuthFailure(err) {
			return fmt.Errorf("authentication failed — check token has contents:write scope: %w", err)
		}
		if attempt == maxAttempts {
			return fmt.Errorf("push failed after %d attempts: %w", maxAttempts, err)
		}
		delay := time.Duration(attempt*attempt) * time.Second
		slog.Warn("push conflict, retrying", "attempt", attempt, "delay", delay)
		time.Sleep(delay)
		if err := c.run("pull", "--rebase", "origin", branch); err != nil {
			return fmt.Errorf("pull --rebase failed: %w", err)
		}
	}
	return nil
}

// CheckoutBranch creates or resets `branch` to `origin/from` and checks it out.
func (c *Client) CheckoutBranch(branch, from string) error {
	if err := c.run("fetch", "origin", from); err != nil {
		return fmt.Errorf("fetching origin/%s: %w", from, err)
	}
	return c.run("checkout", "-B", branch, "origin/"+from)
}

// ForcePush pushes branch to origin with --force.
func (c *Client) ForcePush(branch string) error {
	err := c.run("push", "origin", branch, "--force")
	if err == nil {
		return nil
	}
	if isAuthFailure(err) {
		return fmt.Errorf("authentication failed — check token has contents:write scope: %w", err)
	}
	return fmt.Errorf("force-push %s: %w", branch, err)
}

// RevParse resolves a ref to its full SHA.
func (c *Client) RevParse(ref string) (string, error) {
	out, err := c.cmdOutput("rev-parse", ref)
	if err != nil {
		return "", fmt.Errorf("rev-parse %s: %w", ref, err)
	}
	return strings.TrimSpace(out), nil
}

// CurrentBranch returns the checked-out branch name, or "" when HEAD is detached.
func (c *Client) CurrentBranch() string {
	out, err := c.cmdOutput("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// DefaultBranch reads the remote HEAD symbolic ref. Returns "" if not resolvable.
func (c *Client) DefaultBranch() string {
	out, err := c.cmdOutput("symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(out), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func isAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, p := range []string{"403", "unable to access", "could not read Username", "Authentication failed", "Permission denied"} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func (c *Client) runGlobal(args ...string) error {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

func (c *Client) run(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = c.Dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	slog.Debug("git", "args", args, "output", strings.TrimSpace(string(out)))
	return nil
}

func (c *Client) cmdOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = c.Dir
	out, err := cmd.Output()
	return string(out), err
}
