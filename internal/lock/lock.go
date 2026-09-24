// Package lock provides a distributed mutex backed by GitHub git refs.
// Acquire points a ref under refs/locks/<name> at a lock commit whose
// committer date is the acquisition time and whose message records the
// holder; release deletes it. Identical refs cannot be created twice,
// giving atomic locking semantics.
package lock

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const holderTrailer = "Lock-Holder: "

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

type commit struct {
	Message string `json:"message"`
	Tree    struct {
		SHA string `json:"sha"`
	} `json:"tree"`
	Committer struct {
		Date time.Time `json:"date"`
	} `json:"committer"`
}

// Acquire creates a lock commit on top of sha and points the lock ref at it.
// Returns true if the lock was acquired, false if it is held by someone else.
// A holder that already holds the lock re-acquires it: the ref moves to a new
// lock commit on top of sha, which also renews the acquisition time.
func (c *Client) Acquire(lockName, sha, holder string) (bool, error) {
	lockSHA, err := c.createLockCommit(lockName, sha, holder)
	if err != nil {
		return false, err
	}

	status, err := c.call("POST", "git/refs", map[string]string{"ref": "refs/" + c.refPath(lockName), "sha": lockSHA}, nil)
	if status == http.StatusCreated {
		return true, nil
	}
	if status != http.StatusUnprocessableEntity {
		return false, err
	}
	if holder == "" {
		return false, nil
	}

	current, locked, err := c.Holder(lockName)
	if err != nil || !locked || current != holder {
		return false, err
	}
	if _, err := c.call("PATCH", "git/refs/"+c.refPath(lockName), map[string]any{"sha": lockSHA, "force": true}, nil); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) createLockCommit(lockName, sha, holder string) (string, error) {
	target, err := c.getCommit(sha)
	if err != nil {
		return "", err
	}

	message := fmt.Sprintf("lock %s\n", lockName)
	if holder != "" {
		message += "\n" + holderTrailer + holder + "\n"
	}

	var created struct {
		SHA string `json:"sha"`
	}
	payload := map[string]any{"message": message, "tree": target.Tree.SHA, "parents": []string{sha}}
	if _, err := c.call("POST", "git/commits", payload, &created); err != nil {
		return "", err
	}
	return created.SHA, nil
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

// ReleaseHeld deletes the lock ref only if holder is its recorded holder.
// Returns false without error when the lock is free or held by someone else.
func (c *Client) ReleaseHeld(lockName, holder string) (bool, error) {
	current, locked, err := c.Holder(lockName)
	if err != nil || !locked || current != holder {
		return false, err
	}
	if err := c.Release(lockName); err != nil {
		return false, err
	}
	return true, nil
}

// Holder reports whether the lock is held and by whom. The holder is empty
// for a lock taken without one.
func (c *Client) Holder(lockName string) (string, bool, error) {
	sha, err := c.getRefSHA(c.refPath(lockName))
	if err != nil || sha == "" {
		return "", false, err
	}

	lockCommit, err := c.getCommit(sha)
	if err != nil {
		return "", true, err
	}

	scanner := bufio.NewScanner(strings.NewReader(lockCommit.Message))
	for scanner.Scan() {
		if h, ok := strings.CutPrefix(scanner.Text(), holderTrailer); ok {
			return h, true, nil
		}
	}
	return "", true, nil
}

// LockAge returns the seconds since the lock was acquired, or -1 if the lock doesn't exist.
func (c *Client) LockAge(lockName string) (int, error) {
	sha, err := c.getRefSHA(c.refPath(lockName))
	if err != nil || sha == "" {
		return -1, nil
	}

	lockCommit, err := c.getCommit(sha)
	if err != nil {
		return -1, err
	}

	return int(time.Since(lockCommit.Committer.Date).Seconds()), nil
}

func (c *Client) getRefSHA(ref string) (string, error) {
	var result struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	status, err := c.call("GET", "git/ref/"+ref, nil, &result)
	if status == http.StatusNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return result.Object.SHA, nil
}

func (c *Client) getCommit(sha string) (*commit, error) {
	var result commit
	if _, err := c.call("GET", "git/commits/"+sha, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) call(method, path string, payload, out any) (int, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, fmt.Sprintf("%s/repos/%s/%s", c.baseURL, c.repo, path), body)
	if err != nil {
		return 0, err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("%s %s: unexpected status %d: %s", method, path, resp.StatusCode, string(respBody))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

// SetBaseURL overrides the API base URL — used by tests.
func (c *Client) SetBaseURL(url string) { c.baseURL = url }

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
}
