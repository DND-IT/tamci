package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dnd-it/tamci/internal/gha"
	"github.com/dnd-it/tamci/internal/matrix"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newConfigCmd() *cobra.Command {
	v := newViper()

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read a matrix-config file and emit a workflow matrix.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindFlags(cmd, v)
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return runConfig(v)
		},
	}

	f := cmd.Flags()
	f.String("config-path", ".github/matrix-config.yaml", "Path to the configuration file (JSON or YAML).")
	f.String("dimension", "", "Primary dimension override.")
	f.String("target", "", "Filter by dimension value(s); comma-separated.")
	f.String("environment", "", "Filter environments; comma-separated.")
	f.String("exclude", "", "JSON array of patterns to exclude.")
	f.String("include", "", "JSON array of entries to append.")
	f.Bool("change-detection", false, "Filter the matrix to entries with changes (requires git history).")
	f.Bool("summary", true, "Write all output values to GITHUB_STEP_SUMMARY.")

	return cmd
}

func runConfig(v *viper.Viper) error {
	configPath := v.GetString("config-path")
	dimensionInput := v.GetString("dimension")

	opts := matrix.Options{
		FilterValues:      splitCSV(v.GetString("target")),
		EnvironmentFilter: splitCSV(v.GetString("environment")),
	}

	if exc := v.GetString("exclude"); exc != "" {
		if err := json.Unmarshal([]byte(exc), &opts.InputExclude); err != nil {
			return fmt.Errorf("invalid exclude JSON: %w", err)
		}
	}
	if inc := v.GetString("include"); inc != "" {
		if err := json.Unmarshal([]byte(inc), &opts.InputInclude); err != nil {
			return fmt.Errorf("invalid include JSON: %w", err)
		}
	}

	raw, err := matrix.ParseConfigFile(configPath)
	if err != nil {
		return err
	}

	recorded := newOutputRecorder()
	recorded.set("config_file", configPath)

	optsCfg, dimensions := matrix.ParseOptions(raw)

	// Dimension priority: explicit input > config settings > default "service".
	if dimensionInput == "" && optsCfg.Dimension == "" {
		optsCfg.Dimension = "service"
	}
	opts.FilterKey = optsCfg.Dimension
	matrix.ResolveTarget(dimensions, &optsCfg, &opts, dimensionInput)

	if v.GetBool("change-detection") {
		knownValues := matrix.ExtractDimensionValues(dimensions, optsCfg.Dimension)
		if knownValues == nil {
			gha.Notice(fmt.Sprintf("No %s dimension in config, skipping change detection", optsCfg.Dimension))
		} else {
			changedFiles, err := matrix.DetectChangedFiles()
			if err != nil {
				return fmt.Errorf("detect changed files: %w", err)
			}
			if changedFiles == nil {
				gha.Notice("Change detection not applicable for this event type, including all entries")
			} else {
				changedValues := matrix.FilterChanged(changedFiles, optsCfg.BaseDir, knownValues)
				gha.Notice(fmt.Sprintf("Detected %d changed files, %d/%d %s(s) with changes: %v",
					len(changedFiles), len(changedValues), len(knownValues), optsCfg.Dimension, changedValues))

				if len(changedValues) == 0 {
					recorded.set("matrix", "[]")
					recorded.set("config", "{}")
					recorded.set("length", "0")
					recorded.set("changes_detected", "false")
					if v.GetBool("summary") {
						recorded.writeSummary()
					}
					gha.Notice("No entries with changes, matrix is empty")
					return nil
				}

				if len(opts.FilterValues) > 0 {
					existing := make(map[string]bool, len(opts.FilterValues))
					for _, s := range opts.FilterValues {
						existing[s] = true
					}
					var merged []string
					for _, s := range changedValues {
						if existing[s] {
							merged = append(merged, s)
						}
					}
					opts.FilterValues = merged
				} else {
					opts.FilterValues = changedValues
				}
			}
		}
	}

	entries, err := matrix.Expand(dimensions, optsCfg, opts)
	if err != nil {
		return fmt.Errorf("expand configuration: %w", err)
	}

	matrixJSON, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal matrix: %w", err)
	}

	recorded.set("matrix", string(matrixJSON))
	recorded.set("length", strconv.Itoa(len(entries)))

	if optsCfg.BaseDir != "" {
		recorded.set("base_dir", optsCfg.BaseDir)
	}
	recorded.set("dimension", optsCfg.Dimension)

	if len(entries) > 0 {
		dimKeys := make([]string, 0, len(dimensions))
		for k := range dimensions {
			dimKeys = append(dimKeys, k)
		}
		sort.Strings(dimKeys)

		configBlob := buildConfigBlob(entries, dimKeys)
		if configJSON, err := json.Marshal(configBlob); err == nil {
			recorded.set("config", string(configJSON))
		}

		reserved := map[string]bool{
			"matrix": true, "changes_detected": true, "config": true,
			"length": true, "base_dir": true, "dimension": true, "config_file": true,
		}
		for k, val := range entries[0] {
			if reserved[k] {
				continue
			}
			s := fmt.Sprintf("%v", val)
			uniform := true
			for _, e := range entries[1:] {
				if fmt.Sprintf("%v", e[k]) != s {
					uniform = false
					break
				}
			}
			if uniform {
				recorded.set(k, s)
			}
		}
	}

	if v.GetBool("change-detection") {
		if len(entries) > 0 {
			recorded.set("changes_detected", "true")
		} else {
			recorded.set("changes_detected", "false")
		}
	}

	if v.GetBool("summary") {
		recorded.writeSummary()
	}

	if len(opts.FilterValues) > 0 {
		gha.Notice(fmt.Sprintf("Filtered by %s: %v", opts.FilterKey, opts.FilterValues))
	}
	if len(opts.EnvironmentFilter) > 0 {
		gha.Notice(fmt.Sprintf("Filtered by environment: %v", opts.EnvironmentFilter))
	}
	if len(opts.InputExclude) > 0 {
		gha.Notice("Applied input exclude filter")
	}
	if len(opts.InputInclude) > 0 {
		gha.Notice("Applied input include filter")
	}

	gha.Notice("Matrix configuration loaded successfully:")
	if pretty, err := json.MarshalIndent(entries, "", "  "); err == nil {
		fmt.Println(string(pretty))
	}

	return nil
}

func buildConfigBlob(entries []matrix.Entry, dimKeys []string) map[string]any {
	root := make(map[string]any)
	for _, entry := range entries {
		skip := false
		for _, dk := range dimKeys {
			if _, ok := entry[dk]; !ok {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		current := root
		for i, dk := range dimKeys {
			val := fmt.Sprintf("%v", entry[dk])
			if i == len(dimKeys)-1 {
				current[val] = map[string]any(entry)
			} else {
				next, ok := current[val]
				if !ok {
					next = make(map[string]any)
					current[val] = next
				}
				current = next.(map[string]any)
			}
		}
	}
	return root
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// outputRecorder buffers outputs so we can both write to GITHUB_OUTPUT
// immediately and (optionally) summarize them in GITHUB_STEP_SUMMARY at the end.
type outputRecorder struct {
	entries []outputEntry
}

type outputEntry struct{ name, value string }

func newOutputRecorder() *outputRecorder { return &outputRecorder{} }

func (r *outputRecorder) set(name, value string) {
	r.entries = append(r.entries, outputEntry{name, value})
	_ = gha.SetOutput(name, value)
}

func (r *outputRecorder) writeSummary() {
	if len(r.entries) == 0 {
		return
	}
	var sb strings.Builder
	var details []outputEntry

	sb.WriteString("### Outputs\n\n")
	sb.WriteString("| Name | Value |\n")
	sb.WriteString("|------|-------|\n")

	for _, e := range r.entries {
		if isLongValue(e.value) {
			fmt.Fprintf(&sb, "| `%s` | *(see below)* |\n", e.name)
			details = append(details, e)
		} else {
			fmt.Fprintf(&sb, "| `%s` | `%s` |\n", e.name, e.value)
		}
	}

	for _, e := range details {
		fmt.Fprintf(&sb, "\n<details><summary><code>%s</code></summary>\n\n```json\n%s\n```\n\n</details>\n",
			e.name, prettyJSON(e.value))
	}

	_ = gha.AppendStepSummary(sb.String())
}

func isLongValue(v string) bool {
	return strings.Contains(v, "\n") || len(v) > 100
}

func prettyJSON(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(b)
}
