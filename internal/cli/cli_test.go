package cli

import (
	"encoding/json"
	"github.com/dnd-it/tamci/internal/release/config"
	"github.com/dnd-it/tamci/internal/release/releasepr"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dnd-it/tamci/internal/matrix"
	"github.com/spf13/viper"
)

func TestConfig_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "matrix-config.yaml")
	cfg := `
settings:
  base_dir: services
service:
  api:
    port: 8080
  web:
    port: 3000
environment:
  - dev
  - prod
`
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "output")
	t.Setenv("GITHUB_OUTPUT", outPath)
	t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(dir, "summary"))

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"config", "--config-path", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	output := string(out)
	if !strings.Contains(output, "length=4") {
		t.Errorf("expected 4 matrix entries:\n%s", output)
	}
	if !strings.Contains(output, "dimension=service") {
		t.Errorf("expected default dimension:\n%s", output)
	}
	if !strings.Contains(output, "base_dir=services") {
		t.Errorf("expected base_dir output:\n%s", output)
	}

	// Extract and validate the matrix JSON payload.
	var matrixJSON string
	for line := range strings.SplitSeq(output, "\n") {
		if after, ok := strings.CutPrefix(line, "matrix="); ok {
			matrixJSON = after
		}
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(matrixJSON), &entries); err != nil {
		t.Fatalf("matrix output is not valid JSON: %v\n%s", err, matrixJSON)
	}
	if len(entries) != 4 {
		t.Fatalf("want 4 entries, got %d", len(entries))
	}
	first := entries[0]
	if first["directory"] != "services/"+first["service"].(string) {
		t.Errorf("directory not derived from base_dir + dimension: %v", first)
	}
}

func TestConfig_TargetFilter(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "matrix-config.yaml")
	cfg := "service:\n  api: {}\n  web: {}\nenvironment: [dev]\n"
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "output")
	t.Setenv("GITHUB_OUTPUT", outPath)

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"config", "--config-path", configPath, "--target", "api", "--summary=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out, _ := os.ReadFile(outPath)
	if !strings.Contains(string(out), "length=1") {
		t.Errorf("expected filter to 1 entry:\n%s", out)
	}
}

func TestConfig_InvalidIncludeJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(configPath, []byte("service:\n  api: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"config", "--config-path", configPath, "--include", "{not-json"})
	cmd.SetOut(new(strings.Builder))
	cmd.SetErr(new(strings.Builder))
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid include JSON")
	}
}

func runLockCmd(t *testing.T, args ...string) error {
	t.Helper()
	cmd := NewRootCmd()
	cmd.SetArgs(append([]string{"lock"}, args...))
	cmd.SetOut(new(strings.Builder))
	cmd.SetErr(new(strings.Builder))
	return cmd.Execute()
}

func TestLock_Validation(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "o/r")
	t.Setenv("GITHUB_SHA", "abc")

	if err := runLockCmd(t, "--action", "steal", "--lock-name", "x", "--token", "t"); err == nil {
		t.Error("expected error for invalid action")
	}
	if err := runLockCmd(t, "--action", "acquire", "--token", "t"); err == nil || !strings.Contains(err.Error(), "lock-name") {
		t.Errorf("expected lock-name error, got: %v", err)
	}
	if err := runLockCmd(t, "--action", "acquire", "--lock-name", "x"); err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("expected token error, got: %v", err)
	}

	t.Setenv("GITHUB_REPOSITORY", "")
	if err := runLockCmd(t, "--action", "acquire", "--lock-name", "x", "--token", "t"); err == nil || !strings.Contains(err.Error(), "GITHUB_REPOSITORY") {
		t.Errorf("expected GITHUB_REPOSITORY error, got: %v", err)
	}
}

func TestLock_AcquireRequiresSHA(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "o/r")
	t.Setenv("GITHUB_SHA", "")
	if err := runLockCmd(t, "--action", "acquire", "--lock-name", "x", "--token", "t"); err == nil || !strings.Contains(err.Error(), "GITHUB_SHA") {
		t.Errorf("expected GITHUB_SHA error, got: %v", err)
	}
}

func setViper(kv map[string]any) *viper.Viper {
	v := viper.New()
	for k, val := range kv {
		v.Set(k, val)
	}
	return v
}

func TestLoadSetConfig_Validation(t *testing.T) {
	cases := []struct {
		name    string
		kv      map[string]any
		wantErr string
	}{
		{"no files", map[string]any{"mode": "key"}, "no files to process"},
		{"invalid mode", map[string]any{"files": "a.yaml", "mode": "bogus"}, "invalid mode"},
		{"key mode without keys", map[string]any{"files": "a.yaml", "mode": "key"}, "--keys is required"},
		{"key mode without values", map[string]any{"files": "a.yaml", "mode": "key", "keys": "a.b"}, "--values or --value"},
		{"keys values mismatch", map[string]any{"files": "a.yaml", "mode": "key", "keys": "a\nb", "values": "1"}, "must match"},
		{"image without name", map[string]any{"files": "a.yaml", "mode": "image", "image-tag": "v1"}, "--image-name"},
		{"image without tag", map[string]any{"files": "a.yaml", "mode": "image", "image-name": "api"}, "--image-tag"},
		{"marker without value", map[string]any{"files": "a.yaml", "mode": "marker"}, "--value or --values"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadSetConfig(setViper(tc.kv))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadSetConfig_ValueFanout(t *testing.T) {
	cfg, err := loadSetConfig(setViper(map[string]any{
		"files": "a.yaml",
		"mode":  "key",
		"keys":  "a.b\nc.d",
		"value": "shared",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.values, []string{"shared", "shared"}) {
		t.Errorf("values = %v", cfg.values)
	}
}

func TestLoadSetConfig_MarkerDefaults(t *testing.T) {
	cfg, err := loadSetConfig(setViper(map[string]any{
		"files":  "a.yaml",
		"mode":   "marker",
		"marker": "x-yaml-update",
		"value":  "v2",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.markers, []string{"x-yaml-update"}) {
		t.Errorf("markers = %v", cfg.markers)
	}
	if !reflect.DeepEqual(cfg.markerValues, []string{"v2"}) {
		t.Errorf("markerValues = %v", cfg.markerValues)
	}
}

func TestLoadSetConfig_FilesFromDiscovery(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{
		filepath.Join(dir, "values.yaml"),
		filepath.Join(sub, "values.yaml"),
		filepath.Join(sub, "other.yaml"),
		filepath.Join(sub, "notyaml.txt"),
	} {
		if err := os.WriteFile(f, []byte("a: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg, err := loadSetConfig(setViper(map[string]any{
		"files-from":   dir,
		"files-filter": "values.yaml",
		"mode":         "image",
		"image-name":   "api",
		"image-tag":    "v1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.files) != 2 {
		t.Errorf("files = %v, want the two values.yaml", cfg.files)
	}
	for _, f := range cfg.files {
		if filepath.Base(f) != "values.yaml" {
			t.Errorf("filter leaked file %q", f)
		}
	}
}

func TestParseHelpers(t *testing.T) {
	if got := parseLines(" a \n\n b\n"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("parseLines = %v", got)
	}
	if got := parseLines("  "); got != nil {
		t.Errorf("parseLines blank = %v", got)
	}
	if got := parseCSV("a, b,,c"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("parseCSV = %v", got)
	}
	if got := splitCSV(" x , y "); !reflect.DeepEqual(got, []string{"x", "y"}) {
		t.Errorf("splitCSV = %v", got)
	}
	if got := mergeStringSlices([]string{"a", "b"}, []string{"b", "c"}); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("mergeStringSlices = %v", got)
	}
	owner, repo := splitRepo("acme/widgets")
	if owner != "acme" || repo != "widgets" {
		t.Errorf("splitRepo = %q/%q", owner, repo)
	}
	owner, repo = splitRepo("bogus")
	if owner != "" || repo != "" {
		t.Errorf("splitRepo malformed = %q/%q", owner, repo)
	}
	if got := envify("dry-run"); got != "DRY-RUN" {
		t.Errorf("envify = %q, want hyphens preserved", got)
	}
}

func TestStringOrDefault(t *testing.T) {
	v := setViper(map[string]any{"set": "value"})
	if got := stringOrDefault(v, "set", "def"); got != "value" {
		t.Errorf("got %q", got)
	}
	if got := stringOrDefault(v, "unset", "def"); got != "def" {
		t.Errorf("got %q", got)
	}
}

func TestBuildConfigBlob(t *testing.T) {
	entries := []matrix.Entry{
		{"service": "api", "environment": "dev"},
		{"service": "api", "environment": "prod"},
		{"service": "web", "environment": "dev"},
	}
	blob := buildConfigBlob(entries, []string{"environment", "service"})

	dev, ok := blob["dev"].(map[string]any)
	if !ok {
		t.Fatalf("blob missing dev level: %v", blob)
	}
	if _, ok := dev["api"]; !ok {
		t.Errorf("blob missing dev.api: %v", blob)
	}
}

func TestIsLongValue(t *testing.T) {
	if isLongValue("short") {
		t.Error("short value flagged long")
	}
	if !isLongValue("has\nnewline") {
		t.Error("multiline not flagged")
	}
	if !isLongValue(strings.Repeat("x", 101)) {
		t.Error("101 chars not flagged")
	}
}

func TestPrettyJSON(t *testing.T) {
	if got := prettyJSON(`{"a":1}`); !strings.Contains(got, "\n") {
		t.Errorf("expected indented JSON, got %q", got)
	}
	if got := prettyJSON("not json"); got != "not json" {
		t.Errorf("non-JSON should pass through, got %q", got)
	}
}

func TestRelease_BridgeKeepsRunnerInputsOverFlagDefaults(t *testing.T) {
	// Docker actions receive hyphenated INPUT_* names; the flag defaults must
	// not clobber them.
	t.Setenv("INPUT_RELEASE-MODE", "pr")
	t.Setenv("INPUT_VERSION-STRATEGY", "calver")
	t.Setenv("INPUT_RELEASE_MODE", "")
	t.Setenv("INPUT_VERSION_STRATEGY", "")

	cmd := newReleaseCmd()
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatal(err)
	}
	v := newViper()
	if err := bindFlags(cmd, v); err != nil {
		t.Fatal(err)
	}
	bridgeFlagsToEnv(v, "version-strategy", "release-mode")

	if got := os.Getenv("INPUT_RELEASE-MODE"); got != "pr" {
		t.Errorf("INPUT_RELEASE-MODE = %q, want pr", got)
	}
	if got := os.Getenv("INPUT_VERSION-STRATEGY"); got != "calver" {
		t.Errorf("INPUT_VERSION-STRATEGY = %q, want calver", got)
	}
}

func TestRelease_BridgeExplicitFlag(t *testing.T) {
	t.Setenv("INPUT_RELEASE-MODE", "")
	t.Setenv("INPUT_RELEASE_MODE", "")

	cmd := newReleaseCmd()
	if err := cmd.ParseFlags([]string{"--release-mode", "pr"}); err != nil {
		t.Fatal(err)
	}
	v := newViper()
	if err := bindFlags(cmd, v); err != nil {
		t.Fatal(err)
	}
	bridgeFlagsToEnv(v, "release-mode")

	if got := os.Getenv("INPUT_RELEASE-MODE"); got != "pr" {
		t.Errorf("INPUT_RELEASE-MODE = %q, want pr", got)
	}
}

func TestHandleReleasePRMerge_DryRunDoesNotRelease(t *testing.T) {
	out := filepath.Join(t.TempDir(), "output")
	t.Setenv("GITHUB_OUTPUT", out)

	cfg := config.Config{DryRun: true, ReleaseMode: "pr", TagPrefix: "go-service-v"}
	result := &releasepr.MergeResult{Manifest: &releasepr.Manifest{Version: "1.16.1", Tag: "go-service-v1.16.1"}, PRNumber: 113}

	// A nil PR client and no git repo: any attempt to tag, publish, or clean
	// up would panic or fail, which is exactly what a dry run must avoid.
	if err := handleReleasePRMerge(cfg, nil, result); err != nil {
		t.Fatalf("handleReleasePRMerge: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"version=1.16.1\n", "dry-run=true\n", "release-published=false\n", "tag=\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("outputs missing %q:\n%s", want, got)
		}
	}
}
