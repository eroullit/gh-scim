# gh-scim

A [`gh`](https://cli.github.com) CLI extension to (de)provision users and
groups on a GitHub Enterprise Managed Users (EMU) account using the
[SCIM REST API](https://docs.github.com/en/enterprise-cloud@latest/rest/enterprise-admin/scim),
for admins who are not using a
[paved-path identity provider](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/configuring-scim-provisioning-for-users#configuring-provisioning-for-other-identity-management-systems).

## Install

```sh
gh extension install eroullit/gh-scim
```

## Authentication

Requests are authenticated using `gh`'s normal token resolution (`gh auth
login`, `GH_TOKEN`, etc). The token must have the `scim:enterprise` scope.
GitHub recommends authenticating as the enterprise's setup user; other
identities are typically created through SCIM itself. Only `GET` requests
are safe to run ad hoc — writes (`create`/`replace`/`patch`/`delete`) should
normally come from a single identity management system.

Every command requires the enterprise slug, either via `--enterprise` or the
`GH_SCIM_ENTERPRISE` environment variable. For enterprises on GHE.com, set
`--hostname` (or `GH_HOST`) to `api.SUBDOMAIN.ghe.com`.

## Usage

### Users

```sh
# List provisioned users
gh scim users list
gh scim users list --filter 'userName eq "octocat"'

# Get a single user
gh scim users get <scim-user-id>

# Provision a new user
gh scim users create \
  --external-id E012345 \
  --username E012345 \
  --given-name Mona \
  --family-name Octocat \
  --display-name "Mona Lisa" \
  --email mlisa@example.com \
  --role user

# Replace all of a user's attributes (SCIM PUT)
gh scim users replace <scim-user-id> \
  --external-id E012345 --username E012345 \
  --display-name "Mona Lisa" --email mlisa@example.com

# Update a single attribute (SCIM PATCH)
gh scim users patch <scim-user-id> --path displayName --value "New Name"

# Soft-deprovision (suspend, reversible) / reactivate
gh scim users deprovision <scim-user-id>
gh scim users reactivate <scim-user-id>

# Hard-deprovision (irreversible)
gh scim users delete <scim-user-id> --confirm
```

### Groups

```sh
# List provisioned groups
gh scim groups list
gh scim groups list --excluded-attributes members

# Get a single group
gh scim groups get <scim-group-id>

# Provision a new group (members reference SCIM user ids)
gh scim groups create \
  --external-id 8aa1a0c0-c4c3-4bc0-b4a5-2ef676900159 \
  --display-name Engineering \
  --member <scim-user-id> --member <scim-user-id>

# Replace all of a group's attributes, including membership (SCIM PUT)
gh scim groups replace <scim-group-id> \
  --external-id 8aa1a0c0-c4c3-4bc0-b4a5-2ef676900159 \
  --display-name Engineering

# Update a single attribute (SCIM PATCH)
gh scim groups patch <scim-group-id> --path displayName --value Employees

# Manage membership without replacing the whole group
gh scim groups add-members <scim-group-id> <scim-user-id> [<scim-user-id> ...]
gh scim groups remove-members <scim-group-id> <scim-user-id> [<scim-user-id> ...]

# Delete a group
gh scim groups delete <scim-group-id> --confirm
```

All commands print the API's JSON response to stdout, making them easy to
pipe into `jq` or other tooling.

## Testing

Run the regular tests locally with:

```sh
go test ./...
```

The live end-to-end suite exercises user and group provisioning against
GitHub.com or GHE.com:

```sh
SCIM_TOKEN=... \
SCIM_ENTERPRISE=your-enterprise \
SCIM_TEST_EMAIL_DOMAIN=1yydq3.onmicrosoft.com \
go test -tags=e2e -count=1 -v ./test/e2e
```

GitHub Actions runs the live suite regularly against both targets:

| Environment | Target | Required configuration |
| --- | --- | --- |
| `dotcom` | GitHub.com | Secrets `SCIM_TOKEN`, `SCIM_ENTERPRISE`, and `SCIM_TEST_EMAIL_DOMAIN` |
| `ghecom` | GHE.com subdomain | The same three secrets, plus variable `SCIM_HOSTNAME` set to the API hostname, such as `api.SUBDOMAIN.ghe.com` |

Common pitfalls: use the setup user's token with the `scim:enterprise` scope,
omit the leading `@` from `SCIM_TEST_EMAIL_DOMAIN`, and set `SCIM_HOSTNAME`
for GHE.com. See
[SCIM provisioning errors](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/understanding-iam-for-enterprises/troubleshooting-identity-and-access-management-for-your-enterprise?versionId=enterprise-cloud%40latest&productId=admin&restPage=managing-iam%2Cprovisioning-user-accounts-with-scim%2Cconfiguring-scim-provisioning-for-users#scim-provisioning-errors)
for troubleshooting guidance.

## Examples

- [Scheduled Google Workspace user reconciliation](examples/google-workspace-sync/README.md)

## Support

GitHub Support does not provide support for this integration.
This is a community-supported project 🚀
