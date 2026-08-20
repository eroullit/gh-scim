// google-workspace-sync is a proof-of-concept reconciler that reads Google
// Workspace users and provisions them through the gh-scim extension.
package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	cfg := parseConfig()
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "google-workspace-sync:", err)
		os.Exit(1)
	}
}

func run(cfg config) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	httpClient, err := googleHTTPClient(ctx, cfg)
	if err != nil {
		return err
	}

	directory := directoryClient{httpClient: httpClient, baseURL: directoryBaseURL}
	googleUsers, err := directory.listUsers(ctx, cfg.customerID, cfg.query, cfg.roleAttribute)
	if err != nil {
		return fmt.Errorf("listing Google Workspace users: %w", err)
	}
	roleAssignments, err := directory.loadRoleAssignments(ctx, cfg.roleGroups)
	if err != nil {
		return fmt.Errorf("loading Google role groups: %w", err)
	}
	if err := validateRoleAssignmentScope(googleUsers, roleAssignments); err != nil {
		return err
	}

	command := newSCIMCommand(cfg)
	currentUsers, err := command.listUsers(ctx)
	if err != nil {
		return fmt.Errorf("listing GitHub SCIM users: %w", err)
	}

	operations, skipped, err := plan(googleUsers, currentUsers, cfg.roleAttribute, roleAssignments, cfg.deprovisionMissing)
	if err != nil {
		return err
	}
	for _, email := range skipped {
		fmt.Printf("skip inactive first-seen user %s\n", email)
	}
	for _, op := range operations {
		fmt.Println(op.summary())
	}
	fmt.Printf("summary google=%d github=%d changes=%d apply=%t\n", len(googleUsers), len(currentUsers), len(operations), cfg.apply)

	if len(operations) > cfg.maxChanges {
		return fmt.Errorf("proposed changes %d exceed --max-changes %d", len(operations), cfg.maxChanges)
	}
	if !cfg.apply {
		if len(cfg.groups) > 0 {
			if len(operations) > 0 {
				fmt.Println("defer group reconciliation until user changes converge")
				return nil
			}
			return reconcileGroups(
				ctx,
				directory,
				command,
				cfg.groups,
				googleUsers,
				currentUsers,
				false,
				cfg.maxChanges,
				cfg.maxGroupMemberDelta,
			)
		}
		return nil
	}
	for _, op := range operations {
		if err := command.apply(ctx, op); err != nil {
			return fmt.Errorf("%s %s: %w", op.Kind, op.Desired.UserName, err)
		}
	}
	if len(cfg.groups) > 0 {
		if len(operations) > 0 {
			fmt.Println("defer group reconciliation until the next converged run")
			return nil
		}
		currentUsers, err = command.listUsers(ctx)
		if err != nil {
			return fmt.Errorf("relisting GitHub SCIM users: %w", err)
		}
		return reconcileGroups(
			ctx,
			directory,
			command,
			cfg.groups,
			googleUsers,
			currentUsers,
			true,
			cfg.maxChanges,
			cfg.maxGroupMemberDelta,
		)
	}
	return nil
}
