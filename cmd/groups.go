package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/eroullit/gh-scim/internal/scim"
)

func newGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "Manage SCIM-provisioned enterprise groups",
	}

	cmd.AddCommand(
		newGroupsListCmd(),
		newGroupsGetCmd(),
		newGroupsCreateCmd(),
		newGroupsReplaceCmd(),
		newGroupsPatchCmd(),
		newGroupsAddMembersCmd(),
		newGroupsRemoveMembersCmd(),
		newGroupsDeleteCmd(),
	)

	return cmd
}

func newGroupsListCmd() *cobra.Command {
	var params scim.ListParams

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List provisioned SCIM groups for the enterprise",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			res, err := client.ListGroups(params)
			if err != nil {
				return err
			}
			return printJSON(res)
		},
	}

	cmd.Flags().StringVar(&params.Filter, "filter", "", `SCIM filter, e.g. displayName eq "Engineering"`)
	cmd.Flags().IntVar(&params.StartIndex, "start-index", 0, "1-based index of the first result to return")
	cmd.Flags().IntVar(&params.Count, "count", 0, "Number of results to return per page")
	cmd.Flags().StringVar(&params.ExcludedAttributes, "excluded-attributes", "", `Attribute(s) to exclude from results, e.g. "members" to speed up queries`)

	return cmd
}

func newGroupsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <scim-group-id>",
		Short: "Get a SCIM group by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			g, err := client.GetGroup(args[0])
			if err != nil {
				return err
			}
			return printJSON(g)
		},
	}
}

// groupFlags holds the flag values shared between the create and replace
// commands, since both submit a full Group representation.
type groupFlags struct {
	externalID  string
	displayName string
	members     []string
}

func (f groupFlags) toGroup() scim.Group {
	g := scim.Group{
		ExternalID:  f.externalID,
		DisplayName: f.displayName,
	}
	for _, m := range f.members {
		g.Members = append(g.Members, scim.Member{Value: m})
	}
	return g
}

func addGroupFlags(cmd *cobra.Command, f *groupFlags) {
	cmd.Flags().StringVar(&f.externalID, "external-id", "", "Unique identifier for the group as defined by the provisioning client (required)")
	cmd.Flags().StringVar(&f.displayName, "display-name", "", "Human-readable name for the group (required)")
	cmd.Flags().StringSliceVar(&f.members, "member", nil, "SCIM user id of a member to include (repeatable)")
}

func newGroupsCreateCmd() *cobra.Command {
	var f groupFlags

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Provision a new SCIM group",
		Long: `Provision a new SCIM group.

Members must already exist as provisioned SCIM users; reference them by
their SCIM user id (see "gh scim users create"/"gh scim users list").`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.externalID == "" || f.displayName == "" {
				return fmt.Errorf("--external-id and --display-name are required")
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			g, err := client.CreateGroup(f.toGroup())
			if err != nil {
				return err
			}
			return printJSON(g)
		},
	}

	addGroupFlags(cmd, &f)
	return cmd
}

func newGroupsReplaceCmd() *cobra.Command {
	var f groupFlags

	cmd := &cobra.Command{
		Use:   "replace <scim-group-id>",
		Short: "Replace all attributes of an existing SCIM group (SCIM PUT)",
		Long: `Replace all attributes of an existing SCIM group.

Any attribute not provided is removed, including group membership. Use
"gh scim groups patch" or "gh scim groups add-members"/"remove-members" to
update a single attribute instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.externalID == "" || f.displayName == "" {
				return fmt.Errorf("--external-id and --display-name are required")
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			g, err := client.ReplaceGroup(args[0], f.toGroup())
			if err != nil {
				return err
			}
			return printJSON(g)
		},
	}

	addGroupFlags(cmd, &f)
	return cmd
}

func newGroupsPatchCmd() *cobra.Command {
	var op, path, value string

	cmd := &cobra.Command{
		Use:   "patch <scim-group-id>",
		Short: "Update a single attribute of an existing SCIM group (SCIM PATCH)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				return fmt.Errorf("--path is required")
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			g, err := client.PatchGroup(args[0], scim.PatchOperation{Op: op, Path: path, Value: value})
			if err != nil {
				return err
			}
			return printJSON(g)
		},
	}

	cmd.Flags().StringVar(&op, "op", "replace", `Patch operation: "add", "replace", or "remove"`)
	cmd.Flags().StringVar(&path, "path", "", `Attribute path to modify, e.g. "displayName" (required)`)
	cmd.Flags().StringVar(&value, "value", "", "New value for the attribute")

	return cmd
}

func newGroupsAddMembersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-members <scim-group-id> <scim-user-id>...",
		Short: "Add one or more members to an existing SCIM group",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			g, err := client.AddGroupMembers(args[0], args[1:]...)
			if err != nil {
				return err
			}
			return printJSON(g)
		},
	}
}

func newGroupsRemoveMembersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-members <scim-group-id> <scim-user-id>...",
		Short: "Remove one or more members from an existing SCIM group",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newClient()
			if err != nil {
				return err
			}
			g, err := client.RemoveGroupMembers(args[0], args[1:]...)
			if err != nil {
				return err
			}
			return printJSON(g)
		},
	}
}

func newGroupsDeleteCmd() *cobra.Command {
	var confirm bool

	cmd := &cobra.Command{
		Use:   "delete <scim-group-id>",
		Short: "Delete a SCIM group from the enterprise",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("re-run with --confirm to delete the group")
			}
			client, err := newClient()
			if err != nil {
				return err
			}
			if err := client.DeleteGroup(args[0]); err != nil {
				return err
			}
			fmt.Printf("group %s deleted\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm the deletion")
	return cmd
}
