package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dnd-it/tamci/internal/gh"
	"github.com/dnd-it/tamci/internal/gha"
	"github.com/dnd-it/tamci/internal/git"
	"github.com/dnd-it/tamci/internal/yamlx"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newSetCmd() *cobra.Command {
	v := newViper()

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Update YAML files with format preservation; optionally open a PR.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindFlags(cmd, v)
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSet(v)
		},
	}

	f := cmd.Flags()
	f.String("files", "", "Newline-separated YAML file paths.")
	f.String("files-from", "", "Directory to recursively search for YAML files.")
	f.String("files-filter", "", "Filename filter for files-from discovery (e.g. values.yaml).")
	f.String("mode", "key", "Update mode: key | image | marker.")
	f.String("marker", "x-yaml-update", "Single comment marker (mode=marker).")
	f.String("markers", "", "Newline-separated markers (mode=marker).")
	f.String("keys", "", "Newline-separated dot-notation paths (mode=key).")
	f.String("values", "", "Newline-separated values matching keys/markers.")
	f.String("value", "", "Single value applied to all keys/markers.")
	f.String("image-name", "", "Image name suffix to search for (mode=image).")
	f.String("image-tag", "", "New tag value (mode=image).")
	f.Bool("create-pr", true, "Open a PR instead of committing directly.")
	f.String("target-branch", "", "Base branch for the PR or direct commit target.")
	f.String("pr-branch", "", "PR branch name (auto-generated if empty).")
	f.String("pr-title", "chore: update YAML values", "Pull request title.")
	f.String("pr-body", "", "Pull request body (auto-generated if empty).")
	f.String("pr-labels", "", "Comma-separated PR labels.")
	f.String("pr-reviewers", "", "Comma-separated reviewer usernames.")
	f.String("commit-message", "chore: update YAML values", "Commit message.")
	f.String("token", "", "GitHub token (falls back to GITHUB_TOKEN env).")
	f.Bool("auto-merge", false, "Enable auto-merge on the PR.")
	f.String("merge-method", "SQUASH", "Merge method: MERGE | SQUASH | REBASE.")
	f.Bool("dry-run", false, "Preview changes without modifying anything.")
	f.String("git-user-name", "github-actions[bot]", "Git committer name.")
	f.String("git-user-email", "41898282+github-actions[bot]@users.noreply.github.com", "Git committer email.")

	return cmd
}

type setConfig struct {
	files            []string
	mode             string
	keys, values     []string
	value            string
	markers          []string
	markerValues     []string
	imageName        string
	imageTag         string
	createPR         bool
	targetBranch     string
	prBranch         string
	prTitle          string
	prBody           string
	prLabels         []string
	prReviewers      []string
	commitMessage    string
	token            string
	autoMerge        bool
	mergeMethod      string
	dryRun           bool
	gitUserName      string
	gitUserEmail     string
	githubRepo       string
	githubServerURL  string
	githubAPIURL     string
	githubGraphQLURL string
}

func loadSetConfig(v *viper.Viper) (*setConfig, error) {
	cfg := &setConfig{
		mode:             v.GetString("mode"),
		value:            v.GetString("value"),
		imageName:        v.GetString("image-name"),
		imageTag:         v.GetString("image-tag"),
		createPR:         v.GetBool("create-pr"),
		targetBranch:     v.GetString("target-branch"),
		prBranch:         v.GetString("pr-branch"),
		prTitle:          v.GetString("pr-title"),
		prBody:           v.GetString("pr-body"),
		commitMessage:    v.GetString("commit-message"),
		token:            v.GetString("token"),
		autoMerge:        v.GetBool("auto-merge"),
		mergeMethod:      v.GetString("merge-method"),
		dryRun:           v.GetBool("dry-run"),
		gitUserName:      v.GetString("git-user-name"),
		gitUserEmail:     v.GetString("git-user-email"),
		githubRepo:       os.Getenv("GITHUB_REPOSITORY"),
		githubServerURL:  envOrDefault("GITHUB_SERVER_URL", "https://github.com"),
		githubAPIURL:     envOrDefault("GITHUB_API_URL", "https://api.github.com"),
		githubGraphQLURL: envOrDefault("GITHUB_GRAPHQL_URL", "https://api.github.com/graphql"),
	}

	if cfg.token == "" {
		cfg.token = os.Getenv("GITHUB_TOKEN")
	}

	cfg.files = parseLines(v.GetString("files"))
	if dir := v.GetString("files-from"); dir != "" {
		discovered, err := discoverYAMLFiles(dir, v.GetString("files-filter"))
		if err != nil {
			return nil, fmt.Errorf("files-from discovery: %w", err)
		}
		cfg.files = mergeStringSlices(cfg.files, discovered)
	}
	if len(cfg.files) == 0 {
		return nil, fmt.Errorf("no files to process: set --files and/or --files-from")
	}

	if cfg.mode != "key" && cfg.mode != "image" && cfg.mode != "marker" {
		return nil, fmt.Errorf("invalid mode %q: must be 'key', 'image', or 'marker'", cfg.mode)
	}

	switch cfg.mode {
	case "key":
		cfg.keys = parseLines(v.GetString("keys"))
		cfg.values = parseLines(v.GetString("values"))
		if len(cfg.keys) == 0 {
			return nil, fmt.Errorf("--keys is required for mode=key")
		}
		if cfg.value != "" && len(cfg.values) == 0 {
			cfg.values = make([]string, len(cfg.keys))
			for i := range cfg.values {
				cfg.values[i] = cfg.value
			}
		}
		if len(cfg.values) == 0 {
			return nil, fmt.Errorf("--values or --value is required for mode=key")
		}
		if len(cfg.keys) != len(cfg.values) {
			return nil, fmt.Errorf("keys (%d) must match values (%d)", len(cfg.keys), len(cfg.values))
		}
	case "image":
		if cfg.imageName == "" {
			return nil, fmt.Errorf("--image-name is required for mode=image")
		}
		if cfg.imageTag == "" {
			return nil, fmt.Errorf("--image-tag is required for mode=image")
		}
	case "marker":
		cfg.markers = parseLines(v.GetString("markers"))
		cfg.markerValues = parseLines(v.GetString("values"))
		if len(cfg.markers) == 0 {
			cfg.markers = []string{v.GetString("marker")}
		}
		if cfg.value != "" && len(cfg.markerValues) == 0 {
			cfg.markerValues = make([]string, len(cfg.markers))
			for i := range cfg.markerValues {
				cfg.markerValues[i] = cfg.value
			}
		}
		if len(cfg.markerValues) == 0 {
			return nil, fmt.Errorf("--value or --values is required for mode=marker")
		}
		if len(cfg.markers) != len(cfg.markerValues) {
			return nil, fmt.Errorf("markers (%d) must match values (%d)", len(cfg.markers), len(cfg.markerValues))
		}
	}

	cfg.prLabels = parseCSV(v.GetString("pr-labels"))
	cfg.prReviewers = parseCSV(v.GetString("pr-reviewers"))

	return cfg, nil
}

func runSet(v *viper.Viper) error {
	ctx := context.Background()

	cfg, err := loadSetConfig(v)
	if err != nil {
		return err
	}

	fmt.Printf("Mode: %s\nFiles: %s\n", cfg.mode, strings.Join(cfg.files, ", "))
	if cfg.dryRun {
		fmt.Println("Dry run mode enabled — no changes will be persisted")
	}

	var allChanges []yamlx.Change
	var changedFiles []string
	var allDiffs []string

	for _, filePath := range cfg.files {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", filePath)
		}

		original, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", filePath, err)
		}

		doc, err := yamlx.LoadYAML(original)
		if err != nil {
			return fmt.Errorf("parse %s: %w", filePath, err)
		}
		if doc == nil || doc.Root == nil {
			gha.Warning(fmt.Sprintf("Skipping empty YAML file: %s", filePath))
			continue
		}

		var changes []yamlx.Change
		switch cfg.mode {
		case "key":
			changes, err = yamlx.UpdateKeys(doc, cfg.keys, cfg.values)
			if err != nil {
				return fmt.Errorf("update %s: %w", filePath, err)
			}
		case "image":
			changes = yamlx.UpdateImageTags(doc, cfg.imageName, cfg.imageTag)
		case "marker":
			for i, marker := range cfg.markers {
				changes = append(changes, yamlx.UpdateByMarker(doc, marker, cfg.markerValues[i])...)
			}
		}

		if len(changes) == 0 {
			fmt.Printf("  No changes needed for %s\n", filePath)
			continue
		}

		for _, c := range changes {
			fmt.Printf("  %s: %v -> %v\n", c.Key, c.Old, c.New)
		}
		allChanges = append(allChanges, changes...)
		changedFiles = append(changedFiles, filePath)

		newContent, err := yamlx.DumpYAML(doc)
		if err != nil {
			return fmt.Errorf("dump %s: %w", filePath, err)
		}
		if d := yamlx.Diff(filePath, original, newContent); d != "" {
			allDiffs = append(allDiffs, d)
		}
		if !cfg.dryRun {
			if err := os.WriteFile(filePath, newContent, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", filePath, err)
			}
		}
	}

	hasChanges := len(changedFiles) > 0
	_ = gha.SetOutput("changed", fmt.Sprintf("%t", hasChanges))
	_ = gha.SetOutput("changed_files", strings.Join(changedFiles, "\n"))
	diffText := strings.Join(allDiffs, "\n")
	_ = gha.SetOutput("diff", diffText)

	if !hasChanges {
		fmt.Println("No changes detected across any files")
		_ = gha.SetOutput("pr_number", "")
		_ = gha.SetOutput("pr_url", "")
		_ = gha.SetOutput("commit_sha", "")
		return nil
	}
	if cfg.dryRun {
		fmt.Println("Dry run complete. Changes that would be made:")
		if diffText != "" {
			fmt.Println(diffText)
		}
		_ = gha.SetOutput("pr_number", "")
		_ = gha.SetOutput("pr_url", "")
		_ = gha.SetOutput("commit_sha", "")
		return nil
	}

	owner, repo := splitRepo(cfg.githubRepo)

	if err := git.Configure(cfg.gitUserName, cfg.gitUserEmail, cfg.token, cfg.githubRepo, cfg.githubServerURL); err != nil {
		return fmt.Errorf("git configure: %w", err)
	}

	targetBranch := cfg.targetBranch
	if targetBranch == "" {
		targetBranch = git.GetDefaultBranch()
	}

	commitBranch := targetBranch
	if cfg.createPR {
		prBranch := cfg.prBranch
		if prBranch == "" {
			prBranch = generateSetBranchName(cfg)
		}
		if err := git.CreateBranch(prBranch, targetBranch); err != nil {
			return fmt.Errorf("create branch: %w", err)
		}
		commitBranch = prBranch
	}

	sha, err := git.CommitAndPush(changedFiles, cfg.commitMessage, commitBranch)
	if err != nil {
		return fmt.Errorf("commit and push: %w", err)
	}
	_ = gha.SetOutput("commit_sha", sha)
	fmt.Printf("Committed and pushed: %s\n", sha)

	if !cfg.createPR {
		_ = gha.SetOutput("pr_number", "")
		_ = gha.SetOutput("pr_url", "")
		return nil
	}

	prBody := cfg.prBody
	if prBody == "" {
		var lines []string
		for _, c := range allChanges {
			lines = append(lines, fmt.Sprintf("- `%s`: `%v` → `%v`", c.Key, c.Old, c.New))
		}
		prBody = "## Changes\n\n" + strings.Join(lines, "\n")
	}

	prData, err := gh.FindPullRequest(ctx, cfg.githubAPIURL, cfg.token, owner, repo, commitBranch)
	if err != nil {
		gha.Warning(fmt.Sprintf("Failed to check for existing PR: %v", err))
	}

	if prData != nil {
		prData, err = gh.UpdatePullRequest(ctx, cfg.githubAPIURL, cfg.token, owner, repo, prData.Number, cfg.prTitle, prBody)
		if err != nil {
			return fmt.Errorf("update pull request: %w", err)
		}
		fmt.Printf("Updated PR #%d: %s\n", prData.Number, prData.HTMLURL)
	} else {
		prData, err = gh.CreatePullRequest(ctx, cfg.githubAPIURL, cfg.token, owner, repo, cfg.prTitle, prBody, commitBranch, targetBranch)
		if err != nil {
			return fmt.Errorf("create pull request: %w", err)
		}
		fmt.Printf("Created PR #%d: %s\n", prData.Number, prData.HTMLURL)
	}

	_ = gha.SetOutput("pr_number", fmt.Sprintf("%d", prData.Number))
	_ = gha.SetOutput("pr_url", prData.HTMLURL)

	if len(cfg.prLabels) > 0 {
		if err := gh.AddLabels(ctx, cfg.githubAPIURL, cfg.token, owner, repo, prData.Number, cfg.prLabels); err != nil {
			gha.Warning(fmt.Sprintf("Failed to add labels: %v", err))
		} else {
			fmt.Printf("Added labels: %s\n", strings.Join(cfg.prLabels, ", "))
		}
	}
	if len(cfg.prReviewers) > 0 {
		if err := gh.RequestReviewers(ctx, cfg.githubAPIURL, cfg.token, owner, repo, prData.Number, cfg.prReviewers); err != nil {
			gha.Warning(fmt.Sprintf("Failed to request reviewers: %v", err))
		} else {
			fmt.Printf("Requested reviewers: %s\n", strings.Join(cfg.prReviewers, ", "))
		}
	}
	if cfg.autoMerge && prData.NodeID != "" {
		if err := gh.EnableAutoMerge(ctx, cfg.githubGraphQLURL, cfg.token, prData.NodeID, cfg.mergeMethod); err != nil {
			gha.Warning(fmt.Sprintf("Failed to enable auto-merge: %v", err))
		} else {
			fmt.Printf("Enabled auto-merge (%s)\n", cfg.mergeMethod)
		}
	}
	return nil
}

func generateSetBranchName(cfg *setConfig) string {
	seed := fmt.Sprintf("%v%v%v%s%s", cfg.files, cfg.keys, cfg.values, cfg.imageName, cfg.imageTag)
	hash := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("yaml-update/%x-%d", hash[:4], time.Now().Unix())
}

func discoverYAMLFiles(dir, filter string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("directory not found: %s", dir)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}
	var files []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}
		if filter != "" && d.Name() != filter {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func mergeStringSlices(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	var out []string
	for _, f := range a {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, f := range b {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

func parseLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(s, "\n") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func parseCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitRepo(repo string) (owner, name string) {
	if parts := strings.SplitN(repo, "/", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}
