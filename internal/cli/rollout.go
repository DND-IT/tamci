package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/dnd-it/tamci/internal/rollout"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newRolloutCmd() *cobra.Command {
	v := newViper()

	cmd := &cobra.Command{
		Use:   "rollout",
		Short: "Matrix-driven Helm values updates (matrix.config.yaml) or direct file mode.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindFlags(cmd, v)
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return runRollout(v)
		},
	}

	f := cmd.Flags()
	// Matrix mode
	f.String("service", "", "Service name (must match a key under service: in matrix config).")
	f.String("version", "", "Release version string (e.g. 1.4.0).")
	f.String("sha", "", "Short commit SHA (used when an environment has tag: sha).")
	f.String("config", ".github/matrix-config.yaml", "Path to the matrix config file.")
	f.String("charts-dir", "deploy/charts", "Root directory for per-service Helm chart values.")
	// Direct mode
	f.String("file", "", "Newline-separated YAML file paths. Triggers direct mode.")
	f.String("value", "", "New value to write (direct mode).")
	f.String("mode", "image", "Update mode: image | key | marker (direct mode).")
	f.String("key", "", "Dot-notation key path (direct mode, mode=key).")
	f.String("deploy", "auto", "auto | pr (direct mode).")
	f.String("branch", "", "PR branch name (direct mode, deploy=pr).")
	f.String("commit-message", "", "Override commit message (direct mode).")
	f.Bool("auto-merge", false, "Enable auto-merge on the deploy PR (direct mode, deploy=pr).")
	f.String("merge-method", "SQUASH", "MERGE | SQUASH | REBASE (direct mode, deploy=pr).")
	// Shared
	f.String("token", "", "GitHub token with contents:write and pull-requests:write.")
	f.String("git-user-name", "github-actions[bot]", "Git commit author name.")
	f.String("git-user-email", "github-actions[bot]@users.noreply.github.com", "Git commit author email.")
	f.Bool("dry-run", false, "Print plan without pushing or creating PRs.")

	return cmd
}

func runRollout(v *viper.Viper) error {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if runID := os.Getenv("GITHUB_RUN_ID"); runID != "" {
		slog.SetDefault(slog.Default().With("run_id", runID))
	}

	repo := os.Getenv("GITHUB_REPOSITORY")
	owner, repoName, _ := strings.Cut(repo, "/")

	var (
		result *rollout.Result
		err    error
		mode   string
	)

	if files := splitLinesField(v.GetString("file")); len(files) > 0 {
		mode = "direct"
		result, err = rollout.RunDirect(rollout.DirectOptions{
			Files:         files,
			Value:         requireString(v, "value"),
			Mode:          stringOrDefault(v, "mode", "image"),
			Key:           v.GetString("key"),
			Deploy:        stringOrDefault(v, "deploy", "auto"),
			Branch:        v.GetString("branch"),
			AutoMerge:     v.GetBool("auto-merge"),
			MergeMethod:   stringOrDefault(v, "merge-method", "SQUASH"),
			Token:         requireString(v, "token"),
			GitUserName:   stringOrDefault(v, "git-user-name", "github-actions[bot]"),
			GitUserEmail:  stringOrDefault(v, "git-user-email", "github-actions[bot]@users.noreply.github.com"),
			DryRun:        v.GetBool("dry-run"),
			Owner:         owner,
			Repo:          repoName,
			WorkDir:       ".",
			CommitMessage: v.GetString("commit-message"),
		})
	} else {
		mode = "matrix"
		result, err = rollout.Run(rollout.Options{
			Service:      requireString(v, "service"),
			Version:      requireString(v, "version"),
			SHA:          requireString(v, "sha"),
			Token:        requireString(v, "token"),
			ConfigPath:   stringOrDefault(v, "config", ".github/matrix-config.yaml"),
			ChartsDir:    stringOrDefault(v, "charts-dir", "deploy/charts"),
			GitUserName:  stringOrDefault(v, "git-user-name", "github-actions[bot]"),
			GitUserEmail: stringOrDefault(v, "git-user-email", "github-actions[bot]@users.noreply.github.com"),
			DryRun:       v.GetBool("dry-run"),
			Owner:        owner,
			Repo:         repoName,
			WorkDir:      ".",
		})
	}

	version := v.GetString("version")
	if version == "" {
		version = v.GetString("value")
	}
	label := rolloutModeLabel(mode, v)

	if err != nil {
		_ = rollout.WriteOutputs(result)
		_ = rollout.WriteStepSummary(result, label, version)
		return fmt.Errorf("rollout failed (%s mode): %w", mode, err)
	}
	if err := rollout.WriteOutputs(result); err != nil {
		return fmt.Errorf("write outputs: %w", err)
	}
	if err := rollout.WriteStepSummary(result, label, version); err != nil {
		slog.Warn("writing step summary", "error", err)
	}
	slog.Info("done",
		"mode", mode,
		"deployed", result.Deployed,
		"environments", result.Environments,
		"commit_sha", result.CommitSHA,
		"pr_count", len(result.PRURLs),
	)
	return nil
}

func rolloutModeLabel(mode string, v *viper.Viper) string {
	if mode == "direct" {
		return "direct"
	}
	if s := v.GetString("service"); s != "" {
		return s
	}
	return "matrix"
}

func splitLinesField(s string) []string { return parseLines(s) }

func requireString(v *viper.Viper, key string) string {
	val := v.GetString(key)
	if val == "" {
		// Surface a clean error from the inner call. The deployer used to os.Exit,
		// but cobra/viper let us return an error instead.
		// Note: we intentionally don't fail here — the inner Run/RunDirect
		// will return its own validation error. This wrapper is a no-op.
	}
	return val
}

func stringOrDefault(v *viper.Viper, key, def string) string {
	if val := v.GetString(key); val != "" {
		return val
	}
	return def
}
