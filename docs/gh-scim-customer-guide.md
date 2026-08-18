# Integrating an identity provider with GitHub Enterprise Managed Users using `gh-scim`

> **Status:** `gh-scim` is a community-supported GitHub CLI extension. It is not a GitHub product or a supported IdP integration. GitHub recommends a supported, single-IdP integration when one is available. Custom or mixed identity systems may fall outside GitHub Support's scope.
>
> **Last reviewed:** 2026-08-18

This guide describes an IdP-independent pattern for provisioning users and groups in a GitHub Enterprise Cloud enterprise with Enterprise Managed Users (EMU). It uses [`eroullit/gh-scim`](https://github.com/eroullit/gh-scim) as a command-line adapter to GitHub's enterprise SCIM REST API.

The pattern works with an identity system that can:

- provide authoritative user and group data through an API, export, event, or webhook;
- use SAML 2.0 for authentication;
- supply stable, unique identifiers; and
- trigger or feed a controlled automation workflow.

The exact IdP screens, APIs, and event names are intentionally outside this guide. Follow the current documentation from your IdP for those details.

## Scope and important boundaries

`gh-scim` targets the **enterprise SCIM API for Enterprise Managed Users**. It does not target organization-level SCIM for personal GitHub accounts. See [REST API endpoints for enterprise SCIM](https://docs.github.com/en/enterprise-cloud@latest/rest/enterprise-admin/scim).

Before using this approach:

1. The enterprise must have been created with Enterprise Managed Users.
2. SAML authentication must already be configured and tested.
3. **Open SCIM configuration** must be enabled for the enterprise.
4. The automation must authenticate with a personal access token (classic) for the enterprise setup user, scoped only to `scim:enterprise`.
5. The SCIM REST API approach must not be used for an enterprise configured with OIDC.

GitHub documents these requirements in [Provisioning users and groups with SCIM using the REST API](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/provisioning-users-and-groups-with-scim-using-the-rest-api) and [Configuring SCIM provisioning for Enterprise Managed Users](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/configuring-scim-provisioning-for-users#configuring-provisioning-for-other-identity-management-systems).

## Recommended architecture

Keep the IdP as the source of truth. Run all SCIM writes through one controlled worker.

```text
                       +----------------------+
IdP events/webhooks -->|                      |
                       | IdP adapter          |
Scheduled directory -->|                      |
export or API query     +----------+-----------+
                                  |
                                  v
                       Normalized desired state
                                  |
                                  v
                       +----------+-----------+
                       | Reconciliation worker |
                       | Single SCIM writer     |
                       +----+--------------+----+
                            |              |
                            v              v
                     Mapping/state      gh scim
                     and run logs          |
                                           v
                                  GitHub enterprise
                                     SCIM API
```

The components have separate responsibilities:

| Component | Responsibility |
| --- | --- |
| IdP | Own user status, attributes, group membership, and access assignments. |
| IdP adapter | Read IdP-specific data and convert it to the normalized contract below. |
| Reconciliation worker | Compare desired state with GitHub state, decide the minimum required changes, invoke `gh scim`, retry transient failures, and record outcomes. |
| Mapping store | Persist each IdP `externalId` and the SCIM `id` returned by GitHub. |
| GitHub | Create and maintain managed user accounts and SCIM groups, then connect groups to teams and organizations. |

GitHub strongly recommends that only one system send `POST`, `PUT`, `PATCH`, or `DELETE` requests to the enterprise SCIM API. Other tools may make read-only `GET` requests. Do not run `gh-scim` writes alongside a partner IdP provisioning application. See [Ensure your identity management system is the only source of write operations](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/provisioning-users-and-groups-with-scim-using-the-rest-api#ensure-your-identity-management-system-is-the-only-source-of-write-operations).

## Normalize IdP data

Create a small adapter for your IdP that emits a stable, vendor-neutral representation. For example:

```json
{
  "users": [
    {
      "externalId": "idp-user-012345",
      "userName": "mona.octocat",
      "displayName": "Mona Octocat",
      "givenName": "Mona",
      "familyName": "Octocat",
      "email": "mona@example.com",
      "active": true,
      "roles": ["user"]
    }
  ],
  "groups": [
    {
      "externalId": "idp-group-engineering",
      "displayName": "Engineering",
      "memberExternalIds": ["idp-user-012345"]
    }
  ]
}
```

Use these mapping rules:

| Normalized field | Source and rule |
| --- | --- |
| `externalId` | Use the IdP's immutable object identifier. Never derive it from an email address or display name. It must remain stable through renames. |
| `userName` | Use the identifier supplied in the SAML `NameID` for IdPs other than Entra ID. Authentication and provisioning identifiers must match. |
| `displayName`, `givenName`, `familyName`, `email` | Map from the authoritative IdP directory attributes. |
| `active` | Derive from the assignment or lifecycle state that grants access to GitHub. |
| `roles` | Default to `user`. Grant elevated enterprise roles only through an explicit, reviewed mapping. |
| Group `externalId` | Use the IdP's immutable group identifier. |
| `memberExternalIds` | Resolve these to GitHub-generated SCIM user IDs before writing group membership. |

For non-Entra SAML integrations, GitHub requires SAML `NameID` to match SCIM `userName`. GitHub normalizes `userName`, adds the enterprise shortcode on GitHub.com, and requires the resulting username to be unique and within the applicable length limit. Validate the complete pilot directory before provisioning. See [Mapping of SAML and SCIM data](https://docs.github.com/en/enterprise-cloud@latest/rest/enterprise-admin/scim#mapping-of-saml-and-scim-data) and [Username considerations for external authentication](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/iam-configuration-reference/username-considerations-for-external-authentication).

## Install and authenticate

Install GitHub CLI, authenticate it for the correct hostname, then install the extension:

```bash
gh extension install eroullit/gh-scim
```

For local administration, authenticate the enterprise setup user through GitHub CLI. For unattended automation, inject the setup user's token from an approved secret manager:

```bash
export GH_TOKEN="${SCIM_TOKEN_FROM_SECRET_MANAGER}"
export GH_SCIM_ENTERPRISE="your-enterprise-slug"

# Required only for GitHub Enterprise Cloud with data residency.
# Use the API hostname, not the web hostname.
export GH_HOST="api.SUBDOMAIN.ghe.com"
```

The token must be a personal access token (classic) with only the `scim:enterprise` scope. GitHub recommends using the enterprise setup user because other managed accounts are themselves controlled by SCIM. Review [SCIM authentication requirements](https://docs.github.com/en/enterprise-cloud@latest/rest/enterprise-admin/scim#authentication).

Do not print, persist, or pass the token as a command-line argument. Restrict access to the secret, runner, logs, and workflow definition. Define a tested token rotation and recovery procedure before production rollout.

Verify read access without changing data:

```bash
gh scim users list --count 1
gh scim groups list --count 1 --excluded-attributes members
```

## Implement the lifecycle workflow

### 1. Provision or update users

Use the IdP `externalId` as the lookup key. Store the GitHub-generated SCIM `id` returned by create operations.

```bash
gh scim users list --filter 'externalId eq "idp-user-012345"'
```

Create the user if no matching resource exists and the desired `active` value is `true`. If a first-seen identity is inactive or unassigned, do not create it; record the skipped operation for audit purposes.

```bash
gh scim users create \
  --external-id "idp-user-012345" \
  --username "mona.octocat" \
  --given-name "Mona" \
  --family-name "Octocat" \
  --display-name "Mona Octocat" \
  --email "mona@example.com" \
  --role user
```

For an existing user, compare desired and current attributes, then update only what changed:

```bash
gh scim users patch SCIM_USER_ID \
  --path displayName \
  --value "Mona Lisa Octocat"
```

Use `replace` only when the worker has a complete user representation. It performs a SCIM `PUT`, so omitted attributes are removed.

### 2. Soft-deprovision and reactivate users

Make soft deprovisioning the default offboarding behavior:

```bash
gh scim users deprovision SCIM_USER_ID
gh scim users reactivate SCIM_USER_ID
```

Soft deprovisioning suspends access while retaining the SCIM link, which allows reactivation. A hard deprovision is irreversible:

```bash
gh scim users delete SCIM_USER_ID --confirm
```

Do not automate hard deprovisioning from a routine disable, unassignment, or temporary leave event. If your organization permits hard deprovisioning, require a separate approval, a delay, and a verified target identity. Read [Deprovisioning and reinstating users with SCIM](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/deprovisioning-and-reinstating-users) before implementing offboarding.

### 3. Provision groups after users

GitHub group membership references GitHub-generated SCIM user IDs, not IdP user IDs. Provision users first and resolve every member before creating or updating a group.

```bash
gh scim groups create \
  --external-id "idp-group-engineering" \
  --display-name "Engineering" \
  --member SCIM_USER_ID_1 \
  --member SCIM_USER_ID_2
```

Apply membership changes incrementally:

```bash
gh scim groups add-members SCIM_GROUP_ID SCIM_USER_ID_3
gh scim groups remove-members SCIM_GROUP_ID SCIM_USER_ID_2
```

Use `groups replace` only with the complete intended membership. Omitted members are removed.

After provisioning a group, connect it to the appropriate GitHub team. Group provisioning alone does not grant repository access. See [Managing team memberships with identity provider groups](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/managing-team-memberships-with-identity-provider-groups).

## Choose a trigger model

Use one or both of these inputs, but keep one reconciliation worker as the only writer:

| Model | Use | Design notes |
| --- | --- | --- |
| Event-driven | Fast joiner, mover, leaver processing | Validate webhook authenticity, queue events, serialize changes per identity, and make handlers idempotent. Treat events as prompts to reconcile current IdP state, not as unquestioned commands. |
| Scheduled reconciliation | Recovery from missed events and detection of drift | Read the complete assigned population, compare it with GitHub, then apply bounded changes. Run in report-only mode before enabling writes. |

A production deployment should use events for latency and a scheduled full reconciliation for correctness.

## Make reconciliation safe

Implement these controls around the extension:

1. **Idempotent lookup:** Find resources by immutable `externalId`. Create only when no exact match exists.
2. **Persistent mapping:** Store IdP `externalId` to GitHub SCIM `id` mappings for users and groups. Rebuild mappings from list endpoints when necessary. List commands return one page, so iterate with `--start-index` and `--count` until all `totalResults` resources have been collected.
3. **Minimum changes:** Prefer `patch`, `add-members`, and `remove-members` over full replacement.
4. **Dependency order:** Create users before groups and memberships. Remove memberships before deleting groups. Soft-deprovision users only after access-removal policy is satisfied.
5. **Concurrency control:** Serialize writes for the same user or group. Prevent two reconciliation runs from writing concurrently.
6. **Bounded rollout:** Stop a run when the number or percentage of proposed creates, suspensions, role changes, or membership removals exceeds an approved threshold.
7. **Retries:** Retry `429`, rate-limit `403`, and transient `5xx` responses with exponential backoff and jitter. Classify a `403` using `Retry-After`, `x-ratelimit-remaining`, `x-ratelimit-reset`, and the response body; retry rate-limit responses after the indicated delay. Do not repeatedly retry other `400`, `401`, `403`, or `409` responses without correcting the underlying data or configuration.
8. **Failure handling:** Send exhausted operations to a review queue and alert an owner. Never convert a failed lookup into an automatic create without proving the resource is absent.
9. **Auditability:** Record the IdP external ID, GitHub SCIM ID, operation, timestamp, workflow run, and result. Exclude tokens and unnecessary personal data.

GitHub advises limiting initial assignment to no more than 1,000 users per hour, or 1,000 users added to each assigned group per hour. See [Understand rate limits on GitHub](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/provisioning-users-and-groups-with-scim-using-the-rest-api#understand-rate-limits-on-github).

## Test and roll out

GitHub recommends testing in an environment isolated from production IdP and GitHub data.

1. Confirm SAML authentication and identifier matching.
2. Enable open SCIM configuration.
3. Verify read-only commands.
4. Run the reconciler in report-only mode.
5. Pilot with a small set of test users and one test group.
6. Test create, sign-in, attribute update, group membership, soft deprovision, and reactivation.
7. Confirm team and organization access follows group membership.
8. Confirm audit logs show successful and failed SCIM operations.
9. Test retry, duplicate-event, partial-failure, and rollback procedures.
10. Expand in controlled batches below GitHub's documented rate guidance.

Do not test hard deprovisioning with a real user account.

## Operations and troubleshooting

Commands that return SCIM resources write the API response as JSON to standard output, so the worker can parse the returned `id`, status, and attributes. Delete commands instead print a confirmation message.

Use these read-only commands during investigation:

```bash
gh scim users list --filter 'externalId eq "idp-user-012345"'
gh scim users get SCIM_USER_ID
gh scim groups list --filter 'externalId eq "idp-group-engineering"'
gh scim groups get SCIM_GROUP_ID
```

Check these areas in order:

1. The IdP's current source record and assignment.
2. The worker's normalized desired state.
3. The stored `externalId` to SCIM `id` mapping.
4. The worker's command, exit status, and response body.
5. GitHub enterprise audit log events, especially `external_identity.scim_api_success` and `external_identity.scim_api_failure`.
6. SAML and SCIM identifier matching.
7. Username normalization, uniqueness, and length.
8. GitHub API rate-limit responses.

Enable enterprise audit log streaming and include API request events if your logging policy permits it. GitHub retains enterprise audit log data for 180 days, while streaming lets you apply your own retention and alerting. See [Streaming the audit log for your enterprise](https://docs.github.com/en/enterprise-cloud@latest/admin/monitoring-activity-in-your-enterprise/reviewing-audit-logs-for-your-enterprise/streaming-the-audit-log-for-your-enterprise).

## Ownership and support

Define operational owners before production use:

| Area | Suggested owner |
| --- | --- |
| IdP attributes, assignments, and events | Identity team |
| Adapter and reconciliation code | Platform or identity engineering |
| Token custody and rotation | Security or privileged access management |
| GitHub teams and repository permissions | GitHub enterprise and organization owners |
| Monitoring and incident response | Identity operations |

The [`gh-scim` repository](https://github.com/eroullit/gh-scim) is community-supported. Problems in the extension or custom IdP adapter should be handled through the project's community process and your internal engineering owners. GitHub's documentation notes that mixed or untested identity systems are not expressly supported and that GitHub Support may not be able to assist with those integration issues.

## Official GitHub references

- [Configuring SCIM provisioning for Enterprise Managed Users](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/configuring-scim-provisioning-for-users)
- [Provisioning users and groups with SCIM using the REST API](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/provisioning-users-and-groups-with-scim-using-the-rest-api)
- [REST API endpoints for enterprise SCIM](https://docs.github.com/en/enterprise-cloud@latest/rest/enterprise-admin/scim)
- [Username considerations for external authentication](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/iam-configuration-reference/username-considerations-for-external-authentication)
- [Deprovisioning and reinstating users with SCIM](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/deprovisioning-and-reinstating-users)
- [Managing team memberships with identity provider groups](https://docs.github.com/en/enterprise-cloud@latest/admin/managing-iam/provisioning-user-accounts-with-scim/managing-team-memberships-with-identity-provider-groups)
- [Streaming the audit log for your enterprise](https://docs.github.com/en/enterprise-cloud@latest/admin/monitoring-activity-in-your-enterprise/reviewing-audit-logs-for-your-enterprise/streaming-the-audit-log-for-your-enterprise)
