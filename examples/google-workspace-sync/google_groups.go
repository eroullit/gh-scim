package main

import (
	"context"
	"fmt"
	"net/url"
)

type googleGroup struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type googleMember struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Type  string `json:"type"`
}

type googleMembersResponse struct {
	Members       []googleMember `json:"members"`
	NextPageToken string         `json:"nextPageToken"`
}

func (c directoryClient) getGroup(ctx context.Context, groupKey string) (googleGroup, error) {
	endpoint := c.baseURL + "/groups/" + url.PathEscape(groupKey)
	var group googleGroup
	if err := c.getJSON(ctx, endpoint, &group); err != nil {
		return googleGroup{}, err
	}
	return group, nil
}

func (c directoryClient) listGroupMembers(ctx context.Context, groupKey string) ([]googleMember, error) {
	var members []googleMember
	pageToken := ""
	for {
		endpoint, err := url.Parse(c.baseURL + "/groups/" + url.PathEscape(groupKey) + "/members")
		if err != nil {
			return nil, fmt.Errorf("parsing group members URL: %w", err)
		}
		params := endpoint.Query()
		params.Set("maxResults", "200")
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		endpoint.RawQuery = params.Encode()

		var page googleMembersResponse
		if err := c.getJSON(ctx, endpoint.String(), &page); err != nil {
			return nil, err
		}
		members = append(members, page.Members...)
		if page.NextPageToken == "" {
			return members, nil
		}
		pageToken = page.NextPageToken
	}
}
