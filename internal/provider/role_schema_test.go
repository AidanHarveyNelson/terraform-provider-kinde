// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestNormalizeRolePermissions(t *testing.T) {
	permissions := []string{"perm-b", "", "perm-a", "perm-b", "perm-c"}

	got := normalizeRolePermissions(permissions)
	want := []string{"perm-a", "perm-b", "perm-c"}

	if len(got) != len(want) {
		t.Fatalf("expected %d permissions, got %d: %#v", len(want), len(got), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected normalized permissions %v, got %v", want, got)
		}
	}
}

func TestFlattenRoleResourceFiltersEmptyPermissions(t *testing.T) {
	state, err := flattenRoleResource(context.Background(), &kindeRole{
		ID:          "role-id",
		Name:        "role-name",
		Key:         "role-key",
		Description: "role-description",
	}, []string{""})
	if err != nil {
		t.Fatalf("flattenRoleResource returned error: %v", err)
	}

	if !state.Permissions.IsNull() {
		t.Fatalf("expected permissions to be null when only empty IDs are present, got %#v", state.Permissions)
	}
}

func TestFlattenRoleResourceNormalizesPermissions(t *testing.T) {
	state, err := flattenRoleResource(context.Background(), &kindeRole{
		ID:          "role-id",
		Name:        "role-name",
		Key:         "role-key",
		Description: "role-description",
	}, []string{"perm-b", "perm-a", "perm-b"})
	if err != nil {
		t.Fatalf("flattenRoleResource returned error: %v", err)
	}

	if state.Permissions.IsNull() {
		t.Fatal("expected permissions to be set")
	}

	var got []string
	if diags := state.Permissions.ElementsAs(context.Background(), &got, false); diags.HasError() {
		t.Fatalf("failed to decode permissions: %v", diags)
	}

	sort.Strings(got)
	want := []string{"perm-a", "perm-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected normalized permissions %v, got %v", want, got)
		}
	}
}

func TestFlattenRoleResourceKeepsPermissionsType(t *testing.T) {
	state, err := flattenRoleResource(context.Background(), &kindeRole{
		ID:          "role-id",
		Name:        "role-name",
		Key:         "role-key",
		Description: "role-description",
	}, []string{"perm-a"})
	if err != nil {
		t.Fatalf("flattenRoleResource returned error: %v", err)
	}

	if state.Permissions.ElementType(context.Background()) != types.StringType {
		t.Fatalf("expected permissions element type %v, got %v", types.StringType, state.Permissions.ElementType(context.Background()))
	}
}
