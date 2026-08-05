package scim

import "net/http"

// Name represents the SCIM "name" complex attribute for a user.
type Name struct {
	Formatted  string `json:"formatted,omitempty"`
	FamilyName string `json:"familyName"`
	GivenName  string `json:"givenName"`
	MiddleName string `json:"middleName,omitempty"`
}

// Email represents a single email entry for a SCIM user.
type Email struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary"`
}

// Role represents a role assigned to a SCIM user (e.g. user, enterprise_owner,
// billing_manager, guest_collaborator).
type Role struct {
	Display string `json:"display,omitempty"`
	Type    string `json:"type,omitempty"`
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

// GroupRef references a group a user belongs to, as returned in a user's
// resource representation.
type GroupRef struct {
	Value   string `json:"value,omitempty"`
	Ref     string `json:"$ref,omitempty"`
	Display string `json:"display,omitempty"`
}

// Meta holds SCIM resource metadata.
type Meta struct {
	ResourceType string `json:"resourceType,omitempty"`
	Created      string `json:"created,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Location     string `json:"location,omitempty"`
}

// User represents a SCIM enterprise user resource.
type User struct {
	Schemas     []string   `json:"schemas,omitempty"`
	ID          string     `json:"id,omitempty"`
	ExternalID  string     `json:"externalId,omitempty"`
	Active      *bool      `json:"active,omitempty"`
	UserName    string     `json:"userName,omitempty"`
	Name        *Name      `json:"name,omitempty"`
	DisplayName string     `json:"displayName,omitempty"`
	Emails      []Email    `json:"emails,omitempty"`
	Roles       []Role     `json:"roles,omitempty"`
	Groups      []GroupRef `json:"groups,omitempty"`
	Meta        *Meta      `json:"meta,omitempty"`
}

// ListUsers lists provisioned SCIM users for the enterprise.
//
// GET /scim/v2/enterprises/{enterprise}/Users
func (c *Client) ListUsers(params ListParams) (*ListResponse[User], error) {
	var out ListResponse[User]
	if err := c.do(http.MethodGet, c.path("Users", "", params.query()), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetUser retrieves a single SCIM user by its GitHub-assigned SCIM user id.
//
// GET /scim/v2/enterprises/{enterprise}/Users/{scim_user_id}
func (c *Client) GetUser(scimUserID string) (*User, error) {
	var out User
	if err := c.do(http.MethodGet, c.path("Users", scimUserID, ""), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateUser provisions a new SCIM user for the enterprise.
//
// POST /scim/v2/enterprises/{enterprise}/Users
func (c *Client) CreateUser(u User) (*User, error) {
	if len(u.Schemas) == 0 {
		u.Schemas = []string{UserSchema}
	}
	var out User
	if err := c.do(http.MethodPost, c.path("Users", "", ""), u, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceUser replaces all of an existing user's attributes. Any attribute
// not provided is removed, matching the semantics of a SCIM PUT.
//
// PUT /scim/v2/enterprises/{enterprise}/Users/{scim_user_id}
func (c *Client) ReplaceUser(scimUserID string, u User) (*User, error) {
	if len(u.Schemas) == 0 {
		u.Schemas = []string{UserSchema}
	}
	var out User
	if err := c.do(http.MethodPut, c.path("Users", scimUserID, ""), u, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PatchUser updates individual attributes of an existing user.
//
// PATCH /scim/v2/enterprises/{enterprise}/Users/{scim_user_id}
func (c *Client) PatchUser(scimUserID string, ops ...PatchOperation) (*User, error) {
	var out User
	req := NewPatchRequest(ops...)
	if err := c.do(http.MethodPatch, c.path("Users", scimUserID, ""), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetUserActive is a convenience wrapper around PatchUser to soft-deprovision
// (active=false) or reactivate (active=true) a user.
func (c *Client) SetUserActive(scimUserID string, active bool) (*User, error) {
	return c.PatchUser(scimUserID, PatchOperation{
		Op:    "replace",
		Path:  "active",
		Value: active,
	})
}

// DeleteUser hard-deprovisions (permanently suspends) a user. This action is
// irreversible; the user must be provisioned again as a new user afterwards.
//
// DELETE /scim/v2/enterprises/{enterprise}/Users/{scim_user_id}
func (c *Client) DeleteUser(scimUserID string) error {
	return c.do(http.MethodDelete, c.path("Users", scimUserID, ""), nil, nil)
}
