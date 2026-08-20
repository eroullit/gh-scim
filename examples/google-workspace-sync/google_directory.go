package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type googleName struct {
	FullName   string `json:"fullName"`
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

type googleUser struct {
	ID            string                    `json:"id"`
	PrimaryEmail  string                    `json:"primaryEmail"`
	Name          googleName                `json:"name"`
	Suspended     bool                      `json:"suspended"`
	Archived      bool                      `json:"archived"`
	CustomSchemas map[string]map[string]any `json:"customSchemas"`
}

type googleUsersResponse struct {
	Users         []googleUser `json:"users"`
	NextPageToken string       `json:"nextPageToken"`
}

type directoryClient struct {
	httpClient *http.Client
	baseURL    string
}

func (c directoryClient) listUsers(ctx context.Context, customerID, query, roleAttribute string) ([]googleUser, error) {
	var users []googleUser
	pageToken := ""
	for {
		endpoint, err := url.Parse(c.baseURL + "/users")
		if err != nil {
			return nil, fmt.Errorf("parsing Directory API URL: %w", err)
		}
		params := endpoint.Query()
		params.Set("customer", customerID)
		params.Set("maxResults", "500")
		params.Set("orderBy", "email")
		if roleAttribute == "" {
			params.Set("projection", "basic")
		} else {
			schema, _, err := splitRoleAttribute(roleAttribute)
			if err != nil {
				return nil, err
			}
			params.Set("projection", "custom")
			params.Set("customFieldMask", schema)
		}
		if query != "" {
			params.Set("query", query)
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		endpoint.RawQuery = params.Encode()

		var page googleUsersResponse
		if err := c.getJSON(ctx, endpoint.String(), &page); err != nil {
			return nil, err
		}
		users = append(users, page.Users...)
		if page.NextPageToken == "" {
			return users, nil
		}
		pageToken = page.NextPageToken
	}
}

func (c directoryClient) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("building Directory API request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling Directory API: %w", err)
	}
	body, err := readResponse(resp)
	if err != nil {
		return fmt.Errorf("reading Directory API response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Directory API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding Directory API response: %w", err)
	}
	return nil
}
