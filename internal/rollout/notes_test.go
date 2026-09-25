package rollout

import (
	"errors"
	"strings"
	"testing"

	"github.com/dnd-it/tamci/internal/gh"
)

type fakeLister struct {
	pages [][]gh.Release
	err   error
	calls int
}

func (f *fakeLister) ListReleases(page, _ int) ([]gh.Release, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if page > len(f.pages) {
		return nil, nil
	}
	return f.pages[page-1], nil
}

func tags(releases []gh.Release) string {
	out := make([]string, len(releases))
	for i, r := range releases {
		out[i] = r.TagName
	}
	return strings.Join(out, ",")
}

func TestReleasesBetween(t *testing.T) {
	single := []gh.Release{
		{TagName: "v1.4.0"},
		{TagName: "v1.3.1", Draft: true},
		{TagName: "v1.3.0"},
		{TagName: "v1.2.0"},
		{TagName: "v1.1.0"},
	}
	mono := []gh.Release{
		{TagName: "api/v1.4.0"},
		{TagName: "web/v2.0.0"},
		{TagName: "api/v1.3.0"},
		{TagName: "api/v1.2.0"},
	}
	cases := []struct {
		name     string
		releases []gh.Release
		prefix   string
		old, new string
		want     string
	}{
		{"range excludes old and drafts", single, "", "1.2.0", "1.4.0", "v1.4.0,v1.3.0"},
		{"bare tags match", []gh.Release{{TagName: "1.1.0"}, {TagName: "1.0.0"}}, "", "1.0.0", "1.1.0", "1.1.0"},
		{"unknown old shows only new", single, "", "abc123", "1.3.0", "v1.3.0"},
		{"empty old shows only new", single, "", "", "1.3.0", "v1.3.0"},
		{"unknown new shows nothing", single, "", "1.2.0", "deadbeef", ""},
		{"same tag shows nothing", single, "", "1.4.0", "1.4.0", ""},
		{"prefix skips other services", mono, "api/v", "1.2.0", "1.4.0", "api/v1.4.0,api/v1.3.0"},
		{"prefix is exact", mono, "api/v", "1.2.0", "2.0.0", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := releasesBetween(&fakeLister{pages: [][]gh.Release{tc.releases}}, tc.prefix, tc.old, tc.new)
			if err != nil {
				t.Fatal(err)
			}
			if tags(got) != tc.want {
				t.Errorf("got %q, want %q", tags(got), tc.want)
			}
		})
	}
}

func TestReleasesBetween_Pages(t *testing.T) {
	full := make([]gh.Release, releasesPerPage)
	full[0] = gh.Release{TagName: "v3.0.0"}
	for i := 1; i < releasesPerPage; i++ {
		full[i] = gh.Release{TagName: "other"}
	}
	f := &fakeLister{pages: [][]gh.Release{full, {{TagName: "v2.0.0"}}, {{TagName: "never"}}}}
	got, err := releasesBetween(f, "", "2.0.0", "3.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != releasesPerPage || got[0].TagName != "v3.0.0" {
		t.Errorf("got %d releases starting %q", len(got), got[0].TagName)
	}
	if f.calls != 2 {
		t.Errorf("fetched %d pages, want to stop after 2", f.calls)
	}
}

func TestReleaseNotes(t *testing.T) {
	f := &fakeLister{pages: [][]gh.Release{{
		{TagName: "v1.1.0", Name: "api 1.1.0", URL: "http://rel/1.1.0", Body: "### Features\n\n- new thing\n"},
		{TagName: "v1.0.1", URL: "http://rel/1.0.1"},
		{TagName: "v1.0.0"},
	}}}
	got := releaseNotes(f, "", "1.0.0", "1.1.0")
	for _, want := range []string{
		"## Release notes",
		`<a href="http://rel/1.1.0">api 1.1.0</a>`,
		"- new thing",
		`<a href="http://rel/1.0.1">v1.0.1</a>`,
		"_No release notes._",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("notes missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "1.1.0") > strings.Index(got, "1.0.1") {
		t.Error("releases not newest first")
	}
}

func TestReleaseNotes_ErrorIsEmpty(t *testing.T) {
	if got := releaseNotes(&fakeLister{err: errors.New("boom")}, "", "1.0.0", "1.1.0"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestReleaseNotesSection_SizeCap(t *testing.T) {
	big := strings.Repeat("x", maxNotesBodySize/2)
	got := releaseNotesSection([]gh.Release{
		{TagName: "v3", Body: big},
		{TagName: "v2", Body: big},
		{TagName: "v1", Body: big},
	})
	if len(got) > maxNotesBodySize+200 {
		t.Errorf("section is %d bytes", len(got))
	}
	if !strings.Contains(got, "_2 older releases not shown._") {
		t.Errorf("missing omission note")
	}
}

func TestCommonOldTag(t *testing.T) {
	files := []string{"a", "b"}
	if got := commonOldTag(files, map[string]string{"a": "1.0.0", "b": "1.0.0"}); got != "1.0.0" {
		t.Errorf("got %q", got)
	}
	if got := commonOldTag(files, map[string]string{"a": "1.0.0", "b": "0.9.0"}); got != "" {
		t.Errorf("got %q", got)
	}
}
