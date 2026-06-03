package strategy

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dnd-it/tamci/internal/release/config"
)

// Semver implements VersionStrategy using git-cliff --bumped-version.
type Semver struct{}

func (s *Semver) Name() string         { return "semver" }
func (s *Semver) AlwaysReleases() bool { return false }

func (s *Semver) NextVersion(tags []string, cfg config.Config) (Result, error) {
	prefix := cfg.TagPrefix

	// Find the latest semver tag.
	var latest string
	for _, t := range tags {
		if strings.HasPrefix(t, prefix) {
			latest = t
			break
		}
	}

	// Bootstrap: no prior tag.
	if latest == "" {
		// Check for conventional commits.
		has, err := hasConventionalCommitsSinceRef("")
		if err != nil {
			return Result{}, err
		}
		if !has {
			return Result{Skipped: true}, nil
		}
		return Result{Version: "0.1.0"}, nil
	}

	// Normal case: use git-cliff to determine next version.
	// With filter_commits = true in semver.toml, git-cliff returns the current
	// version unchanged when only non-conventional commits exist — our
	// same-version check below catches that and skips.
	args := []string{"--bumped-version"}
	cliffConfig := cfg.CliffConfig
	if cliffConfig == "" {
		// Use the built-in semver template which has filter_commits = true.
		// This ensures non-conventional commits don't trigger a patch bump.
		cliffConfig = FindBuiltinConfig("semver")
	}
	if cliffConfig != "" {
		args = append([]string{"--config", cliffConfig}, args...)
	}
	// Do NOT pass --include-path here. git-cliff --bumped-version with
	// --include-path fails to see unreleased commits after the latest tag,
	// returning the current version instead of the bumped one. The
	// --tag-pattern alone correctly scopes version boundary detection.
	// --include-path is only used in changelog.Generate() for release notes.
	//
	// Always pass --tag-pattern to scope git-cliff's version boundary detection
	// to this service's tags only; otherwise git-cliff may use unrelated tags
	// (e.g. go-service-v1.13.0) as the latest version when releasing python-api.
	if cfg.CurrentPackage != nil && cfg.CurrentPackage.TagPattern != "" {
		args = append(args, "--tag-pattern", cfg.CurrentPackage.TagPattern)
	} else {
		args = append(args, "--tag-pattern", TagPatternRegex(cfg.TagPrefix, "semver"))
	}

	cmd := exec.Command("git-cliff", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return Result{}, fmt.Errorf("git-cliff --bumped-version failed: %s", string(exitErr.Stderr))
		}
		return Result{}, fmt.Errorf("git-cliff --bumped-version: %w", err)
	}

	nextVersion := strings.TrimSpace(string(out))
	// git-cliff may include the prefix; strip it.
	nextVersion = strings.TrimPrefix(nextVersion, prefix)

	if nextVersion == "" {
		return Result{Skipped: true}, nil
	}

	// Validate that git-cliff returned a proper semver version.
	// This guards against git-cliff picking up unrelated tags and returning garbage.
	if !IsValidVersion("semver", nextVersion) {
		return Result{}, fmt.Errorf("git-cliff returned invalid semver %q (from output %q); check --tag-pattern filtering", nextVersion, strings.TrimSpace(string(out)))
	}

	// If git-cliff returns the same version as the current tag, skip.
	// This happens when filter_commits = true filters out all non-conventional commits.
	currentVersion := strings.TrimPrefix(latest, prefix)
	if nextVersion == currentVersion {
		return Result{Skipped: true, PreviousVersion: latest}, nil
	}

	return Result{
		Version:         nextVersion,
		PreviousVersion: latest,
	}, nil
}

// FindBuiltinConfig locates a built-in cliff template by strategy name.
// Search order:
//  1. CLIFF_TEMPLATES_DIR env var (for local dev/testing)
//  2. /cliff-templates/ (Docker image)
//  3. ./cliff-templates/ (run from repo root)
func FindBuiltinConfig(strategy string) string {
	file := strategy + ".toml"
	if dir := os.Getenv("CLIFF_TEMPLATES_DIR"); dir != "" {
		p := dir + "/" + file
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	candidates := []string{
		"/cliff-templates/" + file,
		"cliff-templates/" + file,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// hasConventionalCommitsSinceRef checks for conventional commits.
// Used only for the bootstrap case (no tags) where git-cliff isn't called.
func hasConventionalCommitsSinceRef(ref string) (bool, error) {
	args := []string{"log", "--format=%s"}
	if ref != "" {
		args = append(args, ref+"..HEAD")
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return false, fmt.Errorf("check commits: %w", err)
	}
	prefixes := []string{"feat", "fix", "perf", "revert", "docs", "style", "chore", "refactor", "test", "build", "ci"}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, p := range prefixes {
			if strings.HasPrefix(line, p+":") || strings.HasPrefix(line, p+"(") || strings.HasPrefix(line, p+"!:") {
				return true, nil
			}
		}
	}
	return false, nil
}
