package scim_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/eroullit/gh-scim/scim"
)

func ExampleNewClient() {
	client, err := scim.NewClient(
		"octo-enterprise",
		scim.WithHost("github.com"),
		scim.WithToken(os.Getenv("SCIM_TOKEN")),
		scim.WithTimeout(30*time.Second),
	)
	if err != nil {
		return
	}

	users, err := client.ListUsers(context.Background(), scim.ListParams{
		Filter: `userName eq "octocat"`,
	})
	if err != nil {
		return
	}
	_ = users.Resources
}

func ExampleAPIError() {
	client, err := scim.NewClient("octo-enterprise")
	if err != nil {
		return
	}

	_, err = client.GetUser(context.Background(), "scim-user-id")
	var apiErr *scim.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		// Handle a missing SCIM user.
	}
}
