package yamlx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateKeys(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		keys    []string
		values  []string
		want    int // number of changes
		wantErr bool
	}{
		{
			name:   "simple key update",
			yaml:   "app:\n  version: v1.0.0\n",
			keys:   []string{"app.version"},
			values: []string{"v2.0.0"},
			want:   1,
		},
		{
			name:   "nested key update",
			yaml:   "a:\n  b:\n    c: old\n",
			keys:   []string{"a.b.c"},
			values: []string{"new"},
			want:   1,
		},
		{
			name:   "multiple keys",
			yaml:   "x: 1\ny: 2\n",
			keys:   []string{"x", "y"},
			values: []string{"10", "20"},
			want:   2,
		},
		{
			name:   "list index",
			yaml:   "items:\n  - name: a\n    value: old\n",
			keys:   []string{"items.0.value"},
			values: []string{"new"},
			want:   1,
		},
		{
			name:   "no change when same value",
			yaml:   "key: same\n",
			keys:   []string{"key"},
			values: []string{"same"},
			want:   0,
		},
		{
			name:    "key not found",
			yaml:    "key: value\n",
			keys:    []string{"missing.path"},
			values:  []string{"val"},
			wantErr: true,
		},
		{
			name:    "invalid list index",
			yaml:    "items:\n  - a\n",
			keys:    []string{"items.notanumber"},
			values:  []string{"val"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := LoadYAML([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("LoadYAML error: %v", err)
			}

			changes, err := UpdateKeys(doc, tt.keys, tt.values)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateKeys error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(changes) != tt.want {
				t.Errorf("UpdateKeys got %d changes, want %d", len(changes), tt.want)
			}
		})
	}
}

func TestTypeCoercion(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		key      string
		value    string
		wantType string
	}{
		{
			name:     "int stays int",
			yaml:     "replicas: 3\n",
			key:      "replicas",
			value:    "5",
			wantType: "int",
		},
		{
			name:     "bool stays bool",
			yaml:     "enabled: true\n",
			key:      "enabled",
			value:    "false",
			wantType: "bool",
		},
		{
			name:     "string stays string",
			yaml:     "name: hello\n",
			key:      "name",
			value:    "world",
			wantType: "string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, _ := LoadYAML([]byte(tt.yaml))
			changes, _ := UpdateKeys(doc, []string{tt.key}, []string{tt.value})

			if len(changes) == 0 {
				t.Fatal("expected a change")
			}

			switch tt.wantType {
			case "int":
				if _, ok := changes[0].New.(int); !ok {
					t.Errorf("expected int, got %T", changes[0].New)
				}
			case "bool":
				if _, ok := changes[0].New.(bool); !ok {
					t.Errorf("expected bool, got %T", changes[0].New)
				}
			case "string":
				if _, ok := changes[0].New.(string); !ok {
					t.Errorf("expected string, got %T", changes[0].New)
				}
			}
		})
	}
}

func TestUpdateImageTags(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		imageName string
		newTag    string
		want      int
	}{
		{
			name:      "helm style match",
			yaml:      "image:\n  repository: ghcr.io/myorg/webapp\n  tag: v1.0.0\n",
			imageName: "webapp",
			newTag:    "v2.0.0",
			want:      1,
		},
		{
			name:      "helm style no match",
			yaml:      "image:\n  repository: ghcr.io/myorg/other\n  tag: v1.0.0\n",
			imageName: "webapp",
			newTag:    "v2.0.0",
			want:      0,
		},
		{
			name:      "kustomize style match",
			yaml:      "images:\n  - name: ghcr.io/myorg/webapp\n    newTag: v1.0.0\n",
			imageName: "webapp",
			newTag:    "v2.0.0",
			want:      1,
		},
		{
			name:      "same tag no change",
			yaml:      "image:\n  repository: ghcr.io/myorg/webapp\n  tag: v1.0.0\n",
			imageName: "webapp",
			newTag:    "v1.0.0",
			want:      0,
		},
		{
			name:      "exact name match",
			yaml:      "image:\n  repository: webapp\n  tag: v1.0.0\n",
			imageName: "webapp",
			newTag:    "v2.0.0",
			want:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, _ := LoadYAML([]byte(tt.yaml))
			changes := UpdateImageTags(doc, tt.imageName, tt.newTag)

			if len(changes) != tt.want {
				t.Errorf("UpdateImageTags got %d changes, want %d", len(changes), tt.want)
			}
		})
	}
}

func TestFormatPreservation(t *testing.T) {
	t.Run("comments preserved", func(t *testing.T) {
		yaml := `# Top comment
app:
  # Version comment
  version: v1.0.0  # inline
  name: test
`
		doc, _ := LoadYAML([]byte(yaml))
		_, _ = UpdateKeys(doc, []string{"app.version"}, []string{"v2.0.0"})
		result, _ := DumpYAML(doc)

		if !strings.Contains(string(result), "# Top comment") {
			t.Error("top comment not preserved")
		}
		if !strings.Contains(string(result), "# Version comment") {
			t.Error("version comment not preserved")
		}
	})

	t.Run("indentation preserved - 2 space", func(t *testing.T) {
		yaml := `app:
  ports:
    - name: http
      port: 8080
  image:
    tag: v1.0.0
`
		doc, _ := LoadYAML([]byte(yaml))
		_, _ = UpdateKeys(doc, []string{"app.image.tag"}, []string{"v2.0.0"})
		result, _ := DumpYAML(doc)

		if !strings.Contains(string(result), "  ports:") {
			t.Errorf("2-space indentation not preserved, got:\n%s", result)
		}
	})

	t.Run("indentation preserved - 4 space", func(t *testing.T) {
		yaml := `app:
    ports:
        - name: http
          port: 8080
    image:
        tag: v1.0.0
`
		doc, _ := LoadYAML([]byte(yaml))
		_, _ = UpdateKeys(doc, []string{"app.image.tag"}, []string{"v2.0.0"})
		result, _ := DumpYAML(doc)

		if !strings.Contains(string(result), "    ports:") {
			t.Errorf("4-space indentation not preserved, got:\n%s", result)
		}
	})
}

func TestBlankLinesPreserved(t *testing.T) {
	t.Run("key mode preserves blank lines", func(t *testing.T) {
		input := `app:
  version: v1.0.0

  name: test

  replicas: 3
`
		doc, _ := LoadYAML([]byte(input))
		_, _ = UpdateKeys(doc, []string{"app.version"}, []string{"v2.0.0"})
		result, _ := DumpYAML(doc)

		if strings.Count(string(result), "\n\n") != 2 {
			t.Errorf("blank lines not preserved, got:\n%s", result)
		}
		if !strings.Contains(string(result), "v2.0.0") {
			t.Error("value not updated")
		}
	})

	t.Run("image mode preserves blank lines", func(t *testing.T) {
		input := `web:
  image:
    repository: ghcr.io/myorg/webapp
    tag: latest

  replicas: 2

  resources:
    limits:
      cpu: 500m
      memory: 512Mi

  configMap:
    data:
      ENVIRONMENT: "development"
`
		doc, _ := LoadYAML([]byte(input))
		changes := UpdateImageTags(doc, "webapp", "v1.5.0")
		result, _ := DumpYAML(doc)

		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}
		if strings.Count(string(result), "\n\n") != 3 {
			t.Errorf("expected 3 blank line gaps, got %d, result:\n%s",
				strings.Count(string(result), "\n\n"), result)
		}
		if !strings.Contains(string(result), "tag: v1.5.0") {
			t.Errorf("tag not updated, got:\n%s", result)
		}
		if strings.Contains(string(result), "tag: latest") {
			t.Error("old tag still present")
		}
	})

	t.Run("quoted values preserved with blank lines", func(t *testing.T) {
		input := `app:
  name: "hello"

  version: v1.0.0
`
		doc, _ := LoadYAML([]byte(input))
		_, _ = UpdateKeys(doc, []string{"app.name"}, []string{"world"})
		result, _ := DumpYAML(doc)

		if !strings.Contains(string(result), `"world"`) {
			t.Errorf("quoted style not preserved, got:\n%s", result)
		}
		if strings.Count(string(result), "\n\n") != 1 {
			t.Errorf("blank lines not preserved, got:\n%s", result)
		}
	})
}

func TestDiff(t *testing.T) {
	t.Run("shows changes", func(t *testing.T) {
		original := "app:\n  version: v1.0.0\n"
		updated := "app:\n  version: v2.0.0\n"

		diff := Diff("test.yaml", []byte(original), []byte(updated))

		if !strings.Contains(diff, "-  version: v1.0.0") {
			t.Error("diff should show removed line")
		}
		if !strings.Contains(diff, "+  version: v2.0.0") {
			t.Error("diff should show added line")
		}
	})

	t.Run("empty when no changes", func(t *testing.T) {
		content := "app:\n  version: v1.0.0\n"
		diff := Diff("test.yaml", []byte(content), []byte(content))

		if diff != "" {
			t.Error("diff should be empty when no changes")
		}
	})
}

func TestMultipleImageMatches(t *testing.T) {
	yaml := `services:
  api:
    image:
      repository: ghcr.io/myorg/api
      tag: v1.0.0
  web:
    image:
      repository: ghcr.io/myorg/web
      tag: v1.0.0
initContainers:
  - name: migrations
    image:
      repository: ghcr.io/myorg/api
      tag: v1.0.0
`
	doc, _ := LoadYAML([]byte(yaml))
	changes := UpdateImageTags(doc, "api", "v5.0.0")

	if len(changes) != 2 {
		t.Errorf("expected 2 changes, got %d", len(changes))
	}
}

func TestUpdateByMarker(t *testing.T) {
	tests := []struct {
		name   string
		yaml   string
		marker string
		value  string
		want   int
		check  string // substring expected in output
	}{
		{
			name: "basic marker match",
			yaml: `api:
  image_tag: v1.0.0 # x-yaml-update
  name: my-api
`,
			marker: "x-yaml-update",
			value:  "v2.0.0",
			want:   1,
			check:  "image_tag: v2.0.0",
		},
		{
			name: "multiple markers",
			yaml: `api:
  image_tag: v1.0.0 # x-yaml-update
  metadata:
    labels:
      version: v1.0.0 # x-yaml-update
frontend:
  image_tag: v1.0.0 # x-yaml-update
`,
			marker: "x-yaml-update",
			value:  "v3.0.0",
			want:   3,
		},
		{
			name: "no marker no change",
			yaml: `api:
  image_tag: v1.0.0
  name: my-api
`,
			marker: "x-yaml-update",
			value:  "v2.0.0",
			want:   0,
		},
		{
			name: "custom marker",
			yaml: `api:
  image_tag: v1.0.0 # my-custom-marker
  name: my-api # x-yaml-update
`,
			marker: "my-custom-marker",
			value:  "v2.0.0",
			want:   1,
			check:  "image_tag: v2.0.0",
		},
		{
			name: "base marker matches all suffixes",
			yaml: `api:
  image_tag: v1.0.0 # x-yaml-update:api
  replicas: 3 # x-yaml-update:replicas
`,
			marker: "x-yaml-update",
			value:  "v2.0.0",
			want:   2,
		},
		{
			name: "specific suffix only matches that suffix",
			yaml: `api:
  image_tag: v1.0.0 # x-yaml-update:api
frontend:
  image_tag: v2.0.0 # x-yaml-update:frontend
shared:
  version: v0.1.0 # x-yaml-update
`,
			marker: "x-yaml-update:api",
			value:  "v3.0.0",
			want:   1,
			check:  "image_tag: v3.0.0 # x-yaml-update:api",
		},
		{
			name: "same value no change",
			yaml: `api:
  image_tag: v1.0.0 # x-yaml-update
`,
			marker: "x-yaml-update",
			value:  "v1.0.0",
			want:   0,
		},
		{
			name: "preserves comments and blank lines",
			yaml: `# Top comment
api:
  image_tag: v1.0.0 # x-yaml-update

  name: my-api
`,
			marker: "x-yaml-update",
			value:  "v2.0.0",
			want:   1,
			check:  "# Top comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := LoadYAML([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("LoadYAML error: %v", err)
			}

			changes := UpdateByMarker(doc, tt.marker, tt.value)
			if len(changes) != tt.want {
				t.Errorf("UpdateByMarker got %d changes, want %d", len(changes), tt.want)
			}

			if tt.check != "" {
				result, _ := DumpYAML(doc)
				if !strings.Contains(string(result), tt.check) {
					t.Errorf("output missing %q, got:\n%s", tt.check, result)
				}
			}
		})
	}
}

func TestHasMarker(t *testing.T) {
	tests := []struct {
		comment string
		marker  string
		want    bool
	}{
		{"# x-yaml-update", "x-yaml-update", true},
		{"#x-yaml-update", "x-yaml-update", true},
		{"#  x-yaml-update", "x-yaml-update", true},
		{"# x-yaml-update:image-tag", "x-yaml-update", true},
		{"# x-yaml-update:replicas", "x-yaml-update", true},
		{"# something-else", "x-yaml-update", false},
		{"# x-yaml-updater", "x-yaml-update", false},
		{"", "x-yaml-update", false},
		{"# my-marker", "my-marker", true},
		// specific suffix targeting
		{"# x-yaml-update:api", "x-yaml-update:api", true},
		{"# x-yaml-update:frontend", "x-yaml-update:api", false},
		{"# x-yaml-update", "x-yaml-update:api", false},
	}

	for _, tt := range tests {
		t.Run(tt.comment, func(t *testing.T) {
			got := hasMarker(tt.comment, tt.marker)
			if got != tt.want {
				t.Errorf("hasMarker(%q, %q) = %v, want %v", tt.comment, tt.marker, got, tt.want)
			}
		})
	}
}

func TestLoadFromFile(t *testing.T) {
	// Create a temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	content := "app:\n  version: v1.0.0\n"
	_ = os.WriteFile(path, []byte(content), 0644)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := LoadYAML(data)
	if err != nil {
		t.Fatal(err)
	}

	changes, err := UpdateKeys(doc, []string{"app.version"}, []string{"v2.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 1 {
		t.Errorf("expected 1 change, got %d", len(changes))
	}
}

func TestAnchoredAndTaggedScalarsWritten(t *testing.T) {
	tests := []struct {
		name   string
		yaml   string
		update func(doc *Document) error
		want   string
	}{
		{
			name: "marker on anchored double-quoted scalar",
			yaml: `_initImage: &initImage "repo/app:1.8.1"  # x-yaml-update
image: *initImage
`,
			update: func(doc *Document) error {
				UpdateByMarker(doc, "x-yaml-update", "repo/app:9.9.9")
				return nil
			},
			want: `_initImage: &initImage "repo/app:9.9.9"  # x-yaml-update
image: *initImage
`,
		},
		{
			name: "marker on explicitly tagged scalar",
			yaml: "version: !!str v1.0.0 # x-yaml-update\n",
			update: func(doc *Document) error {
				UpdateByMarker(doc, "x-yaml-update", "v2.0.0")
				return nil
			},
			want: "version: !!str v2.0.0 # x-yaml-update\n",
		},
		{
			name: "key on anchored and tagged scalar",
			yaml: "app:\n  tag: &tag !!str tag\n",
			update: func(doc *Document) error {
				_, err := UpdateKeys(doc, []string{"app.tag"}, []string{"v2"})
				return err
			},
			want: "app:\n  tag: &tag !!str v2\n",
		},
		{
			name: "image tag with anchor",
			yaml: "image:\n  repository: ghcr.io/org/app\n  tag: &appTag 'v1'\n",
			update: func(doc *Document) error {
				UpdateImageTags(doc, "app", "v2")
				return nil
			},
			want: "image:\n  repository: ghcr.io/org/app\n  tag: &appTag 'v2'\n",
		},
		{
			name: "same anchored scalar matched by two markers",
			yaml: "tag: &t v1 # x-yaml-update:api\n",
			update: func(doc *Document) error {
				UpdateByMarker(doc, "x-yaml-update", "v2")
				UpdateByMarker(doc, "x-yaml-update:api", "v3")
				return nil
			},
			want: "tag: &t v3 # x-yaml-update:api\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := LoadYAML([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("LoadYAML error: %v", err)
			}
			if err := tt.update(doc); err != nil {
				t.Fatalf("update error: %v", err)
			}
			result, err := DumpYAML(doc)
			if err != nil {
				t.Fatalf("DumpYAML error: %v", err)
			}
			if string(result) != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", result, tt.want)
			}
		})
	}
}

func TestDumpYAMLErrorsOnUnappliedEdit(t *testing.T) {
	doc, err := LoadYAML([]byte("base: &b v1\napp:\n  tag: *b\n"))
	if err != nil {
		t.Fatalf("LoadYAML error: %v", err)
	}
	if _, err := UpdateKeys(doc, []string{"app.tag"}, []string{"v2"}); err != nil {
		t.Fatalf("UpdateKeys error: %v", err)
	}
	if _, err := DumpYAML(doc); err == nil {
		t.Fatal("expected an error for an edit that cannot be applied, got nil")
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		name   string
		yaml   string
		update func(*Document)
		want   int
	}{
		{
			name:   "image already at tag",
			yaml:   "image:\n  repository: ghcr.io/myorg/webapp\n  tag: v1.0.0\n",
			update: func(d *Document) { UpdateImageTags(d, "webapp", "v1.0.0") },
			want:   1,
		},
		{
			name:   "image not found",
			yaml:   "image:\n  repository: ghcr.io/myorg/other\n  tag: v1.0.0\n",
			update: func(d *Document) { UpdateImageTags(d, "webapp", "v2.0.0") },
			want:   0,
		},
		{
			name:   "image in scalar anchor",
			yaml:   "x-image: &image ghcr.io/myorg/webapp:v1.0.0\nworker:\n  image: *image\n",
			update: func(d *Document) { UpdateImageTags(d, "webapp", "v2.0.0") },
			want:   0,
		},
		{
			name:   "kustomize already at tag",
			yaml:   "images:\n  - name: ghcr.io/myorg/webapp\n    newTag: v1.0.0\n",
			update: func(d *Document) { UpdateImageTags(d, "webapp", "v1.0.0") },
			want:   1,
		},
		{
			name:   "marker already at value",
			yaml:   "tag: v1.0.0 # x-yaml-update\n",
			update: func(d *Document) { UpdateByMarker(d, "x-yaml-update", "v1.0.0") },
			want:   1,
		},
		{
			name:   "marker not found",
			yaml:   "tag: v1.0.0\n",
			update: func(d *Document) { UpdateByMarker(d, "x-yaml-update", "v2.0.0") },
			want:   0,
		},
		{
			name:   "key already at value",
			yaml:   "app:\n  version: v1.0.0\n",
			update: func(d *Document) { _, _ = UpdateKeys(d, []string{"app.version"}, []string{"v1.0.0"}) },
			want:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := LoadYAML([]byte(tt.yaml))
			if err != nil {
				t.Fatal(err)
			}
			tt.update(doc)
			if got := doc.Matches(); got != tt.want {
				t.Errorf("Matches() = %d, want %d", got, tt.want)
			}
		})
	}
}
