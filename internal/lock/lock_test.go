package lock

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(url string) *Client {
	c := New("owner/repo", "test-token")
	c.SetBaseURL(url)
	return c
}

func TestAcquire_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/git/refs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", got)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if payload["ref"] != "refs/locks/deploy" {
			t.Errorf("unexpected ref: %s", payload["ref"])
		}
		if payload["sha"] != "abc123" {
			t.Errorf("unexpected sha: %s", payload["sha"])
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	acquired, err := c.Acquire("deploy", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Error("expected acquired to be true")
	}
}

func TestAcquire_AlreadyHeld(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	acquired, err := c.Acquire("deploy", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acquired {
		t.Error("expected acquired to be false")
	}
}

func TestAcquire_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	acquired, err := c.Acquire("deploy", "abc123")
	if err == nil {
		t.Fatal("expected error")
	}
	if acquired {
		t.Error("expected acquired to be false")
	}
}

func TestRelease_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/git/refs/locks/deploy" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if err := c.Release("deploy"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelease_NotFound_Idempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if err := c.Release("deploy"); err != nil {
		t.Fatalf("expected nil error for 404, got: %v", err)
	}
}

func TestRelease_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if err := c.Release("deploy"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLockAge_Found(t *testing.T) {
	commitTime := time.Now().Add(-60 * time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/git/ref/locks/deploy":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": map[string]string{"sha": "abc123"},
			})
		case "/repos/owner/repo/git/commits/abc123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"committer": map[string]string{"date": commitTime.Format(time.RFC3339)},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	age, err := c.LockAge("deploy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if age < 59 || age > 65 {
		t.Errorf("expected age ~60s, got %d", age)
	}
}

func TestLockAge_RefNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	age, err := c.LockAge("deploy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if age != -1 {
		t.Errorf("expected -1, got %d", age)
	}
}

func TestLockAge_CommitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/git/ref/locks/deploy":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": map[string]string{"sha": "abc123"},
			})
		case "/repos/owner/repo/git/commits/abc123":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	age, err := c.LockAge("deploy")
	if err == nil {
		t.Fatal("expected error")
	}
	if age != -1 {
		t.Errorf("expected -1, got %d", age)
	}
}
