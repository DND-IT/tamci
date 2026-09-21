package cli

import (
	"fmt"
	"net/url"
	"os"

	"github.com/dnd-it/tamci/internal/gha"
	"github.com/dnd-it/tamci/internal/token"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newTokenCmd() *cobra.Command {
	v := newViper()

	cmd := &cobra.Command{
		Use:   "token",
		Short: "Exchange the job's OIDC token for a GitHub App installation token at an octo-sts broker.",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return bindFlags(cmd, v)
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			// The action's post step runs this same command; state saved by
			// the main step is how it knows to revoke instead.
			if os.Getenv("STATE_isPost") == "true" {
				return runTokenRevoke()
			}
			return runToken(v)
		},
	}

	f := cmd.Flags()
	f.String("url", "", "Broker exchange URL (https://<domain>/sts/exchange).")
	f.String("identity", "", "Trust policy name: .github/chainguard/<identity>.sts.yaml in the scope repository.")
	f.String("scope", "", "Repository (owner/repo) the token is issued for. Defaults to GITHUB_REPOSITORY.")
	f.String("audience", "", "OIDC token audience. Defaults to the host of --url.")

	return cmd
}

func runToken(v *viper.Viper) error {
	exchangeURL := v.GetString("url")
	if exchangeURL == "" {
		return fmt.Errorf("--url (INPUT_URL) is required")
	}
	identity := v.GetString("identity")
	if identity == "" {
		return fmt.Errorf("--identity (INPUT_IDENTITY) is required")
	}
	scope := v.GetString("scope")
	if scope == "" {
		scope = os.Getenv("GITHUB_REPOSITORY")
	}
	if scope == "" {
		return fmt.Errorf("--scope (INPUT_SCOPE) is required outside GitHub Actions")
	}

	audience := v.GetString("audience")
	if audience == "" {
		u, err := url.Parse(exchangeURL)
		if err != nil || u.Host == "" {
			return fmt.Errorf("--url %q is not an absolute URL", exchangeURL)
		}
		audience = u.Host
	}

	requestURL := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	if requestURL == "" {
		return fmt.Errorf("no OIDC token available: grant the job 'permissions: id-token: write'")
	}
	idToken, err := token.IDToken(requestURL, os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN"), audience)
	if err != nil {
		return err
	}
	gha.Mask(idToken)

	installationToken, err := token.Exchange(exchangeURL, idToken, scope, identity)
	if err != nil {
		return err
	}
	gha.Mask(installationToken)

	if err := gha.SetOutput("token", installationToken); err != nil {
		return err
	}
	if os.Getenv("GITHUB_STATE") != "" {
		if err := gha.SaveState("token", installationToken); err != nil {
			return err
		}
		return gha.SaveState("isPost", "true")
	}
	return nil
}

func runTokenRevoke() error {
	installationToken := os.Getenv("STATE_token")
	if installationToken == "" {
		return nil
	}
	apiURL := os.Getenv("GITHUB_API_URL")
	if apiURL == "" {
		apiURL = "https://api.github.com"
	}
	if err := token.Revoke(apiURL, installationToken); err != nil {
		gha.Warning(err.Error())
		return nil
	}
	fmt.Println("Revoked installation token.")
	return nil
}
