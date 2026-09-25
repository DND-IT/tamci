package rollout

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/dnd-it/tamci/internal/gh"
)

const (
	releasesPerPage  = 100
	maxReleasePages  = 5
	maxNotesBodySize = 60000
)

type releaseLister interface {
	ListReleases(page, perPage int) ([]gh.Release, error)
}

// releasesBetween returns the published releases after oldTag up to and
// including newTag, newest first. When oldTag has no release (first deploy,
// sha tags, or older than the pages scanned) only the newTag release is
// returned, so a PR never carries the repo's whole history.
func releasesBetween(c releaseLister, prefix, oldTag, newTag string) ([]gh.Release, error) {
	if newTag == "" || newTag == oldTag {
		return nil, nil
	}
	var collected []gh.Release
	for page := 1; page <= maxReleasePages; page++ {
		releases, err := c.ListReleases(page, releasesPerPage)
		if err != nil {
			return nil, err
		}
		for _, r := range releases {
			if r.Draft {
				continue
			}
			if len(collected) == 0 {
				if tagMatches(r.TagName, prefix, newTag) {
					collected = append(collected, r)
				}
				continue
			}
			if oldTag != "" && tagMatches(r.TagName, prefix, oldTag) {
				return collected, nil
			}
			if prefix == "" || strings.HasPrefix(r.TagName, prefix) {
				collected = append(collected, r)
			}
		}
		if len(releases) < releasesPerPage {
			break
		}
	}
	if len(collected) > 1 {
		collected = collected[:1]
	}
	return collected, nil
}

// tagMatches reports whether a release tag names version. Without an
// explicit prefix both the bare version and the conventional v-prefix match.
func tagMatches(tag, prefix, version string) bool {
	if prefix != "" {
		return tag == prefix+version
	}
	return tag == version || tag == "v"+version
}

func releaseNotesSection(releases []gh.Release) string {
	if len(releases) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Release notes\n")
	for i, r := range releases {
		title := r.Name
		if title == "" {
			title = r.TagName
		}
		body := strings.TrimSpace(r.Body)
		if body == "" {
			body = "_No release notes._"
		}
		if len(body) > maxNotesBodySize {
			body = strings.ToValidUTF8(body[:maxNotesBodySize], "") + "\n\n_Truncated._"
		}
		entry := fmt.Sprintf("\n<details open>\n<summary><a href=\"%s\">%s</a></summary>\n\n%s\n\n</details>\n", r.URL, title, body)
		if i > 0 && sb.Len()+len(entry) > maxNotesBodySize {
			fmt.Fprintf(&sb, "\n_%d older releases not shown._\n", len(releases)-i)
			break
		}
		sb.WriteString(entry)
	}
	return sb.String()
}

// releaseNotes is best-effort: a failed lookup is logged and the PR is
// opened without notes rather than blocking the deploy.
func releaseNotes(c releaseLister, prefix, oldTag, newTag string) string {
	releases, err := releasesBetween(c, prefix, oldTag, newTag)
	if err != nil {
		slog.Warn("fetching release notes", "error", err)
		return ""
	}
	if len(releases) == 0 && newTag != "" && newTag != oldTag {
		slog.Warn("no release found for the deployed tag, leaving out release notes", "tag", newTag, "tag_prefix", prefix)
	}
	return releaseNotesSection(releases)
}
