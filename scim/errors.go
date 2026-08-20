package scim

import (
	"errors"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/api"
)

// APIError describes an error response from the GitHub SCIM API.
type APIError struct {
	StatusCode int
	Message    string
	// SCIMType is the RFC 7644 "scimType" detail code (e.g. "uniqueness"),
	// when the server included one. It is empty if the response body did
	// not carry a SCIM error detail.
	SCIMType   string
	RequestURL string

	err error
}

// Error returns a human-readable API error.
func (e *APIError) Error() string {
	switch {
	case e.Message != "" && e.SCIMType != "":
		return fmt.Sprintf("SCIM API returned HTTP %d: %s (scimType: %s)", e.StatusCode, e.Message, e.SCIMType)
	case e.Message != "":
		return fmt.Sprintf("SCIM API returned HTTP %d: %s", e.StatusCode, e.Message)
	default:
		return fmt.Sprintf("SCIM API returned HTTP %d", e.StatusCode)
	}
}

// Unwrap returns the underlying transport error.
func (e *APIError) Unwrap() error {
	return e.err
}

// IsStatus reports whether err contains an APIError with the given HTTP status.
func IsStatus(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

func translateError(err error) error {
	var httpErr *api.HTTPError
	if !errors.As(err, &httpErr) {
		return err
	}

	requestURL := ""
	if httpErr.RequestURL != nil {
		requestURL = httpErr.RequestURL.String()
	}

	return &APIError{
		StatusCode: httpErr.StatusCode,
		Message:    httpErr.Message,
		RequestURL: requestURL,
		err:        err,
	}
}
