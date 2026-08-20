package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/eroullit/gh-scim/scim"
)

func newUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage SCIM-provisioned enterprise users",
	}

	cmd.AddCommand(
		newUsersListCmd(),
		newUsersGetCmd(),
		newUsersCreateCmd(),
		newUsersReplaceCmd(),
		newUsersPatchCmd(),
		newUsersDeprovisionCmd(),
		newUsersReactivateCmd(),
		newUsersDeleteCmd(),
	)

	return cmd
}

func newUsersListCmd() *cobra.Command {
	var params scim.ListParams

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List provisioned SCIM users for the enterprise",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			res, err := client.ListUsers(cmd.Context(), params)
			if err != nil {
				return err
			}
			return printJSON(res)
		},
	}

	cmd.Flags().StringVar(&params.Filter, "filter", "", `SCIM filter, e.g. userName eq "octocat"`)
	cmd.Flags().IntVar(&params.StartIndex, "start-index", 0, "1-based index of the first result to return")
	cmd.Flags().IntVar(&params.Count, "count", 0, "Number of results to return per page")

	return cmd
}

func newUsersGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <scim-user-id>",
		Short: "Get a SCIM user by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			u, err := client.GetUser(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(u)
		},
	}
}

// userFlags holds the flag values shared between the create and replace
// commands, since both submit a full User representation.
type userFlags struct {
	externalID    string
	userName      string
	givenName     string
	familyName    string
	formattedName string
	displayName   string
	email         string
	emailType     string
	primaryEmail  bool
	roles         []string
	inactive      bool
}

func (f userFlags) toUser() scim.User {
	active := !f.inactive
	u := scim.User{
		ExternalID:  f.externalID,
		Active:      &active,
		UserName:    f.userName,
		DisplayName: f.displayName,
	}
	if f.givenName != "" || f.familyName != "" || f.formattedName != "" {
		u.Name = &scim.Name{
			Formatted:  f.formattedName,
			GivenName:  f.givenName,
			FamilyName: f.familyName,
		}
	}
	if f.email != "" {
		u.Emails = []scim.Email{{Value: f.email, Type: f.emailType, Primary: f.primaryEmail}}
	}
	for _, r := range f.roles {
		u.Roles = append(u.Roles, scim.Role{Value: r})
	}
	return u
}

func addUserFlags(cmd *cobra.Command, f *userFlags) {
	cmd.Flags().StringVar(&f.externalID, "external-id", "", "Unique identifier for the user as defined by the provisioning client (required)")
	cmd.Flags().StringVar(&f.userName, "username", "", "Username for the user (required)")
	cmd.Flags().StringVar(&f.givenName, "given-name", "", "The user's given (first) name")
	cmd.Flags().StringVar(&f.familyName, "family-name", "", "The user's family (last) name")
	cmd.Flags().StringVar(&f.formattedName, "formatted-name", "", "The user's full name formatted for display")
	cmd.Flags().StringVar(&f.displayName, "display-name", "", "Human-readable name for the user (required)")
	cmd.Flags().StringVar(&f.email, "email", "", "Email address for the user (required)")
	cmd.Flags().StringVar(&f.emailType, "email-type", "work", "Type of the email address")
	cmd.Flags().BoolVar(&f.primaryEmail, "primary-email", true, "Whether the email address is the primary address")
	cmd.Flags().StringSliceVar(&f.roles, "role", []string{"user"}, "Role(s) to assign to the user (repeatable)")
	cmd.Flags().BoolVar(&f.inactive, "inactive", false, "Create/replace the user as inactive (soft-deprovisioned)")
}

func newUsersCreateCmd() *cobra.Command {
	var f userFlags

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Provision a new SCIM user",
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.externalID == "" || f.userName == "" || f.displayName == "" || f.email == "" {
				return fmt.Errorf("--external-id, --username, --display-name, and --email are required")
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			u, err := client.CreateUser(cmd.Context(), f.toUser())
			if err != nil {
				return err
			}
			return printJSON(u)
		},
	}

	addUserFlags(cmd, &f)
	return cmd
}

func newUsersReplaceCmd() *cobra.Command {
	var f userFlags

	cmd := &cobra.Command{
		Use:   "replace <scim-user-id>",
		Short: "Replace all attributes of an existing SCIM user (SCIM PUT)",
		Long: `Replace all attributes of an existing SCIM user.

Any attribute not provided is removed, matching SCIM PUT semantics. Use
"gh scim users patch" to update a single attribute instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.externalID == "" || f.userName == "" || f.displayName == "" || f.email == "" {
				return fmt.Errorf("--external-id, --username, --display-name, and --email are required")
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			u, err := client.ReplaceUser(cmd.Context(), args[0], f.toUser())
			if err != nil {
				return err
			}
			return printJSON(u)
		},
	}

	addUserFlags(cmd, &f)
	return cmd
}

func newUsersPatchCmd() *cobra.Command {
	var op, path, value string

	cmd := &cobra.Command{
		Use:   "patch <scim-user-id>",
		Short: "Update a single attribute of an existing SCIM user (SCIM PATCH)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				return fmt.Errorf("--path is required")
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			u, err := client.PatchUser(cmd.Context(), args[0], scim.PatchOperation{Op: op, Path: path, Value: value})
			if err != nil {
				return err
			}
			return printJSON(u)
		},
	}

	cmd.Flags().StringVar(&op, "op", "replace", `Patch operation: "add", "replace", or "remove"`)
	cmd.Flags().StringVar(&path, "path", "", `Attribute path to modify, e.g. "displayName" (required)`)
	cmd.Flags().StringVar(&value, "value", "", "New value for the attribute")

	return cmd
}

func newUsersDeprovisionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deprovision <scim-user-id>",
		Short: "Soft-deprovision a user (suspend and obfuscate login/email, reversible)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			u, err := client.SetUserActive(cmd.Context(), args[0], false)
			if err != nil {
				return err
			}
			return printJSON(u)
		},
	}
}

func newUsersReactivateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reactivate <scim-user-id>",
		Short: "Reactivate a previously soft-deprovisioned user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			u, err := client.SetUserActive(cmd.Context(), args[0], true)
			if err != nil {
				return err
			}
			return printJSON(u)
		},
	}
}

func newUsersDeleteCmd() *cobra.Command {
	var confirm bool

	cmd := &cobra.Command{
		Use:   "delete <scim-user-id>",
		Short: "Hard-deprovision (permanently suspend) a user",
		Long: `Hard-deprovision a user.

This is irreversible: the user's GitHub account is permanently suspended and
cannot be reactivated. To provision the person again, they must be created
as a brand new user.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("this is an irreversible hard-deprovision; re-run with --confirm to proceed")
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			if err := client.DeleteUser(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Printf("user %s hard-deprovisioned\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm the irreversible hard-deprovision")
	return cmd
}
