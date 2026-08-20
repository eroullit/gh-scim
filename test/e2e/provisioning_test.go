//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eroullit/gh-scim/scim"
)

const (
	commandTimeout  = 90 * time.Second
	ownershipPrefix = "gh-scim-e2e-"
	userExternalID  = ownershipPrefix + "user"
	groupExternalID = ownershipPrefix + "group"
)

var safeName = regexp.MustCompile(`[^a-z0-9-]+`)

type liveConfig struct {
	binary     string
	enterprise string
	hostname   string
	token      string
	email      string
	prefix     string
}

type commandOutput struct {
	stdout []byte
	stderr []byte
}

func TestCompactUserName(t *testing.T) {
	first := compactUserName("gh-scim-e2e-1234567890-1")
	second := compactUserName("gh-scim-e2e-1234567890-2")

	if len(first) > 39 {
		t.Fatalf("compact username is %d characters, maximum is 39", len(first))
	}
	if first == second {
		t.Fatalf("different run prefixes generated the same username %q", first)
	}
	if !regexp.MustCompile(`^e2e-[a-f0-9]{12}$`).MatchString(first) {
		t.Fatalf("compact username %q does not match the expected format", first)
	}
}

func TestOwnershipChecks(t *testing.T) {
	ownedGroup := scim.Group{
		ID:          "group-id",
		ExternalID:  groupExternalID,
		DisplayName: ownershipPrefix + "123-group",
	}
	if !isOwnedGroup(ownedGroup) {
		t.Fatal("expected group with stable external ID and ownership prefix to be owned")
	}
	ownedGroup.ExternalID = "unrelated"
	if isOwnedGroup(ownedGroup) {
		t.Fatal("group with unrelated external ID must not be owned")
	}

	ownedUser := scim.User{
		ID:          "user-id",
		ExternalID:  userExternalID,
		DisplayName: ownershipPrefix + "123-user",
	}
	if !isOwnedUser(ownedUser) {
		t.Fatal("expected user with stable external ID and ownership prefix to be owned")
	}
	ownedUser.DisplayName = "unrelated"
	if isOwnedUser(ownedUser) {
		t.Fatal("user without ownership prefix must not be owned")
	}
}

func TestProvisioningLifecycle(t *testing.T) {
	cfg := loadLiveConfig(t)
	cfg.binary = extensionExecutable(t)

	userName := compactUserName(cfg.prefix)
	if len(userName) > 39 {
		t.Fatalf("generated username is %d characters, maximum is 39", len(userName))
	}
	userDisplayName := cfg.prefix + "-user"
	groupName := cfg.prefix + "-group"

	cleanupStaleResources(t, cfg)

	var userID, groupID string
	t.Cleanup(func() {
		if groupID != "" {
			if _, err := runCommand(t, cfg, "groups", "delete", groupID, "--confirm"); err != nil {
				t.Errorf("cleanup group %s: %v", groupID, err)
			}
		}
		if userID != "" {
			if _, err := runCommand(t, cfg, "users", "delete", userID, "--confirm"); err != nil {
				t.Errorf("cleanup user %s: %v", userID, err)
			}
		}
	})

	createdUser := runJSON[scim.User](t, cfg,
		"users", "create",
		"--external-id", userExternalID,
		"--username", userName,
		"--given-name", "SCIM",
		"--family-name", "Test",
		"--display-name", userDisplayName,
		"--email", cfg.email,
		"--role", "user",
	)
	userID = requireID(t, "created user", createdUser.ID)
	requireUser(t, createdUser, userID, userExternalID, cfg.email)

	gotUser := runJSON[scim.User](t, cfg, "users", "get", userID)
	requireUser(t, gotUser, userID, userExternalID, cfg.email)

	listedUsers := runJSON[scim.ListResponse[scim.User]](
		t, cfg, "users", "list", "--filter", fmt.Sprintf(`userName eq %q`, createdUser.UserName),
	)
	requireResource(t, "user list", listedUsers.Resources, func(user scim.User) bool {
		return user.ID == userID
	})

	replacedUser := runJSON[scim.User](t, cfg,
		"users", "replace", userID,
		"--external-id", userExternalID,
		"--username", userName,
		"--given-name", "Provisioning",
		"--family-name", "Test",
		"--display-name", userDisplayName+"-replaced",
		"--email", cfg.email,
		"--role", "user",
	)
	if replacedUser.DisplayName != userDisplayName+"-replaced" {
		t.Fatalf("replace user displayName = %q, want %q", replacedUser.DisplayName, userDisplayName+"-replaced")
	}

	patchedUser := runJSON[scim.User](t, cfg,
		"users", "patch", userID, "--path", "displayName", "--value", userDisplayName+"-patched",
	)
	if patchedUser.DisplayName != userDisplayName+"-patched" {
		t.Fatalf("patch user displayName = %q, want %q", patchedUser.DisplayName, userDisplayName+"-patched")
	}

	deprovisionedUser := runJSON[scim.User](t, cfg, "users", "deprovision", userID)
	requireActive(t, deprovisionedUser, false)

	reactivatedUser := runJSON[scim.User](t, cfg, "users", "reactivate", userID)
	requireActive(t, reactivatedUser, true)

	createdGroup := runJSON[scim.Group](t, cfg,
		"groups", "create",
		"--external-id", groupExternalID,
		"--display-name", groupName,
		"--member", userID,
	)
	groupID = requireID(t, "created group", createdGroup.ID)
	requireMember(t, createdGroup, userID, true)

	gotGroup := runJSON[scim.Group](t, cfg, "groups", "get", groupID)
	requireMember(t, gotGroup, userID, true)

	listedGroups := runJSON[scim.ListResponse[scim.Group]](
		t, cfg, "groups", "list", "--filter", fmt.Sprintf(`displayName eq %q`, groupName),
	)
	requireResource(t, "group list", listedGroups.Resources, func(group scim.Group) bool {
		return group.ID == groupID
	})

	replacedGroupName := groupName + "-replaced"
	replacedGroup := runJSON[scim.Group](t, cfg,
		"groups", "replace", groupID,
		"--external-id", groupExternalID,
		"--display-name", replacedGroupName,
		"--member", userID,
	)
	if replacedGroup.DisplayName != replacedGroupName {
		t.Fatalf("replace group displayName = %q, want %q", replacedGroup.DisplayName, replacedGroupName)
	}
	requireMember(t, replacedGroup, userID, true)

	patchedGroupName := groupName + "-patched"
	patchedGroup := runJSON[scim.Group](t, cfg,
		"groups", "patch", groupID, "--path", "displayName", "--value", patchedGroupName,
	)
	if patchedGroup.DisplayName != patchedGroupName {
		t.Fatalf("patch group displayName = %q, want %q", patchedGroup.DisplayName, patchedGroupName)
	}

	runJSON[scim.Group](t, cfg, "groups", "remove-members", groupID, userID)
	withoutMember := runJSON[scim.Group](t, cfg, "groups", "get", groupID)
	requireMember(t, withoutMember, userID, false)

	runJSON[scim.Group](t, cfg, "groups", "add-members", groupID, userID)
	withMember := runJSON[scim.Group](t, cfg, "groups", "get", groupID)
	requireMember(t, withMember, userID, true)

	if _, err := runCommand(t, cfg, "groups", "delete", groupID, "--confirm"); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	groupID = ""

	if _, err := runCommand(t, cfg, "users", "delete", userID, "--confirm"); err != nil {
		t.Fatalf("hard-delete user: %v", err)
	}
	userID = ""
}

func loadLiveConfig(t *testing.T) liveConfig {
	t.Helper()

	token := os.Getenv("SCIM_TOKEN")
	enterprise := os.Getenv("SCIM_ENTERPRISE")
	domain := strings.TrimPrefix(strings.TrimSpace(os.Getenv("SCIM_TEST_EMAIL_DOMAIN")), "@")
	if token == "" || enterprise == "" || domain == "" {
		t.Skip("live SCIM test requires SCIM_TOKEN, SCIM_ENTERPRISE, and SCIM_TEST_EMAIL_DOMAIN")
	}
	if strings.Contains(domain, "@") {
		t.Fatalf("SCIM_TEST_EMAIL_DOMAIN must be a domain without @, got %q", domain)
	}

	runID := firstNonempty(os.Getenv("GITHUB_RUN_ID"), fmt.Sprintf("%d", time.Now().UnixNano()))
	attempt := firstNonempty(os.Getenv("GITHUB_RUN_ATTEMPT"), "1")
	suffix := strings.Trim(safeName.ReplaceAllString(strings.ToLower(runID+"-"+attempt), "-"), "-")
	if suffix == "" {
		t.Fatal("could not derive a safe test run suffix")
	}

	prefix := ownershipPrefix + suffix
	return liveConfig{
		enterprise: enterprise,
		hostname:   os.Getenv("SCIM_HOSTNAME"),
		token:      token,
		email:      prefix + "@" + domain,
		prefix:     prefix,
	}
}

func extensionExecutable(t *testing.T) string {
	t.Helper()

	if configured := strings.TrimSpace(os.Getenv("SCIM_BINARY")); configured != "" {
		binary, err := filepath.Abs(configured)
		if err != nil {
			t.Fatalf("resolve SCIM_BINARY: %v", err)
		}
		info, err := os.Stat(binary)
		if err != nil {
			t.Fatalf("inspect SCIM_BINARY: %v", err)
		}
		if info.IsDir() {
			t.Fatalf("SCIM_BINARY points to a directory: %s", binary)
		}
		return binary
	}

	root := repositoryRoot(t)
	binary := filepath.Join(t.TempDir(), "gh-scim")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build extension: %v\n%s", err, output)
	}
	return binary
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}

func (cfg liveConfig) run(args ...string) (commandOutput, error) {
	rootArgs := []string{"--enterprise", cfg.enterprise}
	if cfg.hostname != "" {
		rootArgs = append(rootArgs, "--hostname", cfg.hostname)
	}
	rootArgs = append(rootArgs, args...)

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cfg.binary, rootArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = envWith(os.Environ(), map[string]string{
		"GH_DEBUG": "api",
		"GH_TOKEN": cfg.token,
	})
	err := cmd.Run()
	output := commandOutput{
		stdout: stdout.Bytes(),
		stderr: stderr.Bytes(),
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output, fmt.Errorf("command timed out after %s: %s", commandTimeout, strings.Join(args, " "))
	}
	if err != nil {
		return output, fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

func runCommand(t *testing.T, cfg liveConfig, args ...string) ([]byte, error) {
	t.Helper()
	t.Logf("gh-scim call: %s", formatCommand(args))

	output, err := cfg.run(args...)
	if len(output.stderr) > 0 {
		t.Logf("gh-scim HTTP trace:\n%s", strings.TrimSpace(string(output.stderr)))
	}
	if len(output.stdout) > 0 {
		t.Logf("gh-scim stdout:\n%s", strings.TrimSpace(string(output.stdout)))
	} else {
		t.Log("gh-scim stdout: <empty response>")
	}
	return output.stdout, err
}

func runJSON[T any](t *testing.T, cfg liveConfig, args ...string) T {
	t.Helper()

	output, err := runCommand(t, cfg, args...)
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal(output, &value); err != nil {
		t.Fatalf("%s returned invalid JSON: %v\n%s", strings.Join(args, " "), err, output)
	}
	return value
}

func cleanupStaleResources(t *testing.T, cfg liveConfig) {
	t.Helper()

	groups := runJSON[scim.ListResponse[scim.Group]](
		t, cfg, "groups", "list", "--filter", fmt.Sprintf(`externalId eq %q`, groupExternalID),
	)
	for _, group := range groups.Resources {
		if !isOwnedGroup(group) {
			continue
		}
		if _, err := runCommand(t, cfg, "groups", "delete", group.ID, "--confirm"); err != nil {
			t.Fatalf("delete stale owned group %s: %v", group.ID, err)
		}
	}

	users := runJSON[scim.ListResponse[scim.User]](
		t, cfg, "users", "list", "--filter", fmt.Sprintf(`externalId eq %q`, userExternalID),
	)
	for _, user := range users.Resources {
		if !isOwnedUser(user) {
			continue
		}
		if _, err := runCommand(t, cfg, "users", "delete", user.ID, "--confirm"); err != nil {
			t.Fatalf("delete stale owned user %s: %v", user.ID, err)
		}
	}
}

func isOwnedGroup(group scim.Group) bool {
	return group.ID != "" &&
		group.ExternalID == groupExternalID &&
		strings.HasPrefix(group.DisplayName, ownershipPrefix)
}

func isOwnedUser(user scim.User) bool {
	return user.ID != "" &&
		user.ExternalID == userExternalID &&
		strings.HasPrefix(user.DisplayName, ownershipPrefix)
}

func requireUser(t *testing.T, user scim.User, id, externalID, email string) {
	t.Helper()
	if user.ID != id {
		t.Fatalf("user id = %q, want %q", user.ID, id)
	}
	if user.ExternalID != externalID {
		t.Fatalf("user externalId = %q, want %q", user.ExternalID, externalID)
	}
	if !hasEmail(user, email) {
		t.Fatalf("user does not contain expected email %q: %#v", email, user.Emails)
	}
}

func requireActive(t *testing.T, user scim.User, want bool) {
	t.Helper()
	if user.Active == nil || *user.Active != want {
		t.Fatalf("user active = %v, want %t", user.Active, want)
	}
}

func requireMember(t *testing.T, group scim.Group, userID string, want bool) {
	t.Helper()
	found := false
	for _, member := range group.Members {
		if member.Value == userID {
			found = true
			break
		}
	}
	if found != want {
		t.Fatalf("group membership for user %q = %t, want %t; members: %#v", userID, found, want, group.Members)
	}
}

func requireResource[T any](t *testing.T, operation string, resources []T, matches func(T) bool) {
	t.Helper()
	for _, resource := range resources {
		if matches(resource) {
			return
		}
	}
	t.Fatalf("%s did not contain the expected resource", operation)
}

func requireID(t *testing.T, resource, id string) string {
	t.Helper()
	if id == "" {
		t.Fatalf("%s has an empty id", resource)
	}
	return id
}

func hasEmail(user scim.User, expected string) bool {
	for _, email := range user.Emails {
		if strings.EqualFold(email.Value, expected) {
			return true
		}
	}
	return false
}

func envWith(base []string, values map[string]string) []string {
	result := make([]string, 0, len(base)+len(values))
	for _, item := range base {
		name, _, _ := strings.Cut(item, "=")
		if _, replaced := values[name]; !replaced {
			result = append(result, item)
		}
	}
	for name, value := range values {
		result = append(result, name+"="+value)
	}
	return result
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func formatCommand(args []string) string {
	formatted := make([]string, 0, len(args)+1)
	formatted = append(formatted, "gh-scim")
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'") {
			arg = strconv.Quote(arg)
		}
		formatted = append(formatted, arg)
	}
	return strings.Join(formatted, " ")
}

func compactUserName(prefix string) string {
	sum := sha256.Sum256([]byte(prefix))
	return fmt.Sprintf("e2e-%x", sum[:6])
}
