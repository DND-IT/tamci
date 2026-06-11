package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/dnd-it/tamci/internal/gha"
	"github.com/dnd-it/tamci/internal/release/changelog"
	"github.com/dnd-it/tamci/internal/release/config"
	"github.com/dnd-it/tamci/internal/release/gitutil"
	"github.com/dnd-it/tamci/internal/release/publish"
	"github.com/dnd-it/tamci/internal/release/releasepr"
	"github.com/dnd-it/tamci/internal/release/strategy"
	"github.com/google/go-github/v68/github"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newReleaseCmd() *cobra.Command {
	v := newViper()

	cmd := &cobra.Command{
		Use:   "release",
		Short: "Calculate next version and create a tag/release via git-cliff.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindFlags(cmd, v)
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return runRelease(v)
		},
	}

	f := cmd.Flags()
	f.String("version-strategy", "semver", "Versioning strategy: semver | calver.")
	f.String("cliff-config", "", "Path to custom cliff.toml (auto-detect if empty).")
	f.String("tag-prefix", "", "Prefix for git tags (e.g. v).")
	f.String("release-mode", "direct", "Release mode: direct | pr.")
	f.Bool("draft", false, "Create release as draft.")
	f.Bool("prerelease", false, "Mark release as prerelease.")
	f.String("include-path", "", "Glob pattern to scope commits by file path (monorepos).")
	f.Bool("dry-run", false, "Calculate version and changelog without creating tag/release.")
	f.String("github-token", "", "GitHub token (falls back to GITHUB_TOKEN).")

	return cmd
}

func runRelease(v *viper.Viper) error {
	log.SetPrefix("[release] ")
	log.SetFlags(0)

	// Bridge cobra flags into the env vars that config.Load() expects.
	// config.Load() reads .release.yml + INPUT_* env vars natively, so we just
	// set INPUT_<NAME> for every flag the user explicitly passed.
	bridgeFlagsToEnv(v, "version-strategy", "cliff-config", "tag-prefix", "release-mode", "include-path")
	if v.GetBool("draft") {
		_ = os.Setenv("INPUT_DRAFT", "true")
	}
	if v.GetBool("prerelease") {
		_ = os.Setenv("INPUT_PRERELEASE", "true")
	}
	if v.GetBool("dry-run") {
		_ = os.Setenv("INPUT_DRY-RUN", "true")
	}
	if t := v.GetString("github-token"); t != "" {
		_ = os.Setenv("INPUT_GITHUB-TOKEN", t)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log.Printf("strategy=%s tag-prefix=%q release-mode=%s dry-run=%v",
		cfg.VersionStrategy, cfg.TagPrefix, cfg.ReleaseMode, cfg.DryRun)

	gitutil.ConfigureAuth(cfg.GithubToken)

	if cfg.ReleaseMode == "pr" {
		owner, repo, err := publish.OwnerRepoFromEnv()
		if err != nil {
			return err
		}
		ghClient := github.NewClient(nil).WithAuthToken(cfg.GithubToken)
		prClient := releasepr.NewClient(ghClient, owner, repo)

		baseBranch := os.Getenv("GITHUB_REF_NAME")
		if baseBranch == "" {
			baseBranch = "main"
		}

		result, err := prClient.DetectMerge(context.Background(), baseBranch)
		if err != nil {
			return fmt.Errorf("detect merge: %w", err)
		}
		if result != nil {
			log.Printf("release PR merge detected: tag=%s version=%s", result.Manifest.Tag, result.Manifest.Version)
			return handleReleasePRMerge(cfg, prClient, result)
		}
	}

	if err := gitutil.CheckShallowClone(); err != nil {
		return err
	}

	strat, err := strategy.New(cfg.VersionStrategy)
	if err != nil {
		return err
	}

	packages := cfg.Packages
	if len(packages) == 0 {
		packages = []config.Package{{Name: ""}}
	}

	var hadFailure, hadRelease bool
	for _, pkg := range packages {
		pkgCfg := cfg
		if pkg.Name != "" {
			log.Printf("--- package: %s (path=%s)", pkg.Name, pkg.Path)
			pkgCfg.CurrentPackage = &pkg
		}

		if err := processReleasePackage(pkgCfg, strat, pkg); err != nil {
			if pkg.Name != "" {
				log.Printf("error releasing package %s: %v", pkg.Name, err)
				hadFailure = true
				continue
			}
			return err
		}
		hadRelease = true
	}

	if hadFailure {
		if hadRelease {
			log.Printf("partial failure: some packages released, some failed")
			os.Exit(2)
		}
		return fmt.Errorf("all packages failed to release")
	}
	return nil
}

func processReleasePackage(cfg config.Config, strat strategy.VersionStrategy, pkg config.Package) error {
	prefix := cfg.TagPrefix
	if pkg.TagPattern != "" {
		prefix = pkg.TagPattern
	}
	tags, err := gitutil.ListTags(prefix)
	if err != nil {
		return err
	}
	tags = strategy.FilterTags(tags, cfg.TagPrefix, cfg.VersionStrategy)
	log.Printf("found %d valid tags matching prefix %q", len(tags), prefix)

	if pkg.TagPattern == "" {
		cfg.EffectiveTagPattern = strategy.TagPatternRegex(cfg.TagPrefix, cfg.VersionStrategy)
	}

	result, err := strat.NextVersion(tags, cfg)
	if err != nil {
		return fmt.Errorf("calculate version: %w", err)
	}

	if result.Skipped {
		log.Printf("skipped: no release needed")
		return setReleaseOutputs(releaseOutputs{previousVersion: result.PreviousVersion, releaseMode: cfg.ReleaseMode, skipped: true, dryRun: cfg.DryRun})
	}

	tag := cfg.TagPrefix + result.Version
	if pkg.Name != "" {
		tag = pkg.Name + "/" + tag
	}
	log.Printf("next version: %s (tag: %s, previous: %s)", result.Version, tag, result.PreviousVersion)

	cl, err := changelog.Generate(cfg)
	if err != nil {
		return fmt.Errorf("generate changelog: %w", err)
	}
	log.Printf("changelog: %d bytes", len(cl))

	if cfg.DryRun {
		log.Printf("dry-run: skipping tag creation and release (would tag: %s)", tag)
		return setReleaseOutputs(releaseOutputs{version: result.Version, changelog: cl, previousVersion: result.PreviousVersion, releaseMode: cfg.ReleaseMode, dryRun: true})
	}

	if cfg.ReleaseMode == "pr" {
		return createOrUpdateReleasePR(cfg, result.Version, tag, cl)
	}
	return directRelease(cfg, result, tag, cl, pkg)
}

func createOrUpdateReleasePR(cfg config.Config, version, tag, cl string) error {
	owner, repo, err := publish.OwnerRepoFromEnv()
	if err != nil {
		return err
	}
	ghClient := github.NewClient(nil).WithAuthToken(cfg.GithubToken)
	client := releasepr.NewClient(ghClient, owner, repo)

	baseBranch := os.Getenv("GITHUB_REF_NAME")
	if baseBranch == "" {
		baseBranch = "main"
	}

	prURL, prNumber, created, err := client.CreateOrUpdate(context.Background(), version, tag, cl, baseBranch, cfg.ServicePath())
	if err != nil {
		return fmt.Errorf("create/update release PR: %w", err)
	}

	return setReleaseOutputs(releaseOutputs{
		version:         version,
		changelog:       cl,
		tag:             tag,
		prURL:           prURL,
		releaseMode:     "pr",
		releasePRNumber: prNumber,
		prCreated:       created,
	})
}

func handleReleasePRMerge(cfg config.Config, prClient *releasepr.Client, result *releasepr.MergeResult) error {
	tag := result.Manifest.Tag
	version := result.Manifest.Version

	if version == "" {
		log.Printf("manifest missing version, recalculating")
		if err := gitutil.CheckShallowClone(); err != nil {
			return err
		}
		strat, err := strategy.New(cfg.VersionStrategy)
		if err != nil {
			return err
		}
		tags, err := gitutil.ListTags(cfg.TagPrefix)
		if err != nil {
			return err
		}
		vResult, err := strat.NextVersion(tags, cfg)
		if err != nil {
			return err
		}
		if vResult.Skipped {
			log.Printf("skipped after recalculation")
			return nil
		}
		version = vResult.Version
		tag = cfg.TagPrefix + version
	}

	cl, err := changelog.Generate(cfg)
	if err != nil {
		log.Printf("warning: changelog generation failed: %v", err)
		cl = ""
	}

	exists, err := gitutil.TagExists(tag)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("tag %s already exists", tag)
	}

	if err := gitutil.CreateTag(tag, fmt.Sprintf("Release %s", tag)); err != nil {
		return err
	}
	if err := gitutil.PushTag(tag); err != nil {
		return err
	}
	log.Printf("tag %s created and pushed", tag)

	owner, repo, err := publish.OwnerRepoFromEnv()
	if err != nil {
		return err
	}

	res, err := publish.Create(context.Background(), publish.Params{
		Owner: owner, Repo: repo, Tag: tag, Name: tag, Body: cl,
		Draft: cfg.Draft, Prerelease: cfg.Prerelease, Token: cfg.GithubToken,
	})
	if err != nil {
		return fmt.Errorf("create release: %w", err)
	}
	log.Printf("release created: %s", res.URL)

	branchName := releasepr.ReleaseBranchName(tag, version)
	if result.PRNumber > 0 {
		prClient.Cleanup(context.Background(), result.PRNumber, branchName)
	}

	return setReleaseOutputs(releaseOutputs{
		version: version, changelog: cl, tag: res.Tag, releaseURL: res.URL,
		releaseMode: "pr", releasePublished: true,
	})
}

func directRelease(cfg config.Config, result strategy.Result, tag, cl string, pkg config.Package) error {
	exists, err := gitutil.TagExists(tag)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("tag %s already exists: a concurrent release may have created it", tag)
	}

	if err := gitutil.CreateTag(tag, fmt.Sprintf("Release %s", tag)); err != nil {
		return err
	}
	if err := gitutil.PushTag(tag); err != nil {
		return err
	}
	log.Printf("tag %s created and pushed", tag)

	owner, repo, err := publish.OwnerRepoFromEnv()
	if err != nil {
		return err
	}

	releaseName := tag
	if pkg.Name != "" {
		releaseName = fmt.Sprintf("%s %s", pkg.Name, result.Version)
	}

	res, err := publish.Create(context.Background(), publish.Params{
		Owner: owner, Repo: repo, Tag: tag, Name: releaseName, Body: cl,
		Draft: cfg.Draft, Prerelease: cfg.Prerelease, Token: cfg.GithubToken,
	})
	if err != nil {
		return fmt.Errorf("create release: %w", err)
	}
	log.Printf("release created: %s", res.URL)

	return setReleaseOutputs(releaseOutputs{
		version: result.Version, changelog: cl, tag: res.Tag, releaseURL: res.URL,
		previousVersion: result.PreviousVersion, releaseMode: "direct", releasePublished: true,
	})
}

type releaseOutputs struct {
	version, changelog, tag string
	releaseURL              string
	prURL                   string
	previousVersion         string
	skipped                 bool
	dryRun                  bool
	releaseMode             string
	releasePublished        bool
	releasePRNumber         int
	prCreated               bool
}

func setReleaseOutputs(o releaseOutputs) error {
	prNumberStr := ""
	if o.releasePRNumber > 0 {
		prNumberStr = strconv.Itoa(o.releasePRNumber)
	}
	pairs := []struct{ name, value string }{
		{"version", o.version},
		{"changelog", o.changelog},
		{"tag", o.tag},
		{"release-url", o.releaseURL},
		{"pr-url", o.prURL},
		{"previous-version", o.previousVersion},
		{"skipped", boolStr(o.skipped)},
		{"dry-run", boolStr(o.dryRun)},
		{"release-mode", o.releaseMode},
		{"release-published", boolStr(o.releasePublished)},
		{"release-pr-url", o.prURL},
		{"release-pr-number", prNumberStr},
		{"pr-created", boolStr(o.prCreated)},
	}
	for _, p := range pairs {
		if err := gha.SetOutput(p.name, p.value); err != nil {
			return fmt.Errorf("set output %s: %w", p.name, err)
		}
	}
	return nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// bridgeFlagsToEnv copies cobra/viper flag values into INPUT_<NAME> env vars
// (with hyphens preserved) so config.Load() can read them.
func bridgeFlagsToEnv(v *viper.Viper, keys ...string) {
	for _, k := range keys {
		if val := v.GetString(k); val != "" {
			env := "INPUT_" + envify(k)
			_ = os.Setenv(env, val)
		}
	}
}

func envify(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}
