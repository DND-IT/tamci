package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromoteResolve_WritesOutputs(t *testing.T) {
	dir := t.TempDir()
	event := filepath.Join(dir, "event.json")
	if err := os.WriteFile(event, []byte(`{"action":"opened","pull_request":{"labels":[{"name":"deploy:dev"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "output")
	summaryPath := filepath.Join(dir, "summary")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", event)
	t.Setenv("GITHUB_OUTPUT", outPath)
	t.Setenv("GITHUB_STEP_SUMMARY", summaryPath)
	t.Setenv("INPUT_TARGET", "shared")
	t.Setenv("INPUT_KIND", "infra")
	t.Setenv("INPUT_EXPERIMENTAL", "true")

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"promote", "resolve"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := "plan=true\napply=dev\nrelease=false\n"; string(out) != want {
		t.Errorf("outputs = %q, want %q", out, want)
	}
	summary, _ := os.ReadFile(summaryPath)
	if !strings.Contains(string(summary), "| true | dev | false |") {
		t.Errorf("summary missing decision:\n%s", summary)
	}
}

func TestPromote_RequiresExperimental(t *testing.T) {
	t.Setenv("INPUT_EXPERIMENTAL", "")
	t.Setenv("INPUT_TARGET", "shared")
	t.Setenv("INPUT_KIND", "infra")
	for _, args := range [][]string{{"promote", "resolve"}, {"promote", "tag", "shared", "--dry-run"}} {
		cmd := NewRootCmd()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "experimental") {
			t.Errorf("%v: want experimental error, got %v", args, err)
		}
	}
}

func TestPromote_ExperimentalFlag(t *testing.T) {
	t.Setenv("INPUT_EXPERIMENTAL", "")
	t.Setenv("INPUT_TARGET", "shared")
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"--experimental", "promote", "resolve"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--kind") {
		t.Fatalf("want --kind error past the gate, got %v", err)
	}
}

func TestPromoteResolve_RequiresKind(t *testing.T) {
	t.Setenv("INPUT_TARGET", "shared")
	t.Setenv("INPUT_EXPERIMENTAL", "true")
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"promote", "resolve"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--kind") {
		t.Fatalf("want --kind error, got %v", err)
	}
}
