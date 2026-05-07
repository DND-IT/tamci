package gh

// REST client used by tamedia rollout. Lighter than the go-github-based
// helpers (no retry transport, no enterprise URL plumbing) — uses raw
// http.Client + JSON. Tests can inject a base URL.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RESTClient is a minimal GitHub REST/GraphQL client.
type RESTClient struct {
	Token   string
	Owner   string
	Repo    string
	baseURL string
	client  *http.Client
}

// NewREST creates a REST client pointing at api.github.com.
func NewREST(token, owner, repo string) *RESTClient {
	return NewRESTWithBase("https://api.github.com", token, owner, repo)
}

// NewRESTWithBase creates a REST client pointing at a custom base URL (used by tests).
func NewRESTWithBase(baseURL, token, owner, repo string) *RESTClient {
	return &RESTClient{
		Token:   token,
		Owner:   owner,
		Repo:    repo,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// PullRequest is the minimal PR shape used by deploy flows.
type PullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
	State  string `json:"state"`
	NodeID string `json:"node_id"`
}

// EnsurePR creates a PR head→base or finds and updates the existing open PR.
func (c *RESTClient) EnsurePR(head, base, title, body string, labels []string) (*PullRequest, error) {
	existing, err := c.findOpenPR(head, base)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if err := c.updatePR(existing.Number, title, body); err != nil {
			return nil, err
		}
		return existing, nil
	}
	pr, err := c.createPR(head, base, title, body)
	if err != nil {
		return nil, err
	}
	if len(labels) > 0 {
		_ = c.addLabels(pr.Number, labels) // best-effort
	}
	return pr, nil
}

// EnableAutoMerge enables auto-merge on a PR via GraphQL.
func (c *RESTClient) EnableAutoMerge(nodeID, mergeMethod string) error {
	const mutation = `mutation($pullRequestId: ID!, $mergeMethod: PullRequestMergeMethod!) {
  enablePullRequestAutoMerge(input: {pullRequestId: $pullRequestId, mergeMethod: $mergeMethod}) {
    pullRequest { id }
  }
}`
	payload := map[string]any{
		"query": mutation,
		"variables": map[string]string{
			"pullRequestId": nodeID,
			"mergeMethod":   mergeMethod,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling graphql payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/graphql", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("building graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("graphql request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("graphql status %d: %s", resp.StatusCode, body)
	}
	var gqlResp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return fmt.Errorf("decoding graphql response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

func (c *RESTClient) findOpenPR(head, base string) (*PullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&head=%s:%s&base=%s",
		c.baseURL, c.Owner, c.Repo, c.Owner, head, base)
	var prs []PullRequest
	if err := c.get(url, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

func (c *RESTClient) createPR(head, base, title, body string) (*PullRequest, error) {
	payload := map[string]string{"title": title, "head": head, "base": base, "body": body}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls", c.baseURL, c.Owner, c.Repo)
	var pr PullRequest
	if err := c.post(url, payload, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func (c *RESTClient) updatePR(number int, title, body string) error {
	payload := map[string]string{"title": title, "body": body}
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.baseURL, c.Owner, c.Repo, number)
	return c.patch(url, payload, nil)
}

func (c *RESTClient) addLabels(number int, labels []string) error {
	payload := map[string][]string{"labels": labels}
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/labels", c.baseURL, c.Owner, c.Repo, number)
	return c.post(url, payload, nil)
}

func (c *RESTClient) get(url string, out any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET %s: %w", url, err)
	}
	return c.do(req, out)
}

func (c *RESTClient) post(url string, body, out any) error {
	return c.doJSON(http.MethodPost, url, body, out)
}

func (c *RESTClient) patch(url string, body, out any) error {
	return c.doJSON(http.MethodPatch, url, body, out)
}

func (c *RESTClient) doJSON(method, url string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling %s body: %w", method, err)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("building %s %s: %w", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *RESTClient) do(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("http %s %s: %w", req.Method, req.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		var ghErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &ghErr)
		if ghErr.Message != "" {
			return fmt.Errorf("http %s %s: status %d: %s", req.Method, req.URL, resp.StatusCode, ghErr.Message)
		}
		return fmt.Errorf("http %s %s: status %d: %s", req.Method, req.URL, resp.StatusCode, body)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
