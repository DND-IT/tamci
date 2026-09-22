package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// commit appends msg to file in the repo at dir and commits it with msg as
// the subject.
func commit(t *testing.T, dir, file, msg string) {
	t.Helper()
	path := filepath.Join(dir, file)
	existing, _ := os.ReadFile(path)
	writeFile(t, path, string(existing)+msg+"\n")
	git(t, dir, "add", file)
	git(t, dir, "commit", "-q", "-m", msg)
}

// monorepo returns a repository holding two services, where console has been
// released once as console/v1.0.0.
func monorepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git-cliff"); err != nil {
		t.Skip("git-cliff not installed")
	}
	isolateGit(t)
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	commit(t, dir, "projects/console/main.go", "feat(console): initial")
	commit(t, dir, "projects/bot/main.go", "feat(bot): initial")
	git(t, dir, "tag", "-a", "-m", "console/v1.0.0", "console/v1.0.0")
	return dir
}

func releaseConsole(t *testing.T, dir string) result {
	t.Helper()
	templates, err := filepath.Abs("../cliff-templates")
	if err != nil {
		t.Fatal(err)
	}
	r := tamci(t, dir, map[string]string{
		"CLIFF_TEMPLATES_DIR": templates,
		"GITHUB_REPOSITORY":   "DND-IT/app",
	}, "release", "--tag-prefix", "console/v", "--include-path", "projects/console/**", "--dry-run")
	if r.err != nil {
		t.Fatalf("release: %v", r.err)
	}
	return r
}

func TestRelease_ScopedToIncludePath(t *testing.T) {
	dir := monorepo(t)

	commit(t, dir, "stacks/infra/main.tf", "feat(infra): add the token broker")
	commit(t, dir, "projects/console/README.md", "docs(console): explain setup")
	if r := releaseConsole(t, dir); r.outputs["skipped"] != "true" {
		t.Fatalf("released %q for changes outside the path or not worth a release", r.outputs["version"])
	}

	commit(t, dir, "projects/console/main.go", "fix(console): handle empty sessions")
	r := releaseConsole(t, dir)
	if r.outputs["skipped"] != "false" || r.outputs["version"] != "1.0.1" {
		t.Fatalf("skipped=%q version=%q, want a 1.0.1 patch release", r.outputs["skipped"], r.outputs["version"])
	}
	notes := r.outputs["changelog"]
	if !strings.Contains(notes, "### Bug Fixes") || !strings.Contains(notes, "**console:** Handle empty sessions") {
		t.Errorf("release notes miss the console fix:\n%s", notes)
	}
	if strings.Contains(notes, "token broker") || strings.Contains(notes, "explain setup") {
		t.Errorf("release notes include commits that should be left out:\n%s", notes)
	}
}

func TestRelease_BreakingChangeMarked(t *testing.T) {
	dir := monorepo(t)

	commit(t, dir, "projects/console/main.go", "chore(console)!: require Go 1.26")
	r := releaseConsole(t, dir)
	if r.outputs["version"] != "2.0.0" {
		t.Fatalf("version = %q, want 2.0.0", r.outputs["version"])
	}
	if !strings.Contains(r.outputs["changelog"], "**console:** [**breaking**] Require Go 1.26") {
		t.Errorf("breaking change missing from notes:\n%s", r.outputs["changelog"])
	}
}
