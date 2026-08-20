package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type desiredGroup struct {
	ExternalID  string
	DisplayName string
	MemberIDs   []string
}

type groupOperation struct {
	Create  bool
	Desired desiredGroup
	Current *scimGroup
}

func (op groupOperation) memberDelta() int {
	if op.Create {
		return len(op.Desired.MemberIDs)
	}
	currentIDs := make([]string, 0, len(op.Current.Members))
	for _, member := range op.Current.Members {
		currentIDs = append(currentIDs, member.Value)
	}
	return symmetricDifferenceSize(currentIDs, op.Desired.MemberIDs)
}

func reconcileGroups(
	ctx context.Context,
	directory directoryClient,
	command scimCommand,
	groupKeys []string,
	googleUsers []googleUser,
	currentUsers []scimUser,
	apply bool,
	maxChanges int,
	maxGroupMemberDelta int,
) error {
	scimIDs := make(map[string]string, len(currentUsers))
	activeGoogleUsers := make(map[string]bool, len(googleUsers))
	for _, user := range currentUsers {
		if strings.HasPrefix(user.ExternalID, googleExternalID) {
			scimIDs[strings.TrimPrefix(user.ExternalID, googleExternalID)] = user.ID
		}
	}
	for _, user := range googleUsers {
		activeGoogleUsers[user.ID] = !user.Suspended && !user.Archived
	}

	var operations []groupOperation
	groupIDs := make(map[string]string)
	for _, key := range groupKeys {
		group, err := directory.getGroup(ctx, key)
		if err != nil {
			return fmt.Errorf("getting Google group %q: %w", key, err)
		}
		if existingKey := groupIDs[group.ID]; existingKey != "" {
			return fmt.Errorf("group %q resolves to the same Google group as %q", key, existingKey)
		}
		groupIDs[group.ID] = key
		members, err := directory.listGroupMembers(ctx, group.ID)
		if err != nil {
			return fmt.Errorf("listing Google group %q members: %w", key, err)
		}
		desired, err := normalizeGoogleGroup(group, members, activeGoogleUsers, scimIDs)
		if err != nil {
			return fmt.Errorf("normalizing Google group %q: %w", key, err)
		}
		current, err := command.findGroup(ctx, desired.ExternalID)
		if err != nil {
			return fmt.Errorf("finding GitHub SCIM group %q: %w", key, err)
		}
		if current == nil || groupChanged(desired, *current) {
			operations = append(operations, groupOperation{
				Create:  current == nil,
				Desired: desired,
				Current: current,
			})
		}
	}

	sort.Slice(operations, func(i, j int) bool {
		return operations[i].Desired.DisplayName < operations[j].Desired.DisplayName
	})
	for _, op := range operations {
		fmt.Println(op.summary())
		if delta := op.memberDelta(); delta > maxGroupMemberDelta {
			return fmt.Errorf(
				"group %q membership delta %d exceeds --max-group-member-delta %d",
				op.Desired.DisplayName,
				delta,
				maxGroupMemberDelta,
			)
		}
	}
	fmt.Printf("group summary selected=%d changes=%d apply=%t\n", len(groupKeys), len(operations), apply)
	if len(operations) > maxChanges {
		return fmt.Errorf("proposed group changes %d exceed remaining change budget %d", len(operations), maxChanges)
	}
	if !apply {
		return nil
	}
	for _, op := range operations {
		if err := command.applyGroup(ctx, op); err != nil {
			return fmt.Errorf("syncing group %s: %w", op.Desired.DisplayName, err)
		}
	}
	return nil
}

func normalizeGoogleGroup(
	group googleGroup,
	members []googleMember,
	activeGoogleUsers map[string]bool,
	scimIDs map[string]string,
) (desiredGroup, error) {
	if group.ID == "" {
		return desiredGroup{}, fmt.Errorf("group is missing immutable id")
	}
	displayName := strings.TrimSpace(group.Name)
	if displayName == "" {
		displayName = group.Email
	}
	desired := desiredGroup{
		ExternalID:  googleExternalID + group.ID,
		DisplayName: displayName,
	}
	for _, member := range members {
		if member.Type != "" && member.Type != "USER" {
			return desiredGroup{}, fmt.Errorf("nested or non-user member %q of type %q is not supported", member.Email, member.Type)
		}
		isActive, inUserScope := activeGoogleUsers[member.ID]
		if !inUserScope {
			return desiredGroup{}, fmt.Errorf("Google member %q is outside the synchronized user scope", member.Email)
		}
		if !isActive {
			continue
		}
		scimID := scimIDs[member.ID]
		if scimID == "" {
			return desiredGroup{}, fmt.Errorf("active Google member %q is not provisioned in GitHub", member.Email)
		}
		desired.MemberIDs = append(desired.MemberIDs, scimID)
	}
	sort.Strings(desired.MemberIDs)
	return desired, nil
}

func groupChanged(desired desiredGroup, current scimGroup) bool {
	if desired.ExternalID != current.ExternalID || desired.DisplayName != current.DisplayName {
		return true
	}

	currentIDs := make([]string, 0, len(current.Members))
	for _, member := range current.Members {
		currentIDs = append(currentIDs, member.Value)
	}
	sort.Strings(currentIDs)
	return !sameStrings(currentIDs, desired.MemberIDs)
}

func symmetricDifferenceSize(left, right []string) int {
	leftSet := make(map[string]bool, len(left))
	rightSet := make(map[string]bool, len(right))
	for _, value := range left {
		leftSet[value] = true
	}
	for _, value := range right {
		rightSet[value] = true
	}
	var count int
	for value := range leftSet {
		if !rightSet[value] {
			count++
		}
	}
	for value := range rightSet {
		if !leftSet[value] {
			count++
		}
	}
	return count
}

func (op groupOperation) summary() string {
	action := "replace"
	if op.Create {
		action = "create"
	}
	return fmt.Sprintf(
		"%s group %s (members: %d, membership delta: %d)",
		action,
		op.Desired.DisplayName,
		len(op.Desired.MemberIDs),
		op.memberDelta(),
	)
}
