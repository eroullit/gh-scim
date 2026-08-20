package main

type scimName struct {
	FamilyName string `json:"familyName"`
	GivenName  string `json:"givenName"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
}

type scimRole struct {
	Value string `json:"value"`
}

type scimUser struct {
	ID          string      `json:"id"`
	ExternalID  string      `json:"externalId"`
	Active      *bool       `json:"active"`
	UserName    string      `json:"userName"`
	Name        *scimName   `json:"name"`
	DisplayName string      `json:"displayName"`
	Emails      []scimEmail `json:"emails"`
	Roles       []scimRole  `json:"roles"`
}

type scimMember struct {
	Value string `json:"value"`
}

type scimGroup struct {
	ID          string       `json:"id"`
	ExternalID  string       `json:"externalId"`
	DisplayName string       `json:"displayName"`
	Members     []scimMember `json:"members"`
}

type scimListResponse[T any] struct {
	TotalResults int `json:"totalResults"`
	Resources    []T `json:"Resources"`
}
