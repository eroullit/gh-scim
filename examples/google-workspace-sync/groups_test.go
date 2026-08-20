package main

import (
	"testing"
)

func TestNormalizeGoogleGroup(t *testing.T) {
	group := googleGroup{ID: "group-1", Email: "engineering@example.com", Name: "Engineering"}
	activeUsers := map[string]bool{"user-1": true, "user-2": false, "user-3": true}
	scimIDs := map[string]string{"user-1": "scim-1", "user-2": "scim-2"}

	tests := []struct {
		name        string
		members     []googleMember
		wantMembers []string
		wantErr     bool
	}{
		{
			name: "maps active user members",
			members: []googleMember{
				{ID: "user-1", Email: "one@example.com", Type: "USER"},
				{ID: "user-2", Email: "two@example.com", Type: "USER"},
			},
			wantMembers: []string{"scim-1"},
		},
		{
			name:    "rejects nested groups",
			members: []googleMember{{ID: "nested", Email: "nested@example.com", Type: "GROUP"}},
			wantErr: true,
		},
		{
			name:        "rejects unprovisioned active user",
			members:     []googleMember{{ID: "user-3", Email: "three@example.com", Type: "USER"}},
			wantMembers: nil,
			wantErr:     true,
		},
		{
			name:    "rejects member outside user scope",
			members: []googleMember{{ID: "user-4", Email: "four@example.com", Type: "USER"}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeGoogleGroup(group, test.members, activeUsers, scimIDs)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeGoogleGroup error = %v, wantErr %t", err, test.wantErr)
			}
			if err == nil && !sameStrings(got.MemberIDs, test.wantMembers) {
				t.Fatalf("members = %v, want %v", got.MemberIDs, test.wantMembers)
			}
		})
	}
}

func TestGroupChanged(t *testing.T) {
	desired := desiredGroup{
		ExternalID:  "google:group-1",
		DisplayName: "Engineering",
		MemberIDs:   []string{"scim-1", "scim-2"},
	}

	tests := []struct {
		name    string
		current scimGroup
		want    bool
	}{
		{
			name: "matching group",
			current: scimGroup{
				ExternalID:  "google:group-1",
				DisplayName: "Engineering",
				Members:     []scimMember{{Value: "scim-2"}, {Value: "scim-1"}},
			},
		},
		{
			name: "membership changed",
			current: scimGroup{
				ExternalID:  "google:group-1",
				DisplayName: "Engineering",
				Members:     []scimMember{{Value: "scim-1"}},
			},
			want: true,
		},
		{
			name: "display name changed",
			current: scimGroup{
				ExternalID:  "google:group-1",
				DisplayName: "Old Engineering",
				Members:     []scimMember{{Value: "scim-1"}, {Value: "scim-2"}},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := groupChanged(desired, test.current); got != test.want {
				t.Fatalf("groupChanged = %t, want %t", got, test.want)
			}
		})
	}
}

func TestGroupOperationMemberDelta(t *testing.T) {
	tests := []struct {
		name string
		op   groupOperation
		want int
	}{
		{
			name: "new group",
			op: groupOperation{
				Create:  true,
				Desired: desiredGroup{MemberIDs: []string{"one", "two"}},
			},
			want: 2,
		},
		{
			name: "adds and removes",
			op: groupOperation{
				Desired: desiredGroup{MemberIDs: []string{"two", "three"}},
				Current: &scimGroup{Members: []scimMember{{Value: "one"}, {Value: "two"}}},
			},
			want: 2,
		},
		{
			name: "unchanged",
			op: groupOperation{
				Desired: desiredGroup{MemberIDs: []string{"one"}},
				Current: &scimGroup{Members: []scimMember{{Value: "one"}}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.op.memberDelta(); got != test.want {
				t.Fatalf("memberDelta = %d, want %d", got, test.want)
			}
		})
	}
}

func TestParseRoleGroup(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    roleGroupMapping
		wantErr bool
	}{
		{
			name:  "enterprise owner mapping",
			value: "enterprise_owner=owners@example.com",
			want:  roleGroupMapping{Role: "enterprise_owner", GroupKey: "owners@example.com"},
		},
		{
			name:    "rejects normal user group",
			value:   "user=everyone@example.com",
			wantErr: true,
		},
		{
			name:    "rejects malformed mapping",
			value:   "enterprise_owner",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseRoleGroup(test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("parseRoleGroup error = %v, wantErr %t", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("mapping = %#v, want %#v", got, test.want)
			}
		})
	}
}
