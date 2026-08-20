// Package scim implements a client for GitHub's SCIM REST API for Enterprise
// Managed Users (EMU).
//
// See:
// https://docs.github.com/en/enterprise-cloud@latest/rest/enterprise-admin/scim
package scim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
)

const (
	// UserSchema is the SCIM schema URN for User resources.
	UserSchema = "urn:ietf:params:scim:schemas:core:2.0:User"
	// GroupSchema is the SCIM schema URN for Group resources.
	GroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"
	// PatchOpSchema is the SCIM schema URN used for PATCH operations.
	PatchOpSchema = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
)

// Doer executes a REST request. Callers can implement Doer to provide custom
// authentication, routing, retries, or test doubles.
type Doer interface {
	DoWithContext(ctx context.Context, method, path string, body io.Reader, response any) error
}

// Option configures a Client.
type Option interface {
	apply(*clientOptions) error
}

type optionFunc func(*clientOptions) error

func (f optionFunc) apply(opts *clientOptions) error {
	return f(opts)
}

type clientOptions struct {
	doer         Doer
	host         string
	token        string
	timeout      time.Duration
	transport    http.RoundTripper
	hasHost      bool
	hasToken     bool
	hasTimeout   bool
	hasTransport bool
}

// WithHost sets the GitHub.com or GHE.com hostname used for SCIM requests.
// Both SUBDOMAIN.ghe.com and api.SUBDOMAIN.ghe.com are accepted for GHE.com.
func WithHost(host string) Option {
	return optionFunc(func(opts *clientOptions) error {
		host = strings.TrimSpace(host)
		if host == "" {
			return fmt.Errorf("host is required")
		}
		opts.host = host
		opts.hasHost = true
		return nil
	})
}

// WithToken sets the personal access token used to authenticate SCIM requests.
func WithToken(token string) Option {
	return optionFunc(func(opts *clientOptions) error {
		token = strings.TrimSpace(token)
		if token == "" {
			return fmt.Errorf("token is required")
		}
		opts.token = token
		opts.hasToken = true
		return nil
	})
}

// WithTimeout sets the timeout applied to each request by the default
// go-gh-backed Doer.
func WithTimeout(timeout time.Duration) Option {
	return optionFunc(func(opts *clientOptions) error {
		if timeout <= 0 {
			return fmt.Errorf("timeout must be greater than zero")
		}
		opts.timeout = timeout
		opts.hasTimeout = true
		return nil
	})
}

// WithTransport sets the HTTP transport used by the default go-gh-backed Doer.
func WithTransport(transport http.RoundTripper) Option {
	return optionFunc(func(opts *clientOptions) error {
		if transport == nil {
			return fmt.Errorf("transport is required")
		}
		opts.transport = transport
		opts.hasTransport = true
		return nil
	})
}

// WithDoer supplies a complete request executor. It cannot be combined with
// WithHost, WithToken, WithTimeout, or WithTransport because those options
// configure the default go-gh-backed Doer.
func WithDoer(doer Doer) Option {
	return optionFunc(func(opts *clientOptions) error {
		if doer == nil {
			return fmt.Errorf("doer is required")
		}
		opts.doer = doer
		return nil
	})
}

// Client talks to the GitHub SCIM API for a single enterprise. A Client is safe
// for concurrent use when its configured Doer is safe for concurrent use. The
// default Doer is safe for concurrent use.
type Client struct {
	doer       Doer
	enterprise string
}

// NewClient builds a SCIM client for the given enterprise slug.
//
// Without options, NewClient uses go-gh's normal host and token resolution.
// The resolved host must be GitHub.com or a GHE.com tenancy; GitHub Enterprise
// Server does not support this enterprise SCIM API.
func NewClient(enterprise string, options ...Option) (*Client, error) {
	enterprise = strings.TrimSpace(enterprise)
	if enterprise == "" {
		return nil, fmt.Errorf("enterprise slug is required")
	}

	var opts clientOptions
	for i, option := range options {
		if option == nil {
			return nil, fmt.Errorf("option %d is nil", i)
		}
		if err := option.apply(&opts); err != nil {
			return nil, fmt.Errorf("applying option %d: %w", i, err)
		}
	}

	if opts.doer != nil {
		if opts.hasHost || opts.hasToken || opts.hasTimeout || opts.hasTransport {
			return nil, fmt.Errorf("WithDoer cannot be combined with host, token, timeout, or transport options")
		}
		return &Client{doer: opts.doer, enterprise: enterprise}, nil
	}

	host := opts.host
	if host == "" {
		host, _ = auth.DefaultHost()
	}
	if err := validateHost(host); err != nil {
		return nil, err
	}

	rest, err := api.NewRESTClient(api.ClientOptions{
		AuthToken: opts.token,
		Host:      host,
		Headers: map[string]string{
			"Accept": "application/vnd.github+json",
		},
		Timeout:   opts.timeout,
		Transport: opts.transport,
	})
	if err != nil {
		return nil, fmt.Errorf("building REST client: %w", err)
	}

	return &Client{doer: rest, enterprise: enterprise}, nil
}

func validateHost(host string) error {
	normalized := auth.NormalizeHostname(strings.TrimSpace(host))
	if normalized == "github.com" || auth.IsTenancy(normalized) {
		return nil
	}
	return fmt.Errorf("unsupported GitHub host %q: enterprise SCIM supports GitHub.com and GHE.com, not GitHub Enterprise Server", host)
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

// do issues an HTTP request against the SCIM API, marshaling body as JSON and
// unmarshaling the response into out.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
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

	var err error
	if out == nil {
		var discard any
		err = c.doer.DoWithContext(ctx, method, path, reader, &discard)
	} else {
		err = c.doer.DoWithContext(ctx, method, path, reader, out)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, translateError(err))
	}
	return nil
}
