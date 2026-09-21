package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeBroker(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oidc":
			_, _ = w.Write([]byte(`{"value":"oidc-for-` + r.URL.Query().Get("audience") + `"}`))
		case "/sts/exchange":
			want := "Bearer oidc-for-" + r.Host
			if r.Header.Get("Authorization") != want {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"token":"ghs_` + r.URL.Query().Get("scope") + `_` + r.URL.Query().Get("identity") + `"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestToken_ExchangesAndSavesState(t *testing.T) {
	srv := fakeBroker(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "output")
	state := filepath.Join(dir, "state")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL+"/oidc?api-version=2.0")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req")
	t.Setenv("GITHUB_REPOSITORY", "DND-IT/x")
	t.Setenv("GITHUB_OUTPUT", out)
	t.Setenv("GITHUB_STATE", state)
	t.Setenv("STATE_isPost", "")
	t.Setenv("INPUT_URL", srv.URL+"/sts/exchange")
	t.Setenv("INPUT_IDENTITY", "release")

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"token"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got, _ := os.ReadFile(out); string(got) != "token=ghs_DND-IT/x_release\n" {
		t.Errorf("output = %q", got)
	}
	if got, _ := os.ReadFile(state); string(got) != "token=ghs_DND-IT/x_release\nisPost=true\n" {
		t.Errorf("state = %q", got)
	}
}

func TestToken_ExplicitAudienceIsUsed(t *testing.T) {
	srv := fakeBroker(t)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL+"/oidc?api-version=2.0")
	t.Setenv("GITHUB_OUTPUT", filepath.Join(t.TempDir(), "output"))
	t.Setenv("GITHUB_STATE", "")
	t.Setenv("STATE_isPost", "")

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"token", "--url", srv.URL + "/sts/exchange", "--identity", "release", "--scope", "DND-IT/x", "--audience", "other"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("expected the broker to reject the other audience, got %v", err)
	}
}

func TestToken_RequiresIDTokenPermission(t *testing.T) {
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("STATE_isPost", "")

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"token", "--url", "https://sts.example.com/sts/exchange", "--identity", "release", "--scope", "DND-IT/x"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "id-token: write") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToken_PostStepRevokes(t *testing.T) {
	revoked := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/installation/token" {
			revoked = r.Header.Get("Authorization")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("STATE_isPost", "true")
	t.Setenv("STATE_token", "ghs_x")
	t.Setenv("GITHUB_API_URL", srv.URL)

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"token"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if revoked != "Bearer ghs_x" {
		t.Errorf("revoke Authorization = %q", revoked)
	}
}
