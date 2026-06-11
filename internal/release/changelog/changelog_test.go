package changelog

import (
	"strings"
	"testing"
)

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
