package yamlx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests were ported from action-deployer/internal/values/values_test.go
// to lock the file-level SetTag/ReadTag/HasTarget behaviour against the merged engine.

func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

func TestSetTag_ImageMode_Single(t *testing.T) {
	in := `image:
  repository: myrepo/app
  tag: 1.0.0
replicas: 2
`
	path := writeFile(t, in)
	n, err := SetTag(path, "2.0.0", UpdateOptions{Mode: "image"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 replacement, got %d", n)
	}
	got := readFile(t, path)
	want := `image:
  repository: myrepo/app
  tag: 2.0.0
replicas: 2
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetTag_ImageMode_Multiple(t *testing.T) {
	in := `web:
  image:
    repository: myrepo/web
    tag: 1.0.0
worker:
  image:
    repository: myrepo/worker
    tag: 1.0.0
`
	path := writeFile(t, in)
	n, err := SetTag(path, "2.0.0", UpdateOptions{Mode: "image"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 replacements, got %d", n)
	}
	got := readFile(t, path)
	if strings.Count(got, "tag: 2.0.0") != 2 {
		t.Errorf("want 2 tag lines, got:\n%s", got)
	}
	if strings.Contains(got, "1.0.0") {
		t.Errorf("old tag still present:\n%s", got)
	}
}

func TestSetTag_ImageMode_NoBlock(t *testing.T) {
	path := writeFile(t, "foo: bar\nbaz: qux\n")
	_, err := SetTag(path, "2.0.0", UpdateOptions{Mode: "image"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "no image block") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSetTag_ImageMode_PreservesFormatting(t *testing.T) {
	in := `# Top-level comment
image:
  repository: myrepo/app   # pinned
  tag: 1.0.0  # bump me
  pullPolicy: IfNotPresent

# Another comment
replicas: 3
`
	path := writeFile(t, in)
	if _, err := SetTag(path, "2.0.0", UpdateOptions{Mode: "image"}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "# Top-level comment") {
		t.Error("top comment lost")
	}
	if !strings.Contains(got, "# pinned") {
		t.Error("inline comment on repository lost")
	}
	if !strings.Contains(got, "# bump me") {
		t.Error("inline comment on tag lost")
	}
	if !strings.Contains(got, "tag: 2.0.0") {
		t.Errorf("tag not updated:\n%s", got)
	}
}

func TestSetTag_KeyMode_Basic(t *testing.T) {
	in := "image:\n  tag: 1.0.0\n"
	path := writeFile(t, in)
	n, err := SetTag(path, "9.9.9", UpdateOptions{Mode: "key", Key: "image.tag"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
	if got := readFile(t, path); !strings.Contains(got, "tag: 9.9.9") {
		t.Errorf("got:\n%s", got)
	}
}

func TestSetTag_KeyMode_Nested(t *testing.T) {
	in := `web:
  image:
    repository: r
    tag: old
`
	path := writeFile(t, in)
	if _, err := SetTag(path, "new", UpdateOptions{Mode: "key", Key: "web.image.tag"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); !strings.Contains(got, "tag: new") {
		t.Errorf("got:\n%s", got)
	}
}

func TestSetTag_KeyMode_Missing(t *testing.T) {
	path := writeFile(t, "foo: bar\n")
	_, err := SetTag(path, "x", UpdateOptions{Mode: "key", Key: "image.tag"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected: %v", err)
	}
}

func TestSetTag_KeyMode_EmptyKey(t *testing.T) {
	path := writeFile(t, "image:\n  tag: 1.0.0\n")
	_, err := SetTag(path, "x", UpdateOptions{Mode: "key", Key: ""})
	if err == nil {
		t.Fatal("want error for empty key")
	}
}

func TestSetTag_MarkerMode_Basic(t *testing.T) {
	in := "replicas: 3\nimage:\n  tag: 1.0.0 # x-yaml-update\n"
	path := writeFile(t, in)
	n, err := SetTag(path, "9.9.9", UpdateOptions{Mode: "marker"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "tag: 9.9.9") {
		t.Errorf("tag not replaced:\n%s", got)
	}
	if !strings.Contains(got, "# x-yaml-update") {
		t.Errorf("marker lost:\n%s", got)
	}
}

func TestSetTag_MarkerMode_Missing(t *testing.T) {
	path := writeFile(t, "image:\n  tag: 1.0.0\n")
	_, err := SetTag(path, "x", UpdateOptions{Mode: "marker"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "marker") {
		t.Errorf("unexpected: %v", err)
	}
}

func TestReadTag_ImageMode_Found(t *testing.T) {
	path := writeFile(t, "image:\n  repository: r\n  tag: 3.1.4\n")
	got, err := ReadTag(path, UpdateOptions{Mode: "image"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "3.1.4" {
		t.Errorf("got %q, want 3.1.4", got)
	}
}

func TestReadTag_ImageMode_Missing(t *testing.T) {
	path := writeFile(t, "foo: bar\n")
	got, err := ReadTag(path, UpdateOptions{Mode: "image"})
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestReadTag_FileMissing(t *testing.T) {
	got, err := ReadTag("/no/such/file.yaml", UpdateOptions{Mode: "image"})
	if err != nil {
		t.Fatalf("want nil error for missing file, got %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestReadTag_KeyMode(t *testing.T) {
	path := writeFile(t, "web:\n  image:\n    tag: 5.5.5\n")
	got, err := ReadTag(path, UpdateOptions{Mode: "key", Key: "web.image.tag"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "5.5.5" {
		t.Errorf("got %q, want 5.5.5", got)
	}
}
