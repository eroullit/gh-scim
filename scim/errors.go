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
	RequestURL string

	err error
}

// Error returns a human-readable API error.
func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("SCIM API returned HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("SCIM API returned HTTP %d", e.StatusCode)
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
