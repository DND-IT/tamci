package gha

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func outputFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "output")
	t.Setenv("GITHUB_OUTPUT", path)
	return path
}

func TestSetOutput_SingleLine(t *testing.T) {
	path := outputFile(t)

	if err := SetOutput("version", "1.2.3"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "version=1.2.3\n" {
		t.Errorf("got %q, want %q", got, "version=1.2.3\n")
	}
}

func TestSetOutput_Appends(t *testing.T) {
	path := outputFile(t)

	if err := SetOutput("a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := SetOutput("b", "2"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if got := string(data); got != "a=1\nb=2\n" {
		t.Errorf("got %q, want %q", got, "a=1\nb=2\n")
	}
}

func TestSetOutput_MultiLineUsesDelimiter(t *testing.T) {
	path := outputFile(t)

	value := "line1\nline2"
	if err := SetOutput("changelog", value); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)

	// Expect: changelog<<ghadelimiter_N \n line1 \n line2 \n ghadelimiter_N \n
	re := regexp.MustCompile(`^changelog<<(ghadelimiter_\d+)\nline1\nline2\n(ghadelimiter_\d+)\n$`)
	m := re.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("output does not match heredoc format: %q", got)
	}
	if m[1] != m[2] {
		t.Errorf("open/close delimiters differ: %q vs %q", m[1], m[2])
	}
	if strings.Contains(value, m[1]) {
		t.Errorf("delimiter %q collides with value", m[1])
	}
}

func TestSetOutput_NoEnvFallsBackToWorkflowCommand(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	// Must not error; the legacy ::set-output command goes to stdout.
	if err := SetOutput("key", "value"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppendStepSummary_WritesAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary")
	t.Setenv("GITHUB_STEP_SUMMARY", path)

	if err := AppendStepSummary("## First\n"); err != nil {
		t.Fatal(err)
	}
	if err := AppendStepSummary("## Second\n"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "## First\n## Second\n" {
		t.Errorf("got %q", got)
	}
}

func TestAppendStepSummary_NoEnvWritesStdout(t *testing.T) {
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	if err := AppendStepSummary("local run\n"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppendFile_UnwritablePath(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", filepath.Join(t.TempDir(), "missing-dir", "output"))
	if err := SetOutput("k", "v"); err == nil {
		t.Fatal("expected error for unwritable path")
	}
}

// TestSetOutput_MultiLineContainingDelimiter guards the regression where a
// value line matching the heredoc delimiter would terminate the heredoc early,
// corrupting every output written afterwards (e.g. release-published). The
// delimiter must be extended so it never appears as a line in the value.
func TestSetOutput_MultiLineContainingDelimiter(t *testing.T) {
	path := outputFile(t)

	base := fmt.Sprintf("ghadelimiter_%d", os.Getpid())
	value := "line1\n" + base + "\nline3"
	if err := SetOutput("changelog", value); err != nil {
		t.Fatal(err)
	}
	// A scalar written after the multiline value must remain parseable.
	if err := SetOutput("release-published", "true"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	outputs := parseGithubOutput(t, string(data))
	if outputs["changelog"] != value {
		t.Errorf("changelog round-trip mismatch:\n got %q\nwant %q", outputs["changelog"], value)
	}
	if outputs["release-published"] != "true" {
		t.Errorf("release-published = %q, want \"true\" (delimiter collision corrupted later outputs)", outputs["release-published"])
	}
}

// parseGithubOutput parses a $GITHUB_OUTPUT file the way the Actions runner
// does: `name=value` lines and `name<<DELIM ... DELIM` heredoc blocks.
func parseGithubOutput(t *testing.T, content string) map[string]string {
	t.Helper()
	out := map[string]string{}
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if idx := strings.Index(line, "<<"); idx > 0 && !strings.Contains(line[:idx], "=") {
			name := line[:idx]
			delim := line[idx+2:]
			var body []string
			i++
			for ; i < len(lines); i++ {
				if lines[i] == delim {
					break
				}
				body = append(body, lines[i])
			}
			if i == len(lines) {
				t.Fatalf("heredoc for %q never closed with delimiter %q", name, delim)
			}
			out[name] = strings.Join(body, "\n")
			continue
		}
		if eq := strings.Index(line, "="); eq > 0 {
			out[line[:eq]] = line[eq+1:]
		}
	}
	return out
}

func TestSaveState_Appends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	t.Setenv("GITHUB_STATE", path)

	if err := SaveState("isPost", "true"); err != nil {
		t.Fatal(err)
	}
	if err := SaveState("token", "ghs_x"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if got := string(data); got != "isPost=true\ntoken=ghs_x\n" {
		t.Errorf("got %q", got)
	}
}

func TestSaveState_NoEnv(t *testing.T) {
	t.Setenv("GITHUB_STATE", "")
	if err := SaveState("k", "v"); err == nil {
		t.Fatal("expected error when GITHUB_STATE is unset")
	}
}

func TestMask(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	Mask("secret")
	os.Stdout = stdout
	_ = w.Close()

	got, _ := io.ReadAll(r)
	if string(got) != "::add-mask::secret\n" {
		t.Errorf("got %q", got)
	}
}
