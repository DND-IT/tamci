package matrix

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DetectChangedFiles returns the list of changed file paths via git diff.
//   - pull_request:        diffs against origin/{GITHUB_BASE_REF}
//   - push:                diffs against HEAD~1
//   - workflow_dispatch:   returns nil (no filtering)
func DetectChangedFiles() ([]string, error) {
	eventName := os.Getenv("GITHUB_EVENT_NAME")

	workspace := os.Getenv("GITHUB_WORKSPACE")
	if workspace == "" {
		workspace = "."
	}

	_ = exec.Command("git", "config", "--global", "--add", "safe.directory", workspace).Run()

	var args []string
	switch eventName {
	case "pull_request", "pull_request_target":
		baseRef := os.Getenv("GITHUB_BASE_REF")
		if baseRef == "" {
			return nil, fmt.Errorf("GITHUB_BASE_REF not set for %s event", eventName)
		}
		args = []string{"diff", "--name-only", "origin/" + baseRef + "...HEAD"}
	case "push":
		args = []string{"diff", "--name-only", "HEAD~1"}
	default:
		return nil, nil
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = workspace
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
