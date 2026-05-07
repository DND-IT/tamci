// Package releasepr manages the release PR lifecycle:
// create/update a release PR on push, detect merge and trigger release.
//
// Flow:
//
//	push to main → CreateOrUpdate() → release PR with changelog + manifest
//	push to main → DetectMerge()    → finds merged PR via API, returns manifest
//	post-release → Cleanup()        → swap labels, delete branch
package releasepr

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/google/go-github/v68/github"
)

const (
	LabelPending  = "autorelease: pending"
	LabelTagged   = "autorelease: tagged"
	BranchPrefix  = "release/"
	ManifestFile  = ".release-pending.json"
	ChangelogFile = "CHANGELOG.md"
)

// Manifest is the JSON payload committed to the release branch.
type Manifest struct {
	Version  string `json:"version"`
	Strategy string `json:"strategy"`
	Tag      string `json:"tag"`
}

// MergeResult holds the result of a merged release PR detection.
type MergeResult struct {
	Manifest *Manifest
	PRNumber int
}

// Client wraps go-github for release PR operations.
type Client struct {
	gh    *github.Client
	owner string
	repo  string
}

// NewClient creates a release PR client.
func NewClient(gh *github.Client, owner, repo string) *Client {
	return &Client{gh: gh, owner: owner, repo: repo}
}

// ReleaseBranchName derives a stable, reusable release branch name from a tag
// and version. For example, tag "go-service-v1.14.0" with version "1.14.0"
// produces "release/go-service". This ensures each service gets a single
// long-lived release branch rather than a new branch per version, keeping
// PR commit diffs minimal (just the manifest commit).
func ReleaseBranchName(tag, version string) string {
	prefix := strings.TrimSuffix(tag, version)
	name := strings.TrimRight(prefix, "-_/v")
	if name == "" {
		name = "next"
	}
	return BranchPrefix + name
}

// ChangelogPath returns the path for the changelog file.
// When servicePath is set, places it under the service directory so the
// merge commit triggers path-filtered workflows.
func ChangelogPath(servicePath string) string {
	if servicePath == "" {
		return ChangelogFile
	}
	return path.Join(servicePath, ChangelogFile)
}

// CreateOrUpdate creates a new release PR or updates an existing one.
// A .release-pending.json manifest and CHANGELOG.md are committed to the
// release branch. The CHANGELOG.md is placed under servicePath (if set) so
// that merging the PR triggers path-filtered workflows.
// Returns the PR URL, PR number, and whether the PR was newly created (vs updated).
func (c *Client) CreateOrUpdate(ctx context.Context, version, tag, changelog, baseBranch, servicePath string) (prURL string, prNumber int, created bool, err error) {
	branchName := ReleaseBranchName(tag, version)

	// Search for existing release PR by label and branch.
	existing, err := c.findPendingPR(ctx, branchName)
	if err != nil {
		return "", 0, false, fmt.Errorf("search release PRs: %w", err)
	}

	manifest := Manifest{
		Version:  version,
		Strategy: "", // filled by caller if needed
		Tag:      tag,
	}
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")

	if existing != nil {
		log.Printf("found existing release PR #%d, updating", existing.GetNumber())

		// Force-update the release branch to current HEAD.
		if err := c.updateReleaseBranch(ctx, branchName, baseBranch, manifestJSON, changelog, servicePath); err != nil {
			return "", 0, false, fmt.Errorf("update release branch: %w", err)
		}

		// Update PR title and body.
		title := fmt.Sprintf("chore: release %s", tag)
		body := formatPRBody(version, changelog)
		_, _, err := c.gh.PullRequests.Edit(ctx, c.owner, c.repo, existing.GetNumber(), &github.PullRequest{
			Title: github.Ptr(title),
			Body:  github.Ptr(body),
		})
		if err != nil {
			return "", 0, false, fmt.Errorf("update PR #%d: %w", existing.GetNumber(), err)
		}
		log.Printf("release PR #%d updated", existing.GetNumber())
		return existing.GetHTMLURL(), existing.GetNumber(), false, nil
	}

	// No existing PR — create release branch and PR.
	log.Printf("creating release branch %s", branchName)
	if err := c.createReleaseBranch(ctx, branchName, baseBranch, manifestJSON, changelog, servicePath); err != nil {
		return "", 0, false, fmt.Errorf("create release branch: %w", err)
	}

	title := fmt.Sprintf("chore: release %s", tag)
	body := formatPRBody(version, changelog)

	pr, _, err := c.gh.PullRequests.Create(ctx, c.owner, c.repo, &github.NewPullRequest{
		Title: github.Ptr(title),
		Body:  github.Ptr(body),
		Head:  github.Ptr(branchName),
		Base:  github.Ptr(baseBranch),
	})
	if err != nil {
		return "", 0, false, fmt.Errorf("create PR: %w", err)
	}

	// Add pending label.
	if err := c.ensureLabel(ctx, LabelPending, "fbca04"); err != nil {
		log.Printf("warning: could not create label %q: %v", LabelPending, err)
	}
	_, _, err = c.gh.Issues.AddLabelsToIssue(ctx, c.owner, c.repo, pr.GetNumber(), []string{LabelPending})
	if err != nil {
		log.Printf("warning: could not add label to PR #%d: %v", pr.GetNumber(), err)
	}

	log.Printf("release PR #%d created: %s", pr.GetNumber(), pr.GetHTMLURL())
	return pr.GetHTMLURL(), pr.GetNumber(), true, nil
}

// DetectMerge searches for a recently merged release PR via the GitHub API.
// Unlike event-based detection, this works on any event type (push,
// pull_request, workflow_dispatch) — matching how release-please operates.
// Returns nil when no merged release PR is found.
func (c *Client) DetectMerge(ctx context.Context, baseBranch string) (*MergeResult, error) {
	prs, _, err := c.gh.PullRequests.List(ctx, c.owner, c.repo, &github.PullRequestListOptions{
		State:       "closed",
		Base:        baseBranch,
		Sort:        "updated",
		Direction:   "desc",
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("list closed PRs: %w", err)
	}

	for _, pr := range prs {
		// The "List PRs" endpoint does NOT populate the boolean `merged`
		// field — that only appears on the single-PR GET endpoint. We must
		// derive merged-ness from `merged_at` instead, otherwise every PR
		// looks unmerged and DetectMerge silently returns nil.
		if pr.MergedAt == nil {
			continue
		}
		if !strings.HasPrefix(pr.GetHead().GetRef(), BranchPrefix) {
			continue
		}
		hasPendingLabel := false
		for _, l := range pr.Labels {
			if l.GetName() == LabelPending {
				hasPendingLabel = true
				break
			}
		}
		if !hasPendingLabel {
			continue
		}

		log.Printf("found merged release PR #%d (branch: %s)", pr.GetNumber(), pr.GetHead().GetRef())

		// Try to read manifest from the workspace (merged into the base branch).
		manifest, err := readManifestFromWorkspace()
		if err != nil {
			log.Printf("warning: could not read manifest, will recalculate: %v", err)
			manifest = &Manifest{}
		}

		return &MergeResult{
			Manifest: manifest,
			PRNumber: pr.GetNumber(),
		}, nil
	}

	return nil, nil
}

// Cleanup swaps labels and deletes the release branch after a successful release.
func (c *Client) Cleanup(ctx context.Context, prNumber int, branchName string) {
	// Remove pending label, add tagged label.
	_, err := c.gh.Issues.RemoveLabelForIssue(ctx, c.owner, c.repo, prNumber, LabelPending)
	if err != nil {
		log.Printf("warning: could not remove label %q from PR #%d: %v", LabelPending, prNumber, err)
	}

	if err := c.ensureLabel(ctx, LabelTagged, "0e8a16"); err != nil {
		log.Printf("warning: could not create label %q: %v", LabelTagged, err)
	}
	_, _, err = c.gh.Issues.AddLabelsToIssue(ctx, c.owner, c.repo, prNumber, []string{LabelTagged})
	if err != nil {
		log.Printf("warning: could not add label %q to PR #%d: %v", LabelTagged, prNumber, err)
	}

	// Delete the release branch.
	_, err = c.gh.Git.DeleteRef(ctx, c.owner, c.repo, "heads/"+branchName)
	if err != nil {
		log.Printf("warning: could not delete branch %s: %v", branchName, err)
	} else {
		log.Printf("deleted release branch %s", branchName)
	}
}

// --- internal helpers ---

func (c *Client) findPendingPR(ctx context.Context, branchName string) (*github.PullRequest, error) {
	prs, _, err := c.gh.PullRequests.List(ctx, c.owner, c.repo, &github.PullRequestListOptions{
		State: "open",
		Head:  c.owner + ":" + branchName,
	})
	if err != nil {
		return nil, err
	}
	for _, pr := range prs {
		for _, l := range pr.Labels {
			if l.GetName() == LabelPending {
				return pr, nil
			}
		}
	}
	return nil, nil
}

func (c *Client) createReleaseBranch(ctx context.Context, branchName, baseBranch string, manifestJSON []byte, changelog, servicePath string) error {
	// Get the SHA of the base branch.
	baseRef, _, err := c.gh.Git.GetRef(ctx, c.owner, c.repo, "refs/heads/"+baseBranch)
	if err != nil {
		return fmt.Errorf("get base ref %s: %w", baseBranch, err)
	}
	baseSHA := baseRef.GetObject().GetSHA()

	// Delete stale branch if it exists.
	c.gh.Git.DeleteRef(ctx, c.owner, c.repo, "refs/heads/"+branchName) //nolint:errcheck

	// Create the release branch at the same commit as base.
	_, _, err = c.gh.Git.CreateRef(ctx, c.owner, c.repo, &github.Reference{
		Ref:    github.Ptr("refs/heads/" + branchName),
		Object: &github.GitObject{SHA: github.Ptr(baseSHA)},
	})
	if err != nil {
		return fmt.Errorf("create branch %s: %w", branchName, err)
	}

	return c.commitReleaseFiles(ctx, branchName, baseBranch, manifestJSON, changelog, servicePath)
}

func (c *Client) updateReleaseBranch(ctx context.Context, branchName, baseBranch string, manifestJSON []byte, changelog, servicePath string) error {
	// Force-update the branch to match the base without deleting it first.
	// Deleting the branch would cause GitHub to auto-close any open PR whose
	// head is that branch, even if a new branch with the same name is pushed
	// moments later. A force UpdateRef avoids the deletion and keeps the PR open.
	baseRef, _, err := c.gh.Git.GetRef(ctx, c.owner, c.repo, "refs/heads/"+baseBranch)
	if err != nil {
		return fmt.Errorf("get base ref %s: %w", baseBranch, err)
	}
	baseSHA := baseRef.GetObject().GetSHA()

	_, _, err = c.gh.Git.UpdateRef(ctx, c.owner, c.repo, &github.Reference{
		Ref:    github.Ptr("refs/heads/" + branchName),
		Object: &github.GitObject{SHA: github.Ptr(baseSHA)},
	}, true)
	if err != nil {
		return fmt.Errorf("force-update branch %s: %w", branchName, err)
	}

	return c.commitReleaseFiles(ctx, branchName, baseBranch, manifestJSON, changelog, servicePath)
}

// commitReleaseFiles commits the manifest and CHANGELOG.md to the release branch.
// The changelog is placed under servicePath to trigger path-filtered workflows.
// Existing CHANGELOG.md content on the base branch is preserved by prepending.
func (c *Client) commitReleaseFiles(ctx context.Context, branchName, baseBranch string, manifestJSON []byte, changelog, servicePath string) error {
	// Get current commit on the branch.
	ref, _, err := c.gh.Git.GetRef(ctx, c.owner, c.repo, "refs/heads/"+branchName)
	if err != nil {
		return fmt.Errorf("get branch ref: %w", err)
	}
	parentSHA := ref.GetObject().GetSHA()

	// Get the tree of the parent commit.
	parentCommit, _, err := c.gh.Git.GetCommit(ctx, c.owner, c.repo, parentSHA)
	if err != nil {
		return fmt.Errorf("get parent commit: %w", err)
	}

	// Create a blob for the manifest.
	manifestBlob, _, err := c.gh.Git.CreateBlob(ctx, c.owner, c.repo, &github.Blob{
		Content:  github.Ptr(string(manifestJSON)),
		Encoding: github.Ptr("utf-8"),
	})
	if err != nil {
		return fmt.Errorf("create manifest blob: %w", err)
	}

	entries := []*github.TreeEntry{
		{
			Path: github.Ptr(ManifestFile),
			Mode: github.Ptr("100644"),
			Type: github.Ptr("blob"),
			SHA:  manifestBlob.SHA,
		},
	}

	// Add CHANGELOG.md under the service path (or root).
	if changelog != "" {
		clPath := ChangelogPath(servicePath)

		// Read existing changelog from the base branch to prepend.
		existing, err := c.readFileContent(ctx, clPath, baseBranch)
		if err != nil {
			return fmt.Errorf("read existing changelog: %w", err)
		}
		content := PrependChangelog(changelog, existing)

		clBlob, _, err := c.gh.Git.CreateBlob(ctx, c.owner, c.repo, &github.Blob{
			Content:  github.Ptr(content),
			Encoding: github.Ptr("utf-8"),
		})
		if err != nil {
			return fmt.Errorf("create changelog blob: %w", err)
		}

		entries = append(entries, &github.TreeEntry{
			Path: github.Ptr(clPath),
			Mode: github.Ptr("100644"),
			Type: github.Ptr("blob"),
			SHA:  clBlob.SHA,
		})
	}

	// Create a tree with all release files.
	tree, _, err := c.gh.Git.CreateTree(ctx, c.owner, c.repo, parentCommit.GetTree().GetSHA(), entries)
	if err != nil {
		return fmt.Errorf("create tree: %w", err)
	}

	// Create a commit.
	commit, _, err := c.gh.Git.CreateCommit(ctx, c.owner, c.repo, &github.Commit{
		Message: github.Ptr("chore: prepare release"),
		Tree:    tree,
		Parents: []*github.Commit{{SHA: github.Ptr(parentSHA)}},
	}, nil)
	if err != nil {
		return fmt.Errorf("create commit: %w", err)
	}

	// Update the branch ref.
	_, _, err = c.gh.Git.UpdateRef(ctx, c.owner, c.repo, &github.Reference{
		Ref:    github.Ptr("refs/heads/" + branchName),
		Object: &github.GitObject{SHA: commit.SHA},
	}, true)
	if err != nil {
		return fmt.Errorf("update branch ref: %w", err)
	}

	return nil
}

// readFileContent reads a file from the repo at the given ref.
// Returns empty string (and nil error) if the file does not exist.
// Other errors (rate limit, auth, 5xx) are propagated — callers must not
// treat them as "file missing" since that would silently destroy data
// when the result is used to seed file rewrites.
func (c *Client) readFileContent(ctx context.Context, filePath, ref string) (string, error) {
	content, _, resp, err := c.gh.Repositories.GetContents(ctx, c.owner, c.repo, filePath, &github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	if content == nil {
		return "", nil
	}
	return content.GetContent()
}

// PrependChangelog prepends new changelog entries to existing content.
// Maintains a single top-level "# Changelog" header.
func PrependChangelog(newEntries, existing string) string {
	if existing == "" {
		return "# Changelog\n\n" + newEntries + "\n"
	}

	// Strip leading "# Changelog" header from existing content.
	body := existing
	if idx := strings.Index(body, "\n"); idx >= 0 && strings.HasPrefix(body, "# ") {
		body = strings.TrimLeft(body[idx:], "\n")
	}

	return "# Changelog\n\n" + newEntries + "\n\n" + body
}

func (c *Client) ensureLabel(ctx context.Context, name, color string) error {
	_, _, err := c.gh.Issues.CreateLabel(ctx, c.owner, c.repo, &github.Label{
		Name:  github.Ptr(name),
		Color: github.Ptr(color),
	})
	if err != nil && !strings.Contains(err.Error(), "already_exists") {
		return err
	}
	return nil
}

func readManifestFromWorkspace() (*Manifest, error) {
	data, err := os.ReadFile(ManifestFile)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

func formatPRBody(version, changelog string) string {
	var b strings.Builder
	b.WriteString("## Release `")
	b.WriteString(version)
	b.WriteString("`\n\n")
	b.WriteString("This PR was created automatically by [action-releaser](https://github.com/dnd-it/action-releaser).\n")
	b.WriteString("Merge this PR to create the release.\n\n")
	if changelog != "" {
		b.WriteString("### Changelog\n\n")
		b.WriteString(changelog)
	} else {
		b.WriteString("*No changelog entries.*\n")
	}
	return b.String()
}
