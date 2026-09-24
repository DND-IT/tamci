package lock

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(url string) *Client {
	c := New("owner/repo", "test-token")
	c.SetBaseURL(url)
	return c
}

type fakeRepo struct {
	t       *testing.T
	lockSHA string
	commits map[string]map[string]any
	created []map[string]any
	patched bool
	deleted bool
}

func newFakeRepo(t *testing.T) *fakeRepo {
	return &fakeRepo{t: t, commits: map[string]map[string]any{
		"abc123": {"message": "feat: x", "tree": map[string]string{"sha": "tree1"}, "committer": map[string]string{"date": time.Now().Add(-48 * time.Hour).Format(time.RFC3339)}},
	}}
}

func (f *fakeRepo) holdBy(holder string, acquired time.Time) {
	f.lockSHA = "held"
	f.commits["held"] = map[string]any{
		"message":   "lock deploy\n\nLock-Holder: " + holder + "\n",
		"committer": map[string]string{"date": acquired.Format(time.RFC3339)},
	}
}

func (f *fakeRepo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
	}
	path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/")
	switch {
	case r.Method == "GET" && strings.HasPrefix(path, "git/commits/"):
		c, ok := f.commits[strings.TrimPrefix(path, "git/commits/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(c)
	case r.Method == "POST" && path == "git/commits":
		f.created = append(f.created, payload)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"sha": "lock1"})
	case r.Method == "POST" && path == "git/refs":
		if payload["ref"] != "refs/locks/deploy" {
			f.t.Errorf("unexpected ref: %v", payload["ref"])
		}
		if f.lockSHA != "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		f.lockSHA = payload["sha"].(string)
		w.WriteHeader(http.StatusCreated)
	case r.Method == "PATCH" && path == "git/refs/locks/deploy":
		f.lockSHA = payload["sha"].(string)
		f.patched = true
		_, _ = w.Write([]byte("{}"))
	case r.Method == "GET" && path == "git/ref/locks/deploy":
		if f.lockSHA == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": f.lockSHA}})
	case r.Method == "DELETE" && path == "git/refs/locks/deploy":
		f.lockSHA = ""
		f.deleted = true
		w.WriteHeader(http.StatusNoContent)
	default:
		f.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestAcquire_Success(t *testing.T) {
	repo := newFakeRepo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", got)
		}
		repo.ServeHTTP(w, r)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	acquired, err := c.Acquire("deploy", "abc123", "pr-7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Error("expected acquired to be true")
	}
	if repo.lockSHA != "lock1" {
		t.Errorf("expected ref to point at the lock commit, got %q", repo.lockSHA)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected one lock commit, got %d", len(repo.created))
	}
	lc := repo.created[0]
	if lc["tree"] != "tree1" {
		t.Errorf("unexpected tree: %v", lc["tree"])
	}
	if parents, _ := lc["parents"].([]any); len(parents) != 1 || parents[0] != "abc123" {
		t.Errorf("unexpected parents: %v", lc["parents"])
	}
	if msg, _ := lc["message"].(string); !strings.Contains(msg, "Lock-Holder: pr-7") {
		t.Errorf("holder missing from message: %q", msg)
	}
}

func TestAcquire_NoHolder(t *testing.T) {
	repo := newFakeRepo(t)
	srv := httptest.NewServer(repo)
	defer srv.Close()

	c := newTestClient(srv.URL)
	acquired, err := c.Acquire("deploy", "abc123", "")
	if err != nil || !acquired {
		t.Fatalf("expected acquired, got %v, %v", acquired, err)
	}
	if msg, _ := repo.created[0]["message"].(string); strings.Contains(msg, "Lock-Holder") {
		t.Errorf("unexpected holder in message: %q", msg)
	}
}

func TestAcquire_AlreadyHeld(t *testing.T) {
	repo := newFakeRepo(t)
	repo.holdBy("pr-1", time.Now())
	srv := httptest.NewServer(repo)
	defer srv.Close()

	c := newTestClient(srv.URL)
	for _, holder := range []string{"", "pr-2"} {
		acquired, err := c.Acquire("deploy", "abc123", holder)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if acquired {
			t.Errorf("holder %q: expected acquired to be false", holder)
		}
	}
	if repo.lockSHA != "held" || repo.patched {
		t.Error("lock ref must not move")
	}
}

func TestAcquire_SameHolderReacquires(t *testing.T) {
	repo := newFakeRepo(t)
	repo.holdBy("pr-1", time.Now().Add(-time.Hour))
	srv := httptest.NewServer(repo)
	defer srv.Close()

	c := newTestClient(srv.URL)
	acquired, err := c.Acquire("deploy", "abc123", "pr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !acquired {
		t.Error("expected acquired to be true")
	}
	if !repo.patched || repo.lockSHA != "lock1" {
		t.Error("expected the ref to move to the new lock commit")
	}
}

func TestAcquire_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	acquired, err := c.Acquire("deploy", "abc123", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if acquired {
		t.Error("expected acquired to be false")
	}
}

func TestHolder(t *testing.T) {
	repo := newFakeRepo(t)
	srv := httptest.NewServer(repo)
	defer srv.Close()
	c := newTestClient(srv.URL)

	holder, locked, err := c.Holder("deploy")
	if err != nil || locked || holder != "" {
		t.Errorf("free lock: got %q, %v, %v", holder, locked, err)
	}

	repo.holdBy("pr-9", time.Now())
	holder, locked, err = c.Holder("deploy")
	if err != nil || !locked || holder != "pr-9" {
		t.Errorf("held lock: got %q, %v, %v", holder, locked, err)
	}
}

func TestReleaseHeld(t *testing.T) {
	repo := newFakeRepo(t)
	repo.holdBy("pr-1", time.Now())
	srv := httptest.NewServer(repo)
	defer srv.Close()
	c := newTestClient(srv.URL)

	released, err := c.ReleaseHeld("deploy", "pr-2")
	if err != nil || released || repo.deleted {
		t.Errorf("other holder: got %v, %v, deleted=%v", released, err, repo.deleted)
	}

	released, err = c.ReleaseHeld("deploy", "pr-1")
	if err != nil || !released || !repo.deleted {
		t.Errorf("same holder: got %v, %v, deleted=%v", released, err, repo.deleted)
	}

	released, err = c.ReleaseHeld("deploy", "pr-1")
	if err != nil || released {
		t.Errorf("free lock: got %v, %v", released, err)
	}
}

func TestLockAge_FromAcquisition(t *testing.T) {
	repo := newFakeRepo(t)
	srv := httptest.NewServer(repo)
	defer srv.Close()
	c := newTestClient(srv.URL)

	repo.holdBy("pr-1", time.Now().Add(-30*time.Second))
	age, err := c.LockAge("deploy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if age < 29 || age > 35 {
		t.Errorf("expected age ~30s from acquisition, got %d", age)
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
