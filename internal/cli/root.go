package cli

import (
	"fmt"
	"os"
	"strconv"
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

	cmd.PersistentFlags().Bool("experimental", false, "Enable experimental subcommands (currently: promote). Also INPUT_EXPERIMENTAL.")

	cmd.AddCommand(
		newConfigCmd(),
		newLockCmd(),
		newPromoteCmd(),
		newReleaseCmd(),
		newRolloutCmd(),
		newSetCmd(),
		newStsLintCmd(),
		newSummaryCmd(),
		newTokenCmd(),
	)

	return cmd
}

// requireExperimental fails unless --experimental or INPUT_EXPERIMENTAL=true
// is set, so a subcommand can ship without anyone relying on it by accident.
func requireExperimental(cmd *cobra.Command, _ []string) error {
	enabled, _ := cmd.Flags().GetBool("experimental")
	if !enabled {
		enabled, _ = strconv.ParseBool(os.Getenv(envPrefix + "_EXPERIMENTAL"))
	}
	if !enabled {
		return fmt.Errorf("%s is experimental: pass --experimental, or experimental: true on the action", cmd.CommandPath())
	}
	return nil
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
