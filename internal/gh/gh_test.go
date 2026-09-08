package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRetryTransport_RetriesOn429ThenSucceeds(t *testing.T) {
	var attempts int
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := &http.Client{Transport: &retryTransport{}}
	resp, err := client.Post(srv.URL, "text/plain", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
	for i, b := range bodies {
		if b != "payload" {
			t.Errorf("attempt %d body = %q, want request body replayed", i+1, b)
		}
	}
}

func TestRetryTransport_GivesUpAfterMaxRetries(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &retryTransport{}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want final 429 passed through", resp.StatusCode)
	}
	if attempts != maxRetries {
		t.Errorf("attempts = %d, want %d", attempts, maxRetries)
	}
}

func TestRetryTransport_BailsWhenResetExceedsBudget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix()))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &http.Client{Transport: &retryTransport{}}
	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected error when reset time exceeds retry budget")
	}
	if !strings.Contains(err.Error(), "exceeds retry budget") {
		t.Errorf("unexpected error: %v", err)
	}
}

func newAPIServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestFindPullRequest(t *testing.T) {
	srv := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/repos/o/r/pulls" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("head"); got != "o:mybranch" {
			t.Errorf("head = %q", got)
		}
		_, _ = w.Write([]byte(`[{"number": 12, "html_url": "http://pr/12", "node_id": "n12"}]`))
	})

	pr, err := FindPullRequest(context.Background(), srv.URL, "tok", "o", "r", "mybranch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil || pr.Number != 12 || pr.NodeID != "n12" {
		t.Errorf("unexpected PR: %+v", pr)
	}
}

func TestFindPullRequest_NoneOpen(t *testing.T) {
	srv := newAPIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	pr, err := FindPullRequest(context.Background(), srv.URL, "tok", "o", "r", "mybranch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr != nil {
		t.Errorf("expected nil, got %+v", pr)
	}
}

func TestCreatePullRequest(t *testing.T) {
	srv := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v3/repos/o/r/pulls" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]string
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		if payload["head"] != "feature" || payload["base"] != "main" {
			t.Errorf("unexpected payload: %v", payload)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number": 5, "html_url": "http://pr/5", "node_id": "n5"}`))
	})

	pr, err := CreatePullRequest(context.Background(), srv.URL, "tok", "o", "r", "title", "body", "feature", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.Number != 5 {
		t.Errorf("number = %d, want 5", pr.Number)
	}
}

func TestAddLabelsAndRequestReviewers(t *testing.T) {
	var labelPath, reviewerPath string
	srv := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/labels"):
			labelPath = r.URL.Path
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/requested_reviewers"):
			reviewerPath = r.URL.Path
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	})

	if err := AddLabels(context.Background(), srv.URL, "tok", "o", "r", 9, []string{"deploy"}); err != nil {
		t.Fatalf("AddLabels: %v", err)
	}
	if labelPath != "/api/v3/repos/o/r/issues/9/labels" {
		t.Errorf("label path = %q", labelPath)
	}
	if err := RequestReviewers(context.Background(), srv.URL, "tok", "o", "r", 9, []string{"alice"}); err != nil {
		t.Fatalf("RequestReviewers: %v", err)
	}
	if reviewerPath != "/api/v3/repos/o/r/pulls/9/requested_reviewers" {
		t.Errorf("reviewer path = %q", reviewerPath)
	}
}

func TestEnableAutoMerge_GoGithub(t *testing.T) {
	srv := newAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Variables map[string]string `json:"variables"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		if payload.Variables["mergeMethod"] != "MERGE" {
			t.Errorf("unexpected variables: %v", payload.Variables)
		}
		_, _ = w.Write([]byte(`{"data": {}}`))
	})

	if err := EnableAutoMerge(context.Background(), srv.URL, "tok", "node1", "MERGE"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnableAutoMerge_GoGithub_Errors(t *testing.T) {
	srv := newAPIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors": [{"message": "boom"}, {"message": "second"}]}`))
	})

	err := EnableAutoMerge(context.Background(), srv.URL, "tok", "node1", "MERGE")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "second") {
		t.Errorf("expected all messages joined, got: %v", err)
	}
}
