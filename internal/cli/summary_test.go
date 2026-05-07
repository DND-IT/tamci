package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummary_StringInput_ViaEnv(t *testing.T) {
	out := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("INPUT_STRING", `{"key": "value"}`)
	t.Setenv("INPUT_SUMMARY_HEADER", "Test")
	t.Setenv("INPUT_DATA_TYPE", "json")
	t.Setenv("GITHUB_STEP_SUMMARY", out)

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"summary"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("summary file empty")
	}
	if !strings.Contains(string(body), "## Test") {
		t.Errorf("expected header in output, got:\n%s", body)
	}
}

func TestSummary_FileInput_ViaFlag(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "summary.md")
	in := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(in, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	t.Setenv("GITHUB_STEP_SUMMARY", out)

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"summary", "--path", in, "--summary-header", "File Test"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	want := "## File Test\n<details><summary>Click to expand</summary>\n\n```\nhello world\n```\n</details>\n"
	if string(body) != want {
		t.Errorf("unexpected content:\n%s", body)
	}
}

func TestSummary_NestedJSON_Deserializes(t *testing.T) {
	out := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("INPUT_STRING", `{"key": "{\"nested\": \"value\"}"}`)
	t.Setenv("INPUT_SUMMARY_HEADER", "Nested")
	t.Setenv("INPUT_DATA_TYPE", "json")
	t.Setenv("GITHUB_STEP_SUMMARY", out)

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"summary"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "nested") || !strings.Contains(got, "value") {
		t.Errorf("expected deserialized nested JSON in output:\n%s", got)
	}
}

func TestSummary_RequiresExactlyOneOfStringOrPath(t *testing.T) {
	// Make sure ambient INPUT_* from a prior test doesn't bleed in.
	t.Setenv("INPUT_STRING", "")
	t.Setenv("INPUT_PATH", "")

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"summary"})
	cmd.SetOut(new(strings.Builder))
	cmd.SetErr(new(strings.Builder))
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when neither --string nor --path is set")
	}
	if !strings.Contains(err.Error(), "either --string or --path") {
		t.Errorf("unexpected error: %v", err)
	}
}
