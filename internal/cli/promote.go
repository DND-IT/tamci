package cli

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/dnd-it/tamci/internal/gh"
	"github.com/dnd-it/tamci/internal/gha"
	"github.com/dnd-it/tamci/internal/git"
	"github.com/dnd-it/tamci/internal/promote"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newPromoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "promote",
		Short: "Promote to prod by tag: resolve what a CI run deploys, or create the prod tag locally.",
	}
	cmd.AddCommand(newPromoteResolveCmd(), newPromoteTagCmd())
	return cmd
}

func newPromoteResolveCmd() *cobra.Command {
	v := newViper()

	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Decide from the GitHub event whether this run plans, which environment it applies to, and whether it releases the dev lock.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindFlags(cmd, v)
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return runPromoteResolve(v)
		},
	}

	f := cmd.Flags()
	f.String("target", "", "Stack or service name; part of the prod tag prefix.")
	f.String("kind", "", "Tag kind, e.g. infra or app: prod tags are <prod-environment>/<kind>/<target>/v<semver>.")
	f.String("label", "deploy:dev", "Pull request label that applies the pull request to dev.")
	f.String("default-branch", "", "Branch dev is applied from and prod tags must be on. Defaults to the repository's default branch, then main.")
	f.String("dev-environment", "dev", "Name of the dev environment.")
	f.String("prod-environment", "prod", "Name of the prod environment.")

	return cmd
}

func runPromoteResolve(v *viper.Viper) error {
	target := v.GetString("target")
	if target == "" {
		return fmt.Errorf("--target (INPUT_TARGET) is required")
	}
	kind := v.GetString("kind")
	if kind == "" {
		return fmt.Errorf("--kind (INPUT_KIND) is required")
	}

	ev, err := promote.LoadEvent(
		os.Getenv("GITHUB_EVENT_NAME"),
		os.Getenv("GITHUB_EVENT_PATH"),
		os.Getenv("GITHUB_REF_TYPE"),
		os.Getenv("GITHUB_REF_NAME"),
		os.Getenv("GITHUB_SHA"),
	)
	if err != nil {
		return err
	}

	prod := v.GetString("prod-environment")
	rules := promote.Rules{
		TagPrefix:     fmt.Sprintf("%s/%s/%s/v", prod, kind, target),
		Label:         v.GetString("label"),
		DefaultBranch: cmp.Or(v.GetString("default-branch"), ev.DefaultBranch, "main"),
		Dev:           v.GetString("dev-environment"),
		Prod:          prod,
	}

	g := &git.Client{}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		if err := g.MarkSafe(); err != nil {
			return err
		}
	}

	d, err := promote.Resolve(ev, rules, g)
	if err != nil {
		return err
	}

	fmt.Printf("plan=%t apply=%s release=%t\n", d.Plan, cmp.Or(d.Apply, "none"), d.Release)
	for _, o := range [][2]string{
		{"plan", fmt.Sprintf("%t", d.Plan)},
		{"apply", d.Apply},
		{"release", fmt.Sprintf("%t", d.Release)},
	} {
		if err := gha.SetOutput(o[0], o[1]); err != nil {
			return err
		}
	}
	return gha.AppendStepSummary(fmt.Sprintf(
		"### %s\n\n| plan | apply | release |\n|---|---|---|\n| %t | %s | %t |\n",
		target, d.Plan, cmp.Or(d.Apply, "none"), d.Release))
}

func newPromoteTagCmd() *cobra.Command {
	v := newViper()

	cmd := &cobra.Command{
		Use:   "tag <target>",
		Short: "Create and push the prod tag for a stack (what the default branch last applied to dev) or a service (its latest release).",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindFlags(cmd, v)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPromoteTag(v, args[0], cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}

	f := cmd.Flags()
	f.String("kind", "", "infra or app. Inferred from --stacks-dir and --charts-dir when unset.")
	f.String("version", "", "Version to tag instead of the computed one.")
	f.Bool("yes", false, "Push without asking.")
	f.Bool("dry-run", false, "Show the tag and commit, create nothing.")
	f.String("stacks-dir", "stacks", "Directory holding one Terraform stack per subdirectory.")
	f.String("charts-dir", "deploy/charts", "Directory holding the Helm charts with envs/prod/values.yaml.")

	return cmd
}

func runPromoteTag(v *viper.Viper, target string, in io.Reader, out io.Writer) error {
	root, err := (&git.Client{}).TopLevel()
	if err != nil {
		return err
	}
	g := &git.Client{Dir: root}
	branch := cmp.Or(g.DefaultBranch(), "main")
	if err := g.Fetch("--tags", branch); err != nil {
		return err
	}

	opts := promote.Options{
		Target:        target,
		Kind:          v.GetString("kind"),
		Version:       v.GetString("version"),
		DefaultBranch: branch,
		StacksDir:     v.GetString("stacks-dir"),
		ChartsDir:     v.GetString("charts-dir"),
	}
	if opts.Kind == "" {
		if opts.Kind, err = promote.InferKind(root, target, opts.StacksDir, opts.ChartsDir); err != nil {
			return err
		}
	}

	var runs promote.RunLister
	if opts.Kind == promote.KindInfra {
		if runs, err = githubClient(g); err != nil {
			return err
		}
	}

	plan, err := promote.Prepare(g, runs, opts, out)
	if err != nil {
		return err
	}
	line, err := g.OneLine(plan.Commit)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "\n  tag:    %s\n  commit: %s\n", plan.Tag, line)

	if v.GetBool("dry-run") {
		_, _ = fmt.Fprintln(out, "dry run: not tagging")
		return nil
	}
	if !v.GetBool("yes") {
		_, _ = fmt.Fprint(out, "Push it and deploy to prod? [y/N] ")
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.TrimSpace(answer); a != "y" && a != "Y" {
			return fmt.Errorf("not pushed")
		}
	}

	if err := g.CreateAnnotatedTag(plan.Tag, plan.Commit, fmt.Sprintf("promote %s %s to prod", target, plan.Version)); err != nil {
		return err
	}
	if err := g.PushTag(plan.Tag); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "pushed %s; follow it with: gh run list --commit %s\n", plan.Tag, plan.Commit)
	return nil
}

func githubClient(g *git.Client) (*gh.RESTClient, error) {
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		remote, err := g.RemoteURL()
		if err != nil {
			return nil, err
		}
		if repo, err = promote.RepoFromRemote(remote); err != nil {
			return nil, err
		}
	}
	owner, name, _ := strings.Cut(repo, "/")

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		out, err := exec.Command("gh", "auth", "token").Output()
		if err != nil {
			return nil, fmt.Errorf("no GITHUB_TOKEN and `gh auth token` failed: %w", err)
		}
		token = strings.TrimSpace(string(out))
	}
	return gh.NewREST(token, owner, name), nil
}
