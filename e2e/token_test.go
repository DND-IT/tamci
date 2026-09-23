package e2e

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type broker struct {
	*httptest.Server
	revoked atomic.Bool
}

// newBroker fakes the runner's OIDC endpoint, an octo-sts exchange that
// trusts identity "release" for scope DND-IT/app, and GitHub's revoke API.
func newBroker(t *testing.T) *broker {
	b := &broker{}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/oidc":
			if auth != "Bearer runner-request-token" || q.Get("audience") != r.Host {
				http.Error(w, "bad OIDC request", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"value":"oidc-jwt"}`))
		case r.URL.Path == "/sts/exchange":
			if auth != "Bearer oidc-jwt" || q.Get("scope") != "DND-IT/app" {
				http.Error(w, "bad exchange request", http.StatusUnauthorized)
				return
			}
			if q.Get("identity") != "release" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"no trust policy matches identity"}`))
				return
			}
			_, _ = w.Write([]byte(`{"token":"ghs_e2e"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/installation/token":
			if auth != "Bearer ghs_e2e" {
				http.Error(w, "bad credentials", http.StatusUnauthorized)
				return
			}
			b.revoked.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(b.Close)
	return b
}

func (b *broker) env(identity string) map[string]string {
	return map[string]string{
		"ACTIONS_ID_TOKEN_REQUEST_URL":   b.URL + "/oidc?api-version=2.0",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN": "runner-request-token",
		"GITHUB_REPOSITORY":              "DND-IT/app",
		"GITHUB_API_URL":                 b.URL,
		"INPUT_URL":                      b.URL + "/sts/exchange",
		"INPUT_IDENTITY":                 identity,
	}
}

func TestToken_MainAndPostStep(t *testing.T) {
	b := newBroker(t)

	main := tamci(t, t.TempDir(), b.env("release"), "token")
	if main.err != nil {
		t.Fatalf("main step: %v", main.err)
	}
	if got := main.outputs["token"]; got != "ghs_e2e" {
		t.Errorf("token output = %q, want ghs_e2e", got)
	}
	for _, secret := range []string{"oidc-jwt", "ghs_e2e"} {
		if !strings.Contains(main.log, "::add-mask::"+secret) {
			t.Errorf("%s is not masked", secret)
		}
	}
	if main.state["isPost"] != "true" || main.state["token"] != "ghs_e2e" {
		t.Fatalf("saved state = %v", main.state)
	}

	postEnv := b.env("release")
	for k, v := range main.state {
		postEnv["STATE_"+k] = v
	}
	post := tamci(t, t.TempDir(), postEnv, "token")
	if post.err != nil {
		t.Fatalf("post step: %v", post.err)
	}
	if !b.revoked.Load() {
		t.Error("post step did not revoke the installation token")
	}
}

func TestToken_ExchangeDenied(t *testing.T) {
	b := newBroker(t)

	r := tamci(t, t.TempDir(), b.env("deploy"), "token")
	if r.err == nil {
		t.Fatal("expected the step to fail")
	}
	if !strings.Contains(r.log, "HTTP 403") || !strings.Contains(r.log, "no trust policy matches identity") {
		t.Errorf("error does not surface the broker's reason:\n%s", r.log)
	}
	if _, ok := r.outputs["token"]; ok {
		t.Error("token output set on failure")
	}
	if len(r.state) != 0 {
		t.Errorf("state saved on failure: %v", r.state)
	}
}

func TestToken_WithoutIDTokenPermission(t *testing.T) {
	b := newBroker(t)
	env := b.env("release")
	delete(env, "ACTIONS_ID_TOKEN_REQUEST_URL")
	delete(env, "ACTIONS_ID_TOKEN_REQUEST_TOKEN")

	r := tamci(t, t.TempDir(), env, "token")
	if r.err == nil || !strings.Contains(r.log, "id-token: write") {
		t.Fatalf("expected a hint about id-token permission, got err=%v\n%s", r.err, r.log)
	}
}
