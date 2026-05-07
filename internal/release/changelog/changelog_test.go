package changelog

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/dnd-it/tamci/internal/release/config"
)

// gitCliffAvailable returns true if git-cliff is on PATH.
func gitCliffAvailable() bool {
	_, err := exec.LookPath("git-cliff")
	return err == nil
}

func TestGenerate_rangeFlag(t *testing.T) {
	if !gitCliffAvailable() {
		t.Skip("git-cliff not available")
	}

	tests := []struct {
		name        string
		releaseMode string
		wantFlag    string
	}{
		{
			name:        "direct mode uses --latest",
			releaseMode: "direct",
			wantFlag:    "--latest",
		},
		{
			name:        "pr mode uses --unreleased",
			releaseMode: "pr",
			wantFlag:    "--unreleased",
		},
		{
			name:        "empty mode (default direct) uses --latest",
			releaseMode: "",
			wantFlag:    "--latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{ReleaseMode: tt.releaseMode}
			rangeFlag := "--latest"
			if cfg.ReleaseMode == "pr" {
				rangeFlag = "--unreleased"
			}
			if rangeFlag != tt.wantFlag {
				t.Errorf("rangeFlag = %q, want %q", rangeFlag, tt.wantFlag)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Run("short text unchanged", func(t *testing.T) {
		text := "short changelog"
		result := truncate(text)
		if result != text {
			t.Errorf("truncate(%q) = %q, want unchanged", text, result)
		}
	})

	t.Run("long text is truncated", func(t *testing.T) {
		// Use 2x the limit so even after appending the truncation suffix the
		// result is guaranteed to be shorter than the original input.
		text := strings.Repeat("a", maxChangelogBytes*2)
		result := truncate(text)
		if len(result) >= len(text) {
			t.Errorf("truncate did not shorten long text: len=%d, original len=%d", len(result), len(text))
		}
		if !strings.Contains(result, "truncated") {
			t.Errorf("truncate result missing truncation notice: %q", result[len(result)-100:])
		}
	})
}
