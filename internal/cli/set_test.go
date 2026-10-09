package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runSetCmd(t *testing.T, args ...string) error {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GITHUB_OUTPUT", filepath.Join(dir, "output"))
	cmd := NewRootCmd()
	cmd.SetArgs(append([]string{"set", "--dry-run"}, args...))
	return cmd.Execute()
}

func writeValues(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSet_ImageNoMatchFails(t *testing.T) {
	// Regression for #40: an image kept in a scalar anchor is invisible to
	// mode=image, which used to log "No changes needed" and exit 0.
	path := writeValues(t, "x-image: &image ghcr.io/dnd-it/api:1.7.0\nworker:\n  image: *image\n")

	err := runSetCmd(t, "--files", path, "--mode", "image", "--image-name", "api", "--image-tag", "1.8.0")
	if err == nil {
		t.Fatal("expected an error when nothing matches")
	}
	if !strings.Contains(err.Error(), `image "api"`) {
		t.Errorf("error should name the image: %v", err)
	}
}

func TestSet_MarkerNoMatchFails(t *testing.T) {
	path := writeValues(t, "image:\n  tag: 1.0.0\n")

	if err := runSetCmd(t, "--files", path, "--mode", "marker", "--value", "1.4.0"); err == nil {
		t.Fatal("expected an error when no marker matches")
	}
}

func TestSet_NoMatchAllowed(t *testing.T) {
	path := writeValues(t, "image:\n  repository: ghcr.io/dnd-it/other\n  tag: 1.0.0\n")

	err := runSetCmd(t, "--files", path, "--mode", "image", "--image-name", "api", "--image-tag", "1.8.0", "--fail-on-no-match=false")
	if err != nil {
		t.Fatalf("expected success with --fail-on-no-match=false: %v", err)
	}
}

func TestSet_AlreadyUpToDateSucceeds(t *testing.T) {
	path := writeValues(t, "image:\n  repository: ghcr.io/dnd-it/api\n  tag: 1.8.0\n")

	if err := runSetCmd(t, "--files", path, "--mode", "image", "--image-name", "api", "--image-tag", "1.8.0"); err != nil {
		t.Fatalf("a matching node that already holds the value is not a failure: %v", err)
	}
}

func TestSet_PartialMatchSucceeds(t *testing.T) {
	matched := writeValues(t, "image:\n  repository: ghcr.io/dnd-it/api\n  tag: 1.7.0\n")
	unmatched := writeValues(t, "image:\n  repository: ghcr.io/dnd-it/other\n  tag: 1.0.0\n")

	err := runSetCmd(t, "--files", matched+"\n"+unmatched, "--mode", "image", "--image-name", "api", "--image-tag", "1.8.0")
	if err != nil {
		t.Fatalf("one matching file is enough: %v", err)
	}
}
