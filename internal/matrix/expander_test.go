package matrix

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestParseConfigFile_InvalidJSON(t *testing.T) {
	path := filepath.Join("testdata", "invalid-config.json")
	_, err := ParseConfigFile(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseConfigFile_MissingFile(t *testing.T) {
	_, err := ParseConfigFile("nonexistent.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseConfigFile_UnsupportedExtension(t *testing.T) {
	tmp, err := os.CreateTemp("", "config-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	_, _ = tmp.WriteString(`{"key": "value"}`)
	_ = tmp.Close()

	_, err = ParseConfigFile(tmp.Name())
	if err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestParseConfigFile_NonObjectConfig(t *testing.T) {
	tmp, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	_, _ = tmp.WriteString(`["not", "an", "object"]`)
	_ = tmp.Close()

	_, err = ParseConfigFile(tmp.Name())
	if err == nil {
		t.Fatal("expected error for non-object config")
	}
}

func TestParseConfigFile_ValidJSON(t *testing.T) {
	path := filepath.Join("testdata", "valid-list-config.json")
	raw, err := ParseConfigFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestParseConfigFile_ValidYAML(t *testing.T) {
	path := filepath.Join("testdata", "valid-list-config.yml")
	raw, err := ParseConfigFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestParseOptions_DefaultDimension(t *testing.T) {
	raw := RawConfig{
		"service": []any{"api", "frontend"},
	}

	optsCfg, dims := ParseOptions(raw)

	if optsCfg.Dimension != "" {
		t.Errorf("expected empty default dimension, got %q", optsCfg.Dimension)
	}
	if _, ok := dims["service"]; !ok {
		t.Error("expected 'service' in dimensions")
	}
}

func TestParseOptions_CustomDimension(t *testing.T) {
	raw := RawConfig{
		"settings": map[string]any{
			"dimension": "app",
			"base_dir":  "deploy",
		},
		"app": []any{"web", "worker"},
	}

	optsCfg, dims := ParseOptions(raw)

	if optsCfg.Dimension != "app" {
		t.Errorf("expected dimension 'app', got %q", optsCfg.Dimension)
	}
	if optsCfg.BaseDir != "deploy" {
		t.Errorf("expected base_dir 'deploy', got %q", optsCfg.BaseDir)
	}
	if _, ok := dims["settings"]; ok {
		t.Error("settings should not appear in dimensions")
	}
	if _, ok := dims["app"]; !ok {
		t.Error("expected 'app' in dimensions")
	}
}

func TestParseOptions_MissingSettings(t *testing.T) {
	raw := RawConfig{
		"service": []any{"api"},
	}

	optsCfg, _ := ParseOptions(raw)

	if optsCfg.Dimension != "" {
		t.Errorf("expected empty dimension when no settings, got %q", optsCfg.Dimension)
	}
	if optsCfg.BaseDir != "" {
		t.Errorf("expected empty base_dir, got %q", optsCfg.BaseDir)
	}
}

func TestParseOptions_GlobalConfigValues(t *testing.T) {
	raw := RawConfig{
		"settings": map[string]any{
			"dimension": "service",
		},
		"global": map[string]any{
			"aws_region": "us-east-1",
			"timeout":    "30",
		},
		"service": []any{"api"},
	}

	optsCfg, dims := ParseOptions(raw)

	if optsCfg.GlobalConfig == nil {
		t.Fatal("expected GlobalConfig to be set")
	}
	if optsCfg.GlobalConfig["aws_region"] != "us-east-1" {
		t.Errorf("expected aws_region 'us-east-1', got %v", optsCfg.GlobalConfig["aws_region"])
	}
	if optsCfg.GlobalConfig["timeout"] != "30" {
		t.Errorf("expected timeout '30', got %v", optsCfg.GlobalConfig["timeout"])
	}
	if _, ok := dims["global"]; ok {
		t.Error("global should not appear in dimensions")
	}
	if _, ok := dims["settings"]; ok {
		t.Error("settings should not appear in dimensions")
	}
}

func TestParseOptions_WithExcludeInclude(t *testing.T) {
	raw := RawConfig{
		"global": map[string]any{
			"aws_region": "us-east-1",
		},
		"exclude": []any{
			map[string]any{"service": "shared", "environment": "dev"},
		},
		"include": []any{
			map[string]any{"service": "shared", "environment": "all"},
		},
		"environment": map[string]any{
			"dev": map[string]any{"aws_account_id": "111"},
		},
		"service": []any{"api", "shared"},
	}

	optsCfg, dims := ParseOptions(raw)

	if optsCfg.GlobalConfig == nil {
		t.Fatal("expected GlobalConfig to be set")
	}
	if len(optsCfg.Exclude) != 1 {
		t.Fatalf("expected 1 exclude pattern, got %d", len(optsCfg.Exclude))
	}
	if len(optsCfg.Include) != 1 {
		t.Fatalf("expected 1 include entry, got %d", len(optsCfg.Include))
	}
	for _, key := range []string{"global", "exclude", "include"} {
		if _, ok := dims[key]; ok {
			t.Errorf("%s should not appear in dimensions", key)
		}
	}
}

func TestExpand_BasicExpansion(t *testing.T) {
	dims := RawConfig{
		"service": map[string]any{"api": nil, "frontend": nil},
		"environment": map[string]any{
			"dev":  map[string]any{"aws_account_id": "111111111111"},
			"prod": map[string]any{"aws_account_id": "222222222222"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2 services x 2 environments = 4 entries
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	for _, entry := range entries {
		if _, ok := entry["service"]; !ok {
			t.Error("entry missing 'service' field")
		}
		if _, ok := entry["environment"]; !ok {
			t.Error("entry missing 'environment' field")
		}
		if _, ok := entry["aws_account_id"]; !ok {
			t.Error("entry missing 'aws_account_id' field")
		}
		if _, ok := entry["directory"]; !ok {
			t.Error("entry missing 'directory' field")
		}
	}
}

func TestExpand_PerDimensionValueConfig(t *testing.T) {
	dims := RawConfig{
		"service": map[string]any{
			"api": map[string]any{"port": "8080"},
		},
		"environment": map[string]any{
			"dev": map[string]any{"aws_account_id": "111111111111"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e["port"] != "8080" {
		t.Errorf("expected port '8080' from service per-value config, got %v", e["port"])
	}
	if e["aws_account_id"] != "111111111111" {
		t.Errorf("expected aws_account_id '111111111111' from environment per-value config, got %v", e["aws_account_id"])
	}
}

func TestExpand_PerDimensionValueConfigOverrideOrder(t *testing.T) {
	// Per-dimension-value configs merge in alphabetical dimension key order.
	// "service" comes after "environment" alphabetically, so service config overrides.
	dims := RawConfig{
		"service": map[string]any{
			"api": map[string]any{"port": "8080", "region": "us-west-2"},
		},
		"environment": map[string]any{
			"dev": map[string]any{"region": "us-east-1"},
		},
	}
	optsCfg := OptionsConfig{Dimension: "service"}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// "service" is alphabetically after "environment", so service's region wins
	if entries[0]["region"] != "us-west-2" {
		t.Errorf("expected region 'us-west-2' (service overrides environment), got %v", entries[0]["region"])
	}
}

func TestExpand_JSONFixture(t *testing.T) {
	path := filepath.Join("testdata", "valid-list-config.json")
	raw, err := ParseConfigFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	optsCfg, dims := ParseOptions(raw)
	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
}

func TestExpand_YAMLFixture(t *testing.T) {
	path := filepath.Join("testdata", "valid-list-config.yml")
	raw, err := ParseConfigFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	optsCfg, dims := ParseOptions(raw)
	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
}

func TestExpand_JSONAndYAMLProduceSameResult(t *testing.T) {
	jsonPath := filepath.Join("testdata", "valid-list-config.json")
	yamlPath := filepath.Join("testdata", "valid-list-config.yml")

	jsonRaw, err := ParseConfigFile(jsonPath)
	if err != nil {
		t.Fatalf("unexpected error parsing JSON: %v", err)
	}
	yamlRaw, err := ParseConfigFile(yamlPath)
	if err != nil {
		t.Fatalf("unexpected error parsing YAML: %v", err)
	}

	jsonOptsCfg, jsonDims := ParseOptions(jsonRaw)
	yamlOptsCfg, yamlDims := ParseOptions(yamlRaw)

	jsonEntries, err := Expand(jsonDims, jsonOptsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error expanding JSON: %v", err)
	}
	yamlEntries, err := Expand(yamlDims, yamlOptsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error expanding YAML: %v", err)
	}

	if len(jsonEntries) != len(yamlEntries) {
		t.Fatalf("JSON entries (%d) != YAML entries (%d)", len(jsonEntries), len(yamlEntries))
	}

	sortEntries := func(entries []Entry) {
		sort.Slice(entries, func(i, j int) bool {
			si := entries[i]["service"].(string) + entries[i]["environment"].(string)
			sj := entries[j]["service"].(string) + entries[j]["environment"].(string)
			return si < sj
		})
	}
	sortEntries(jsonEntries)
	sortEntries(yamlEntries)

	jsonJSON, _ := json.Marshal(jsonEntries)
	yamlJSON, _ := json.Marshal(yamlEntries)

	if string(jsonJSON) != string(yamlJSON) {
		t.Errorf("JSON and YAML results differ:\nJSON: %s\nYAML: %s", jsonJSON, yamlJSON)
	}
}

func TestExpand_OptionsExclude(t *testing.T) {
	dims := RawConfig{
		"service": map[string]any{"api": nil, "shared": nil},
		"environment": map[string]any{
			"dev":  map[string]any{"aws_account_id": "111111111111"},
			"prod": map[string]any{"aws_account_id": "222222222222"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
		Exclude: []Entry{
			{"service": "shared", "environment": "dev"},
		},
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2x2=4 minus 1 excluded = 3
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	for _, entry := range entries {
		if entry["service"] == "shared" && entry["environment"] == "dev" {
			t.Error("shared/dev should have been excluded")
		}
	}
}

func TestExpand_OptionsInclude(t *testing.T) {
	dims := RawConfig{
		"service": []any{"api"},
		"environment": map[string]any{
			"dev": map[string]any{"aws_account_id": "111111111111"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
		Include: []Entry{
			{"service": "shared", "environment": "all"},
		},
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1x1=1 plus 1 included = 2
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	found := false
	for _, entry := range entries {
		if entry["service"] == "shared" && entry["environment"] == "all" {
			found = true
		}
	}
	if !found {
		t.Error("shared/all should have been included")
	}
}

func TestExpand_InputDimensionFilter(t *testing.T) {
	dims := RawConfig{
		"service": []any{"api", "frontend", "backend"},
		"environment": map[string]any{
			"dev": map[string]any{"aws_account_id": "111111111111"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{
		FilterKey:    "service",
		FilterValues: []string{"api", "frontend"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	for _, entry := range entries {
		svc := entry["service"].(string)
		if svc != "api" && svc != "frontend" {
			t.Errorf("unexpected service %q", svc)
		}
	}
}

func TestExpand_InputEnvironmentFilter(t *testing.T) {
	dims := RawConfig{
		"service": []any{"api"},
		"environment": map[string]any{
			"dev":     map[string]any{"aws_account_id": "111111111111"},
			"staging": map[string]any{"aws_account_id": "222222222222"},
			"prod":    map[string]any{"aws_account_id": "333333333333"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{
		EnvironmentFilter: []string{"dev", "prod"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	for _, entry := range entries {
		env := entry["environment"].(string)
		if env != "dev" && env != "prod" {
			t.Errorf("unexpected environment %q", env)
		}
	}
}

func TestExpand_InputExclude(t *testing.T) {
	dims := RawConfig{
		"service": map[string]any{"api": nil, "frontend": nil},
		"environment": map[string]any{
			"dev":  map[string]any{"aws_account_id": "111111111111"},
			"prod": map[string]any{"aws_account_id": "222222222222"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{
		InputExclude: []Entry{
			{"service": "api", "environment": "dev"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestExpand_InputInclude(t *testing.T) {
	dims := RawConfig{
		"service": []any{"api"},
		"environment": map[string]any{
			"dev": map[string]any{"aws_account_id": "111111111111"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{
		InputInclude: []Entry{
			{"service": "monitoring", "environment": "global"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestExpand_NoDimensions(t *testing.T) {
	dims := RawConfig{
		"app_name": "myapp",
		"version":  "1.0",
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0]["app_name"] != "myapp" {
		t.Errorf("expected app_name 'myapp', got %v", entries[0]["app_name"])
	}
	if entries[0]["version"] != "1.0" {
		t.Errorf("expected version '1.0', got %v", entries[0]["version"])
	}
}

func TestExpand_MultipleCustomDimensions(t *testing.T) {
	dims := RawConfig{
		"service": []any{"api", "frontend"},
		"region":  []any{"us-east-1", "eu-west-1"},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2 services x 2 regions = 4 entries
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	for _, entry := range entries {
		if _, ok := entry["service"]; !ok {
			t.Error("entry missing 'service' field")
		}
		if _, ok := entry["region"]; !ok {
			t.Error("entry missing 'region' field")
		}
	}
}

func TestExpand_BaseConfigPropagated(t *testing.T) {
	dims := RawConfig{
		"service": []any{"api"},
		"environment": map[string]any{
			"dev": map[string]any{"aws_account_id": "111111111111"},
		},
		"app_name": "myapp",
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0]["app_name"] != "myapp" {
		t.Errorf("expected app_name 'myapp', got %v", entries[0]["app_name"])
	}
	if entries[0]["service"] != "api" {
		t.Errorf("expected service 'api', got %v", entries[0]["service"])
	}
	if entries[0]["environment"] != "dev" {
		t.Errorf("expected environment 'dev', got %v", entries[0]["environment"])
	}
}

func TestExpand_MapDimensionOnlySpecifiedValues(t *testing.T) {
	dims := RawConfig{
		"service": []any{"api"},
		"environment": map[string]any{
			"staging": map[string]any{"aws_account_id": "222222222222"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 1 service x 1 environment = 1 entry
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0]["environment"] != "staging" {
		t.Errorf("expected environment 'staging', got %v", entries[0]["environment"])
	}
	if entries[0]["aws_account_id"] != "222222222222" {
		t.Errorf("expected aws_account_id '222222222222', got %v", entries[0]["aws_account_id"])
	}
}

func TestExpand_ExcludePartialMatch(t *testing.T) {
	dims := RawConfig{
		"service": map[string]any{"api": nil, "frontend": nil},
		"environment": map[string]any{
			"dev":  map[string]any{"aws_account_id": "111111111111"},
			"prod": map[string]any{"aws_account_id": "222222222222"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
		Exclude: []Entry{
			{"environment": "dev"},
		},
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should exclude all dev entries: 4-2=2
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	for _, entry := range entries {
		if entry["environment"] == "dev" {
			t.Error("dev entries should have been excluded")
		}
	}
}

func TestExpand_EmptyDimensionArray(t *testing.T) {
	dims := RawConfig{
		"service": []any{},
		"environment": map[string]any{
			"dev": map[string]any{"aws_account_id": "111111111111"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 0 services x 1 env = 0 entries
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}

	data, _ := json.Marshal(entries)
	if string(data) != "[]" {
		t.Errorf("expected JSON \"[]\", got %q", string(data))
	}
}

func TestExpand_EmptyResultMarshalsToEmptyArray(t *testing.T) {
	dims := RawConfig{
		"service": map[string]any{"api": nil},
		"environment": map[string]any{
			"prod": map[string]any{"aws_account_id": "111111111111"},
		},
	}
	optsCfg := OptionsConfig{Dimension: "service"}
	// Filter by environment=dev which doesn't exist → 0 entries
	opts := Options{EnvironmentFilter: []string{"dev"}}

	entries, err := Expand(dims, optsCfg, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}

	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("expected JSON \"[]\", got %q", string(data))
	}
}

func TestExpand_DimensionsUsedAsIs(t *testing.T) {
	dims := RawConfig{
		"services": []any{"web", "worker"},
	}
	optsCfg := OptionsConfig{
		Dimension: "services",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	for _, entry := range entries {
		if _, ok := entry["services"]; !ok {
			t.Error("entry should use key 'services' as-is")
		}
	}
}

func TestExpand_DirectoryFieldWithBaseDir(t *testing.T) {
	dims := RawConfig{
		"service": map[string]any{"api": nil, "frontend": nil},
		"environment": map[string]any{
			"dev": map[string]any{"aws_account_id": "111111111111"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
		BaseDir:      "deploy",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, entry := range entries {
		dir, ok := entry["directory"].(string)
		if !ok {
			t.Fatal("entry missing 'directory' field")
		}
		svc := entry["service"].(string)
		expected := "deploy/" + svc
		if dir != expected {
			t.Errorf("expected directory %q, got %q", expected, dir)
		}
	}
}

func TestExpand_DirectoryFieldWithoutBaseDir(t *testing.T) {
	dims := RawConfig{
		"service": []any{"api"},
		"environment": map[string]any{
			"dev": map[string]any{"aws_account_id": "111111111111"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	dir, ok := entries[0]["directory"].(string)
	if !ok {
		t.Fatal("entry missing 'directory' field")
	}
	if dir != "api" {
		t.Errorf("expected directory 'api', got %q", dir)
	}
}

func TestExpand_GlobalConfig(t *testing.T) {
	dims := RawConfig{
		"service": []any{"api", "frontend"},
		"environment": map[string]any{
			"dev": map[string]any{
				"aws_account_id": "111111111111",
			},
			"prod": map[string]any{
				"aws_account_id": "222222222222",
				"aws_region":     "us-west-2",
			},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
		GlobalConfig: map[string]any{
			"aws_region": "us-east-1",
			"timeout":    "30",
		},
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2 services x 2 envs = 4
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	for _, entry := range entries {
		env := entry["environment"].(string)
		// All entries should have timeout from global
		if entry["timeout"] != "30" {
			t.Errorf("expected timeout '30' from global, got %v", entry["timeout"])
		}
		// dev entries should have us-east-1 from global (not overridden)
		if env == "dev" && entry["aws_region"] != "us-east-1" {
			t.Errorf("expected aws_region 'us-east-1' for dev (from global), got %v", entry["aws_region"])
		}
		// prod entries should override global aws_region
		if env == "prod" && entry["aws_region"] != "us-west-2" {
			t.Errorf("expected aws_region 'us-west-2' for prod (override), got %v", entry["aws_region"])
		}
	}
}

func TestExpand_SortByDefault(t *testing.T) {
	dims := RawConfig{
		"service": []any{"frontend", "api"},
		"environment": map[string]any{
			"prod": map[string]any{"aws_account_id": "222222222222"},
			"dev":  map[string]any{"aws_account_id": "111111111111"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Default sort_by is ["environment"], so entries should be sorted by environment
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	// First two should be dev, last two should be prod
	if entries[0]["environment"] != "dev" {
		t.Errorf("expected first entry environment 'dev', got %v", entries[0]["environment"])
	}
	if entries[1]["environment"] != "dev" {
		t.Errorf("expected second entry environment 'dev', got %v", entries[1]["environment"])
	}
	if entries[2]["environment"] != "prod" {
		t.Errorf("expected third entry environment 'prod', got %v", entries[2]["environment"])
	}
	if entries[3]["environment"] != "prod" {
		t.Errorf("expected fourth entry environment 'prod', got %v", entries[3]["environment"])
	}
}

func TestExpand_SortByCustom(t *testing.T) {
	dims := RawConfig{
		"service": []any{"frontend", "api"},
		"environment": map[string]any{
			"prod": map[string]any{"aws_account_id": "222222222222"},
			"dev":  map[string]any{"aws_account_id": "111111111111"},
		},
	}
	optsCfg := OptionsConfig{
		Dimension: "service",
		SortBy:       []string{"service", "environment"},
	}

	entries, err := Expand(dims, optsCfg, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	// Should be sorted by service first, then environment
	expected := []struct{ svc, env string }{
		{"api", "dev"},
		{"api", "prod"},
		{"frontend", "dev"},
		{"frontend", "prod"},
	}
	for i, exp := range expected {
		if entries[i]["service"] != exp.svc || entries[i]["environment"] != exp.env {
			t.Errorf("entry %d: expected %s/%s, got %v/%v", i, exp.svc, exp.env, entries[i]["service"], entries[i]["environment"])
		}
	}
}

func TestParseOptions_SortBy(t *testing.T) {
	raw := RawConfig{
		"settings": map[string]any{
			"sort_by": []any{"service", "environment"},
		},
		"service": []any{"api"},
	}

	optsCfg, _ := ParseOptions(raw)

	if len(optsCfg.SortBy) != 2 {
		t.Fatalf("expected 2 sort_by keys, got %d", len(optsCfg.SortBy))
	}
	if optsCfg.SortBy[0] != "service" || optsCfg.SortBy[1] != "environment" {
		t.Errorf("expected sort_by [service environment], got %v", optsCfg.SortBy)
	}
}

func TestUniqueValues(t *testing.T) {
	entries := []Entry{
		{"service": "api", "environment": "dev"},
		{"service": "api", "environment": "prod"},
		{"service": "frontend", "environment": "dev"},
		{"service": "frontend", "environment": "prod"},
	}

	services := UniqueValues(entries, "service")
	if len(services) != 2 {
		t.Fatalf("expected 2 unique services, got %d", len(services))
	}
	if services[0] != "api" || services[1] != "frontend" {
		t.Errorf("expected [api frontend], got %v", services)
	}

	envs := UniqueValues(entries, "environment")
	if len(envs) != 2 {
		t.Fatalf("expected 2 unique environments, got %d", len(envs))
	}
	if envs[0] != "dev" || envs[1] != "prod" {
		t.Errorf("expected [dev prod], got %v", envs)
	}

	// Key not present
	missing := UniqueValues(entries, "nonexistent")
	if len(missing) != 0 {
		t.Fatalf("expected 0 values for missing key, got %d", len(missing))
	}
}

func TestUniqueValues_PreservesOrder(t *testing.T) {
	entries := []Entry{
		{"environment": "prod"},
		{"environment": "dev"},
		{"environment": "staging"},
		{"environment": "prod"},
	}

	envs := UniqueValues(entries, "environment")
	if len(envs) != 3 {
		t.Fatalf("expected 3 unique environments, got %d", len(envs))
	}
	// Should preserve first-occurrence order
	if envs[0] != "prod" || envs[1] != "dev" || envs[2] != "staging" {
		t.Errorf("expected [prod dev staging], got %v", envs)
	}
}

func TestFilterChanged_WithBaseDir(t *testing.T) {
	files := []string{
		"deploy/infra/waf.tf",
		"deploy/infra/variables.tf",
		"deploy/networking/vpc.tf",
		"README.md",
	}
	changed := FilterChanged(files, "deploy", []string{"infra", "networking", "frontend"})
	if len(changed) != 2 {
		t.Fatalf("expected 2 values, got %d: %v", len(changed), changed)
	}
	if changed[0] != "infra" || changed[1] != "networking" {
		t.Errorf("expected [infra networking], got %v", changed)
	}
}

func TestFilterChanged_WithoutBaseDir(t *testing.T) {
	files := []string{
		"infra/waf.tf",
		"networking/vpc.tf",
	}
	changed := FilterChanged(files, "", []string{"infra", "networking"})
	if len(changed) != 2 {
		t.Fatalf("expected 2 values, got %d: %v", len(changed), changed)
	}
	if changed[0] != "infra" || changed[1] != "networking" {
		t.Errorf("expected [infra networking], got %v", changed)
	}
}

func TestFilterChanged_NoMatchingFiles(t *testing.T) {
	files := []string{
		"README.md",
		".github/workflows/ci.yaml",
	}
	changed := FilterChanged(files, "deploy", []string{"infra", "frontend"})
	if len(changed) != 0 {
		t.Fatalf("expected 0 values, got %d: %v", len(changed), changed)
	}
}

func TestFilterChanged_EmptyFiles(t *testing.T) {
	changed := FilterChanged([]string{}, "deploy", []string{"infra"})
	if len(changed) != 0 {
		t.Fatalf("expected 0 values, got %d: %v", len(changed), changed)
	}
}

func TestFilterChanged_OnlyMatchesKnownValues(t *testing.T) {
	files := []string{
		"deploy/infra/waf.tf",
		"deploy/unknown/file.tf",
	}
	changed := FilterChanged(files, "deploy", []string{"infra", "frontend"})
	if len(changed) != 1 {
		t.Fatalf("expected 1 value, got %d: %v", len(changed), changed)
	}
	if changed[0] != "infra" {
		t.Errorf("expected [infra], got %v", changed)
	}
}

func TestFilterChanged_MultipleFilesInSameValue(t *testing.T) {
	files := []string{
		"deploy/infra/waf.tf",
		"deploy/infra/outputs.tf",
		"deploy/infra/variables.tf",
	}
	changed := FilterChanged(files, "deploy", []string{"infra", "frontend"})
	if len(changed) != 1 {
		t.Fatalf("expected 1 value, got %d: %v", len(changed), changed)
	}
	if changed[0] != "infra" {
		t.Errorf("expected [infra], got %v", changed)
	}
}

func TestExtractDimensionValues_ArrayPresent(t *testing.T) {
	raw := RawConfig{
		"service": []any{"api", "infra"},
	}
	values := ExtractDimensionValues(raw, "service")
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}
	if values[0] != "api" || values[1] != "infra" {
		t.Errorf("expected [api infra], got %v", values)
	}
}

func TestExtractDimensionValues_MapPresent(t *testing.T) {
	raw := RawConfig{
		"environment": map[string]any{
			"prod": map[string]any{"aws_account_id": "222"},
			"dev":  map[string]any{"aws_account_id": "111"},
		},
	}
	values := ExtractDimensionValues(raw, "environment")
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}
	// Map keys are sorted
	if values[0] != "dev" || values[1] != "prod" {
		t.Errorf("expected [dev prod], got %v", values)
	}
}

func TestExtractDimensionValues_NotPresent(t *testing.T) {
	raw := RawConfig{
		"app_name": "myapp",
	}
	values := ExtractDimensionValues(raw, "service")
	if values != nil {
		t.Fatalf("expected nil, got %v", values)
	}
}

func TestExtractDimensionValues_NotArrayOrMap(t *testing.T) {
	raw := RawConfig{
		"service": "not-an-array",
	}
	values := ExtractDimensionValues(raw, "service")
	if values != nil {
		t.Fatalf("expected nil, got %v", values)
	}
}

func TestResolveTarget_ExplicitOverride(t *testing.T) {
	raw := RawConfig{
		"service":     map[string]any{"api": nil, "frontend": nil},
		"environment": map[string]any{"dev": nil, "prod": nil},
		"terraform":   map[string]any{"infra": nil},
	}
	optsCfg := OptionsConfig{Dimension: "service"}
	opts := Options{FilterKey: "service"}

	ResolveTarget(raw, &optsCfg, &opts, "terraform")

	if optsCfg.Dimension != "terraform" {
		t.Errorf("expected dimension 'terraform', got %q", optsCfg.Dimension)
	}
	if opts.FilterKey != "terraform" {
		t.Errorf("expected filter_key 'terraform', got %q", opts.FilterKey)
	}
	if _, ok := raw["service"]; ok {
		t.Error("service dimension should have been removed")
	}
	if _, ok := raw["terraform"]; !ok {
		t.Error("terraform dimension should still exist")
	}
	if _, ok := raw["environment"]; !ok {
		t.Error("environment dimension should still exist")
	}
}

func TestResolveTarget_TargetShorthand(t *testing.T) {
	raw := RawConfig{
		"service":     map[string]any{"api": nil, "frontend": nil},
		"environment": map[string]any{"dev": nil, "prod": nil},
		"terraform":   map[string]any{"infra": nil},
	}
	optsCfg := OptionsConfig{Dimension: "service"}
	opts := Options{FilterKey: "service", FilterValues: []string{"terraform"}}

	ResolveTarget(raw, &optsCfg, &opts, "")

	if optsCfg.Dimension != "terraform" {
		t.Errorf("expected dimension 'terraform', got %q", optsCfg.Dimension)
	}
	if opts.FilterKey != "terraform" {
		t.Errorf("expected filter_key 'terraform', got %q", opts.FilterKey)
	}
	if opts.FilterValues != nil {
		t.Errorf("expected nil filter_values, got %v", opts.FilterValues)
	}
	if _, ok := raw["service"]; ok {
		t.Error("service dimension should have been removed")
	}
}

func TestResolveTarget_ValueFilter(t *testing.T) {
	raw := RawConfig{
		"service":     map[string]any{"api": nil, "frontend": nil},
		"environment": map[string]any{"dev": nil, "prod": nil},
	}
	optsCfg := OptionsConfig{Dimension: "service"}
	opts := Options{FilterKey: "service", FilterValues: []string{"api"}}

	ResolveTarget(raw, &optsCfg, &opts, "")

	// Should not switch — "api" is a value of service, not a dimension name
	if optsCfg.Dimension != "service" {
		t.Errorf("expected dimension 'service', got %q", optsCfg.Dimension)
	}
	if len(opts.FilterValues) != 1 || opts.FilterValues[0] != "api" {
		t.Errorf("expected filter_values [api], got %v", opts.FilterValues)
	}
}

func TestResolveTarget_MultipleTargets(t *testing.T) {
	raw := RawConfig{
		"service":     map[string]any{"api": nil, "frontend": nil},
		"environment": map[string]any{"dev": nil, "prod": nil},
		"terraform":   map[string]any{"infra": nil},
	}
	optsCfg := OptionsConfig{Dimension: "service"}
	opts := Options{FilterKey: "service", FilterValues: []string{"terraform", "api"}}

	ResolveTarget(raw, &optsCfg, &opts, "")

	// Multiple targets → no dimension switch
	if optsCfg.Dimension != "service" {
		t.Errorf("expected dimension 'service', got %q", optsCfg.Dimension)
	}
	if len(opts.FilterValues) != 2 {
		t.Errorf("expected 2 filter_values, got %d", len(opts.FilterValues))
	}
}

func TestResolveTarget_ExplicitWithTarget(t *testing.T) {
	raw := RawConfig{
		"service":     map[string]any{"api": nil, "frontend": nil},
		"environment": map[string]any{"dev": nil, "prod": nil},
		"terraform":   map[string]any{"infra": nil},
	}
	optsCfg := OptionsConfig{Dimension: "service"}
	opts := Options{FilterKey: "service", FilterValues: []string{"infra"}}

	ResolveTarget(raw, &optsCfg, &opts, "terraform")

	// Explicit override switches dimension, target filter is preserved for value filtering
	if optsCfg.Dimension != "terraform" {
		t.Errorf("expected dimension 'terraform', got %q", optsCfg.Dimension)
	}
	if opts.FilterKey != "terraform" {
		t.Errorf("expected filter_key 'terraform', got %q", opts.FilterKey)
	}
	// FilterValues should still contain "infra" for value filtering within the new dimension
	if len(opts.FilterValues) != 1 || opts.FilterValues[0] != "infra" {
		t.Errorf("expected filter_values [infra], got %v", opts.FilterValues)
	}
}

func TestResolveTarget_ExplicitSameAsConfig(t *testing.T) {
	raw := RawConfig{
		"service":     map[string]any{"api": nil, "frontend": nil},
		"environment": map[string]any{"dev": nil, "prod": nil},
	}
	optsCfg := OptionsConfig{Dimension: "service"}
	opts := Options{FilterKey: "service", FilterValues: []string{"api"}}

	ResolveTarget(raw, &optsCfg, &opts, "service")

	// Same as config → no-op, treat target as value filter
	if optsCfg.Dimension != "service" {
		t.Errorf("expected dimension 'service', got %q", optsCfg.Dimension)
	}
	if len(opts.FilterValues) != 1 || opts.FilterValues[0] != "api" {
		t.Errorf("expected filter_values [api], got %v", opts.FilterValues)
	}
}

func TestResolveTarget_TargetIsAlsoDimensionValue(t *testing.T) {
	// Edge case: "terraform" is both a dimension name AND a value of service
	raw := RawConfig{
		"service":     map[string]any{"api": nil, "terraform": nil},
		"environment": map[string]any{"dev": nil, "prod": nil},
		"terraform":   map[string]any{"infra": nil},
	}
	optsCfg := OptionsConfig{Dimension: "service"}
	opts := Options{FilterKey: "service", FilterValues: []string{"terraform"}}

	ResolveTarget(raw, &optsCfg, &opts, "")

	// "terraform" is a value of the current dimension, so treat as value filter
	if optsCfg.Dimension != "service" {
		t.Errorf("expected dimension 'service', got %q", optsCfg.Dimension)
	}
	if len(opts.FilterValues) != 1 || opts.FilterValues[0] != "terraform" {
		t.Errorf("expected filter_values [terraform], got %v", opts.FilterValues)
	}
}

func TestResolveTarget_ArrayDimension(t *testing.T) {
	raw := RawConfig{
		"service":     []any{"api", "frontend"},
		"environment": map[string]any{"dev": nil, "prod": nil},
		"terraform":   map[string]any{"infra": nil},
	}
	optsCfg := OptionsConfig{Dimension: "service"}
	opts := Options{FilterKey: "service", FilterValues: []string{"terraform"}}

	ResolveTarget(raw, &optsCfg, &opts, "")

	if optsCfg.Dimension != "terraform" {
		t.Errorf("expected dimension 'terraform', got %q", optsCfg.Dimension)
	}
	if _, ok := raw["service"]; ok {
		t.Error("service dimension should have been removed")
	}
}
