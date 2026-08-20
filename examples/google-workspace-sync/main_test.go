package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func boolPointer(value bool) *bool {
	return &value
}

func TestPlan(t *testing.T) {
	sourceActive := googleUser{
		ID:           "google-id-1",
		PrimaryEmail: "mona@example.com",
		Name: googleName{
			FullName:   "Mona Octocat",
			GivenName:  "Mona",
			FamilyName: "Octocat",
		},
	}

	currentActive := scimUser{
		ID:          "scim-id-1",
		ExternalID:  "google:google-id-1",
		Active:      boolPointer(true),
		UserName:    "mona@example.com",
		DisplayName: "Mona Octocat",
		Name:        &scimName{GivenName: "Mona", FamilyName: "Octocat"},
		Emails:      []scimEmail{{Value: "mona@example.com", Primary: true}},
	}

	tests := []struct {
		name               string
		googleUsers        []googleUser
		currentUsers       []scimUser
		deprovisionMissing bool
		wantKind           operationKind
		wantOperations     int
		wantSkipped        int
		wantErr            bool
	}{
		{
			name:           "creates active first-seen user",
			googleUsers:    []googleUser{sourceActive},
			wantKind:       createUser,
			wantOperations: 1,
		},
		{
			name: "skips inactive first-seen user",
			googleUsers: []googleUser{{
				ID:           "google-id-1",
				PrimaryEmail: "mona@example.com",
				Suspended:    true,
			}},
			wantSkipped: 1,
		},
		{
			name:         "does nothing for matching user",
			googleUsers:  []googleUser{sourceActive},
			currentUsers: []scimUser{currentActive},
		},
		{
			name:        "rejects email-derived legacy external id",
			googleUsers: []googleUser{sourceActive},
			currentUsers: []scimUser{func() scimUser {
				user := currentActive
				user.ExternalID = "google:mona@example.com"
				return user
			}()},
			wantErr: true,
		},
		{
			name:        "rejects username owned by another immutable identity",
			googleUsers: []googleUser{sourceActive},
			currentUsers: []scimUser{func() scimUser {
				user := currentActive
				user.ExternalID = "google:different-google-id"
				return user
			}()},
			wantErr: true,
		},
		{
			name: "rejects email change to another SCIM username",
			googleUsers: []googleUser{{
				ID:           "google-id-1",
				PrimaryEmail: "other@example.com",
			}},
			currentUsers: []scimUser{
				currentActive,
				{
					ID:          "scim-id-2",
					ExternalID:  "google:google-id-2",
					Active:      boolPointer(true),
					UserName:    "other@example.com",
					DisplayName: "Other User",
					Emails:      []scimEmail{{Value: "other@example.com", Primary: true}},
				},
			},
			wantErr: true,
		},
		{
			name:        "reactivates restored user",
			googleUsers: []googleUser{sourceActive},
			currentUsers: []scimUser{func() scimUser {
				user := currentActive
				user.Active = boolPointer(false)
				return user
			}()},
			wantKind:       reactivateUser,
			wantOperations: 1,
		},
		{
			name:               "deprovisions missing managed user when enabled",
			currentUsers:       []scimUser{currentActive},
			deprovisionMissing: true,
			wantKind:           deprovisionUser,
			wantOperations:     1,
		},
		{
			name:         "preserves missing managed user by default",
			currentUsers: []scimUser{currentActive},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operations, skipped, err := plan(test.googleUsers, test.currentUsers, "GitHub.role", nil, test.deprovisionMissing)
			if (err != nil) != test.wantErr {
				t.Fatalf("plan error = %v, wantErr %t", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if len(operations) != test.wantOperations {
				t.Fatalf("got %d operations, want %d", len(operations), test.wantOperations)
			}
			if len(skipped) != test.wantSkipped {
				t.Fatalf("got %d skipped users, want %d", len(skipped), test.wantSkipped)
			}
			if test.wantOperations > 0 && operations[0].Kind != test.wantKind {
				t.Fatalf("got operation %q, want %q", operations[0].Kind, test.wantKind)
			}
		})
	}
}

func TestValidateConfigRejectsScopedDeprovision(t *testing.T) {
	cfg := config{
		serviceAccountEmail: "sync@example.iam.gserviceaccount.com",
		adminSubject:        "admin@example.com",
		customerID:          "my_customer",
		enterprise:          "example",
		query:               "orgUnitPath='/pilot'",
		deprovisionMissing:  true,
		maxChanges:          20,
		maxGroupMemberDelta: 20,
		timeout:             2 * time.Minute,
	}

	if err := validateConfig(cfg); err == nil {
		t.Fatal("validateConfig returned nil error for scoped deprovision")
	}
}

func TestValidateConfigRejectsInvalidCustomerAndTimeout(t *testing.T) {
	valid := config{
		serviceAccountEmail: "sync@example.iam.gserviceaccount.com",
		adminSubject:        "admin@example.com",
		customerID:          "my_customer",
		enterprise:          "example",
		maxChanges:          20,
		maxGroupMemberDelta: 20,
		timeout:             2 * time.Minute,
	}

	tests := []struct {
		name    string
		update  func(*config)
		wantErr string
	}{
		{
			name:    "empty customer",
			update:  func(cfg *config) { cfg.customerID = "" },
			wantErr: "missing required configuration: --customer",
		},
		{
			name:    "whitespace customer",
			update:  func(cfg *config) { cfg.customerID = " \t" },
			wantErr: "missing required configuration: --customer",
		},
		{
			name:    "zero timeout",
			update:  func(cfg *config) { cfg.timeout = 0 },
			wantErr: "--timeout must be greater than 0",
		},
		{
			name:    "negative timeout",
			update:  func(cfg *config) { cfg.timeout = -time.Second },
			wantErr: "--timeout must be greater than 0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.update(&cfg)
			err := validateConfig(cfg)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("validateConfig error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateRoleAssignmentScope(t *testing.T) {
	users := []googleUser{{ID: "one"}}

	tests := []struct {
		name        string
		assignments map[string]string
		wantErr     bool
	}{
		{
			name:        "assignment in scope",
			assignments: map[string]string{"one": "enterprise_owner"},
		},
		{
			name:        "assignment outside scope",
			assignments: map[string]string{"two": "enterprise_owner"},
			wantErr:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRoleAssignmentScope(users, test.assignments)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateRoleAssignmentScope error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestOperationSummaryShowsLifecycleChange(t *testing.T) {
	current := scimUser{Active: boolPointer(true)}
	op := operation{
		Kind:    replaceUser,
		Desired: desiredUser{UserName: "mona@example.com", Active: false, Roles: []string{"user"}},
		Current: &current,
	}

	const want = "replace user mona@example.com (fields: userName, active true -> false)"
	if got := op.summary(); got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestNormalizeGoogleUser(t *testing.T) {
	tests := []struct {
		name       string
		source     googleUser
		wantActive bool
		wantErr    bool
	}{
		{
			name: "active user",
			source: googleUser{
				ID:           "123",
				PrimaryEmail: "user@example.com",
				Name:         googleName{FullName: "Example User"},
			},
			wantActive: true,
		},
		{
			name: "suspended user",
			source: googleUser{
				ID:           "123",
				PrimaryEmail: "user@example.com",
				Suspended:    true,
			},
		},
		{
			name:    "missing identifiers",
			source:  googleUser{},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeGoogleUser(test.source, "GitHub.role", nil)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeGoogleUser error = %v, wantErr %t", err, test.wantErr)
			}
			if err == nil && got.Active != test.wantActive {
				t.Fatalf("active = %t, want %t", got.Active, test.wantActive)
			}
		})
	}
}

func TestGoogleRole(t *testing.T) {
	tests := []struct {
		name     string
		user     googleUser
		want     string
		explicit bool
		wantErr  bool
	}{
		{
			name: "defaults to user",
			want: "user",
		},
		{
			name: "maps enterprise owner",
			user: googleUser{CustomSchemas: map[string]map[string]any{
				"GitHub": {"role": "enterprise_owner"},
			}},
			want:     "enterprise_owner",
			explicit: true,
		},
		{
			name: "rejects unsupported role",
			user: googleUser{CustomSchemas: map[string]map[string]any{
				"GitHub": {"role": "organization_owner"},
			}},
			wantErr: true,
		},
		{
			name: "rejects multi-valued role",
			user: googleUser{CustomSchemas: map[string]map[string]any{
				"GitHub": {"role": []any{"user", "enterprise_owner"}},
			}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, explicit, err := googleRole(test.user, "GitHub.role")
			if (err != nil) != test.wantErr {
				t.Fatalf("googleRole error = %v, wantErr %t", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("role = %q, want %q", got, test.want)
			}
			if err == nil && explicit != test.explicit {
				t.Fatalf("explicit = %t, want %t", explicit, test.explicit)
			}
		})
	}
}

func TestSplitRoleAttributeNormalizesWhitespace(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantSchema string
		wantField  string
	}{
		{
			name:       "trims schema",
			value:      " GitHub.role",
			wantSchema: "GitHub",
			wantField:  "role",
		},
		{
			name:       "trims field",
			value:      "GitHub. role ",
			wantSchema: "GitHub",
			wantField:  "role",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema, field, err := splitRoleAttribute(test.value)
			if err != nil {
				t.Fatalf("splitRoleAttribute error = %v", err)
			}
			if schema != test.wantSchema || field != test.wantField {
				t.Fatalf("splitRoleAttribute = %q, %q; want %q, %q", schema, field, test.wantSchema, test.wantField)
			}
		})
	}
}

func TestNormalizeGoogleUserRejectsConflictingRoleSources(t *testing.T) {
	user := googleUser{
		ID:           "google-id-1",
		PrimaryEmail: "mona@example.com",
		CustomSchemas: map[string]map[string]any{
			"GitHub": {"role": "user"},
		},
	}

	_, err := normalizeGoogleUser(
		user,
		"GitHub.role",
		map[string]string{"google-id-1": "enterprise_owner"},
	)
	if err == nil {
		t.Fatal("normalizeGoogleUser returned nil error for conflicting role sources")
	}
}

func TestDirectoryClientListUsersPaginates(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.URL.Query().Get("customer"); got != "my_customer" {
			t.Errorf("customer = %q, want my_customer", got)
		}
		if got := r.URL.Query().Get("query"); got != "orgUnitPath='/pilot'" {
			t.Errorf("query = %q, want pilot query", got)
		}

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageToken") == "" {
			if _, err := w.Write([]byte(`{"users":[{"id":"1","primaryEmail":"one@example.com"}],"nextPageToken":"next"}`)); err != nil {
				t.Errorf("write first page: %v", err)
			}
			return
		}
		if _, err := w.Write([]byte(`{"users":[{"id":"2","primaryEmail":"two@example.com"}]}`)); err != nil {
			t.Errorf("write second page: %v", err)
		}
	}))
	defer server.Close()

	client := directoryClient{httpClient: server.Client(), baseURL: server.URL}
	users, err := client.listUsers(context.Background(), "my_customer", "orgUnitPath='/pilot'", "GitHub.role")
	if err != nil {
		t.Fatalf("listUsers returned error: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want 2", len(users))
	}
	if requests != 2 {
		t.Fatalf("got %d requests, want 2", requests)
	}
}

func TestKeylessDWDTokenSource(t *testing.T) {
	fixedNow := time.Date(2026, time.August, 20, 8, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sign":
			if got := r.Header.Get("Authorization"); got != "Bearer base-token" {
				t.Errorf("authorization = %q, want base token", got)
			}
			var request struct {
				Payload string `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode sign request: %v", err)
				return
			}
			var claims map[string]any
			if err := json.Unmarshal([]byte(request.Payload), &claims); err != nil {
				t.Errorf("decode claims: %v", err)
				return
			}
			if got := claims["sub"]; got != "admin@example.com" {
				t.Errorf("sub = %v, want admin@example.com", got)
			}
			if got := claims["scope"]; got != directoryUserScope {
				t.Errorf("scope = %v, want %s", got, directoryUserScope)
			}
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(`{"signedJwt":"signed-jwt"}`)); err != nil {
				t.Errorf("write sign response: %v", err)
			}
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
				return
			}
			if got := r.Form.Get("assertion"); got != "signed-jwt" {
				t.Errorf("assertion = %q, want signed-jwt", got)
			}
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(`{"access_token":"delegated-token","token_type":"Bearer","expires_in":3600}`)); err != nil {
				t.Errorf("write token response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := keylessDWDTokenSource{
		ctx:                 context.Background(),
		base:                oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "base-token"}),
		httpClient:          server.Client(),
		serviceAccountEmail: "sync@example.iam.gserviceaccount.com",
		subject:             "admin@example.com",
		scope:               directoryUserScope,
		signURL:             server.URL + "/sign",
		tokenURL:            server.URL + "/token",
		now:                 func() time.Time { return fixedNow },
	}
	token, err := source.Token()
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if token.AccessToken != "delegated-token" {
		t.Fatalf("access token = %q, want delegated-token", token.AccessToken)
	}
	if want := fixedNow.Add(time.Hour); !token.Expiry.Equal(want) {
		t.Fatalf("expiry = %s, want %s", token.Expiry, want)
	}
}
