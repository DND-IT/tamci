package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const envPrefix = "INPUT"

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "tamci",
		Short:         "Tamedia CI CLI — runs identically locally and inside GitHub Actions",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(
		newConfigCmd(),
		newLockCmd(),
		newReleaseCmd(),
		newRolloutCmd(),
		newSetCmd(),
		newStsLintCmd(),
		newSummaryCmd(),
		newTokenCmd(),
	)

	return cmd
}

// newViper returns a fresh Viper bound to INPUT_* env vars, with kebab-case
// flag names translated to SCREAMING_SNAKE_CASE for env lookup.
// Each subcommand binds its own flags into this Viper in PreRunE.
func newViper() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	return v
}

// bindFlags binds every flag on cmd to v, so reading via v.GetString(name)
// returns the flag value if set, otherwise the INPUT_<NAME> env var,
// otherwise the flag default.
func bindFlags(cmd *cobra.Command, v *viper.Viper) error {
	return v.BindPFlags(cmd.Flags())
}
