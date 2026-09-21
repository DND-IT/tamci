// Package token exchanges a GitHub Actions OIDC token for a GitHub App
// installation token at an octo-sts token broker, and revokes it again.
package token

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Backoff is the wait before each retry of a failed exchange.
var Backoff = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}

var client = &http.Client{Timeout: 30 * time.Second}

// IDToken requests a GitHub Actions OIDC token for audience from the runner's
// ACTIONS_ID_TOKEN_REQUEST_URL endpoint.
func IDToken(requestURL, requestToken, audience string) (string, error) {
	u, err := url.Parse(requestURL)
	if err != nil {
		return "", fmt.Errorf("parse OIDC request URL: %w", err)
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+requestToken)

	var body struct {
		Value string `json:"value"`
	}
	status, raw, err := do(req)
	if err != nil {
		return "", fmt.Errorf("request OIDC token: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("request OIDC token: HTTP %d: %s", status, raw)
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Value == "" {
		return "", fmt.Errorf("request OIDC token: no token in response")
	}
	return body.Value, nil
}

// Exchange calls the broker's exchange endpoint with idToken and returns the
// installation token for scope, as allowed by the trust policy named identity.
// Transport errors and 5xx responses are retried per Backoff.
func Exchange(exchangeURL, idToken, scope, identity string) (string, error) {
	u, err := url.Parse(exchangeURL)
	if err != nil {
		return "", fmt.Errorf("parse exchange URL: %w", err)
	}
	q := u.Query()
	q.Set("scope", scope)
	q.Set("identity", identity)
	u.RawQuery = q.Encode()

	var status int
	var raw []byte
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+idToken)

		status, raw, err = do(req)
		retryable := err != nil || status >= 500
		if !retryable || attempt >= len(Backoff) {
			if err != nil {
				return "", fmt.Errorf("exchange: %w", err)
			}
			break
		}
		time.Sleep(Backoff[attempt])
	}

	if status != http.StatusOK {
		return "", fmt.Errorf("exchange for scope %q and identity %q: HTTP %d: %s", scope, identity, status, raw)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Token == "" {
		return "", fmt.Errorf("exchange: no token in response")
	}
	return body.Token, nil
}

// Revoke invalidates an installation token through the GitHub API at apiURL.
func Revoke(apiURL, installationToken string) error {
	req, err := http.NewRequest(http.MethodDelete, strings.TrimSuffix(apiURL, "/")+"/installation/token", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+installationToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	status, raw, err := do(req)
	if err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("revoke: HTTP %d: %s", status, raw)
	}
	return nil
}

func do(req *http.Request) (int, []byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}
