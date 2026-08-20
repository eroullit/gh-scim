package scim

import (
	"context"
	"net/http"
)

// Member represents a single member entry within a SCIM group.
type Member struct {
	Value       string `json:"value"`
	Ref         string `json:"$ref,omitempty"`
	Display     string `json:"display,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// Group represents a SCIM enterprise group resource.
type Group struct {
	Schemas     []string `json:"schemas,omitempty"`
	ID          string   `json:"id,omitempty"`
	ExternalID  string   `json:"externalId,omitempty"`
	DisplayName string   `json:"displayName,omitempty"`
	Members     []Member `json:"members,omitempty"`
	Meta        *Meta    `json:"meta,omitempty"`
}

// ListGroups lists provisioned SCIM groups for the enterprise.
//
// GET /scim/v2/enterprises/{enterprise}/Groups
func (c *Client) ListGroups(ctx context.Context, params ListParams) (*ListResponse[Group], error) {
	var out ListResponse[Group]
	if err := c.do(ctx, http.MethodGet, c.collectionPath("Groups", params.query()), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetGroup retrieves a single SCIM group by its GitHub-assigned SCIM group id.
//
// GET /scim/v2/enterprises/{enterprise}/Groups/{scim_group_id}
func (c *Client) GetGroup(ctx context.Context, scimGroupID string) (*Group, error) {
	p, err := c.itemPath("Groups", scimGroupID, "")
	if err != nil {
		return nil, err
	}
	var out Group
	if err := c.do(ctx, http.MethodGet, p, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateGroup provisions a new SCIM group for the enterprise. Members
// referenced by Value must already exist as provisioned users.
//
// POST /scim/v2/enterprises/{enterprise}/Groups
func (c *Client) CreateGroup(ctx context.Context, g Group) (*Group, error) {
	if len(g.Schemas) == 0 {
		g.Schemas = []string{GroupSchema}
	}
	var out Group
	if err := c.do(ctx, http.MethodPost, c.collectionPath("Groups", ""), g, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplaceGroup replaces all of an existing group's attributes, including its
// membership list. Any attribute not provided is removed.
//
// PUT /scim/v2/enterprises/{enterprise}/Groups/{scim_group_id}
func (c *Client) ReplaceGroup(ctx context.Context, scimGroupID string, g Group) (*Group, error) {
	p, err := c.itemPath("Groups", scimGroupID, "")
	if err != nil {
		return nil, err
	}
	if len(g.Schemas) == 0 {
		g.Schemas = []string{GroupSchema}
	}
	var out Group
	if err := c.do(ctx, http.MethodPut, p, g, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PatchGroup updates individual attributes of an existing group, such as its
// displayName or membership list.
//
// PATCH /scim/v2/enterprises/{enterprise}/Groups/{scim_group_id}
func (c *Client) PatchGroup(ctx context.Context, scimGroupID string, ops ...PatchOperation) (*Group, error) {
	p, err := c.itemPath("Groups", scimGroupID, "")
	if err != nil {
		return nil, err
	}
	var out Group
	req := NewPatchRequest(ops...)
	if err := c.do(ctx, http.MethodPatch, p, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddGroupMembers is a convenience wrapper around PatchGroup that adds the
// given member ids to a group without affecting existing members.
func (c *Client) AddGroupMembers(ctx context.Context, scimGroupID string, memberIDs ...string) (*Group, error) {
	members := make([]Member, 0, len(memberIDs))
	for _, id := range memberIDs {
		members = append(members, Member{Value: id})
	}
	return c.PatchGroup(ctx, scimGroupID, PatchOperation{
		Op:    "add",
		Path:  "members",
		Value: members,
	})
}

// RemoveGroupMembers is a convenience wrapper around PatchGroup that removes
// the given member ids from a group.
func (c *Client) RemoveGroupMembers(ctx context.Context, scimGroupID string, memberIDs ...string) (*Group, error) {
	members := make([]Member, 0, len(memberIDs))
	for _, id := range memberIDs {
		members = append(members, Member{Value: id})
	}
	return c.PatchGroup(ctx, scimGroupID, PatchOperation{
		Op:    "remove",
		Path:  "members",
		Value: members,
	})
}

// DeleteGroup deletes a SCIM group from the enterprise.
//
// DELETE /scim/v2/enterprises/{enterprise}/Groups/{scim_group_id}
func (c *Client) DeleteGroup(ctx context.Context, scimGroupID string) error {
	p, err := c.itemPath("Groups", scimGroupID, "")
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, p, nil, nil)
}
