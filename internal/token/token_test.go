package token

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIDToken_SendsAudienceAndKeepsQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer req" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("api-version") != "2.0" || r.URL.Query().Get("audience") != "sts.example.com" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"value":"oidc"}`))
	}))
	defer srv.Close()

	got, err := IDToken(srv.URL+"/?api-version=2.0", "req", "sts.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "oidc" {
		t.Errorf("got %q", got)
	}
}

func TestExchange_ReturnsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/sts/exchange" || r.Header.Get("Authorization") != "Bearer oidc" ||
			q.Get("scope") != "DND-IT/x" || q.Get("identity") != "release" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"token":"ghs_x"}`))
	}))
	defer srv.Close()

	got, err := Exchange(srv.URL+"/sts/exchange", "oidc", "DND-IT/x", "release")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ghs_x" {
		t.Errorf("got %q", got)
	}
}

func TestExchange_ClientErrorIsNotRetried(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"trust policy: subject did not match"}`))
	}))
	defer srv.Close()

	_, err := Exchange(srv.URL, "oidc", "DND-IT/x", "release")
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || !strings.Contains(err.Error(), "subject did not match") {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func noBackoff(t *testing.T) {
	t.Helper()
	orig := Backoff
	Backoff = []time.Duration{0, 0}
	t.Cleanup(func() { Backoff = orig })
}

func TestExchange_RetriesServerErrors(t *testing.T) {
	noBackoff(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"token":"ghs_x"}`))
	}))
	defer srv.Close()

	if _, err := Exchange(srv.URL, "oidc", "DND-IT/x", "release"); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestRevoke(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/installation/token" || r.Header.Get("Authorization") != "Bearer ghs_x" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := Revoke(srv.URL+"/", "ghs_x"); err != nil {
		t.Fatal(err)
	}
	if err := Revoke(srv.URL, "wrong"); err == nil {
		t.Fatal("expected error for rejected revoke")
	}
}

func closedServerURL() string {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	return srv.URL
}

func respond(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestIDToken_Errors(t *testing.T) {
	forbidden := respond(http.StatusForbidden, "no id-token permission")
	defer forbidden.Close()
	empty := respond(http.StatusOK, `{}`)
	defer empty.Close()

	tests := []struct {
		name, url, want string
	}{
		{"invalid URL", "http://[::1", "parse OIDC request URL"},
		{"transport error", closedServerURL(), "request OIDC token"},
		{"HTTP error", forbidden.URL, "HTTP 403: no id-token permission"},
		{"no token", empty.URL, "no token in response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := IDToken(tt.url, "req", "aud")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestExchange_Errors(t *testing.T) {
	noBackoff(t)
	empty := respond(http.StatusOK, `{"token":""}`)
	defer empty.Close()
	down := respond(http.StatusBadGateway, "down")
	defer down.Close()

	tests := []struct {
		name, url, want string
	}{
		{"invalid URL", "http://[::1", "parse exchange URL"},
		{"transport error after retries", closedServerURL(), "exchange:"},
		{"server error after retries", down.URL, "HTTP 502: down"},
		{"no token", empty.URL, "no token in response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Exchange(tt.url, "oidc", "DND-IT/x", "release")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestRevoke_Errors(t *testing.T) {
	for name, apiURL := range map[string]string{
		"invalid URL":     "http://[::1",
		"transport error": closedServerURL(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Revoke(apiURL, "ghs_x"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDo_TruncatedBody(t *testing.T) {
	noBackoff(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("{"))
	}))
	defer srv.Close()

	if _, err := Exchange(srv.URL, "oidc", "DND-IT/x", "release"); err == nil {
		t.Fatal("expected error for truncated body")
	}
}
