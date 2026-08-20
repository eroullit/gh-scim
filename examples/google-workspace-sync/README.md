# Google Workspace scheduled sync example

This proof of concept reads users and allowlisted groups from the Google
Workspace Admin SDK and reconciles them through `gh scim`. It follows the
architecture and safety controls in the
[`gh-scim` customer guide](https://github.com/eroullit/gh-scim/blob/32e793a088b6db584c301c2443f568f34e824a29/docs/gh-scim-customer-guide.md).

It is an example, not a supported Google Workspace connector. It defaults to
report-only mode and never hard-deletes users or groups.

## Code layout

| File | Responsibility |
| --- | --- |
| `main.go` | Reconciliation orchestration and phase ordering |
| `config.go` | Flags, environment variables, and safety validation |
| `google_auth.go` | Keyless domain-wide delegation and token exchange |
| `google_directory.go` | Google user-directory adapter |
| `google_groups.go` | Google group and membership adapter |
| `role_mapping.go` | Custom-attribute and role-group policy |
| `user_reconcile.go` | User desired state, diffing, and lifecycle plan |
| `group_reconcile.go` | Group desired state, membership diffing, and limits |
| `scim_adapter.go` | `gh-scim` command execution and JSON transport |

## Google configuration

1. Enable the
   [Admin SDK API](https://console.cloud.google.com/apis/library/admin.googleapis.com)
   in a Google Cloud project.
2. Create a service account and enable
   [domain-wide delegation](https://knowledge.workspace.google.com/admin/apps/control-api-access-with-domain-wide-delegation).
3. In Google Admin, authorize the service account client ID for only:

   ```text
   https://www.googleapis.com/auth/admin.directory.user.readonly,
   https://www.googleapis.com/auth/admin.directory.group.readonly
   ```

4. Enable the
   [IAM Service Account Credentials API](https://console.cloud.google.com/apis/library/iamcredentials.googleapis.com).
5. Grant the identity running the example **Service Account Token Creator**
   on the service account. This permits keyless `signJwt` calls.
6. Choose a Google Workspace administrator for the service account to
   impersonate.

The script uses Google's immutable directory user ID as SCIM `externalId`.
Google primary email is used as SCIM `userName`, so it matches the SAML
`NameID`. If an existing SCIM user has the same email but a different
`externalId`, the run stops. Migrate that mapping only after verifying the
identity. The script never adopts an account by email because addresses can be
reassigned.

Dedicated Google groups are the recommended role source. Map them explicitly
with repeatable flags such as:

```sh
--role-group enterprise_owner=github-owners@example.com
--role-group billing_manager=github-billing@example.com
--role-group guest_collaborator=github-guests@example.com
```

Users outside these groups default to `user`. A user in more than one different
elevated-role group stops the run. Membership in multiple ordinary access
groups is supported.

As an alternative, create a single-valued Google custom user attribute such as
`GitHub.role`, then pass `--role-attribute GitHub.role`. It may contain `user`,
`enterprise_owner`, `billing_manager`, or `guest_collaborator`. Missing values
default to `user`; unsupported values stop the run. If both mechanisms are
configured, conflicting assignments stop the run.

## Run a report-only reconciliation

Install `gh-scim`, then provide the setup user's `scim:enterprise` token
through `GH_TOKEN`. For a local keyless test, authenticate with
[Application Default Credentials](https://cloud.google.com/docs/authentication/provide-credentials-adc):

```sh
gcloud auth application-default login

export GOOGLE_SERVICE_ACCOUNT_EMAIL=googleadmin-sa@your-project.iam.gserviceaccount.com
export GOOGLE_ADMIN_SUBJECT=admin@example.com
export GH_TOKEN="$(your-secret-manager read github-scim-token)"
export GH_SCIM_ENTERPRISE=your-enterprise

cd examples/google-workspace-sync
go run .
```

The caller's short-lived Application Default Credential calls IAM
`signJwt`. The service account's Google-managed key signs the delegated JWT,
so no service-account JSON key is downloaded. For GitHub Actions, use
[Workload Identity Federation](https://github.com/google-github-actions/auth#workload-identity-federation)
to provide ADC instead of `gcloud`.

JSON-key authentication remains available for environments that explicitly
permit it by setting `GOOGLE_SERVICE_ACCOUNT_KEY_FILE` or
`--service-account-file`, but Google recommends keyless authentication for
domain-wide delegation. In keyless environments such as GitHub Actions,
`GOOGLE_APPLICATION_CREDENTIALS` can point to an external-account ADC file.

Use `--google-query` to pilot a subset supported by the
[Directory API query syntax](https://developers.google.com/workspace/admin/directory/v1/guides/search-users).
The selected user population must contain every member of each `--group` and
`--role-group`; otherwise the run stops before writing.

Review the plan, then explicitly enable writes:

```sh
go run . \
  --google-query "orgUnitPath='/github-scim-pilot'" \
  --role-group enterprise_owner=github-owners@example.com \
  --group engineering@example.com \
  --group security@example.com \
  --max-changes 10 \
  --apply
```

Only groups passed with repeatable `--group` flags are synchronized. The
example uses each Google group's immutable ID and replaces the complete
membership of that SCIM group. Nested Google groups are rejected. If user
changes are pending, group reconciliation is deferred until users converge.
After provisioning, connect each SCIM group to the intended GitHub team;
provisioning an external group alone does not grant repository access.
Group reconciliation is also deferred in apply mode whenever that run changes
users, ensuring membership replacements are reviewed and applied only on a
subsequent converged run. With a scheduled job, expect group changes to lag
new user provisioning by one schedule interval.

`--max-group-member-delta` limits the total additions and removals permitted
for any one group (default `20`). This protects against an unexpectedly empty
or substantially changed Google group even when the group operation itself
fits within `--max-changes`.

`--deprovision-missing` soft-deprovisions `google:`-managed SCIM users that are
absent from the complete Google result. It cannot be combined with
`--google-query`, because every user outside a scoped query would otherwise
appear missing. Keep it disabled until the assignment boundary has been
validated.

For local development without an installed extension, build this repository
and point the example to the binary:

```sh
go build -o /tmp/gh-scim .
cd examples/google-workspace-sync
GH_SCIM_BINARY=/tmp/gh-scim go run .
```

## Schedule

Run report-only first. A Linux cron entry can serialize runs with `flock`:

```cron
15 */6 * * * cd /opt/gh-scim && flock -n /tmp/gh-scim-google.lock ./run-google-sync.sh
```

The wrapper should establish ADC, retrieve the GitHub token from an approved
secret manager, export the environment variables above, and execute:

```sh
cd /opt/gh-scim/examples/google-workspace-sync
go run . --max-changes 20 --apply
```

Do not place either credential directly in the crontab, command arguments,
repository, or logs.
