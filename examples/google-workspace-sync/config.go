package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	directoryBaseURL    = "https://admin.googleapis.com/admin/directory/v1"
	directoryUserScope  = "https://www.googleapis.com/auth/admin.directory.user.readonly"
	directoryGroupScope = "https://www.googleapis.com/auth/admin.directory.group.readonly"
	cloudPlatformScope  = "https://www.googleapis.com/auth/cloud-platform"
	iamCredentialsURL   = "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/"
	googleTokenURL      = "https://oauth2.googleapis.com/token"
	googleExternalID    = "google:"
)

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("value cannot be empty")
	}
	*values = append(*values, value)
	return nil
}

type config struct {
	serviceAccountFile  string
	serviceAccountEmail string
	adminSubject        string
	customerID          string
	query               string
	roleAttribute       string
	roleGroups          stringList
	groups              stringList
	enterprise          string
	hostname            string
	scimBinary          string
	apply               bool
	deprovisionMissing  bool
	maxChanges          int
	maxGroupMemberDelta int
	timeout             time.Duration
}

func parseConfig() config {
	var cfg config
	flag.StringVar(&cfg.serviceAccountFile, "service-account-file", os.Getenv("GOOGLE_SERVICE_ACCOUNT_KEY_FILE"), "Optional Google service account JSON key file")
	flag.StringVar(&cfg.serviceAccountEmail, "service-account-email", os.Getenv("GOOGLE_SERVICE_ACCOUNT_EMAIL"), "Google service account email for keyless signing")
	flag.StringVar(&cfg.adminSubject, "admin-subject", os.Getenv("GOOGLE_ADMIN_SUBJECT"), "Google Workspace admin to impersonate")
	flag.StringVar(&cfg.customerID, "customer", envOrDefault("GOOGLE_CUSTOMER_ID", "my_customer"), "Google Workspace customer ID")
	flag.StringVar(&cfg.query, "google-query", "", "Optional Admin SDK users.list query")
	flag.StringVar(&cfg.roleAttribute, "role-attribute", "", "Optional Google custom attribute mapped to the GitHub role")
	flag.Var(&cfg.roleGroups, "role-group", "GitHub role=Google group email or ID mapping (repeatable)")
	flag.Var(&cfg.groups, "group", "Google group email or ID to synchronize (repeatable)")
	flag.StringVar(&cfg.enterprise, "enterprise", os.Getenv("GH_SCIM_ENTERPRISE"), "GitHub enterprise slug")
	flag.StringVar(&cfg.hostname, "hostname", os.Getenv("GH_HOST"), "Optional GitHub API hostname")
	flag.StringVar(&cfg.scimBinary, "scim-binary", os.Getenv("GH_SCIM_BINARY"), "Optional gh-scim binary; defaults to 'gh scim'")
	flag.BoolVar(&cfg.apply, "apply", false, "Apply the proposed SCIM writes")
	flag.BoolVar(&cfg.deprovisionMissing, "deprovision-missing", false, "Soft-deprovision managed users absent from Google")
	flag.IntVar(&cfg.maxChanges, "max-changes", 20, "Abort when proposed writes exceed this number")
	flag.IntVar(&cfg.maxGroupMemberDelta, "max-group-member-delta", 20, "Abort when a group would add or remove more members")
	flag.DurationVar(&cfg.timeout, "timeout", 2*time.Minute, "Overall reconciliation timeout")
	flag.Parse()
	return cfg
}

func validateConfig(cfg config) error {
	var missing []string
	for name, value := range map[string]string{
		"--admin-subject": cfg.adminSubject,
		"--enterprise":    cfg.enterprise,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}
	if cfg.serviceAccountFile == "" && cfg.serviceAccountEmail == "" {
		return errors.New("set --service-account-email for keyless signing or --service-account-file for JSON-key authentication")
	}
	if cfg.serviceAccountFile != "" && cfg.serviceAccountEmail != "" {
		return errors.New("--service-account-email and --service-account-file are mutually exclusive")
	}
	if cfg.maxChanges < 1 {
		return errors.New("--max-changes must be at least 1")
	}
	if cfg.maxGroupMemberDelta < 0 {
		return errors.New("--max-group-member-delta cannot be negative")
	}
	if cfg.deprovisionMissing && cfg.query != "" {
		return errors.New("--deprovision-missing cannot be combined with --google-query because users outside the query would appear missing")
	}
	if cfg.roleAttribute != "" {
		if _, _, err := splitRoleAttribute(cfg.roleAttribute); err != nil {
			return err
		}
	}
	for _, mapping := range cfg.roleGroups {
		if _, err := parseRoleGroup(mapping); err != nil {
			return err
		}
	}
	seenGroups := make(map[string]bool, len(cfg.groups))
	for _, group := range cfg.groups {
		key := strings.ToLower(group)
		if seenGroups[key] {
			return fmt.Errorf("duplicate --group %q", group)
		}
		seenGroups[key] = true
	}
	return nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
