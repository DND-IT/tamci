// Package gh wraps the GitHub REST/GraphQL APIs for tamedia subcommands.
// Includes 429-aware retry transport and helpers for PR open/update/find,
// labels, reviewers, and auto-merge.
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"
)

const maxRetries = 3

type retryTransport struct{ base http.RoundTripper }

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body for retry: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	for attempt := range maxRetries {
		if attempt > 0 && bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err := base.RoundTrip(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		if attempt == maxRetries-1 {
			return resp, nil
		}

		remaining := resp.Header.Get("X-RateLimit-Remaining")
		limit := resp.Header.Get("X-RateLimit-Limit")
		resetHeader := resp.Header.Get("X-RateLimit-Reset")

		var resetTime time.Time
		if resetHeader != "" {
			if resetUnix, parseErr := strconv.ParseInt(resetHeader, 10, 64); parseErr == nil {
				resetTime = time.Unix(resetUnix, 0)
			}
		}

		wait := time.Duration(math.Pow(2, float64(attempt+1))) * time.Second
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, parseErr := strconv.Atoi(ra); parseErr == nil {
				wait = time.Duration(secs) * time.Second
			}
		}

		totalWait := wait
		for future := attempt + 2; future < maxRetries; future++ {
			totalWait += time.Duration(math.Pow(2, float64(future))) * time.Second
		}

		if !resetTime.IsZero() && time.Until(resetTime) > totalWait {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("rate limited (429): limit %s, remaining %s, resets at %s (in %s) — exceeds retry budget",
				limit, remaining, resetTime.Format(time.RFC3339), time.Until(resetTime).Truncate(time.Second))
		}

		_ = resp.Body.Close()

		resetInfo := ""
		if !resetTime.IsZero() {
			resetInfo = fmt.Sprintf(", resets in %s", time.Until(resetTime).Truncate(time.Second))
		}
		fmt.Printf("::warning::Rate limited (429): limit=%s, remaining=%s%s — retrying in %s (attempt %d/%d)\n",
			limit, remaining, resetInfo, wait, attempt+1, maxRetries)

		select {
		case <-time.After(wait):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}

	return nil, fmt.Errorf("retry loop exited without returning")
}

// PRData identifies a pull request enough to act on it.
type PRData struct {
	Number  int
	HTMLURL string
	NodeID  string
}

// FindPullRequest finds an open PR for the given head branch.
// Returns nil if no open PR exists.
func FindPullRequest(ctx context.Context, apiURL, token, owner, repo, head string) (*PRData, error) {
	client, err := newClient(apiURL, token)
	if err != nil {
		return nil, err
	}

	prs, _, err := client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		Head:        owner + ":" + head,
		State:       "open",
		ListOptions: github.ListOptions{PerPage: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("list pull requests: %w", err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &PRData{
		Number:  prs[0].GetNumber(),
		HTMLURL: prs[0].GetHTMLURL(),
		NodeID:  prs[0].GetNodeID(),
	}, nil
}

// CreatePullRequest opens a new PR.
func CreatePullRequest(ctx context.Context, apiURL, token, owner, repo, title, body, head, base string) (*PRData, error) {
	client, err := newClient(apiURL, token)
	if err != nil {
		return nil, err
	}
	pr, _, err := client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
		Title: &title, Body: &body, Head: &head, Base: &base,
	})
	if err != nil {
		return nil, fmt.Errorf("create pull request: %w", err)
	}
	return &PRData{Number: pr.GetNumber(), HTMLURL: pr.GetHTMLURL(), NodeID: pr.GetNodeID()}, nil
}

// UpdatePullRequest updates title and body of an existing PR.
func UpdatePullRequest(ctx context.Context, apiURL, token, owner, repo string, number int, title, body string) (*PRData, error) {
	client, err := newClient(apiURL, token)
	if err != nil {
		return nil, err
	}
	pr, _, err := client.PullRequests.Edit(ctx, owner, repo, number, &github.PullRequest{Title: &title, Body: &body})
	if err != nil {
		return nil, fmt.Errorf("update pull request: %w", err)
	}
	return &PRData{Number: pr.GetNumber(), HTMLURL: pr.GetHTMLURL(), NodeID: pr.GetNodeID()}, nil
}

// AddLabels attaches labels to a PR.
func AddLabels(ctx context.Context, apiURL, token, owner, repo string, prNumber int, labels []string) error {
	client, err := newClient(apiURL, token)
	if err != nil {
		return err
	}
	_, _, err = client.Issues.AddLabelsToIssue(ctx, owner, repo, prNumber, labels)
	if err != nil {
		return fmt.Errorf("add labels: %w", err)
	}
	return nil
}

// RequestReviewers requests reviewers on a PR.
func RequestReviewers(ctx context.Context, apiURL, token, owner, repo string, prNumber int, reviewers []string) error {
	client, err := newClient(apiURL, token)
	if err != nil {
		return err
	}
	_, _, err = client.PullRequests.RequestReviewers(ctx, owner, repo, prNumber, github.ReviewersRequest{Reviewers: reviewers})
	if err != nil {
		return fmt.Errorf("request reviewers: %w", err)
	}
	return nil
}

// EnableAutoMerge enables auto-merge on a PR via GraphQL.
func EnableAutoMerge(ctx context.Context, graphqlURL, token, prNodeID, mergeMethod string) error {
	query := `mutation EnableAutoMerge($pullRequestId: ID!, $mergeMethod: PullRequestMergeMethod!) {
		enablePullRequestAutoMerge(input: {
			pullRequestId: $pullRequestId,
			mergeMethod: $mergeMethod
		}) {
			pullRequest {
				autoMergeRequest {
					enabledAt
				}
			}
		}
	}`

	payload := map[string]any{
		"query":     query,
		"variables": map[string]string{"pullRequestId": prNodeID, "mergeMethod": mergeMethod},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", graphqlURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Transport: &retryTransport{base: http.DefaultTransport}}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("graphql request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("graphql request failed with status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read graphql response: %w", err)
	}

	var result struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("decode graphql response: %w", err)
	}
	if len(result.Errors) > 0 {
		msgs := make([]string, len(result.Errors))
		for i, e := range result.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}
	return nil
}

func newClient(apiURL, token string) (*github.Client, error) {
	httpClient := &http.Client{Transport: &retryTransport{base: http.DefaultTransport}}
	client := github.NewClient(httpClient).WithAuthToken(token)
	if apiURL != "" && apiURL != "https://api.github.com" {
		var err error
		client, err = client.WithEnterpriseURLs(apiURL, apiURL)
		if err != nil {
			return nil, fmt.Errorf("configure enterprise GitHub client: %w", err)
		}
	}
	return client, nil
}
