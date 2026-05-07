package releasepr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/go-github/v68/github"
)

func TestReleaseBranchName(t *testing.T) {
	tests := []struct {
		tag     string
		version string
		want    string
	}{
		{"go-service-v1.14.0", "1.14.0", "release/go-service"},
		{"python-api-v2.0.0", "2.0.0", "release/python-api"},
		{"ts-spa-2026.03.31", "2026.03.31", "release/ts-spa"},
		{"v1.0.0", "1.0.0", "release/next"},
		{"sdk/v1.0.0", "1.0.0", "release/sdk"},
		{"cli/v2.1.0", "2.1.0", "release/cli"},
		{"build-42", "42", "release/build"},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			got := ReleaseBranchName(tt.tag, tt.version)
			if got != tt.want {
				t.Errorf("ReleaseBranchName(%q, %q) = %q, want %q", tt.tag, tt.version, got, tt.want)
			}
		})
	}
}

func TestChangelogPath(t *testing.T) {
	tests := []struct {
		servicePath string
		want        string
	}{
		{"", "CHANGELOG.md"},
		{"services/go-service", "services/go-service/CHANGELOG.md"},
		{"libs/shared", "libs/shared/CHANGELOG.md"},
	}
	for _, tt := range tests {
		t.Run(tt.servicePath, func(t *testing.T) {
			got := ChangelogPath(tt.servicePath)
			if got != tt.want {
				t.Errorf("ChangelogPath(%q) = %q, want %q", tt.servicePath, got, tt.want)
			}
		})
	}
}

func TestPrependChangelog_NoExisting(t *testing.T) {
	got := PrependChangelog("## [1.0.0]\n\n- Initial release", "")
	if !strings.HasPrefix(got, "# Changelog") {
		t.Error("should start with # Changelog header")
	}
	if !strings.Contains(got, "## [1.0.0]") {
		t.Error("should contain new entries")
	}
}

func TestPrependChangelog_WithExisting(t *testing.T) {
	existing := "# Changelog\n\n## [0.9.0]\n\n- Previous release\n"
	got := PrependChangelog("## [1.0.0]\n\n- New release", existing)

	if !strings.HasPrefix(got, "# Changelog") {
		t.Error("should start with # Changelog header")
	}
	// Should have only one "# Changelog" header.
	if strings.Count(got, "# Changelog") != 1 {
		t.Errorf("should have exactly one top-level header, got:\n%s", got)
	}
	// New entries should appear before old.
	newIdx := strings.Index(got, "## [1.0.0]")
	oldIdx := strings.Index(got, "## [0.9.0]")
	if newIdx < 0 || oldIdx < 0 {
		t.Fatalf("missing sections in:\n%s", got)
	}
	if newIdx > oldIdx {
		t.Error("new entries should appear before old entries")
	}
}

func TestPrependChangelog_ExistingWithoutHeader(t *testing.T) {
	existing := "## [0.9.0]\n\n- Previous release\n"
	got := PrependChangelog("## [1.0.0]\n\n- New release", existing)

	// The "## [0.9.0]" line starts with "## " not "# ", so the header-strip
	// logic should NOT strip it (it only strips lines starting with "# ").
	if !strings.Contains(got, "## [0.9.0]") {
		t.Error("should preserve existing entries")
	}
	if !strings.Contains(got, "## [1.0.0]") {
		t.Error("should contain new entries")
	}
}

func TestFormatPRBody(t *testing.T) {
	body := formatPRBody("1.2.0", "### Features\n- Added foo")
	if !strings.Contains(body, "1.2.0") {
		t.Error("body should contain version")
	}
	if !strings.Contains(body, "### Features") {
		t.Error("body should contain changelog")
	}
	if !strings.Contains(body, "action-releaser") {
		t.Error("body should mention action-releaser")
	}
}

func TestFormatPRBody_EmptyChangelog(t *testing.T) {
	body := formatPRBody("1.0.0", "")
	if !strings.Contains(body, "No changelog entries") {
		t.Error("body should indicate empty changelog")
	}
}

func TestManifestRoundtrip(t *testing.T) {
	m := Manifest{Version: "1.0.0", Strategy: "semver", Tag: "v1.0.0"}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	var m2 Manifest
	if err := json.Unmarshal(data, &m2); err != nil {
		t.Fatal(err)
	}
	if m2.Version != m.Version || m2.Strategy != m.Strategy || m2.Tag != m.Tag {
		t.Errorf("roundtrip failed: got %+v, want %+v", m2, m)
	}
}

func TestReadManifestFromWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	manifest := Manifest{Version: "2.0.0", Strategy: "semver", Tag: "v2.0.0"}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(ManifestFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := readManifestFromWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0", m.Version)
	}
	if m.Tag != "v2.0.0" {
		t.Errorf("tag = %q, want v2.0.0", m.Tag)
	}
}

func TestReadManifestFromWorkspace_Missing(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := readManifestFromWorkspace()
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

// newTestClient returns a Client wired to an httptest server.
// The mux handles the requests you register; anything else 404s so tests
// fail loudly on unexpected calls.
func newTestClient(t *testing.T, mux *http.ServeMux) (*Client, func()) {
	t.Helper()
	server := httptest.NewServer(mux)
	gh := github.NewClient(nil)
	u, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	gh.BaseURL = u
	gh.UploadURL = u
	return NewClient(gh, "owner", "repo"), server.Close
}

// prFixture builds a minimal JSON object for a PR list entry.
// Mirrors the real "List PRs" API response shape: it sets `merged_at` (not
// the boolean `merged`, which is only present on the single-PR GET endpoint).
// Tests that use the LIST endpoint must rely on `merged_at` to detect merges.
func prFixture(number int, merged bool, headRef string, labels ...string) map[string]any {
	lbls := make([]map[string]string, 0, len(labels))
	for _, l := range labels {
		lbls = append(lbls, map[string]string{"name": l})
	}
	pr := map[string]any{
		"number": number,
		"head":   map[string]any{"ref": headRef},
		"labels": lbls,
	}
	if merged {
		pr["merged_at"] = "2026-04-17T12:00:00Z"
	}
	return pr
}

func TestDetectMerge_EmptyList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[]`)
	})
	c, cleanup := newTestClient(t, mux)
	defer cleanup()

	result, err := c.DetectMerge(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("expected nil result, got %+v", result)
	}
}

func TestDetectMerge_SkipsNonMerged(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		prs := []map[string]any{
			prFixture(10, false, "release/go-service", LabelPending),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(prs)
	})
	c, cleanup := newTestClient(t, mux)
	defer cleanup()

	result, err := c.DetectMerge(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil for closed-but-not-merged PR")
	}
}

func TestDetectMerge_SkipsNonReleaseBranch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		prs := []map[string]any{
			prFixture(11, true, "feature/unrelated", LabelPending),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(prs)
	})
	c, cleanup := newTestClient(t, mux)
	defer cleanup()

	result, err := c.DetectMerge(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil for non-release branch")
	}
}

func TestDetectMerge_SkipsMissingPendingLabel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		prs := []map[string]any{
			prFixture(12, true, "release/go-service", LabelTagged),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(prs)
	})
	c, cleanup := newTestClient(t, mux)
	defer cleanup()

	result, err := c.DetectMerge(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil when pending label is absent")
	}
}

func TestDetectMerge_ReturnsMergedReleasePR(t *testing.T) {
	t.Chdir(t.TempDir()) // no manifest in CWD — exercises fallback path

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		prs := []map[string]any{
			prFixture(42, true, "release/go-service", LabelPending, "autorelease: something-else"),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(prs)
	})
	c, cleanup := newTestClient(t, mux)
	defer cleanup()

	result, err := c.DetectMerge(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected MergeResult for valid merged release PR")
	}
	if result.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", result.PRNumber)
	}
	if result.Manifest == nil {
		t.Error("Manifest should be non-nil (fallback to empty manifest)")
	}
}

func TestDetectMerge_PicksFirstMatchWhenMultiplePresent(t *testing.T) {
	t.Chdir(t.TempDir())

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		// Sort=updated, Direction=desc is enforced server-side in production;
		// the handler returns them already in desc order. The first match wins.
		prs := []map[string]any{
			prFixture(100, false, "release/go-service", LabelPending), // skipped: not merged
			prFixture(99, true, "release/go-service", LabelPending),   // match
			prFixture(98, true, "release/go-service", LabelPending),   // also matches but too late
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(prs)
	})
	c, cleanup := newTestClient(t, mux)
	defer cleanup()

	result, err := c.DetectMerge(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.PRNumber != 99 {
		t.Fatalf("expected PR #99, got %+v", result)
	}
}

func TestDetectMerge_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	c, cleanup := newTestClient(t, mux)
	defer cleanup()

	_, err := c.DetectMerge(context.Background(), "main")
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	if !strings.Contains(err.Error(), "list closed PRs") {
		t.Errorf("error should be wrapped with context: %v", err)
	}
}
