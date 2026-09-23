package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStsLint_FailsOnFindings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release.sts.yaml"), []byte("issuer: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INPUT_PATH", dir)

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"sts-lint"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "finding(s)") {
		t.Fatalf("want findings error, got %v", err)
	}
}

func TestStsLint_PassesWithoutPolicies(t *testing.T) {
	t.Setenv("INPUT_PATH", filepath.Join(t.TempDir(), "absent"))

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"sts-lint"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
}
