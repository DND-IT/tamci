package strategy

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dnd-it/tamci/internal/release/config"
)

// Result holds the outcome of a version calculation.
type Result struct {
	Version         string // Next version (without tag prefix). Empty if skipped.
	PreviousVersion string // Previous version tag. Empty if first release.
	Skipped         bool   // True if no release is needed.
}

// VersionStrategy determines the next version based on existing tags and commits.
type VersionStrategy interface {
	// NextVersion calculates the next version.
	// Returns a Result with Skipped=true if no release is needed.
	NextVersion(tags []string, cfg config.Config) (Result, error)

	// AlwaysReleases returns true if this strategy always produces a release
	// (calver) vs. conditionally (semver).
	AlwaysReleases() bool

	// Name returns the strategy name for logging.
	Name() string
}

// New creates a VersionStrategy for the given strategy name.
func New(name string) (VersionStrategy, error) {
	switch name {
	case "semver":
		return &Semver{}, nil
	case "calver":
		return &CalVer{}, nil
	default:
		return nil, fmt.Errorf("unknown strategy %q: use semver or calver", name)
	}
}

// Version format regexes — used to validate the version part after stripping the tag prefix.
var (
	semverVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)
	calverVersionRe = regexp.MustCompile(`^\d{4}\.\d{2}\.\d+$`)
)

// IsValidVersion checks whether a version string (prefix already stripped)
// matches the expected format for the given strategy.
func IsValidVersion(strategyName, version string) bool {
	switch strategyName {
	case "semver":
		return semverVersionRe.MatchString(version)
	case "calver":
		return calverVersionRe.MatchString(version)
	default:
		return false
	}
}

// FilterTags returns only tags where the prefix matches AND the remainder
// is a valid version for the strategy. This prevents cross-contamination
// in monorepos where a tag like "python-api-vgo-service-v1.13.0" could
// match the prefix "python-api-v" but has an invalid version suffix.
func FilterTags(tags []string, prefix, strategyName string) []string {
	var out []string
	for _, t := range tags {
		if !strings.HasPrefix(t, prefix) {
			continue
		}
		version := strings.TrimPrefix(t, prefix)
		if IsValidVersion(strategyName, version) {
			out = append(out, t)
		}
	}
	return out
}

// TagPatternRegex returns a regex suitable for git-cliff's --tag-pattern flag.
// It scopes git-cliff's version boundary detection to only tags belonging to
// this service/prefix, preventing it from using unrelated tags. The pattern
// is anchored at the start so a short prefix such as "v" cannot match inside
// another service's tag such as "go-service-v1.2.3".
func TagPatternRegex(prefix, strategyName string) string {
	escaped := "^" + regexp.QuoteMeta(prefix)
	switch strategyName {
	case "semver":
		return escaped + `\d+\.\d+\.\d+`
	case "calver":
		return escaped + `\d{4}\.\d{2}\.\d+`
	default:
		return escaped + `.*`
	}
}
