// Package matrix parses matrix-config.yaml/json files and expands them into
// the cartesian product used by GitHub Actions matrix jobs.
package matrix

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Entry represents a single entry in the expanded matrix.
type Entry map[string]any

// RawConfig represents the parsed configuration file.
type RawConfig map[string]any

// OptionsConfig holds the parsed "settings" and "global" blocks from the config file.
type OptionsConfig struct {
	Dimension    string
	BaseDir      string
	SortBy       []string
	GlobalConfig map[string]any
	Exclude      []Entry
	Include      []Entry
}

// Options controls the expansion behavior.
type Options struct {
	FilterKey         string
	FilterValues      []string
	EnvironmentFilter []string
	InputExclude      []Entry
	InputInclude      []Entry
}

// ParseConfigFile reads and validates a JSON or YAML configuration file.
func ParseConfigFile(path string) (RawConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("configuration file not found: %s", path)
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var raw RawConfig
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("invalid YAML in %s: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("unsupported file type. Use .json, .yaml, or .yml")
	}

	if raw == nil {
		return nil, fmt.Errorf("configuration must be an object")
	}

	// yaml.v3 may produce named map types — round-trip through JSON to
	// flatten everything to plain map[string]any / []any.
	return normalizeViaJSON(raw)
}

var reservedKeys = map[string]bool{
	"settings": true,
	"global":   true,
	"exclude":  true,
	"include":  true,
}

// ParseOptions extracts reserved top-level keys, returning the parsed options
// and the remaining dimensions-only config.
func ParseOptions(raw RawConfig) (OptionsConfig, RawConfig) {
	optsCfg := OptionsConfig{}

	dimensions := make(RawConfig)
	for k, v := range raw {
		if !reservedKeys[k] {
			dimensions[k] = v
		}
	}

	if exc, ok := raw["exclude"]; ok {
		if entries, err := toEntries(exc); err == nil {
			optsCfg.Exclude = entries
		}
	}

	if inc, ok := raw["include"]; ok {
		if entries, err := toEntries(inc); err == nil {
			optsCfg.Include = entries
		}
	}

	if settingsRaw, ok := raw["settings"]; ok {
		if settingsMap, ok := settingsRaw.(map[string]any); ok {
			if d, ok := settingsMap["dimension"].(string); ok && d != "" {
				optsCfg.Dimension = d
			}
			if bd, ok := settingsMap["base_dir"].(string); ok {
				optsCfg.BaseDir = bd
			}
			if sb, ok := settingsMap["sort_by"]; ok {
				if arr, ok := toSlice(sb); ok {
					sortBy := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							sortBy = append(sortBy, s)
						}
					}
					optsCfg.SortBy = sortBy
				}
			}
		}
	}

	if globalRaw, ok := raw["global"]; ok {
		if globalMap, ok := globalRaw.(map[string]any); ok {
			if len(globalMap) > 0 {
				gc := make(map[string]any, len(globalMap))
				for k, v := range globalMap {
					gc[k] = v
				}
				optsCfg.GlobalConfig = gc
			}
		}
	}

	return optsCfg, dimensions
}

type dimension struct {
	key    string
	values []any
}

// FilterChanged returns the subset of knownValues that have at least one
// matching changed file.
func FilterChanged(changedFiles []string, baseDir string, knownValues []string) []string {
	var changed []string
	for _, val := range knownValues {
		prefix := val + "/"
		if baseDir != "" {
			prefix = baseDir + "/" + val + "/"
		}
		for _, f := range changedFiles {
			if strings.HasPrefix(strings.TrimSpace(f), prefix) {
				changed = append(changed, val)
				break
			}
		}
	}
	return changed
}

// ExtractDimensionValues returns the values for a given dimension key.
// For arrays, returns values as strings. For maps, returns sorted keys.
func ExtractDimensionValues(raw RawConfig, key string) []string {
	val, ok := raw[key]
	if !ok {
		return nil
	}
	if arr, ok := toSlice(val); ok {
		values := make([]string, 0, len(arr))
		for _, v := range arr {
			values = append(values, fmt.Sprintf("%v", v))
		}
		return values
	}
	if m, ok := val.(map[string]any); ok {
		return sortedKeys(m)
	}
	return nil
}

// Expand produces the expanded matrix from a dimensions-only config plus options.
func Expand(raw RawConfig, optsCfg OptionsConfig, opts Options) ([]Entry, error) {
	dimensions := extractDimensions(raw)

	var entries []Entry
	if len(dimensions) == 0 {
		entry := make(Entry)
		for k, v := range raw {
			entry[k] = v
		}
		entries = []Entry{entry}
	} else {
		entries = cartesianProduct(dimensions)
		baseConfig := extractBaseConfig(raw)
		entries = mergeConfig(entries, baseConfig, optsCfg.GlobalConfig, raw)
	}

	if len(optsCfg.Exclude) > 0 {
		entries = applyExclude(entries, optsCfg.Exclude)
	}
	if len(optsCfg.Include) > 0 {
		entries = applyInclude(entries, optsCfg.Include)
	}
	if len(opts.FilterValues) > 0 && opts.FilterKey != "" {
		entries = applyFilter(entries, opts.FilterKey, opts.FilterValues)
	}
	if len(opts.EnvironmentFilter) > 0 {
		entries = applyFilter(entries, "environment", opts.EnvironmentFilter)
	}
	if len(opts.InputExclude) > 0 {
		entries = applyExclude(entries, opts.InputExclude)
	}
	if len(opts.InputInclude) > 0 {
		entries = applyInclude(entries, opts.InputInclude)
	}

	addDirectoryField(entries, optsCfg)

	sortBy := optsCfg.SortBy
	if sortBy == nil {
		sortBy = []string{"environment"}
	}
	sortEntries(entries, sortBy)

	if entries == nil {
		entries = []Entry{}
	}
	return entries, nil
}

func sortEntries(entries []Entry, keys []string) {
	sort.SliceStable(entries, func(i, j int) bool {
		for _, key := range keys {
			vi := fmt.Sprintf("%v", entries[i][key])
			vj := fmt.Sprintf("%v", entries[j][key])
			if vi != vj {
				return vi < vj
			}
		}
		return false
	})
}

func addDirectoryField(entries []Entry, optsCfg OptionsConfig) {
	for _, entry := range entries {
		val, ok := entry[optsCfg.Dimension]
		if !ok {
			if optsCfg.BaseDir != "" {
				entry["directory"] = optsCfg.BaseDir
			}
			continue
		}
		strVal := fmt.Sprintf("%v", val)
		if optsCfg.BaseDir != "" {
			entry["directory"] = optsCfg.BaseDir + "/" + strVal
		} else {
			entry["directory"] = strVal
		}
	}
}

func extractDimensions(raw RawConfig) []dimension {
	var dims []dimension
	keys := sortedKeys(raw)
	for _, k := range keys {
		v := raw[k]
		if arr, ok := toSlice(v); ok {
			dims = append(dims, dimension{key: k, values: arr})
		} else if m, ok := v.(map[string]any); ok {
			mapKeys := sortedKeys(m)
			values := make([]any, len(mapKeys))
			for i, mk := range mapKeys {
				values[i] = mk
			}
			dims = append(dims, dimension{key: k, values: values})
		}
	}
	return dims
}

func extractBaseConfig(raw RawConfig) Entry {
	base := make(Entry)
	for k, v := range raw {
		if _, isArr := toSlice(v); isArr {
			continue
		}
		if _, isMap := v.(map[string]any); isMap {
			continue
		}
		base[k] = v
	}
	return base
}

func cartesianProduct(dims []dimension) []Entry {
	result := []Entry{{}}
	for _, dim := range dims {
		var next []Entry
		for _, entry := range result {
			for _, val := range dim.values {
				newEntry := make(Entry)
				for k, v := range entry {
					newEntry[k] = v
				}
				newEntry[dim.key] = val
				next = append(next, newEntry)
			}
		}
		result = next
	}
	return result
}

func mergeConfig(entries []Entry, baseConfig Entry, globalConfig map[string]any, raw RawConfig) []Entry {
	result := make([]Entry, len(entries))
	for i, combo := range entries {
		entry := make(Entry)
		for k, v := range baseConfig {
			entry[k] = v
		}
		for k, v := range globalConfig {
			entry[k] = v
		}
		for k, v := range combo {
			entry[k] = v
		}
		dimKeys := sortedKeys(combo)
		for _, dimKey := range dimKeys {
			dimValue := fmt.Sprintf("%v", combo[dimKey])
			if dimMap, ok := raw[dimKey].(map[string]any); ok {
				if valConfig, ok := dimMap[dimValue].(map[string]any); ok {
					for ck, cv := range valConfig {
						entry[ck] = cv
					}
				}
			}
		}
		result[i] = entry
	}
	return result
}

func applyExclude(entries []Entry, patterns []Entry) []Entry {
	var result []Entry
	for _, entry := range entries {
		excluded := false
		for _, pattern := range patterns {
			if matchesPattern(entry, pattern) {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, entry)
		}
	}
	return result
}

func applyInclude(entries []Entry, includes []Entry) []Entry {
	return append(entries, includes...)
}

func applyFilter(entries []Entry, key string, allowed []string) []Entry {
	allowedSet := make(map[string]bool, len(allowed))
	for _, v := range allowed {
		allowedSet[v] = true
	}
	var result []Entry
	for _, entry := range entries {
		if val, ok := entry[key]; ok {
			if allowedSet[fmt.Sprintf("%v", val)] {
				result = append(result, entry)
			}
		}
	}
	return result
}

func matchesPattern(entry, pattern Entry) bool {
	for k, pv := range pattern {
		ev, ok := entry[k]
		if !ok {
			return false
		}
		if fmt.Sprintf("%v", ev) != fmt.Sprintf("%v", pv) {
			return false
		}
	}
	return true
}

func toSlice(v any) ([]any, bool) {
	if val, ok := v.([]any); ok {
		return val, true
	}
	return nil, false
}

func toEntries(v any) ([]Entry, error) {
	arr, ok := toSlice(v)
	if !ok {
		return nil, fmt.Errorf("expected array")
	}
	result := make([]Entry, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			result = append(result, Entry(m))
		}
	}
	return result, nil
}

func normalizeViaJSON(raw RawConfig) (RawConfig, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var result RawConfig
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// UniqueValues extracts unique string values for a given key, preserving
// first-occurrence order.
func UniqueValues(entries []Entry, key string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, entry := range entries {
		if val, ok := entry[key]; ok {
			s := fmt.Sprintf("%v", val)
			if !seen[s] {
				seen[s] = true
				result = append(result, s)
			}
		}
	}
	return result
}

// ResolveTarget handles dimension selection. If dimensionOverride is set, it
// overrides the config's dimension. Otherwise, if the single target value
// matches a dimension name (but not a value of the current dimension), it
// triggers the same switch.
func ResolveTarget(raw RawConfig, optsCfg *OptionsConfig, opts *Options, dimensionOverride string) {
	configDim := optsCfg.Dimension

	if dimensionOverride != "" && dimensionOverride != configDim {
		if isDimension(raw[dimensionOverride]) {
			delete(raw, configDim)
			optsCfg.Dimension = dimensionOverride
			opts.FilterKey = dimensionOverride
			return
		}
	}

	if len(opts.FilterValues) != 1 {
		return
	}
	target := opts.FilterValues[0]

	if !isDimension(raw[target]) {
		return
	}

	for _, v := range ExtractDimensionValues(raw, configDim) {
		if v == target {
			return
		}
	}

	delete(raw, configDim)
	optsCfg.Dimension = target
	opts.FilterKey = target
	opts.FilterValues = nil
}

func isDimension(v any) bool {
	if _, ok := v.(map[string]any); ok {
		return true
	}
	if _, ok := toSlice(v); ok {
		return true
	}
	return false
}

func sortedKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
