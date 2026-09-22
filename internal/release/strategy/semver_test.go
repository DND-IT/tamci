package strategy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/dnd-it/tamci/internal/release/config"
)

func monorepo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git-cliff"); err != nil {
		t.Skip("git-cliff not installed")
	}
	root, err := filepath.Abs("../../../cliff-templates")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLIFF_TEMPLATES_DIR", root)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Chdir(t.TempDir())
	git(t, "init", "-q")
	git(t, "config", "user.email", "t@example.com")
	git(t, "config", "user.name", "t")
}

func git(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commit(t *testing.T, file, msg string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(msg + "\n")
	_ = f.Close()
	git(t, "add", "-A")
	git(t, "commit", "-q", "-m", msg)
}

func TestSemver_NextVersion_ScopedToIncludePath(t *testing.T) {
	monorepo(t)
	commit(t, "svc/a", "feat(svc): initial")
	git(t, "tag", "svc/v1.0.0")
	cfg := config.Config{TagPrefix: "svc/v", IncludePath: "svc/**"}
	tags := []string{"svc/v1.0.0"}

	commit(t, "other/b", "feat(other): unrelated")
	got, err := (&Semver{}).NextVersion(tags, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped {
		t.Fatalf("change outside include-path released %q", got.Version)
	}

	commit(t, "svc/a", "fix(svc): real fix")
	got, err = (&Semver{}).NextVersion(tags, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.0.1" {
		t.Fatalf("version = %q, want 1.0.1 (patch from the in-path fix only)", got.Version)
	}
}

func TestSemver_NextVersion_BootstrapScopedToIncludePath(t *testing.T) {
	monorepo(t)
	commit(t, "other/b", "feat(other): unrelated")
	cfg := config.Config{TagPrefix: "svc/v", IncludePath: "svc/**"}

	got, err := (&Semver{}).NextVersion(nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped {
		t.Fatalf("first release with no commits under include-path: got %q", got.Version)
	}

	commit(t, "svc/a", "feat(svc): initial")
	if got, err = (&Semver{}).NextVersion(nil, cfg); err != nil || got.Version != "0.1.0" {
		t.Fatalf("got %+v, %v; want 0.1.0", got, err)
	}
}
