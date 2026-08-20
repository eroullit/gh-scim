package scim_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eroullit/gh-scim/scim"
)

type recordedCall struct {
	method string
	path   string
	body   []byte
}

type recordingDoer struct {
	mu       sync.Mutex
	calls    []recordedCall
	response []byte
	err      error
}

func (d *recordingDoer) DoWithContext(ctx context.Context, method, path string, body io.Reader, response any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	d.mu.Lock()
	d.calls = append(d.calls, recordedCall{method: method, path: path, body: data})
	d.mu.Unlock()

	if d.err != nil {
		return d.err
	}
	if len(d.response) == 0 {
		return nil
	}
	return json.Unmarshal(d.response, response)
}

func (d *recordingDoer) lastCall(t *testing.T) recordedCall {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(d.calls))
	}
	return d.calls[0]
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClientOperations(t *testing.T) {
	user := scim.User{UserName: "octocat"}
	group := scim.Group{DisplayName: "Engineering"}

	tests := []struct {
		name     string
		run      func(context.Context, *scim.Client) error
		method   string
		path     string
		bodyJSON string
	}{
		{
			name: "list users",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.ListUsers(ctx, scim.ListParams{
					Filter:     `userName eq "octocat"`,
					StartIndex: 2,
					Count:      25,
				})
				return err
			},
			method: http.MethodGet,
			path:   `scim/v2/enterprises/acme/Users?count=25&filter=userName+eq+%22octocat%22&startIndex=2`,
		},
		{
			name: "get user",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.GetUser(ctx, "user/id")
				return err
			},
			method: http.MethodGet,
			path:   "scim/v2/enterprises/acme/Users/user%2Fid",
		},
		{
			name: "create user",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.CreateUser(ctx, user)
				return err
			},
			method:   http.MethodPost,
			path:     "scim/v2/enterprises/acme/Users",
			bodyJSON: `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"octocat"}`,
		},
		{
			name: "replace user",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.ReplaceUser(ctx, "user-id", user)
				return err
			},
			method:   http.MethodPut,
			path:     "scim/v2/enterprises/acme/Users/user-id",
			bodyJSON: `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"octocat"}`,
		},
		{
			name: "patch user",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.PatchUser(ctx, "user-id", scim.PatchOperation{
					Op: "replace", Path: "displayName", Value: "Octocat",
				})
				return err
			},
			method:   http.MethodPatch,
			path:     "scim/v2/enterprises/acme/Users/user-id",
			bodyJSON: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"displayName","value":"Octocat"}]}`,
		},
		{
			name: "set user active",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.SetUserActive(ctx, "user-id", false)
				return err
			},
			method:   http.MethodPatch,
			path:     "scim/v2/enterprises/acme/Users/user-id",
			bodyJSON: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"active","value":false}]}`,
		},
		{
			name: "delete user",
			run: func(ctx context.Context, client *scim.Client) error {
				return client.DeleteUser(ctx, "user-id")
			},
			method: http.MethodDelete,
			path:   "scim/v2/enterprises/acme/Users/user-id",
		},
		{
			name: "list groups",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.ListGroups(ctx, scim.ListParams{
					Filter:             `displayName eq "Engineering"`,
					ExcludedAttributes: "members",
				})
				return err
			},
			method: http.MethodGet,
			path:   `scim/v2/enterprises/acme/Groups?excludedAttributes=members&filter=displayName+eq+%22Engineering%22`,
		},
		{
			name: "get group",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.GetGroup(ctx, "group/id")
				return err
			},
			method: http.MethodGet,
			path:   "scim/v2/enterprises/acme/Groups/group%2Fid",
		},
		{
			name: "create group",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.CreateGroup(ctx, group)
				return err
			},
			method:   http.MethodPost,
			path:     "scim/v2/enterprises/acme/Groups",
			bodyJSON: `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"displayName":"Engineering"}`,
		},
		{
			name: "replace group",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.ReplaceGroup(ctx, "group-id", group)
				return err
			},
			method:   http.MethodPut,
			path:     "scim/v2/enterprises/acme/Groups/group-id",
			bodyJSON: `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"displayName":"Engineering"}`,
		},
		{
			name: "patch group",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.PatchGroup(ctx, "group-id", scim.PatchOperation{
					Op: "replace", Path: "displayName", Value: "Employees",
				})
				return err
			},
			method:   http.MethodPatch,
			path:     "scim/v2/enterprises/acme/Groups/group-id",
			bodyJSON: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"displayName","value":"Employees"}]}`,
		},
		{
			name: "add group members",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.AddGroupMembers(ctx, "group-id", "user-1", "user-2")
				return err
			},
			method:   http.MethodPatch,
			path:     "scim/v2/enterprises/acme/Groups/group-id",
			bodyJSON: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"add","path":"members","value":[{"value":"user-1"},{"value":"user-2"}]}]}`,
		},
		{
			name: "remove group members",
			run: func(ctx context.Context, client *scim.Client) error {
				_, err := client.RemoveGroupMembers(ctx, "group-id", "user-1")
				return err
			},
			method:   http.MethodPatch,
			path:     "scim/v2/enterprises/acme/Groups/group-id",
			bodyJSON: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"remove","path":"members","value":[{"value":"user-1"}]}]}`,
		},
		{
			name: "delete group",
			run: func(ctx context.Context, client *scim.Client) error {
				return client.DeleteGroup(ctx, "group-id")
			},
			method: http.MethodDelete,
			path:   "scim/v2/enterprises/acme/Groups/group-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doer := &recordingDoer{response: []byte(`{}`)}
			client, err := scim.NewClient("acme", scim.WithDoer(doer))
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}

			if err := tt.run(context.Background(), client); err != nil {
				t.Fatalf("operation error = %v", err)
			}

			call := doer.lastCall(t)
			if call.method != tt.method {
				t.Errorf("method = %q, want %q", call.method, tt.method)
			}
			if call.path != tt.path {
				t.Errorf("path = %q, want %q", call.path, tt.path)
			}
			assertJSONEqual(t, call.body, tt.bodyJSON)
		})
	}
}

func TestNewClientValidation(t *testing.T) {
	doer := &recordingDoer{}
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("not called")
	})
	var nilOption scim.Option

	tests := []struct {
		name    string
		company string
		options []scim.Option
		want    string
	}{
		{name: "empty enterprise", want: "enterprise slug is required"},
		{name: "nil option", company: "acme", options: []scim.Option{nilOption}, want: "option 0 is nil"},
		{name: "empty host", company: "acme", options: []scim.Option{scim.WithHost("")}, want: "host is required"},
		{name: "empty token", company: "acme", options: []scim.Option{scim.WithToken("")}, want: "token is required"},
		{name: "zero timeout", company: "acme", options: []scim.Option{scim.WithTimeout(0)}, want: "timeout must be greater than zero"},
		{name: "nil transport", company: "acme", options: []scim.Option{scim.WithTransport(nil)}, want: "transport is required"},
		{name: "nil doer", company: "acme", options: []scim.Option{scim.WithDoer(nil)}, want: "doer is required"},
		{name: "unsupported GHES host", company: "acme", options: []scim.Option{scim.WithHost("github.example.com")}, want: "not GitHub Enterprise Server"},
		{name: "doer and host", company: "acme", options: []scim.Option{scim.WithDoer(doer), scim.WithHost("github.com")}, want: "WithDoer cannot be combined"},
		{name: "doer and token", company: "acme", options: []scim.Option{scim.WithDoer(doer), scim.WithToken("token")}, want: "WithDoer cannot be combined"},
		{name: "doer and timeout", company: "acme", options: []scim.Option{scim.WithDoer(doer), scim.WithTimeout(time.Second)}, want: "WithDoer cannot be combined"},
		{name: "doer and transport", company: "acme", options: []scim.Option{scim.WithDoer(doer), scim.WithTransport(transport)}, want: "WithDoer cannot be combined"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scim.NewClient(tt.company, tt.options...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewClient() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestDefaultDoerRoutesSupportedHosts(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		wantHost string
	}{
		{name: "GitHub.com", host: "github.com", wantHost: "api.github.com"},
		{name: "GHE.com tenant", host: "acme.ghe.com", wantHost: "api.acme.ghe.com"},
		{name: "GHE.com API host", host: "api.acme.ghe.com", wantHost: "api.acme.ghe.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotRequest *http.Request
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				gotRequest = req.Clone(req.Context())
				return jsonResponse(req, http.StatusOK, `{}`), nil
			})

			client, err := scim.NewClient(
				"acme",
				scim.WithHost(tt.host),
				scim.WithToken("secret-token"),
				scim.WithTransport(transport),
				scim.WithTimeout(time.Second),
			)
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if _, err := client.GetUser(context.Background(), "user-id"); err != nil {
				t.Fatalf("GetUser() error = %v", err)
			}

			if gotRequest == nil {
				t.Fatal("transport did not receive a request")
			}
			if gotRequest.URL.Scheme != "https" || gotRequest.URL.Host != tt.wantHost {
				t.Errorf("request URL = %s, want https://%s/...", gotRequest.URL, tt.wantHost)
			}
			if got := gotRequest.URL.Path; got != "/scim/v2/enterprises/acme/Users/user-id" {
				t.Errorf("request path = %q", got)
			}
			if got := gotRequest.Header.Get("Authorization"); got != "token secret-token" {
				t.Errorf("Authorization = %q", got)
			}
			if got := gotRequest.Header.Get("Accept"); got != "application/vnd.github+json" {
				t.Errorf("Accept = %q", got)
			}
		})
	}
}

func TestAPIError(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, http.StatusNotFound, `{"message":"user not found"}`), nil
	})
	client, err := scim.NewClient(
		"acme",
		scim.WithHost("github.com"),
		scim.WithToken("secret-token"),
		scim.WithTransport(transport),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetUser(context.Background(), "missing")
	if err == nil {
		t.Fatal("GetUser() error = nil")
	}

	var apiErr *scim.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %T does not contain *scim.APIError: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if apiErr.Message != "user not found" {
		t.Errorf("Message = %q", apiErr.Message)
	}
	if apiErr.RequestURL != "https://api.github.com/scim/v2/enterprises/acme/Users/missing" {
		t.Errorf("RequestURL = %q", apiErr.RequestURL)
	}
	if !scim.IsStatus(err, http.StatusNotFound) {
		t.Error("IsStatus(error, 404) = false")
	}
	if scim.IsStatus(err, http.StatusConflict) {
		t.Error("IsStatus(error, 409) = true")
	}
}

func TestResponseDecoding(t *testing.T) {
	doer := &recordingDoer{response: []byte(`{
		"id": "user-id",
		"userName": "octocat",
		"active": true
	}`)}
	client, err := scim.NewClient("acme", scim.WithDoer(doer))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	user, err := client.GetUser(context.Background(), "user-id")
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if user.ID != "user-id" || user.UserName != "octocat" || user.Active == nil || !*user.Active {
		t.Errorf("GetUser() = %#v", user)
	}
}

func TestDoerErrorsAndCancellationArePreserved(t *testing.T) {
	t.Run("doer error", func(t *testing.T) {
		sentinel := errors.New("transport failed")
		client, err := scim.NewClient("acme", scim.WithDoer(&recordingDoer{err: sentinel}))
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}

		_, err = client.GetUser(context.Background(), "user-id")
		if !errors.Is(err, sentinel) {
			t.Fatalf("GetUser() error = %v, want wrapping sentinel", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		client, err := scim.NewClient("acme", scim.WithDoer(&recordingDoer{}))
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = client.GetUser(ctx, "user-id")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("GetUser() error = %v, want context.Canceled", err)
		}
	})
}

func TestDefaultDoerTimeout(t *testing.T) {
	started := make(chan struct{})
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	client, err := scim.NewClient(
		"acme",
		scim.WithHost("github.com"),
		scim.WithToken("secret-token"),
		scim.WithTransport(transport),
		scim.WithTimeout(10*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.GetUser(context.Background(), "user-id")
	<-started
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetUser() error = %v, want context deadline exceeded", err)
	}
}

func TestClientConcurrentUse(t *testing.T) {
	doer := &recordingDoer{response: []byte(`{}`)}
	client, err := scim.NewClient("acme", scim.WithDoer(doer))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	const calls = 32
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.GetUser(context.Background(), "user-id")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("GetUser() error = %v", err)
		}
	}

	doer.mu.Lock()
	defer doer.mu.Unlock()
	if len(doer.calls) != calls {
		t.Errorf("recorded %d calls, want %d", len(doer.calls), calls)
	}
}

func assertJSONEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	if want == "" {
		if len(got) != 0 {
			t.Errorf("body = %s, want empty", got)
		}
		return
	}

	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("body is invalid JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("test expectation is invalid JSON: %v\n%s", err, want)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func jsonResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
