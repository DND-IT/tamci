package gh

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnsurePR_CreatesWhenNoneOpen(t *testing.T) {
	var createdBody map[string]string
	var labelsAdded []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/o/r/pulls":
			q := r.URL.Query()
			if q.Get("head") != "o:deploy/api/prod" || q.Get("base") != "main" {
				t.Errorf("unexpected query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte("[]"))
		case r.Method == "POST" && r.URL.Path == "/repos/o/r/pulls":
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &createdBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(PullRequest{Number: 7, URL: "http://pr/7", State: "open", NodeID: "node7"})
		case r.Method == "POST" && r.URL.Path == "/repos/o/r/issues/7/labels":
			var payload map[string][]string
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &payload)
			labelsAdded = payload["labels"]
			_, _ = w.Write([]byte("[]"))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewRESTWithBase(srv.URL, "tok", "o", "r")
	pr, err := c.EnsurePR("deploy/api/prod", "main", "title", "body", []string{"deploy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.Number != 7 || pr.NodeID != "node7" {
		t.Errorf("unexpected PR: %+v", pr)
	}
	if createdBody["title"] != "title" || createdBody["head"] != "deploy/api/prod" || createdBody["base"] != "main" {
		t.Errorf("unexpected create payload: %v", createdBody)
	}
	if len(labelsAdded) != 1 || labelsAdded[0] != "deploy" {
		t.Errorf("unexpected labels: %v", labelsAdded)
	}
}

func TestEnsurePR_UpdatesExisting(t *testing.T) {
	var patched bool
	var labelCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/o/r/pulls":
			_ = json.NewEncoder(w).Encode([]PullRequest{{Number: 3, URL: "http://pr/3", State: "open", NodeID: "node3"}})
		case r.Method == "PATCH" && r.URL.Path == "/repos/o/r/pulls/3":
			patched = true
			_, _ = w.Write([]byte("{}"))
		case strings.Contains(r.URL.Path, "labels"):
			labelCalled = true
			_, _ = w.Write([]byte("[]"))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewRESTWithBase(srv.URL, "tok", "o", "r")
	pr, err := c.EnsurePR("head", "main", "new title", "new body", []string{"deploy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.Number != 3 {
		t.Errorf("expected existing PR 3, got %d", pr.Number)
	}
	if !patched {
		t.Error("expected PATCH on existing PR")
	}
	if labelCalled {
		t.Error("labels must not be re-added when updating an existing PR")
	}
}

func TestEnsurePR_SurfacesAPIErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte("[]"))
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message": "Validation Failed"}`))
	}))
	defer srv.Close()

	c := NewRESTWithBase(srv.URL, "tok", "o", "r")
	_, err := c.EnsurePR("head", "main", "t", "b", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Validation Failed") {
		t.Errorf("error should carry API message, got: %v", err)
	}
}

func TestRESTClient_SetsAuthHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("unexpected auth header: %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got == "" {
			t.Error("missing API version header")
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := NewRESTWithBase(srv.URL, "tok", "o", "r")
	if _, err := c.findOpenPR("h", "b"); err != nil {
		t.Fatal(err)
	}
}

func TestEnableAutoMerge_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Variables map[string]string `json:"variables"`
		}
		_ = json.Unmarshal(body, &payload)
		if payload.Variables["pullRequestId"] != "node7" || payload.Variables["mergeMethod"] != "SQUASH" {
			t.Errorf("unexpected variables: %v", payload.Variables)
		}
		_, _ = w.Write([]byte(`{"data": {}}`))
	}))
	defer srv.Close()

	c := NewRESTWithBase(srv.URL, "tok", "o", "r")
	if err := c.EnableAutoMerge("node7", "SQUASH"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnableAutoMerge_GraphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors": [{"message": "Pull request is not mergeable"}]}`))
	}))
	defer srv.Close()

	c := NewRESTWithBase(srv.URL, "tok", "o", "r")
	err := c.EnableAutoMerge("node7", "SQUASH")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not mergeable") {
		t.Errorf("error should carry GraphQL message, got: %v", err)
	}
}

func TestEnableAutoMerge_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	c := NewRESTWithBase(srv.URL, "tok", "o", "r")
	if err := c.EnableAutoMerge("node7", "SQUASH"); err == nil {
		t.Fatal("expected error")
	}
}
