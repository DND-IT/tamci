package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// remote creates a bare repository seeded with files on main and returns it
// with a working clone, like a checkout in a workflow job.
func remote(t *testing.T, files map[string]string) (bare, clone string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "remote.git")
	git(t, root, "init", "-q", "--bare", bare)
	seed := filepath.Join(root, "seed")
	git(t, root, "clone", "-q", bare, seed)
	for name, content := range files {
		writeFile(t, filepath.Join(seed, name), content)
	}
	git(t, seed, "add", ".")
	git(t, seed, "commit", "-q", "-m", "chore: initial")
	git(t, seed, "push", "-q", "-u", "origin", "main")
	clone = filepath.Join(root, "clone")
	git(t, root, "clone", "-q", bare, clone)
	return bare, clone
}

type pullRequest struct {
	Title string `json:"title"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Body  string `json:"body"`
}

// fakeGitHub accepts the calls rollout makes to open a deploy PR and records
// the PR it was asked to create.
func fakeGitHub(t *testing.T) (*httptest.Server, func() []pullRequest) {
	var mu sync.Mutex
	var created []pullRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/DND-IT/app/pulls":
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/DND-IT/app/pulls":
			var pr pullRequest
			if err := json.NewDecoder(r.Body).Decode(&pr); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			created = append(created, pr)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":7,"html_url":"https://github.com/DND-IT/app/pull/7","state":"open","node_id":"PR_7"}`))
		case strings.HasSuffix(r.URL.Path, "/labels"):
			_, _ = w.Write([]byte("[]"))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []pullRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]pullRequest(nil), created...)
	}
}

func TestRollout_DirectPRNamedByServiceAndEnvironment(t *testing.T) {
	isolateGit(t)
	const values = "deploy/charts/console/envs/prod/values.yaml"
	bare, clone := remote(t, map[string]string{
		values: "image:\n  repository: ghcr.io/dnd-it/console\n  tag: \"0.18.1\"\n",
	})
	srv, created := fakeGitHub(t)

	r := tamci(t, clone, map[string]string{
		"GITHUB_REPOSITORY": "DND-IT/app",
		"GITHUB_API_URL":    srv.URL,
		"INPUT_FILE":        values,
		"INPUT_VALUE":       "0.19.0",
		"INPUT_DEPLOY":      "pr",
		"INPUT_BRANCH":      "deploy/console/prod",
		"INPUT_TOKEN":       "ghs_test",
	}, "rollout")
	if r.err != nil {
		t.Fatalf("rollout: %v", r.err)
	}

	const title = "deploy(console/prod): 0.18.1 → 0.19.0"
	prs := created()
	if len(prs) != 1 {
		t.Fatalf("created %d PRs, want 1", len(prs))
	}
	if prs[0].Title != title || prs[0].Head != "deploy/console/prod" || prs[0].Base != "main" {
		t.Errorf("PR = %+v", prs[0])
	}
	if !strings.Contains(prs[0].Body, "`0.18.1` | `0.19.0`") {
		t.Errorf("PR body lacks the before/after row:\n%s", prs[0].Body)
	}

	if got := git(t, bare, "log", "-1", "--format=%s", "deploy/console/prod"); got != title {
		t.Errorf("pushed commit subject = %q, want %q", got, title)
	}
	if got := git(t, bare, "show", "deploy/console/prod:"+values); !strings.Contains(got, `tag: "0.19.0"`) {
		t.Errorf("pushed values.yaml not updated:\n%s", got)
	}
	if got := r.outputs["pr_urls"]; got != `["https://github.com/DND-IT/app/pull/7"]` {
		t.Errorf("pr_urls output = %q", got)
	}
}
