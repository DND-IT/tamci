package changelog

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dnd-it/tamci/internal/release/config"
	"github.com/dnd-it/tamci/internal/release/strategy"
)

const maxChangelogBytes = 100 * 1024 // 100KB — safety margin under GitHub's 125KB limit.

// Generate produces a changelog using git-cliff.
// Returns the changelog text, truncated to maxChangelogBytes if needed.
func Generate(cfg config.Config) (string, error) {
	// Always use --unreleased. In both direct and pr modes the new tag is
	// created *after* this function runs, so --latest would resolve to the
	// previous tag and emit that release's range — one release behind.
	// --unreleased captures every commit since the last tag, which is what
	// the release notes should describe.
	args := []string{"--unreleased", "--strip", "all"}

	cliffConfig := cfg.CliffConfig
	if cliffConfig == "" {
		// Without --config, git-cliff falls back to its embedded keepachangelog
		// template (emoji-prefixed headers, "## [unreleased]" section). Pin to
		// the built-in template for the active strategy so direct mode renders
		// the same sections that drive version bumping.
		cliffConfig = strategy.FindBuiltinConfig(cfg.VersionStrategy)
	}
	if cliffConfig != "" {
		args = append([]string{"--config", cliffConfig}, args...)
	}
	if glob := cfg.IncludeGlob(); glob != "" {
		args = append(args, "--include-path", glob)
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
