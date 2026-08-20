// Package cmd implements the gh-scim command line interface: a gh CLI
// extension for (de)provisioning users and groups in a GitHub Enterprise
// Managed Users (EMU) account via the SCIM REST API, for admins who are not
// using a paved-path identity provider.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/eroullit/gh-scim/scim"
)

var (
	enterprise string
	hostname   string
)

// Execute runs the root gh-scim command.
func Execute() error {
	return ExecuteContext(context.Background())
}

// ExecuteContext runs the root gh-scim command with ctx.
func ExecuteContext(ctx context.Context) error {
	return newRootCmd().ExecuteContext(ctx)
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "scim",
		Short: "(De)provision Enterprise Managed Users via the GitHub SCIM REST API",
		Long: `gh-scim manages the lifecycle of Enterprise Managed Users (EMU) accounts
and groups using GitHub's SCIM REST API. It is intended for enterprise admins
who are not using a paved-path identity provider and need to provision users
and groups directly.

Authentication requires a token with the scim:enterprise scope, provided
through gh's normal authentication (gh auth login / GH_TOKEN).`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&enterprise, "enterprise", os.Getenv("GH_SCIM_ENTERPRISE"), "Slug of the enterprise to manage (or set GH_SCIM_ENTERPRISE)")
	root.PersistentFlags().StringVar(&hostname, "hostname", os.Getenv("GH_HOST"), "GitHub host to talk to, e.g. api.contoso.ghe.com (or set GH_HOST)")

	root.AddCommand(newUsersCmd())
	root.AddCommand(newGroupsCmd())

	return root
}

func newClient() (*scim.Client, error) {
	if enterprise == "" {
		return nil, fmt.Errorf("missing enterprise slug: pass --enterprise or set GH_SCIM_ENTERPRISE")
	}
	if hostname != "" {
		return scim.NewClient(enterprise, scim.WithHost(hostname))
	}
	return scim.NewClient(enterprise)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
