// Package scim implements a thin client around GitHub's SCIM REST API for
// Enterprise Managed Users (EMU), allowing (de)provisioning of users and
// groups without a paved-path identity provider.
//
// See:
// https://docs.github.com/en/enterprise-cloud@latest/rest/enterprise-admin/scim
package scim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/cli/go-gh/v2/pkg/api"
)

const (
	// UserSchema is the SCIM schema URN for User resources.
	UserSchema = "urn:ietf:params:scim:schemas:core:2.0:User"
	// GroupSchema is the SCIM schema URN for Group resources.
	GroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"
	// PatchOpSchema is the SCIM schema URN used for PATCH operations.
	PatchOpSchema = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
)

// Client wraps a go-gh REST client to talk to the SCIM API for a single
// enterprise.
type Client struct {
	rest       *api.RESTClient
	enterprise string
}

// NewClient builds a SCIM client for the given enterprise slug, optionally
// targeting a specific GitHub host (e.g. for GHE.com enterprises). If host is
// empty, the go-gh default host resolution is used.
func NewClient(enterprise, host string) (*Client, error) {
	if enterprise == "" {
		return nil, fmt.Errorf("enterprise slug is required")
	}

	opts := api.ClientOptions{
		Headers: map[string]string{
			"Accept": "application/vnd.github+json",
		},
	}
	if host != "" {
		opts.Host = host
	}

	rest, err := api.NewRESTClient(opts)
	if err != nil {
		return nil, fmt.Errorf("building REST client: %w", err)
	}

	return &Client{rest: rest, enterprise: enterprise}, nil
}

// ListParams holds common pagination/filter query parameters supported by
// the SCIM list endpoints.
type ListParams struct {
	// Filter is a SCIM filter expression, e.g. `userName eq "octocat"`.
	Filter string
	// StartIndex is the 1-based index of the first result to return.
	StartIndex int
	// Count is the number of results to return per page.
	Count int
	// ExcludedAttributes, when set to "members", speeds up group listing by
	// omitting membership information from the response.
	ExcludedAttributes string
}

func (p ListParams) query() string {
	q := url.Values{}
	if p.Filter != "" {
		q.Set("filter", p.Filter)
	}
	if p.StartIndex > 0 {
		q.Set("startIndex", fmt.Sprintf("%d", p.StartIndex))
	}
	if p.Count > 0 {
		q.Set("count", fmt.Sprintf("%d", p.Count))
	}
	if p.ExcludedAttributes != "" {
		q.Set("excludedAttributes", p.ExcludedAttributes)
	}
	encoded := q.Encode()
	if encoded == "" {
		return ""
	}
	return "?" + encoded
}

// ListResponse is the generic SCIM ListResponse envelope returned by the
// Users and Groups list endpoints.
type ListResponse[T any] struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	ItemsPerPage int      `json:"itemsPerPage"`
	StartIndex   int      `json:"startIndex"`
	Resources    []T      `json:"Resources"`
}

// PatchOperation represents a single SCIM PATCH operation.
type PatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path,omitempty"`
	Value any    `json:"value,omitempty"`
}

// PatchRequest is the body sent to SCIM PATCH endpoints.
type PatchRequest struct {
	Schemas    []string         `json:"schemas"`
	Operations []PatchOperation `json:"Operations"`
}

// NewPatchRequest builds a PatchRequest wrapping the given operations with
// the required PatchOp schema.
func NewPatchRequest(ops ...PatchOperation) PatchRequest {
	return PatchRequest{
		Schemas:    []string{PatchOpSchema},
		Operations: ops,
	}
}

func (c *Client) path(resource, id, query string) string {
	p := fmt.Sprintf("scim/v2/enterprises/%s/%s", c.enterprise, resource)
	if id != "" {
		p = fmt.Sprintf("%s/%s", p, url.PathEscape(id))
	}
	return p + query
}

// do issues an HTTP request against the SCIM API, marshaling body (if any)
// as JSON and unmarshaling the response into out (if non-nil).
func (c *Client) do(method, path string, body, out any) error {
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	if out == nil {
		var discard any
		return c.rest.Do(method, path, reader, &discard)
	}
	return c.rest.Do(method, path, reader, out)
}
