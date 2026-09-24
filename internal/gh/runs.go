package gh

import (
	"fmt"
	"net/url"
)

// WorkflowRun is the minimal workflow run shape used by promote.
type WorkflowRun struct {
	HeadSHA string `json:"head_sha"`
	Event   string `json:"event"`
}

// WorkflowRuns lists the latest 50 runs of a workflow file on branch with the
// given status, newest first.
func (c *RESTClient) WorkflowRuns(workflow, branch, status string) ([]WorkflowRun, error) {
	q := url.Values{"branch": {branch}, "status": {status}, "per_page": {"50"}}
	u := fmt.Sprintf("%s/repos/%s/%s/actions/workflows/%s/runs?%s",
		c.baseURL, c.Owner, c.Repo, url.PathEscape(workflow), q.Encode())
	var resp struct {
		WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	}
	if err := c.get(u, &resp); err != nil {
		return nil, err
	}
	return resp.WorkflowRuns, nil
}
