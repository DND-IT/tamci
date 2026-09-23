package cli

import (
	"fmt"

	"github.com/dnd-it/tamci/internal/gha"
	"github.com/dnd-it/tamci/internal/stslint"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newStsLintCmd() *cobra.Command {
	v := newViper()

	cmd := &cobra.Command{
		Use:   "sts-lint",
		Short: "Check octo-sts trust policies (.github/chainguard/*.sts.yaml) against the token broker's rules.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindFlags(cmd, v)
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			return runStsLint(v)
		},
	}

	cmd.Flags().String("path", ".github/chainguard", "Directory holding the trust policies.")

	return cmd
}

func runStsLint(v *viper.Viper) error {
	dir := v.GetString("path")
	findings, count, err := stslint.LintDir(dir)
	if err != nil {
		return err
	}
	for _, f := range findings {
		gha.ErrorAt(f.File, f.Message)
	}
	if len(findings) > 0 {
		return fmt.Errorf("%d finding(s) in %s", len(findings), dir)
	}
	fmt.Printf("%d trust policy file(s) in %s pass.\n", count, dir)
	return nil
}
