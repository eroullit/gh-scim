package main

import (
	"fmt"
	"sort"
	"strings"
)

type desiredUser struct {
	ExternalID  string
	UserName    string
	DisplayName string
	GivenName   string
	FamilyName  string
	Email       string
	Active      bool
	Roles       []string
}

type operationKind string

const (
	createUser      operationKind = "create"
	replaceUser     operationKind = "replace"
	reactivateUser  operationKind = "reactivate"
	deprovisionUser operationKind = "deprovision"
)

type operation struct {
	Kind    operationKind
	Desired desiredUser
	Current *scimUser
}

func plan(
	googleUsers []googleUser,
	currentUsers []scimUser,
	roleAttribute string,
	roleAssignments map[string]string,
	deprovisionMissing bool,
) ([]operation, []string, error) {
	byExternalID := make(map[string]*scimUser, len(currentUsers))
	byUserName := make(map[string]*scimUser, len(currentUsers))
	for i := range currentUsers {
		user := &currentUsers[i]
		if user.ExternalID != "" {
			if _, exists := byExternalID[user.ExternalID]; exists {
				return nil, nil, fmt.Errorf("duplicate SCIM externalId %q", user.ExternalID)
			}
			byExternalID[user.ExternalID] = user
		}
		key := strings.ToLower(user.UserName)
		if key != "" {
			if _, exists := byUserName[key]; exists {
				return nil, nil, fmt.Errorf("duplicate SCIM userName %q", user.UserName)
			}
			byUserName[key] = user
		}
	}

	var operations []operation
	var skipped []string
	matched := make(map[string]bool, len(googleUsers))
	for _, source := range googleUsers {
		desired, err := normalizeGoogleUser(source, roleAttribute, roleAssignments)
		if err != nil {
			return nil, nil, err
		}
		current := byExternalID[desired.ExternalID]
		usernameOwner := byUserName[strings.ToLower(desired.UserName)]
		if current == nil && usernameOwner != nil {
			return nil, nil, fmt.Errorf(
				"SCIM userName %q belongs to externalId %q, not Google id %q; migrate the mapping explicitly",
				desired.UserName,
				usernameOwner.ExternalID,
				desired.ExternalID,
			)
		}
		if current == nil {
			if desired.Active {
				operations = append(operations, operation{Kind: createUser, Desired: desired})
			} else {
				skipped = append(skipped, desired.UserName)
			}
			continue
		}
		if usernameOwner != nil && usernameOwner.ID != current.ID {
			return nil, nil, fmt.Errorf(
				"Google user %q wants SCIM userName %q, which belongs to SCIM id %q",
				desired.ExternalID,
				desired.UserName,
				usernameOwner.ID,
			)
		}
		if matched[current.ID] {
			return nil, nil, fmt.Errorf("multiple Google users resolve to SCIM id %q", current.ID)
		}
		matched[current.ID] = true
		if profileChanged(desired, *current) {
			operations = append(operations, operation{Kind: replaceUser, Desired: desired, Current: current})
			continue
		}
		if desired.Active != active(current.Active) {
			kind := deprovisionUser
			if desired.Active {
				kind = reactivateUser
			}
			operations = append(operations, operation{Kind: kind, Desired: desired, Current: current})
		}
	}

	if deprovisionMissing {
		for i := range currentUsers {
			current := &currentUsers[i]
			if matched[current.ID] || !strings.HasPrefix(current.ExternalID, googleExternalID) || !active(current.Active) {
				continue
			}
			operations = append(operations, operation{
				Kind: deprovisionUser,
				Desired: desiredUser{
					ExternalID:  current.ExternalID,
					UserName:    current.UserName,
					DisplayName: current.DisplayName,
					Email:       primaryEmail(current.Emails),
					Active:      false,
				},
				Current: current,
			})
		}
	}

	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Desired.UserName == operations[j].Desired.UserName {
			return operations[i].Kind < operations[j].Kind
		}
		return operations[i].Desired.UserName < operations[j].Desired.UserName
	})
	sort.Strings(skipped)
	return operations, skipped, nil
}

func normalizeGoogleUser(user googleUser, roleAttribute string, roleAssignments map[string]string) (desiredUser, error) {
	if strings.TrimSpace(user.ID) == "" || strings.TrimSpace(user.PrimaryEmail) == "" {
		return desiredUser{}, fmt.Errorf("Google user is missing immutable id or primaryEmail")
	}
	displayName := strings.TrimSpace(user.Name.FullName)
	if displayName == "" {
		displayName = user.PrimaryEmail
	}
	role, roleExplicit, err := googleRole(user, roleAttribute)
	if err != nil {
		return desiredUser{}, fmt.Errorf("Google user %s: %w", user.PrimaryEmail, err)
	}
	if groupRole := roleAssignments[user.ID]; groupRole != "" {
		if roleExplicit && role != groupRole {
			return desiredUser{}, fmt.Errorf(
				"Google user %s has conflicting custom-attribute role %q and group role %q",
				user.PrimaryEmail,
				role,
				groupRole,
			)
		}
		role = groupRole
	}
	return desiredUser{
		ExternalID:  googleExternalID + user.ID,
		UserName:    user.PrimaryEmail,
		DisplayName: displayName,
		GivenName:   user.Name.GivenName,
		FamilyName:  user.Name.FamilyName,
		Email:       user.PrimaryEmail,
		Active:      !user.Suspended && !user.Archived,
		Roles:       []string{role},
	}, nil
}

func profileChanged(desired desiredUser, current scimUser) bool {
	return len(profileChanges(desired, current)) > 0
}

func profileChanges(desired desiredUser, current scimUser) []string {
	var changes []string
	if current.ExternalID != desired.ExternalID {
		changes = append(changes, "externalId")
	}
	if !strings.EqualFold(current.UserName, desired.UserName) {
		changes = append(changes, "userName")
	}
	if current.DisplayName != desired.DisplayName {
		changes = append(changes, "displayName")
	}
	if !strings.EqualFold(primaryEmail(current.Emails), desired.Email) {
		changes = append(changes, "email")
	}
	if !sameStrings(roleValues(current.Roles), desired.Roles) {
		changes = append(changes, "roles")
	}
	if current.Name == nil {
		if desired.GivenName != "" {
			changes = append(changes, "givenName")
		}
		if desired.FamilyName != "" {
			changes = append(changes, "familyName")
		}
		return changes
	}
	if current.Name.GivenName != desired.GivenName {
		changes = append(changes, "givenName")
	}
	if current.Name.FamilyName != desired.FamilyName {
		changes = append(changes, "familyName")
	}
	return changes
}

func active(value *bool) bool {
	return value == nil || *value
}

func primaryEmail(emails []scimEmail) string {
	for _, email := range emails {
		if email.Primary {
			return email.Value
		}
	}
	if len(emails) > 0 {
		return emails[0].Value
	}
	return ""
}

func roleValues(roles []scimRole) []string {
	values := make([]string, 0, len(roles))
	for _, role := range roles {
		if role.Value != "" {
			values = append(values, role.Value)
		}
	}
	if len(values) == 0 {
		return []string{"user"}
	}
	return values
}

func sameStrings(left, right []string) bool {
	leftCopy := append([]string(nil), left...)
	rightCopy := append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	return strings.Join(leftCopy, "\x00") == strings.Join(rightCopy, "\x00")
}

func userFlags(user desiredUser, roles []string) []string {
	args := []string{
		"--external-id", user.ExternalID,
		"--username", user.UserName,
		"--display-name", user.DisplayName,
		"--email", user.Email,
	}
	if user.GivenName != "" {
		args = append(args, "--given-name", user.GivenName)
	}
	if user.FamilyName != "" {
		args = append(args, "--family-name", user.FamilyName)
	}
	for _, role := range roles {
		args = append(args, "--role", role)
	}
	if !user.Active {
		args = append(args, "--inactive")
	}
	return args
}

func (op operation) summary() string {
	if op.Kind == replaceUser && op.Current != nil {
		details := profileChanges(op.Desired, *op.Current)
		if op.Desired.Active != active(op.Current.Active) {
			details = append(details, fmt.Sprintf("active %t -> %t", active(op.Current.Active), op.Desired.Active))
		}
		return fmt.Sprintf("%s user %s (fields: %s)", op.Kind, op.Desired.UserName, strings.Join(details, ", "))
	}
	return fmt.Sprintf("%s user %s", op.Kind, op.Desired.UserName)
}
