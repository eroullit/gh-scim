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

Run the non-destructive test suite locally with:

```sh
go test ./...
```

The live end-to-end suite builds the extension and exercises the complete user
and group provisioning lifecycle against a real Enterprise account:

```sh
SCIM_TOKEN=... \
SCIM_ENTERPRISE=your-enterprise \
SCIM_TEST_EMAIL_DOMAIN=1yydq3.onmicrosoft.com \
go test -tags=e2e -count=1 -v ./test/e2e
```

To test a specific prebuilt `gh-scim` executable directly, set `SCIM_BINARY`:

```sh
go build -o ./gh-scim .
SCIM_BINARY="$PWD/gh-scim" \
SCIM_TOKEN="$(gh auth token)" \
SCIM_ENTERPRISE="your-enterprise-slug" \
SCIM_TEST_EMAIL_DOMAIN="1yydq3.onmicrosoft.com" \
go test -tags=e2e -count=1 -v ./test/e2e
```

When `SCIM_BINARY` is unset, the suite builds the current checkout into a
temporary directory and tests that executable.

With `-v`, the test writes each `gh-scim` call and the JSON returned by the API
to standard output. Authentication tokens are never included in the logged
command.

`SCIM_TOKEN` must belong to the Enterprise setup user and include the
`scim:enterprise` scope. `SCIM_TEST_EMAIL_DOMAIN` must be configured without
the leading `@`; each run generates a unique address under that domain. Set
`SCIM_HOSTNAME` when testing a GHE.com Enterprise.

The live suite creates, updates, suspends, reactivates, and irreversibly deletes
a user. It also creates, updates, changes membership for, and deletes a group.
Resources use a `gh-scim-e2e-` ownership prefix, and cleanup additionally checks
their generated external IDs and email before deleting them. Usernames use a
compact `e2e-<hash>` form to remain safely below GitHub's 39-character limit.

The `test` GitHub Actions workflow runs on every pull request and push, can be
started manually, and runs daily at 06:00 UTC. It runs the live suite once for
each Actions environment:

| Environment | Target | Required configuration |
| --- | --- | --- |
| `dotcom` | GitHub.com | Secrets `SCIM_TOKEN`, `SCIM_ENTERPRISE`, and `SCIM_TEST_EMAIL_DOMAIN` |
| `ghecom` | GHE.com subdomain | The same three secrets, plus variable `SCIM_HOSTNAME` set to the API hostname, such as `api.SUBDOMAIN.ghe.com` |

Define the secrets and variable on their respective Actions environments, not
at repository level, so each target receives its own Enterprise credentials
and test email domain. `SCIM_TOKEN` must include `scim:enterprise`, and
`SCIM_TEST_EMAIL_DOMAIN` must omit the leading `@`.

Live jobs are serialized independently per environment to avoid concurrent
cleanup against the same Enterprise. Both targets may run in parallel. When
secrets are not available, as on pull requests from forks, the ordinary tests
still run and the affected live lifecycle is skipped. A `ghecom` run with
credentials but no `SCIM_HOSTNAME` fails as a configuration error.
