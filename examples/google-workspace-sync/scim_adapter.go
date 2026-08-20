package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type scimCommand struct {
	binary     string
	prefixArgs []string
	enterprise string
	hostname   string
}

func newSCIMCommand(cfg config) scimCommand {
	command := scimCommand{
		binary:     cfg.scimBinary,
		enterprise: cfg.enterprise,
		hostname:   cfg.hostname,
	}
	if command.binary == "" {
		command.binary = "gh"
		command.prefixArgs = []string{"scim"}
	}
	return command
}

func (c scimCommand) baseArgs() []string {
	args := append([]string{}, c.prefixArgs...)
	args = append(args, "--enterprise", c.enterprise)
	if c.hostname != "" {
		args = append(args, "--hostname", c.hostname)
	}
	return args
}

func (c scimCommand) listUsers(ctx context.Context) ([]scimUser, error) {
	const pageSize = 100
	var users []scimUser
	for start := 1; ; start += pageSize {
		args := append(c.baseArgs(), "users", "list", "--start-index", strconv.Itoa(start), "--count", strconv.Itoa(pageSize))
		var page scimListResponse[scimUser]
		if err := c.runJSON(ctx, args, &page); err != nil {
			return nil, err
		}
		users = append(users, page.Resources...)
		if len(users) >= page.TotalResults || len(page.Resources) == 0 {
			return users, nil
		}
	}
}

func (c scimCommand) apply(ctx context.Context, op operation) error {
	var args []string
	switch op.Kind {
	case createUser:
		args = append(c.baseArgs(), "users", "create")
		args = append(args, userFlags(op.Desired, op.Desired.Roles)...)
	case replaceUser:
		args = append(c.baseArgs(), "users", "replace", op.Current.ID)
		args = append(args, userFlags(op.Desired, op.Desired.Roles)...)
	case reactivateUser:
		args = append(c.baseArgs(), "users", "reactivate", op.Current.ID)
	case deprovisionUser:
		args = append(c.baseArgs(), "users", "deprovision", op.Current.ID)
	default:
		return fmt.Errorf("unsupported operation %q", op.Kind)
	}

	var response json.RawMessage
	return c.runJSON(ctx, args, &response)
}

func (c scimCommand) findGroup(ctx context.Context, externalID string) (*scimGroup, error) {
	filter := fmt.Sprintf(`externalId eq "%s"`, externalID)
	args := append(c.baseArgs(), "groups", "list", "--filter", filter, "--count", "2")
	var page scimListResponse[scimGroup]
	if err := c.runJSON(ctx, args, &page); err != nil {
		return nil, err
	}
	switch len(page.Resources) {
	case 0:
		return nil, nil
	case 1:
		return &page.Resources[0], nil
	default:
		return nil, fmt.Errorf("multiple SCIM groups use externalId %q", externalID)
	}
}

func (c scimCommand) applyGroup(ctx context.Context, op groupOperation) error {
	args := c.baseArgs()
	if op.Create {
		args = append(args, "groups", "create")
	} else {
		args = append(args, "groups", "replace", op.Current.ID)
	}
	args = append(args,
		"--external-id", op.Desired.ExternalID,
		"--display-name", op.Desired.DisplayName,
	)
	for _, memberID := range op.Desired.MemberIDs {
		args = append(args, "--member", memberID)
	}
	var response json.RawMessage
	return c.runJSON(ctx, args, &response)
}

func (c scimCommand) runJSON(ctx context.Context, args []string, out any) error {
	cmd := exec.CommandContext(ctx, c.binary, args...)
	cmd.Env = append(os.Environ(),
		"GH_NO_UPDATE_NOTIFIER=1",
		"GH_PROMPT_DISABLED=1",
		"NO_COLOR=1",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(string(output))
		}
		return fmt.Errorf("running %s: %w: %s", strings.Join(append([]string{c.binary}, args...), " "), err, message)
	}
	if err := json.Unmarshal(output, out); err != nil {
		return fmt.Errorf("decoding gh-scim response: %w; stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
