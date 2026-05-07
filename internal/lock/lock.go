// Package lock provides a distributed mutex backed by GitHub git refs.
// Acquire creates a ref under refs/locks/<name>; release deletes it.
// Identical refs cannot be created twice, giving atomic locking semantics.
package lock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	repo    string
	token   string
	http    *http.Client
	baseURL string
}

func New(repo, token string) *Client {
	return &Client{
		repo:    repo,
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
		baseURL: "https://api.github.com",
	}
}

func (c *Client) refPath(lockName string) string {
	return fmt.Sprintf("locks/%s", lockName)
}

// Acquire attempts to create a git ref as an atomic lock.
// Returns true if the lock was acquired, false if it already exists.
func (c *Client) Acquire(lockName, sha string) (bool, error) {
	ref := fmt.Sprintf("refs/%s", c.refPath(lockName))

	body, _ := json.Marshal(map[string]string{"ref": ref, "sha": sha})

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/repos/%s/git/refs", c.baseURL, c.repo), bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusCreated {
		return true, nil
	}
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return false, nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return false, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
}

// Release deletes the lock ref. 404 is treated as success (idempotent).
func (c *Client) Release(lockName string) error {
	ref := c.refPath(lockName)

	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/repos/%s/git/refs/%s", c.baseURL, c.repo, ref), nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
}

// LockAge returns the age of the lock in seconds, or -1 if the lock doesn't exist.
func (c *Client) LockAge(lockName string) (int, error) {
	ref := c.refPath(lockName)

	sha, err := c.getRefSHA(ref)
	if err != nil {
		return -1, nil
	}

	commitDate, err := c.getCommitDate(sha)
	if err != nil {
		return -1, err
	}

	return int(time.Since(commitDate).Seconds()), nil
}

func (c *Client) getRefSHA(ref string) (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/repos/%s/git/ref/%s", c.baseURL, c.repo, ref), nil)
	if err != nil {
		return "", err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ref not found: %d", resp.StatusCode)
	}

	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Object.SHA, nil
}

func (c *Client) getCommitDate(sha string) (time.Time, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/repos/%s/git/commits/%s", c.baseURL, c.repo, sha), nil)
	if err != nil {
		return time.Time{}, err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("commit not found: %d", resp.StatusCode)
	}

	var result struct {
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return time.Time{}, err
	}
	return result.Committer.Date, nil
}

// SetBaseURL overrides the API base URL — used by tests.
func (c *Client) SetBaseURL(url string) { c.baseURL = url }

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
}
