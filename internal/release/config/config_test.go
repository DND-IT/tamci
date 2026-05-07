package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.VersionStrategy != "semver" {
		t.Errorf("default strategy = %q, want semver", cfg.VersionStrategy)
	}
	if cfg.TagPrefix != "" {
		t.Errorf("default prefix = %q, want empty", cfg.TagPrefix)
	}
}

func TestLoad_NoFile(t *testing.T) {
	// Run in a temp dir with no .release.yml.
	t.Chdir(t.TempDir())

	// Clear env.
	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.VersionStrategy != "semver" {
		t.Errorf("strategy = %q, want semver", cfg.VersionStrategy)
	}
}

func TestLoad_WithFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, ".release.yml"), []byte(`
version-strategy: calver
tag-prefix: "release-"
draft: true
`), 0644); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.VersionStrategy != "calver" {
		t.Errorf("strategy = %q, want calver", cfg.VersionStrategy)
	}
	if cfg.TagPrefix != "release-" {
		t.Errorf("prefix = %q, want release-", cfg.TagPrefix)
	}
	if !cfg.Draft {
		t.Error("draft should be true")
	}
}

func TestLoad_WithYAMLExtension(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, ".release.yaml"), []byte(`
version-strategy: calver
tag-prefix: "deploy-"
`), 0644); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.VersionStrategy != "calver" {
		t.Errorf("strategy = %q, want calver", cfg.VersionStrategy)
	}
	if cfg.TagPrefix != "deploy-" {
		t.Errorf("prefix = %q, want deploy-", cfg.TagPrefix)
	}
}

func TestLoad_YMLTakesPrecedenceOverYAML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, ".release.yml"), []byte(`
tag-prefix: "from-yml"
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".release.yaml"), []byte(`
tag-prefix: "from-yaml"
`), 0644); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TagPrefix != "from-yml" {
		t.Errorf("prefix = %q, want from-yml (.yml should take precedence)", cfg.TagPrefix)
	}
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, ".release.yml"), []byte(`
version-strategy: calver
tag-prefix: "release-"
`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("INPUT_VERSION_STRATEGY", "calver")
	t.Setenv("INPUT_TAG_PREFIX", "build-")
	for _, k := range []string{"INPUT_CLIFF_CONFIG", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.VersionStrategy != "calver" {
		t.Errorf("strategy = %q, want calver (env override)", cfg.VersionStrategy)
	}
	if cfg.TagPrefix != "build-" {
		t.Errorf("prefix = %q, want build- (env override)", cfg.TagPrefix)
	}
}

func TestLoad_InvalidStrategy(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Setenv("INPUT_VERSION_STRATEGY", "invalid")
	for _, k := range []string{"INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid strategy")
	}
}

func TestLoad_UnknownFields(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, ".release.yml"), []byte(`
version-strategy: semver
unknown-field: "oops"
`), 0644); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unknown field (strict mode)")
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, ".release.yml"), []byte(`{{{not yaml`), 0644); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoad_DryRun(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Setenv("INPUT_DRY_RUN", "true")
	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.DryRun {
		t.Error("dry-run should be true")
	}
}

func TestLoad_Packages(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, ".release.yml"), []byte(`
version-strategy: semver
packages:
  - name: api
    path: services/api
    tag-pattern: "api/v*"
  - name: web
    path: services/web
    tag-pattern: "web/v*"
`), 0644); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Packages) != 2 {
		t.Fatalf("packages = %d, want 2", len(cfg.Packages))
	}
	if cfg.Packages[0].Name != "api" {
		t.Errorf("packages[0].Name = %q, want api", cfg.Packages[0].Name)
	}
	if cfg.Packages[1].Path != "services/web" {
		t.Errorf("packages[1].Path = %q, want services/web", cfg.Packages[1].Path)
	}
}

func TestLoad_ReleaseModePR(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Setenv("INPUT_RELEASE_MODE", "pr")
	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReleaseMode != "pr" {
		t.Errorf("release-mode = %q, want pr", cfg.ReleaseMode)
	}
}

func TestLoad_ReleaseModeDefault(t *testing.T) {
	t.Chdir(t.TempDir())

	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReleaseMode != "direct" {
		t.Errorf("release-mode = %q, want direct", cfg.ReleaseMode)
	}
}

func TestLoad_ReleaseModeInvalid(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Setenv("INPUT_RELEASE_MODE", "invalid")
	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid release-mode")
	}
}

func TestLoad_IncludePath(t *testing.T) {
	t.Chdir(t.TempDir())

	t.Setenv("INPUT_INCLUDE-PATH", "services/python-api/**")
	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.IncludePath != "services/python-api/**" {
		t.Errorf("include-path = %q, want %q", cfg.IncludePath, "services/python-api/**")
	}
}

func TestLoad_IncludePathNotInYAML(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// include-path in .release.yml should be rejected as unknown field.
	if err := os.WriteFile(filepath.Join(dir, ".release.yml"), []byte(`
version-strategy: semver
include-path: "services/api/**"
`), 0644); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN", "INPUT_INCLUDE-PATH", "INPUT_INCLUDE_PATH"} {
		t.Setenv(k, "")
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected error: include-path in .release.yml should be rejected as unknown field")
	}
}

func TestServicePath(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "no path info",
			cfg:  Config{},
			want: "",
		},
		{
			name: "package path",
			cfg:  Config{CurrentPackage: &Package{Path: "services/go-service"}},
			want: "services/go-service",
		},
		{
			name: "include-path glob",
			cfg:  Config{IncludePath: "services/python-api/**"},
			want: "services/python-api",
		},
		{
			name: "include-path single star",
			cfg:  Config{IncludePath: "libs/*"},
			want: "libs",
		},
		{
			name: "include-path bare glob",
			cfg:  Config{IncludePath: "**"},
			want: "",
		},
		{
			name: "package path takes precedence over include-path",
			cfg: Config{
				CurrentPackage: &Package{Path: "services/api"},
				IncludePath:    "services/other/**",
			},
			want: "services/api",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.ServicePath()
			if got != tt.want {
				t.Errorf("ServicePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoad_ReleaseModeFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, ".release.yml"), []byte(`
version-strategy: semver
release-mode: pr
`), 0644); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"INPUT_VERSION_STRATEGY", "INPUT_TAG_PREFIX", "INPUT_CLIFF_CONFIG", "INPUT_RELEASE_MODE", "INPUT_DRAFT", "INPUT_PRERELEASE", "INPUT_DRY_RUN", "INPUT_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReleaseMode != "pr" {
		t.Errorf("release-mode = %q, want pr", cfg.ReleaseMode)
	}
}
