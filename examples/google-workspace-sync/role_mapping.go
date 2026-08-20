package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type roleGroupMapping struct {
	Role     string
	GroupKey string
}

func parseRoleGroup(value string) (roleGroupMapping, error) {
	role, groupKey, found := strings.Cut(value, "=")
	role = strings.TrimSpace(role)
	groupKey = strings.TrimSpace(groupKey)
	if !found || role == "" || groupKey == "" {
		return roleGroupMapping{}, fmt.Errorf("--role-group must use role=group format")
	}
	switch role {
	case "enterprise_owner", "billing_manager", "guest_collaborator":
		return roleGroupMapping{Role: role, GroupKey: groupKey}, nil
	default:
		return roleGroupMapping{}, fmt.Errorf("--role-group contains unsupported elevated role %q", role)
	}
}

func (c directoryClient) loadRoleAssignments(ctx context.Context, values []string) (map[string]string, error) {
	assignments := make(map[string]string)
	groupIDs := make(map[string]string)
	for _, value := range values {
		mapping, err := parseRoleGroup(value)
		if err != nil {
			return nil, err
		}
		group, err := c.getGroup(ctx, mapping.GroupKey)
		if err != nil {
			return nil, fmt.Errorf("getting role group %q: %w", mapping.GroupKey, err)
		}
		if existingKey := groupIDs[group.ID]; existingKey != "" {
			return nil, fmt.Errorf("role group %q resolves to the same Google group as %q", mapping.GroupKey, existingKey)
		}
		groupIDs[group.ID] = mapping.GroupKey
		members, err := c.listGroupMembers(ctx, group.ID)
		if err != nil {
			return nil, fmt.Errorf("listing role group %q members: %w", mapping.GroupKey, err)
		}
		for _, member := range members {
			if member.Type != "" && member.Type != "USER" {
				return nil, fmt.Errorf("role group %q contains unsupported member %q of type %q", mapping.GroupKey, member.Email, member.Type)
			}
			if existing := assignments[member.ID]; existing != "" && existing != mapping.Role {
				return nil, fmt.Errorf(
					"Google user %q belongs to conflicting role groups for %q and %q",
					member.Email,
					existing,
					mapping.Role,
				)
			}
			assignments[member.ID] = mapping.Role
		}
	}
	return assignments, nil
}

func validateRoleAssignmentScope(users []googleUser, assignments map[string]string) error {
	userIDs := make(map[string]bool, len(users))
	for _, user := range users {
		userIDs[user.ID] = true
	}
	var missing []string
	for userID := range assignments {
		if !userIDs[userID] {
			missing = append(missing, userID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("role-group members with Google ids %s are outside the synchronized user scope", strings.Join(missing, ", "))
}

func googleRole(user googleUser, roleAttribute string) (string, bool, error) {
	if roleAttribute == "" {
		return "user", false, nil
	}
	schema, field, err := splitRoleAttribute(roleAttribute)
	if err != nil {
		return "", false, err
	}
	role := "user"
	explicit := false
	if fields := user.CustomSchemas[schema]; fields != nil {
		if value, exists := fields[field]; exists {
			text, ok := value.(string)
			if !ok {
				return "", false, fmt.Errorf("%s must be a single string", roleAttribute)
			}
			if strings.TrimSpace(text) != "" {
				role = strings.TrimSpace(text)
				explicit = true
			}
		}
	}
	switch role {
	case "user", "enterprise_owner", "billing_manager", "guest_collaborator":
		return role, explicit, nil
	default:
		return "", false, fmt.Errorf("%s contains unsupported role %q", roleAttribute, role)
	}
}

func splitRoleAttribute(value string) (string, string, error) {
	schema, field, found := strings.Cut(value, ".")
	if !found || strings.TrimSpace(schema) == "" || strings.TrimSpace(field) == "" {
		return "", "", fmt.Errorf("--role-attribute must use Schema.field format")
	}
	return schema, field, nil
}
