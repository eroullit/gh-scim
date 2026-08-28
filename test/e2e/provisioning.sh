#!/bin/sh

set -efu

readonly ownership_prefix="gh-scim-e2e-"
readonly user_external_id="${ownership_prefix}user"
readonly group_external_id="${ownership_prefix}group"

for command in gh jq git; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "Required command not found: $command" >&2
    exit 1
  fi
done

if [ -z "${SCIM_TOKEN:-}" ]; then
  echo "SCIM_TOKEN is required" >&2
  exit 1
fi
if [ -z "${SCIM_ENTERPRISE:-}" ]; then
  echo "SCIM_ENTERPRISE is required" >&2
  exit 1
fi
if [ -z "${SCIM_TEST_EMAIL_DOMAIN:-}" ]; then
  echo "SCIM_TEST_EMAIL_DOMAIN is required" >&2
  exit 1
fi

email_domain="${SCIM_TEST_EMAIL_DOMAIN#@}"
case "$email_domain" in
  ""|*"@"*)
    echo "SCIM_TEST_EMAIL_DOMAIN must be a domain without @, got: $email_domain" >&2
    exit 1
    ;;
esac

run_seed="${GITHUB_RUN_ID:-$(date +%s)}-${GITHUB_RUN_ATTEMPT:-1}-$$"
run_suffix="$(printf '%s' "$run_seed" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9-' '-' | sed 's/^-//; s/-$//')"
if [ -z "$run_suffix" ]; then
  echo "Could not derive a safe test run suffix" >&2
  exit 1
fi

prefix="${ownership_prefix}${run_suffix}"
user_name="e2e-$(printf '%s' "$prefix" | git hash-object --stdin | cut -c1-12)"
user_display_name="${prefix}-user"
group_name="${prefix}-group"
email="${prefix}@${email_domain}"
user_id=""
group_id=""

run_scim() {
  {
    printf 'gh scim --enterprise %s' "$SCIM_ENTERPRISE"
    if [ -n "${SCIM_HOSTNAME:-}" ]; then
      printf ' --hostname %s' "$SCIM_HOSTNAME"
    fi
    printf ' %s' "$@"
    echo
  } >&2

  if [ -n "${SCIM_HOSTNAME:-}" ]; then
    GH_TOKEN="$SCIM_TOKEN" GH_DEBUG=api gh scim \
      --enterprise "$SCIM_ENTERPRISE" --hostname "$SCIM_HOSTNAME" "$@"
  else
    GH_TOKEN="$SCIM_TOKEN" GH_DEBUG=api gh scim \
      --enterprise "$SCIM_ENTERPRISE" "$@"
  fi
}

assert_json() {
  description="$1"
  json="$2"
  filter="$3"
  shift 3

  if ! printf '%s\n' "$json" | jq -e "$@" "$filter" >/dev/null; then
    echo "Assertion failed: $description" >&2
    printf '%s\n' "$json" | jq . >&2 || true
    exit 1
  fi
}

cleanup() {
  status=$?
  trap - 0
  set +e
  if [ -n "$group_id" ]; then
    run_scim groups delete "$group_id" --confirm
  fi
  if [ -n "$user_id" ]; then
    run_scim users delete "$user_id" --confirm
  fi
  exit "$status"
}
trap cleanup 0

stale_groups="$(run_scim groups list --filter "externalId eq \"$group_external_id\"")"
stale_group_ids="$(
  printf '%s\n' "$stale_groups" |
    jq -r --arg external "$group_external_id" --arg prefix "$ownership_prefix" \
      '.Resources[]? | select(.externalId == $external and (.displayName | startswith($prefix))) | .id'
)"
for stale_group_id in $stale_group_ids; do
  run_scim groups delete "$stale_group_id" --confirm
done

stale_users="$(run_scim users list --filter "externalId eq \"$user_external_id\"")"
stale_user_ids="$(
  printf '%s\n' "$stale_users" |
    jq -r --arg external "$user_external_id" --arg prefix "$ownership_prefix" \
      '.Resources[]? | select(.externalId == $external and (.displayName | startswith($prefix))) | .id'
)"
for stale_user_id in $stale_user_ids; do
  run_scim users delete "$stale_user_id" --confirm
done

created_user="$(run_scim users create \
  --external-id "$user_external_id" \
  --username "$user_name" \
  --given-name SCIM \
  --family-name Test \
  --display-name "$user_display_name" \
  --email "$email" \
  --role user)"
user_id="$(printf '%s\n' "$created_user" | jq -er '.id | select(length > 0)')"
assert_json "created user attributes" "$created_user" \
  '.id == $id and .externalId == $external and any(.emails[]?; (.value | ascii_downcase) == ($email | ascii_downcase))' \
  --arg id "$user_id" --arg external "$user_external_id" --arg email "$email"

got_user="$(run_scim users get "$user_id")"
assert_json "retrieved user attributes" "$got_user" \
  '.id == $id and .externalId == $external and any(.emails[]?; (.value | ascii_downcase) == ($email | ascii_downcase))' \
  --arg id "$user_id" --arg external "$user_external_id" --arg email "$email"

listed_users="$(run_scim users list --filter "userName eq \"$user_name\"")"
assert_json "user appears in filtered list" "$listed_users" \
  'any(.Resources[]?; .id == $id)' --arg id "$user_id"

replaced_user="$(run_scim users replace "$user_id" \
  --external-id "$user_external_id" \
  --username "$user_name" \
  --given-name Provisioning \
  --family-name Test \
  --display-name "${user_display_name}-replaced" \
  --email "$email" \
  --role user)"
assert_json "user replace updates displayName" "$replaced_user" \
  '.displayName == $name' --arg name "${user_display_name}-replaced"

patched_user="$(run_scim users patch "$user_id" --path displayName --value "${user_display_name}-patched")"
assert_json "user patch updates displayName" "$patched_user" \
  '.displayName == $name' --arg name "${user_display_name}-patched"

deprovisioned_user="$(run_scim users deprovision "$user_id")"
assert_json "user is deprovisioned" "$deprovisioned_user" '.active == false'

reactivated_user="$(run_scim users reactivate "$user_id")"
assert_json "user is reactivated" "$reactivated_user" '.active == true'

created_group="$(run_scim groups create \
  --external-id "$group_external_id" \
  --display-name "$group_name" \
  --member "$user_id")"
group_id="$(printf '%s\n' "$created_group" | jq -er '.id | select(length > 0)')"
assert_json "created group contains user" "$created_group" \
  'any(.members[]?; .value == $id)' --arg id "$user_id"

got_group="$(run_scim groups get "$group_id")"
assert_json "retrieved group contains user" "$got_group" \
  'any(.members[]?; .value == $id)' --arg id "$user_id"

listed_groups="$(run_scim groups list --filter "displayName eq \"$group_name\"")"
assert_json "group appears in filtered list" "$listed_groups" \
  'any(.Resources[]?; .id == $id)' --arg id "$group_id"

replaced_group="$(run_scim groups replace "$group_id" \
  --external-id "$group_external_id" \
  --display-name "${group_name}-replaced" \
  --member "$user_id")"
assert_json "group replace updates name and preserves member" "$replaced_group" \
  '.displayName == $name and any(.members[]?; .value == $id)' \
  --arg name "${group_name}-replaced" --arg id "$user_id"

patched_group="$(run_scim groups patch "$group_id" --path displayName --value "${group_name}-patched")"
assert_json "group patch updates displayName" "$patched_group" \
  '.displayName == $name' --arg name "${group_name}-patched"

run_scim groups remove-members "$group_id" "$user_id"
group_without_member="$(run_scim groups get "$group_id")"
assert_json "group member is removed" "$group_without_member" \
  'any(.members[]?; .value == $id) | not' --arg id "$user_id"

run_scim groups add-members "$group_id" "$user_id"
group_with_member="$(run_scim groups get "$group_id")"
assert_json "group member is restored" "$group_with_member" \
  'any(.members[]?; .value == $id)' --arg id "$user_id"

run_scim groups delete "$group_id" --confirm
group_id=""

run_scim users delete "$user_id" --confirm
user_id=""

echo "SCIM provisioning lifecycle passed"
