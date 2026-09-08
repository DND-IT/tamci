package strategy

import (
	"regexp"
	"testing"
	"time"

	"github.com/dnd-it/tamci/internal/release/config"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"semver", false},
		{"calver", false},
		{"unknown", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(tt.name)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s.Name() != tt.name {
				t.Errorf("Name() = %q, want %q", s.Name(), tt.name)
			}
		})
	}
}

func TestCalVer_NextVersion(t *testing.T) {
	fixedTime := time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC)
	cfg := config.Config{TagPrefix: "v"}

	tests := []struct {
		name    string
		tags    []string
		want    string
		wantPre string
	}{
		{
			name:    "first release of the month",
			tags:    []string{"v2026.02.4"},
			want:    "2026.03.0",
			wantPre: "v2026.02.4",
		},
		{
			name:    "second release same month",
			tags:    []string{"v2026.03.0", "v2026.02.4"},
			want:    "2026.03.1",
			wantPre: "v2026.03.0",
		},
		{
			name:    "third release same month",
			tags:    []string{"v2026.03.1", "v2026.03.0", "v2026.02.4"},
			want:    "2026.03.2",
			wantPre: "v2026.03.1",
		},
		{
			name:    "counter continues past gaps",
			tags:    []string{"v2026.03.5", "v2026.03.2"},
			want:    "2026.03.6",
			wantPre: "v2026.03.5",
		},
		{
			name:    "bootstrap - no tags",
			tags:    nil,
			want:    "2026.03.0",
			wantPre: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &CalVer{Now: func() time.Time { return fixedTime }}
			result, err := d.NextVersion(tt.tags, cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Skipped {
				t.Fatal("unexpected skip")
			}
			if result.Version != tt.want {
				t.Errorf("Version = %q, want %q", result.Version, tt.want)
			}
			if result.PreviousVersion != tt.wantPre {
				t.Errorf("PreviousVersion = %q, want %q", result.PreviousVersion, tt.wantPre)
			}
		})
	}
}

func TestCalVer_AlwaysReleases(t *testing.T) {
	d := &CalVer{}
	if !d.AlwaysReleases() {
		t.Error("CalVer should always release")
	}
}

func TestSemver_AlwaysReleases(t *testing.T) {
	s := &Semver{}
	if s.AlwaysReleases() {
		t.Error("Semver should NOT always release")
	}
}

func TestIsValidVersion(t *testing.T) {
	tests := []struct {
		strategy string
		version  string
		want     bool
	}{
		// Semver valid
		{"semver", "1.2.3", true},
		{"semver", "0.1.0", true},
		{"semver", "10.20.30", true},
		{"semver", "1.2.3-beta.1", true},
		// Semver invalid
		{"semver", "go-service-v1.13.0", false},
		{"semver", "deploy/go-service/prod/1.0.0", false},
		{"semver", "abc", false},
		{"semver", "", false},

		// CalVer valid
		{"calver", "2026.03.0", true},
		{"calver", "2026.03.2", true},
		{"calver", "2026.03.10", true},
		// CalVer invalid
		{"calver", "1.2.3", false},
		{"calver", "2026.03", false},
		{"calver", "go-service-v1.13.0", false},
		{"calver", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.strategy+"/"+tt.version, func(t *testing.T) {
			got := IsValidVersion(tt.strategy, tt.version)
			if got != tt.want {
				t.Errorf("IsValidVersion(%q, %q) = %v, want %v", tt.strategy, tt.version, got, tt.want)
			}
		})
	}
}

func TestFilterTags(t *testing.T) {
	tests := []struct {
		name     string
		tags     []string
		prefix   string
		strategy string
		want     []string
	}{
		{
			name:     "semver filters out cross-contaminated tags",
			tags:     []string{"python-api-v1.3.0", "python-api-vgo-service-v1.13.0", "python-api-vdeploy/go-service/prod/1.0.0"},
			prefix:   "python-api-v",
			strategy: "semver",
			want:     []string{"python-api-v1.3.0"},
		},
		{
			name:     "semver keeps all valid tags",
			tags:     []string{"v2.0.0", "v1.1.0", "v1.0.0"},
			prefix:   "v",
			strategy: "semver",
			want:     []string{"v2.0.0", "v1.1.0", "v1.0.0"},
		},
		{
			name:     "calver filters non-date versions",
			tags:     []string{"ts-spa-2026.03.4", "ts-spa-go-service-v1.13.0"},
			prefix:   "ts-spa-",
			strategy: "calver",
			want:     []string{"ts-spa-2026.03.4"},
		},
		{
			name:     "returns nil when no tags match",
			tags:     []string{"go-service-v1.13.0"},
			prefix:   "python-api-v",
			strategy: "semver",
			want:     nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterTags(tt.tags, tt.prefix, tt.strategy)
			if len(got) != len(tt.want) {
				t.Fatalf("FilterTags() returned %d tags, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("FilterTags()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTagPatternRegex(t *testing.T) {
	tests := []struct {
		prefix   string
		strategy string
		wantRe   string
	}{
		{"python-api-v", "semver", `^python-api-v\d+\.\d+\.\d+`},
		{"v", "semver", `^v\d+\.\d+\.\d+`},
		{"", "semver", `^\d+\.\d+\.\d+`},
		{"ts-spa-", "calver", `^ts-spa-\d{4}\.\d{2}\.\d+`},
		{"", "calver", `^\d{4}\.\d{2}\.\d+`},
	}
	for _, tt := range tests {
		t.Run(tt.prefix+"/"+tt.strategy, func(t *testing.T) {
			got := TagPatternRegex(tt.prefix, tt.strategy)
			if got != tt.wantRe {
				t.Errorf("TagPatternRegex(%q, %q) = %q, want %q", tt.prefix, tt.strategy, got, tt.wantRe)
			}
		})
	}
}

func TestParseCalVerVersion(t *testing.T) {
	tests := []struct {
		input       string
		wantMonth   string
		wantCounter int
		wantErr     bool
	}{
		{"2026.03.0", "2026.03", 0, false},
		{"2026.03.2", "2026.03", 2, false},
		{"2026.03.10", "2026.03", 10, false},
		{"bad", "", 0, true},
		{"2026.03", "", 0, true},
		{"2026.03.abc", "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			month, counter, err := ParseCalVerVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if month != tt.wantMonth {
				t.Errorf("month = %q, want %q", month, tt.wantMonth)
			}
			if counter != tt.wantCounter {
				t.Errorf("counter = %d, want %d", counter, tt.wantCounter)
			}
		})
	}
}

func TestTagPatternRegex_DoesNotMatchForeignPrefix(t *testing.T) {
	re := regexp.MustCompile(TagPatternRegex("v", "semver"))
	if re.MatchString("go-service-v1.16.1") {
		t.Fatal("pattern for prefix v must not match go-service-v1.16.1")
	}
	if !re.MatchString("v0.2.1") {
		t.Fatal("pattern for prefix v must match v0.2.1")
	}
}
