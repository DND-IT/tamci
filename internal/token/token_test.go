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

func TestExchange_RetriesServerErrors(t *testing.T) {
	Backoff = []time.Duration{0, 0}
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
