package publish

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"
)

// Params holds everything needed to create a GitHub release.
type Params struct {
	Owner      string
	Repo       string
	Tag        string
	Name       string
	Body       string
	Draft      bool
	Prerelease bool
	Token      string
}

// Result holds the outcome of a release creation.
type Result struct {
	URL string
	Tag string
}

// Create creates a GitHub release using the go-github SDK.
// Retries on 5xx errors with exponential backoff.
func Create(ctx context.Context, p Params) (Result, error) {
	client := github.NewClient(nil).WithAuthToken(p.Token)

	release := &github.RepositoryRelease{
		TagName:              github.Ptr(p.Tag),
		Name:                 github.Ptr(p.Name),
		Body:                 github.Ptr(p.Body),
		Draft:                github.Ptr(p.Draft),
		Prerelease:           github.Ptr(p.Prerelease),
		MakeLatest:           github.Ptr("true"),
		GenerateReleaseNotes: github.Ptr(false),
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 2 * time.Second
			log.Printf("[releaser] retrying release creation in %s (attempt %d/3)", backoff, attempt+1)
			time.Sleep(backoff)
		}

		rel, resp, err := client.Repositories.CreateRelease(ctx, p.Owner, p.Repo, release)
		if err == nil {
			return Result{
				URL: rel.GetHTMLURL(),
				Tag: rel.GetTagName(),
			}, nil
		}

		// Classify the error for actionable messages.
		if resp != nil {
			switch resp.StatusCode {
			case 401:
				return Result{}, fmt.Errorf("authentication failed (401): check your github-token")
			case 403:
				return Result{}, fmt.Errorf("permission denied (403): ensure the token has contents:write scope")
			case 422:
				if strings.Contains(err.Error(), "already_exists") {
					return Result{}, fmt.Errorf("tag %s already exists: a concurrent release may have created it", p.Tag)
				}
				return Result{}, fmt.Errorf("validation failed (422): %w", err)
			}
			// 5xx: retry.
			if resp.StatusCode >= 500 {
				lastErr = fmt.Errorf("GitHub API error (%d): %w", resp.StatusCode, err)
				continue
			}
		}

		return Result{}, fmt.Errorf("create release: %w", err)
	}

	return Result{}, fmt.Errorf("create release failed after 3 attempts: %w", lastErr)
}

// OwnerRepoFromEnv extracts owner and repo from GITHUB_REPOSITORY env var.
func OwnerRepoFromEnv() (owner, repo string, err error) {
	full := os.Getenv("GITHUB_REPOSITORY")
	if full == "" {
		return "", "", fmt.Errorf("GITHUB_REPOSITORY not set")
	}
	parts := strings.SplitN(full, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid GITHUB_REPOSITORY: %s", full)
	}
	return parts[0], parts[1], nil
}
