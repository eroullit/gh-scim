package scim

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type restDoer struct {
	client  *http.Client
	baseURL string
}

func (d *restDoer) DoWithContext(ctx context.Context, method, path string, body io.Reader, response any) error {
	req, err := http.NewRequestWithContext(ctx, method, d.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeAPIError(resp)
	}
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusResetContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

func decodeAPIError(resp *http.Response) error {
	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		RequestURL: resp.Request.URL.String(),
	}

	var body struct {
		Detail   string `json:"detail"`
		Message  string `json:"message"`
		SCIMType string `json:"scimType"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil && err != io.EOF {
		apiErr.Message = resp.Status
		apiErr.err = fmt.Errorf("decoding error response: %w", err)
		return apiErr
	}

	apiErr.Message = body.Detail
	if apiErr.Message == "" {
		apiErr.Message = body.Message
	}
	if apiErr.Message == "" {
		apiErr.Message = resp.Status
	}
	apiErr.SCIMType = body.SCIMType
	return apiErr
}
