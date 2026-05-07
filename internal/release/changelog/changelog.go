package changelog

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dnd-it/tamci/internal/release/config"
)

const maxChangelogBytes = 100 * 1024 // 100KB — safety margin under GitHub's 125KB limit.

// Generate produces a changelog using git-cliff.
// Returns the changelog text, truncated to maxChangelogBytes if needed.
func Generate(cfg config.Config) (string, error) {
	// In PR mode no tag has been created yet, so --latest would resolve to the
	// previous release's range. Use --unreleased to capture all commits since
	// the last tag. In direct mode the tag is created before this runs, so
	// --latest correctly resolves to the new release's range.
	rangeFlag := "--latest"
	if cfg.ReleaseMode == "pr" {
		rangeFlag = "--unreleased"
	}
	args := []string{rangeFlag, "--strip", "all"}

	if cfg.CliffConfig != "" {
		args = append([]string{"--config", cfg.CliffConfig}, args...)
	}
	if cfg.CurrentPackage != nil && cfg.CurrentPackage.Path != "" {
		args = append(args, "--include-path", cfg.CurrentPackage.Path+"/**")
	} else if cfg.IncludePath != "" {
		args = append(args, "--include-path", cfg.IncludePath)
	}
	// Always pass --tag-pattern to scope git-cliff's tag scanning to this
	// service's tags. Without this, git-cliff may use unrelated tags as
	// version boundaries, producing incorrect changelogs.
	if cfg.CurrentPackage != nil && cfg.CurrentPackage.TagPattern != "" {
		args = append(args, "--tag-pattern", cfg.CurrentPackage.TagPattern)
	} else if cfg.EffectiveTagPattern != "" {
		args = append(args, "--tag-pattern", cfg.EffectiveTagPattern)
	}

	cmd := exec.Command("git-cliff", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git-cliff failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("git-cliff: %w", err)
	}

	text := strings.TrimSpace(string(out))
	return truncate(text), nil
}

func truncate(text string) string {
	if len(text) <= maxChangelogBytes {
		return text
	}
	truncated := text[:maxChangelogBytes]
	if idx := strings.LastIndex(truncated, "\n"); idx > 0 {
		truncated = truncated[:idx]
	}

	repo := repoURL()
	suffix := "\n\n---\n*Changelog truncated."
	if repo != "" {
		suffix += " See the [full diff](" + repo + "/commits) for complete changes.*"
	} else {
		suffix += " See the full diff for complete changes.*"
	}
	return truncated + suffix
}

func repoURL() string {
	server := os.Getenv("GITHUB_SERVER_URL")
	if server == "" {
		server = "https://github.com"
	}
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return ""
	}
	return strings.TrimSuffix(server, "/") + "/" + repo
}
