package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/dnd-it/tamci/internal/gha"
	"github.com/dnd-it/tamci/internal/summary"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newSummaryCmd() *cobra.Command {
	v := newViper()

	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Read input text or a file and append it to GITHUB_STEP_SUMMARY.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindFlags(cmd, v)
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSummary(v)
		},
	}

	f := cmd.Flags()
	f.String("string", "", "Input string to summarize.")
	f.String("path", "", "Path to a file containing text to summarize.")
	f.Int("max-size", 1048576, "Maximum size for the output in bytes (1 MiB GitHub job summary limit).")
	f.String("summary-header", "Summary", "Header for the summary output.")
	f.String("data-type", "", "Markdown code-block language tag.")

	return cmd
}

func runSummary(v *viper.Viper) error {
	inputString := v.GetString("string")
	inputPath := v.GetString("path")
	maxSize := v.GetInt("max-size")
	header := v.GetString("summary-header")
	dataType := v.GetString("data-type")

	if (inputString == "" && inputPath == "") || (inputString != "" && inputPath != "") {
		return fmt.Errorf("provide either --string or --path, but not both")
	}

	var data string
	if inputPath != "" {
		raw, err := os.ReadFile(inputPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", inputPath, err)
		}
		data = string(raw)
	} else {
		data = inputString
	}

	if data == "" {
		return fmt.Errorf("input data is empty")
	}

	if len(data) > maxSize {
		fmt.Printf("::warning::String content too long (%d bytes); exceeds %d byte limit.\n", len(data), maxSize)
		return nil
	}

	var parsed any
	if err := json.Unmarshal([]byte(data), &parsed); err == nil {
		pretty, err := json.MarshalIndent(summary.DeserializeNestedJSON(parsed), "", "  ")
		if err == nil {
			data = string(pretty)
		}
	}

	return gha.AppendStepSummary(summary.FormatOutput(header, dataType, data))
}
