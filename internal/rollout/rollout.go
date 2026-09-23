// Package rollout orchestrates the matrix-driven and direct deploy flows
// (`tamedia rollout`). Resolves environments → updates values files →
// atomic commit OR creates PRs.
package rollout

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/dnd-it/tamci/internal/gh"
	"github.com/dnd-it/tamci/internal/git"
	"github.com/dnd-it/tamci/internal/yamlx"
)

// Options are inputs for matrix-mode runs.
type Options struct {
	Service       string
	Version       string
	SHA           string
	Token         string
	ConfigPath    string
	ChartsDir     string
	GitUserName   string
	GitUserEmail  string
	DryRun        bool
	Owner         string
	Repo          string
	WorkDir       string
	GitHubBaseURL string
}

// DirectOptions are inputs for direct-mode runs (single value, list of files).
type DirectOptions struct {
	Files         []string
	Value         string
	Mode          string
	Key           string
	Deploy        string
	Branch        string
	AutoMerge     bool
	MergeMethod   string
	Token         string
	Owner         string
	Repo          string
	GitUserName   string
	GitUserEmail  string
	WorkDir       string
	DryRun        bool
	GitHubBaseURL string
	CommitMessage string
}

// FileDiff captures the before/after value of a single updated file.
type FileDiff struct {
	File     string `json:"file"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
}

// EnvResult captures the per-environment deploy outcome for the step summary.
type EnvResult struct {
	Name   string
	Tag    string
	Status string // "deployed" | "pr-opened" | "pr-updated" | "dry-run"
	PRURL  string
}

// Result summarises what was done.
type Result struct {
	Deployed     bool
	Environments []string
	CommitSHA    string
	PRURLs       []string
	DiffSummary  []FileDiff
	EnvResults   []EnvResult
}

// RunDirect applies Value to every file in Files (auto-commit or PR).
func RunDirect(opts DirectOptions) (*Result, error) {
	if len(opts.Files) == 0 {
		return nil, fmt.Errorf("direct mode: at least one file is required")
	}
	if opts.Value == "" {
		return nil, fmt.Errorf("direct mode: value is required")
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	if opts.Mode == "" {
		opts.Mode = "image"
	}
	if opts.Deploy == "" {
		opts.Deploy = "auto"
	}
	if opts.MergeMethod == "" {
		opts.MergeMethod = "SQUASH"
	}
	if opts.Mode == "key" && opts.Key == "" {
		return nil, fmt.Errorf("direct mode: key is required when mode=key")
	}
	if opts.Deploy == "pr" && opts.Branch == "" {
		return nil, fmt.Errorf("direct mode: branch is required when deploy=pr")
	}
	if opts.Deploy == "pr" && opts.Token == "" && !opts.DryRun {
		return nil, fmt.Errorf("direct mode: token is required when deploy=pr")
	}

	result := &Result{}
	updateOpts := yamlx.UpdateOptions{Mode: opts.Mode, Key: opts.Key}

	resolved := make([]string, len(opts.Files))
	for i, f := range opts.Files {
		if filepath.IsAbs(f) {
			resolved[i] = f
		} else {
			resolved[i] = filepath.Join(opts.WorkDir, f)
		}
	}

	oldTags := make(map[string]string, len(resolved))
	for _, f := range resolved {
		v, _ := yamlx.ReadTag(f, updateOpts)
		oldTags[f] = v
	}

	if opts.DryRun {
		for _, f := range resolved {
			slog.Info("[dry-run] would update", "file", f, "value", opts.Value, "mode", opts.Mode, "deploy", opts.Deploy)
			result.DiffSummary = append(result.DiffSummary, FileDiff{File: f, OldValue: oldTags[f], NewValue: opts.Value})
			result.EnvResults = append(result.EnvResults, EnvResult{Name: directName(f), Tag: opts.Value, Status: "dry-run"})
		}
		return result, nil
	}

	if err := preflightDirect(resolved, updateOpts); err != nil {
		return result, err
	}

	gc := &git.Client{Dir: opts.WorkDir, UserName: opts.GitUserName, UserEmail: opts.GitUserEmail}
	if err := gc.Configure(); err != nil {
		return result, err
	}

	switch opts.Deploy {
	case "auto":
		return runDirectAuto(opts, gc, resolved, updateOpts, oldTags, result)
	case "pr":
		return runDirectPR(opts, gc, resolved, updateOpts, oldTags, result)
	default:
		return nil, fmt.Errorf("direct mode: unknown deploy=%q (want auto|pr)", opts.Deploy)
	}
}

func preflightDirect(files []string, opts yamlx.UpdateOptions) error {
	for _, f := range files {
		ok, err := yamlx.HasTarget(f, opts)
		if err != nil {
			return fmt.Errorf("pre-flight %s: %w", f, err)
		}
		if !ok {
			return fmt.Errorf("pre-flight %s: no target found for mode=%q key=%q", f, opts.Mode, opts.Key)
		}
	}
	return nil
}

func runDirectAuto(opts DirectOptions, gc *git.Client, files []string, updateOpts yamlx.UpdateOptions, oldTags map[string]string, result *Result) (*Result, error) {
	for _, f := range files {
		if _, err := yamlx.SetTag(f, opts.Value, updateOpts); err != nil {
			return result, fmt.Errorf("%s: %w", f, err)
		}
		if err := gc.Add(f); err != nil {
			return result, err
		}
	}
	msg := directCommitMessage(opts, files, oldTags)
	if err := gc.Commit(msg); err != nil {
		return result, err
	}
	branch := currentBranch()
	if err := gc.Push(branch, 3); err != nil {
		return result, fmt.Errorf("push failed: %w", err)
	}
	if sha, err := gc.RevParse("HEAD"); err == nil {
		result.CommitSHA = sha
	}
	result.Deployed = true
	for _, f := range files {
		result.DiffSummary = append(result.DiffSummary, FileDiff{File: f, OldValue: oldTags[f], NewValue: opts.Value})
		result.EnvResults = append(result.EnvResults, EnvResult{Name: directName(f), Tag: opts.Value, Status: "deployed"})
	}
	return result, nil
}

func runDirectPR(opts DirectOptions, gc *git.Client, files []string, updateOpts yamlx.UpdateOptions, oldTags map[string]string, result *Result) (*Result, error) {
	base := defaultBranch(gc)
	if err := gc.CheckoutBranch(opts.Branch, base); err != nil {
		return result, fmt.Errorf("checkout %s: %w", opts.Branch, err)
	}
	for _, f := range files {
		if _, err := yamlx.SetTag(f, opts.Value, updateOpts); err != nil {
			return result, fmt.Errorf("%s: %w", f, err)
		}
		if err := gc.Add(f); err != nil {
			return result, err
		}
	}
	title := directCommitMessage(opts, files, oldTags)
	if err := gc.Commit(title); err != nil {
		return result, fmt.Errorf("commit: %w", err)
	}
	if err := gc.ForcePush(opts.Branch); err != nil {
		return result, err
	}

	var ghc *gh.RESTClient
	if opts.GitHubBaseURL != "" {
		ghc = gh.NewRESTWithBase(opts.GitHubBaseURL, opts.Token, opts.Owner, opts.Repo)
	} else {
		ghc = gh.NewREST(opts.Token, opts.Owner, opts.Repo)
	}

	body := buildDirectPRBody(files, oldTags, opts.Value)
	pr, err := ghc.EnsurePR(opts.Branch, base, title, body, []string{"deploy"})
	if err != nil {
		return result, err
	}
	slog.Info("PR ready", "url", pr.URL, "number", pr.Number)

	if opts.AutoMerge {
		if err := ghc.EnableAutoMerge(pr.NodeID, opts.MergeMethod); err != nil {
			slog.Warn("failed to enable auto-merge", "pr", pr.URL, "error", err)
		} else {
			slog.Info("auto-merge enabled", "pr", pr.URL, "method", opts.MergeMethod)
		}
	}

	result.Deployed = true
	result.PRURLs = append(result.PRURLs, pr.URL)
	for _, f := range files {
		result.DiffSummary = append(result.DiffSummary, FileDiff{File: f, OldValue: oldTags[f], NewValue: opts.Value})
		result.EnvResults = append(result.EnvResults, EnvResult{Name: directName(f), Tag: opts.Value, Status: "pr-opened", PRURL: pr.URL})
	}
	return result, nil
}

func directCommitMessage(opts DirectOptions, files []string, oldTags map[string]string) string {
	if opts.CommitMessage != "" {
		return opts.CommitMessage
	}
	scopes := make([]string, len(files))
	for i, f := range files {
		scopes[i] = directScope(f)
	}
	scope := strings.Join(scopes, ", ")
	if len(files) == 1 {
		if old := oldTags[files[0]]; old != "" && old != opts.Value {
			return fmt.Sprintf("deploy(%s): %s → %s", scope, old, opts.Value)
		}
	}
	return fmt.Sprintf("deploy(%s): %s", scope, opts.Value)
}

// directScope names a file by its service and environment when it follows
// the <service>/envs/<env>/ chart layout, else by its directory.
func directScope(file string) string {
	dir := filepath.ToSlash(filepath.Dir(file))
	if dir == "." {
		return filepath.Base(file)
	}
	parts := strings.Split(dir, "/")
	if n := len(parts); n >= 3 && parts[n-2] == "envs" {
		return parts[n-3] + "/" + parts[n-1]
	}
	return dir
}

func directName(file string) string { return filepath.Base(file) }

func buildDirectPRBody(files []string, oldTags map[string]string, newVal string) string {
	var sb strings.Builder
	sb.WriteString("## Update\n\n")
	sb.WriteString("| File | Before | After |\n|---|---|---|\n")
	for _, f := range files {
		fmt.Fprintf(&sb, "| `%s` | %s | `%s` |\n", f, valueOrMarkdown(oldTags[f]), newVal)
	}
	return sb.String()
}

func valueOrMarkdown(s string) string {
	if s == "" {
		return "_(none)_"
	}
	return "`" + s + "`"
}

// Run executes the matrix-mode deploy flow.
func Run(opts Options) (*Result, error) {
	if opts.Service == "" {
		return nil, fmt.Errorf("matrix mode: service is required")
	}
	if opts.Version == "" {
		return nil, fmt.Errorf("matrix mode: version is required")
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}

	cfg, err := LoadConfig(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	envs, err := cfg.Resolve(opts.Service)
	if err != nil {
		return nil, err
	}

	var autoEnvs, prEnvs []Environment
	for _, e := range envs {
		if e.Tag == "sha" && opts.SHA == "" {
			return nil, fmt.Errorf("matrix mode: environment %s uses tag: sha but no sha was provided", e.Name)
		}
		switch e.Deploy {
		case "auto":
			autoEnvs = append(autoEnvs, e)
		case "pr":
			prEnvs = append(prEnvs, e)
		default:
			slog.Warn("unknown deploy type, skipping", "environment", e.Name, "deploy", e.Deploy)
		}
	}
	if len(prEnvs) > 0 && opts.Token == "" && !opts.DryRun {
		return nil, fmt.Errorf("matrix mode: token is required for pr deploys")
	}

	result := &Result{}
	if err := deployAuto(opts, autoEnvs, result); err != nil {
		return result, err
	}
	if err := deployPR(opts, prEnvs, result); err != nil {
		return result, err
	}
	return result, nil
}

func deployAuto(opts Options, envs []Environment, result *Result) error {
	if len(envs) == 0 {
		return nil
	}

	gc := &git.Client{Dir: opts.WorkDir, UserName: opts.GitUserName, UserEmail: opts.GitUserEmail}
	if !opts.DryRun {
		if err := gc.Configure(); err != nil {
			return err
		}
	}

	for _, e := range envs {
		tag := resolveTag(e, opts)
		file, err := valuesPath(opts, e.Name)
		if err != nil {
			return fmt.Errorf("environment %s: %w", e.Name, err)
		}

		updateOpts := yamlx.UpdateOptions{Mode: e.ValuesMode, Key: e.ValuesKey}
		oldTag, _ := yamlx.ReadTag(file, updateOpts)

		slog.Info("updating values file", "environment", e.Name, "file", file, "tag", tag, "mode", e.ValuesMode)

		if opts.DryRun {
			slog.Info("[dry-run] would update", "file", file, "tag", tag)
			result.Environments = append(result.Environments, e.Name)
			result.DiffSummary = append(result.DiffSummary, FileDiff{File: file, OldValue: oldTag, NewValue: tag})
			result.EnvResults = append(result.EnvResults, EnvResult{Name: e.Name, Tag: tag, Status: "dry-run"})
			continue
		}

		if _, err := yamlx.SetTag(file, tag, updateOpts); err != nil {
			return fmt.Errorf("environment %s: %w", e.Name, err)
		}
		if err := gc.Add(file); err != nil {
			return err
		}
		result.Environments = append(result.Environments, e.Name)
		result.DiffSummary = append(result.DiffSummary, FileDiff{File: file, OldValue: oldTag, NewValue: tag})
		result.EnvResults = append(result.EnvResults, EnvResult{Name: e.Name, Tag: tag, Status: "deployed"})
	}

	if opts.DryRun {
		return nil
	}

	envList := strings.Join(result.Environments, ",")
	msg := fmt.Sprintf("deploy(%s/%s): %s", opts.Service, envList, opts.Version)
	if err := gc.Commit(msg); err != nil {
		return err
	}
	branch := currentBranch()
	if err := gc.Push(branch, 3); err != nil {
		return fmt.Errorf("push failed: %w", err)
	}
	if sha, err := gc.RevParse("HEAD"); err == nil {
		result.CommitSHA = sha
	}
	result.Deployed = len(result.Environments) > 0
	return nil
}

func deployPR(opts Options, envs []Environment, result *Result) error {
	if len(envs) == 0 {
		return nil
	}

	var ghc *gh.RESTClient
	if opts.GitHubBaseURL != "" {
		ghc = gh.NewRESTWithBase(opts.GitHubBaseURL, opts.Token, opts.Owner, opts.Repo)
	} else {
		ghc = gh.NewREST(opts.Token, opts.Owner, opts.Repo)
	}
	gc := &git.Client{Dir: opts.WorkDir, UserName: opts.GitUserName, UserEmail: opts.GitUserEmail}

	if !opts.DryRun {
		if err := gc.Configure(); err != nil {
			return err
		}
	}

	base := defaultBranch(gc)

	for _, e := range envs {
		tag := resolveTag(e, opts)
		file, err := valuesPath(opts, e.Name)
		if err != nil {
			return fmt.Errorf("environment %s: %w", e.Name, err)
		}
		updateOpts := yamlx.UpdateOptions{Mode: e.ValuesMode, Key: e.ValuesKey}

		oldTag, _ := yamlx.ReadTag(file, updateOpts)

		branch := fmt.Sprintf("deploy/%s/%s", opts.Service, e.Name)
		title := fmt.Sprintf("deploy(%s/%s): %s", opts.Service, e.Name, opts.Version)

		slog.Info("preparing deploy PR", "environment", e.Name, "branch", branch, "tag", tag, "base", base)

		if opts.DryRun {
			slog.Info("[dry-run] would create PR", "branch", branch, "title", title)
			result.Environments = append(result.Environments, e.Name)
			result.DiffSummary = append(result.DiffSummary, FileDiff{File: file, OldValue: oldTag, NewValue: tag})
			result.EnvResults = append(result.EnvResults, EnvResult{Name: e.Name, Tag: tag, Status: "dry-run"})
			continue
		}

		if err := gc.CheckoutBranch(branch, base); err != nil {
			return fmt.Errorf("environment %s: checkout %s: %w", e.Name, branch, err)
		}
		if _, err := yamlx.SetTag(file, tag, updateOpts); err != nil {
			return fmt.Errorf("environment %s: %w", e.Name, err)
		}
		if err := gc.Add(file); err != nil {
			return err
		}
		if err := gc.Commit(title); err != nil {
			return fmt.Errorf("environment %s: commit: %w", e.Name, err)
		}
		if err := gc.ForcePush(branch); err != nil {
			return fmt.Errorf("environment %s: %w", e.Name, err)
		}

		body := buildPRBody(opts, e.Name, oldTag, tag)
		pr, err := ghc.EnsurePR(branch, base, title, body, []string{"deploy"})
		if err != nil {
			return fmt.Errorf("environment %s: %w", e.Name, err)
		}
		status := "pr-updated"
		if pr.Number == 0 || oldTag == "" {
			status = "pr-opened"
		}
		slog.Info("PR ready", "url", pr.URL, "number", pr.Number)

		if e.AutoMerge {
			if err := ghc.EnableAutoMerge(pr.NodeID, e.MergeMethod); err != nil {
				slog.Warn("failed to enable auto-merge", "pr", pr.URL, "error", err)
			} else {
				slog.Info("auto-merge enabled", "pr", pr.URL, "method", e.MergeMethod)
			}
		}

		result.Environments = append(result.Environments, e.Name)
		result.PRURLs = append(result.PRURLs, pr.URL)
		result.DiffSummary = append(result.DiffSummary, FileDiff{File: file, OldValue: oldTag, NewValue: tag})
		result.EnvResults = append(result.EnvResults, EnvResult{Name: e.Name, Tag: tag, Status: status, PRURL: pr.URL})
		result.Deployed = true
	}
	return nil
}

func resolveTag(e Environment, opts Options) string {
	if e.Tag == "sha" {
		return opts.SHA
	}
	return opts.Version
}

func valuesPath(opts Options, env string) (string, error) {
	absCharts, err := filepath.Abs(opts.ChartsDir)
	if err != nil {
		return "", fmt.Errorf("charts dir abs: %w", err)
	}
	absCharts, err = filepath.EvalSymlinks(absCharts)
	if err != nil {
		return "", fmt.Errorf("charts dir %q not accessible: %w", opts.ChartsDir, err)
	}

	full := filepath.Clean(filepath.Join(absCharts, opts.Service, "envs", env, "values.yaml"))
	if !strings.HasPrefix(full, absCharts+string(os.PathSeparator)) {
		return "", fmt.Errorf("values path %q escapes charts dir %q", full, absCharts)
	}
	return full, nil
}

func currentBranch() string {
	if b := os.Getenv("GITHUB_REF_NAME"); b != "" {
		return b
	}
	return "main"
}

func defaultBranch(gc *git.Client) string {
	if b := gc.DefaultBranch(); b != "" {
		return b
	}
	if b := os.Getenv("GITHUB_DEFAULT_BRANCH"); b != "" {
		return b
	}
	return "main"
}

func buildPRBody(opts Options, env, oldTag, newTag string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Deploy: %s/%s\n\n", opts.Service, env)
	fmt.Fprintf(&sb, "|        | Tag |\n|---|---|\n")
	before := oldTag
	if before == "" {
		before = "_(none)_"
	} else {
		before = "`" + before + "`"
	}
	fmt.Fprintf(&sb, "| **Before** | %s |\n", before)
	fmt.Fprintf(&sb, "| **After** | `%s` |\n", newTag)

	if runID := os.Getenv("GITHUB_RUN_ID"); runID != "" && opts.Owner != "" && opts.Repo != "" {
		fmt.Fprintf(&sb, "\nTriggered by: https://github.com/%s/%s/actions/runs/%s\n", opts.Owner, opts.Repo, runID)
	}
	return sb.String()
}

// WriteOutputs writes GitHub Actions outputs to $GITHUB_OUTPUT.
func WriteOutputs(result *Result) error {
	outputFile := os.Getenv("GITHUB_OUTPUT")
	if outputFile == "" || result == nil {
		return nil
	}

	envsJSON, err := json.Marshal(result.Environments)
	if err != nil {
		return err
	}
	prsJSON, err := json.Marshal(result.PRURLs)
	if err != nil {
		return err
	}

	var diffLines []string
	var changedFiles []string
	for _, d := range result.DiffSummary {
		diffLines = append(diffLines, fmt.Sprintf("%s: %s → %s", d.File, valueOrNone(d.OldValue), d.NewValue))
		changedFiles = append(changedFiles, d.File)
	}

	deployed := "false"
	if result.Deployed {
		deployed = "true"
	}

	content := strings.Join([]string{
		"deployed=" + deployed,
		"environments=" + string(envsJSON),
		"commit_sha=" + result.CommitSHA,
		"pr_urls=" + string(prsJSON),
		"changed_files<<__EOF__\n" + strings.Join(changedFiles, "\n") + "\n__EOF__",
		"diff<<__EOF__\n" + strings.Join(diffLines, "\n") + "\n__EOF__",
	}, "\n") + "\n"

	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(content)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// WriteStepSummary writes a markdown table to $GITHUB_STEP_SUMMARY.
func WriteStepSummary(result *Result, service, version string) error {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" || result == nil {
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Deploy Summary: %s @ %s\n\n", service, version)
	fmt.Fprintf(&sb, "| Environment | Tag | Status | PR |\n|---|---|---|---|\n")
	for _, e := range result.EnvResults {
		pr := "—"
		if e.PRURL != "" {
			pr = e.PRURL
		}
		fmt.Fprintf(&sb, "| %s | `%s` | %s | %s |\n", e.Name, e.Tag, e.Status, pr)
	}
	if result.CommitSHA != "" {
		fmt.Fprintf(&sb, "\nCommit: `%s`\n", result.CommitSHA)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(sb.String())
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

func valueOrNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
