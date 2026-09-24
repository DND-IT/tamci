package gh

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkflowRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/workflows/shared.yaml/runs" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("branch") != "main" || q.Get("status") != "success" || q.Get("per_page") != "50" {
			t.Errorf("unexpected query %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"total_count":2,"workflow_runs":[{"head_sha":"abc","event":"pull_request"},{"head_sha":"def","event":"push"}]}`))
	}))
	defer srv.Close()

	runs, err := NewRESTWithBase(srv.URL, "tok", "o", "r").WorkflowRuns("shared.yaml", "main", "success")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[1].HeadSHA != "def" || runs[1].Event != "push" {
		t.Errorf("unexpected runs %+v", runs)
	}
}
