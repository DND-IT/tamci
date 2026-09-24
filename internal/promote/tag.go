package promote

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dnd-it/tamci/internal/gh"
	"github.com/dnd-it/tamci/internal/git"
	"github.com/dnd-it/tamci/internal/yamlx"
	"gopkg.in/yaml.v3"
)

const (
	KindInfra = "infra"
	KindApp   = "app"
)

// Options select what Prepare promotes.
type Options struct {
	Target        string
	Kind          string
	Version       string
	DefaultBranch string
	StacksDir     string
	ChartsDir     string
}

// Plan is the prod tag to create and the commit it points at.
type Plan struct {
	Tag     string
	Version string
	Commit  string
}

// RunLister lists workflow runs; *gh.RESTClient implements it.
type RunLister interface {
	WorkflowRuns(workflow, branch, status string) ([]gh.WorkflowRun, error)
}

// ProdValuesPath is the chart values file a service's prod image tag lives in.
func ProdValuesPath(chartsDir, target string) string {
	return filepath.Join(chartsDir, target, "envs", "prod", "values.yaml")
}

// InferKind returns infra when <stacksDir>/<target> is a directory and app
// when the target's prod values file exists, both relative to root.
func InferKind(root, target, stacksDir, chartsDir string) (string, error) {
	if fi, err := os.Stat(filepath.Join(root, stacksDir, target)); err == nil && fi.IsDir() {
		return KindInfra, nil
	}
	if _, err := os.Stat(filepath.Join(root, ProdValuesPath(chartsDir, target))); err == nil {
		return KindApp, nil
	}
	return "", fmt.Errorf("%s is neither a stack under %s/ nor a chart with a prod environment under %s/", target, stacksDir, chartsDir)
}

var (
	breakingSubject = regexp.MustCompile(`^[a-z]+(\([^)]*\))?!:`)
	featSubject     = regexp.MustCompile(`^feat(\([^)]*\))?:`)
	breakingFooter  = regexp.MustCompile(`(?m)^BREAKING[ -]CHANGE:`)
)

// NextVersion bumps last (a semver without prefix, "" for none) by the
// conventional commits in messages: breaking is major, feat is minor, anything
// else is patch.
func NextVersion(last string, messages []string) (string, error) {
	parts := strings.Split(cmp.Or(last, "0.0.0"), ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("%q is not a semver version", last)
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", fmt.Errorf("%q is not a semver version", last)
		}
		v[i] = n
	}
	var major, minor bool
	for _, m := range messages {
		subject, _, _ := strings.Cut(m, "\n")
		major = major || breakingSubject.MatchString(subject) || breakingFooter.MatchString(m)
		minor = minor || featSubject.MatchString(subject)
	}
	switch {
	case major:
		return fmt.Sprintf("%d.0.0", v[0]+1), nil
	case minor:
		return fmt.Sprintf("%d.%d.0", v[0], v[1]+1), nil
	default:
		return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]+1), nil
	}
}

// PushPaths reads on.push.paths from a workflow file.
func PushPaths(workflow []byte) ([]string, error) {
	var wf struct {
		On struct {
			Push struct {
				Paths []string `yaml:"paths"`
			} `yaml:"push"`
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal(workflow, &wf); err != nil {
		return nil, err
	}
	return wf.On.Push.Paths, nil
}

// CurrentProdTag returns the image tag the values file pins for service.
func CurrentProdTag(values []byte, service string) (string, error) {
	doc, err := yamlx.LoadYAML(values)
	if err != nil {
		return "", err
	}
	return yamlx.ImageTag(doc, service), nil
}

// Prepare works out the prod tag for o.Target and the commit it goes on,
// printing what it found to out. g must be at the repository root with tags
// fetched; runs is only used for infra.
func Prepare(g *git.Client, runs RunLister, o Options, out io.Writer) (Plan, error) {
	var version, commit string
	var err error
	switch o.Kind {
	case KindInfra:
		version, commit, err = prepareInfra(g, runs, o, out)
	case KindApp:
		version, commit, err = prepareApp(g, o, out)
	default:
		return Plan{}, fmt.Errorf("unknown kind %q: must be %s or %s", o.Kind, KindInfra, KindApp)
	}
	if err != nil {
		return Plan{}, err
	}

	tag := fmt.Sprintf("prod/%s/%s/v%s", o.Kind, o.Target, version)
	if g.TagExists(tag) {
		return Plan{}, fmt.Errorf("%s already exists", tag)
	}
	return Plan{Tag: tag, Version: version, Commit: commit}, nil
}

func prepareInfra(g *git.Client, runs RunLister, o Options, out io.Writer) (string, string, error) {
	workflow := o.Target + ".yaml"
	data, err := os.ReadFile(filepath.Join(g.Dir, ".github", "workflows", workflow))
	if err != nil {
		return "", "", fmt.Errorf("reading the %s workflow: %w", o.Target, err)
	}
	paths, err := PushPaths(data)
	if err != nil {
		return "", "", fmt.Errorf("reading on.push.paths from %s: %w", workflow, err)
	}

	list, err := runs.WorkflowRuns(workflow, o.DefaultBranch, "success")
	if err != nil {
		return "", "", err
	}
	var commit string
	for _, r := range list {
		if r.Event == "push" || r.Event == "workflow_dispatch" {
			commit = r.HeadSHA
			break
		}
	}
	if commit == "" {
		return "", "", fmt.Errorf("no successful run of %s on %s yet; nothing in dev to promote", workflow, o.DefaultBranch)
	}

	lastTag, err := g.LatestTag(fmt.Sprintf("prod/%s/%s/v*", KindInfra, o.Target))
	if err != nil {
		return "", "", err
	}
	revRange := commit
	if lastTag != "" {
		revRange = lastTag + ".." + commit
	}
	messages, err := g.CommitMessages(revRange, paths...)
	if err != nil {
		return "", "", err
	}
	version := o.Version
	if len(messages) == 0 && version == "" {
		return "", "", fmt.Errorf("nothing under %s's paths changed since %s", o.Target, cmp.Or(lastTag, "the start"))
	}
	if version == "" {
		if version, err = NextVersion(versionOf(lastTag), messages); err != nil {
			return "", "", err
		}
	}

	_, _ = fmt.Fprintf(out, "Commits since %s that dev already has:\n", cmp.Or(lastTag, "the first commit"))
	for _, m := range messages {
		subject, _, _ := strings.Cut(m, "\n")
		_, _ = fmt.Fprintf(out, "  %s\n", subject)
	}
	return version, commit, nil
}

func prepareApp(g *git.Client, o Options, out io.Writer) (string, string, error) {
	version := o.Version
	if version == "" {
		latest, err := g.LatestTag(o.Target + "/v*")
		if err != nil {
			return "", "", err
		}
		if latest == "" {
			return "", "", fmt.Errorf("%s has no release tag yet", o.Target)
		}
		version = versionOf(latest)
	}
	releaseTag := fmt.Sprintf("%s/v%s", o.Target, version)
	commit, err := g.RevParse("refs/tags/" + releaseTag + "^{commit}")
	if err != nil {
		return "", "", fmt.Errorf("%s does not exist", releaseTag)
	}

	current := "unknown"
	if values, err := g.Show("origin/"+o.DefaultBranch, filepath.ToSlash(ProdValuesPath(o.ChartsDir, o.Target))); err == nil {
		if tag, err := CurrentProdTag(values, o.Target); err == nil && tag != "" {
			current = tag
		}
	}
	_, _ = fmt.Fprintf(out, "prod runs %s %s; promoting %s\n", o.Target, current, version)
	return version, commit, nil
}

var remotePattern = regexp.MustCompile(`[:/]([^/:]+/[^/]+?)(\.git)?/?$`)

// RepoFromRemote returns owner/repo from a GitHub remote URL (https or ssh).
func RepoFromRemote(remote string) (string, error) {
	m := remotePattern.FindStringSubmatch(remote)
	if m == nil {
		return "", fmt.Errorf("cannot read owner/repo from remote %q", remote)
	}
	return m[1], nil
}

func versionOf(tag string) string {
	if i := strings.LastIndex(tag, "/v"); i >= 0 {
		return tag[i+2:]
	}
	return tag
}
