package promote

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dnd-it/tamci/internal/git"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitFile(t *testing.T, dir, file, msg string) string {
	t.Helper()
	path := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", file)
	gitCmd(t, dir, "commit", "-q", "-m", msg)
	return gitCmd(t, dir, "rev-parse", "HEAD")
}

// repoWithOrigin returns a clone whose origin has one commit on main, plus the
// SHA of that commit and of a commit that only exists on a local branch.
func repoWithOrigin(t *testing.T) (dir, onMain, offMain string) {
	t.Helper()
	origin := t.TempDir()
	gitCmd(t, origin, "init", "-q", "--bare", "-b", "main")
	dir = t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "main")
	gitCmd(t, dir, "remote", "add", "origin", origin)
	onMain = commitFile(t, dir, "a", "feat: a")
	gitCmd(t, dir, "push", "-q", "origin", "main")
	gitCmd(t, dir, "checkout", "-q", "-b", "side")
	offMain = commitFile(t, dir, "b", "feat: b")
	return dir, onMain, offMain
}

func writeEvent(t *testing.T, payload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolve(t *testing.T) {
	dir, onMain, offMain := repoWithOrigin(t)
	g := &git.Client{Dir: dir}
	rules := Rules{TagPrefix: "prod/infra/shared/v", Label: "deploy:dev", DefaultBranch: "main", Dev: "dev", Prod: "prod"}
	const prodTag = "prod/infra/shared/v1.2.3"

	tests := []struct {
		name    string
		event   string
		payload string
		refType string
		refName string
		sha     string
		want    Decision
		wantErr string
	}{
		{name: "pr opened without label plans", event: "pull_request", refType: "branch", refName: "1/merge",
			payload: `{"action":"opened","pull_request":{"labels":[{"name":"other"}]}}`,
			want:    Decision{Plan: true}},
		{name: "pr opened with label plans and applies dev", event: "pull_request",
			payload: `{"action":"opened","pull_request":{"labels":[{"name":"deploy:dev"}]}}`,
			want:    Decision{Plan: true, Apply: "dev"}},
		{name: "pr reopened with label", event: "pull_request",
			payload: `{"action":"reopened","pull_request":{"labels":[{"name":"deploy:dev"}]}}`,
			want:    Decision{Plan: true, Apply: "dev"}},
		{name: "pr synchronize without label", event: "pull_request",
			payload: `{"action":"synchronize","pull_request":{"labels":[]}}`,
			want:    Decision{Plan: true}},
		{name: "pr labeled with the label applies dev", event: "pull_request",
			payload: `{"action":"labeled","label":{"name":"deploy:dev"}}`,
			want:    Decision{Apply: "dev"}},
		{name: "pr labeled with another label does nothing", event: "pull_request",
			payload: `{"action":"labeled","label":{"name":"bug"},"pull_request":{"labels":[{"name":"deploy:dev"}]}}`},
		{name: "pr unlabeled the label releases", event: "pull_request",
			payload: `{"action":"unlabeled","label":{"name":"deploy:dev"}}`,
			want:    Decision{Release: true}},
		{name: "pr unlabeled another label does nothing", event: "pull_request",
			payload: `{"action":"unlabeled","label":{"name":"bug"}}`},
		{name: "pr closed releases", event: "pull_request",
			payload: `{"action":"closed"}`,
			want:    Decision{Release: true}},
		{name: "pr edited does nothing", event: "pull_request",
			payload: `{"action":"edited"}`},
		{name: "push to default branch applies dev", event: "push", refType: "branch", refName: "main",
			want: Decision{Apply: "dev"}},
		{name: "push to another branch fails", event: "push", refType: "branch", refName: "feature",
			wantErr: "dev is applied from main or from a pull request labelled deploy:dev; label the pull request instead"},
		{name: "push of prod tag on default branch applies prod", event: "push", refType: "tag", refName: prodTag, sha: onMain,
			want: Decision{Apply: "prod"}},
		{name: "push of prod tag off default branch fails", event: "push", refType: "tag", refName: prodTag, sha: offMain,
			wantErr: prodTag + " points at " + offMain + ", which is not on main"},
		{name: "push of another tag fails", event: "push", refType: "tag", refName: "prod/infra/other/v1.0.0", sha: onMain,
			wantErr: "prod is only applied from a prod/infra/shared/v<semver> tag, not prod/infra/other/v1.0.0"},
		{name: "dispatch without environment applies dev from default branch", event: "workflow_dispatch", refType: "branch", refName: "main",
			payload: `{"inputs":{}}`,
			want:    Decision{Apply: "dev"}},
		{name: "dispatch dev from another branch fails", event: "workflow_dispatch", refType: "branch", refName: "feature",
			payload: `{"inputs":{"environment":"dev"}}`,
			wantErr: "label the pull request instead"},
		{name: "dispatch prod on prod tag applies prod", event: "workflow_dispatch", refType: "tag", refName: prodTag, sha: onMain,
			payload: `{"inputs":{"environment":"prod"}}`,
			want:    Decision{Apply: "prod"}},
		{name: "dispatch prod from a branch fails", event: "workflow_dispatch", refType: "branch", refName: "main", sha: onMain,
			payload: `{"inputs":{"environment":"prod"}}`,
			wantErr: "prod is only applied from a prod/infra/shared/v<semver> tag, not main"},
		{name: "dispatch prod on tag off default branch fails", event: "workflow_dispatch", refType: "tag", refName: prodTag, sha: offMain,
			payload: `{"inputs":{"environment":"prod"}}`,
			wantErr: "which is not on main"},
		{name: "dispatch unknown environment fails", event: "workflow_dispatch", refType: "branch", refName: "main",
			payload: `{"inputs":{"environment":"staging"}}`,
			wantErr: "unknown environment staging"},
		{name: "other events do nothing", event: "schedule", refType: "branch", refName: "main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := ""
			if tt.payload != "" {
				path = writeEvent(t, tt.payload)
			}
			ev, err := LoadEvent(tt.event, path, tt.refType, tt.refName, tt.sha)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Resolve(ev, rules, g)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadEvent_DefaultBranch(t *testing.T) {
	ev, err := LoadEvent("push", writeEvent(t, `{"repository":{"default_branch":"trunk"}}`), "branch", "trunk", "")
	if err != nil {
		t.Fatal(err)
	}
	if ev.DefaultBranch != "trunk" {
		t.Errorf("default branch = %q, want trunk", ev.DefaultBranch)
	}
}
