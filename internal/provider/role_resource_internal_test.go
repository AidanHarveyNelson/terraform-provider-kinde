// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"testing"
)

// TestGetRoleUsesAllPermissionPages proves that RoleResource.getRole follows
// next_token when reading a role's permissions, so roles with more
// permissions than fit on one page of /api/v1/roles/{id}/permissions are not
// silently truncated.
func TestGetRoleUsesAllPermissionPages(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var permissionRequests []string

	client := newTestManagementAPI(t, map[string]http.HandlerFunc{
		"/api/v1/roles/role_1": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"role": map[string]any{
					"id":          "role_1",
					"name":        "Role",
					"key":         "role",
					"description": "Role description",
				},
			})
		},
		"/api/v1/roles/role_1/permissions": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			permissionRequests = append(permissionRequests, r.URL.RawQuery)
			mu.Unlock()
			if r.URL.Query().Get("next_token") == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"next_token": "page-2",
					"permissions": []map[string]string{
						{"id": "perm_01"},
						{"id": "perm_02"},
						{"id": "perm_03"},
						{"id": "perm_04"},
						{"id": "perm_05"},
						{"id": "perm_06"},
						{"id": "perm_07"},
						{"id": "perm_08"},
						{"id": "perm_09"},
						{"id": "perm_10"},
					},
				})
				return
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"permissions": []map[string]string{
					{"id": "perm_11"},
				},
			})
		},
	})

	roleResource := RoleResource{client: client}
	role, err := roleResource.getRole(ctx, "role_1")
	if err != nil {
		t.Fatalf("get role: %s", err)
	}

	if got := len(role.Permissions); got != 11 {
		t.Fatalf("expected 11 permissions across pages, got %d: %#v", got, role.Permissions)
	}

	if len(permissionRequests) != 2 {
		t.Fatalf("expected 2 permission requests, got %d: %#v", len(permissionRequests), permissionRequests)
	}

	wantQueries := []url.Values{
		{"page_size": []string{"100"}},
		{"page_size": []string{"100"}, "next_token": []string{"page-2"}},
	}
	for i, wantQuery := range wantQueries {
		gotQuery, err := url.ParseQuery(permissionRequests[i])
		if err != nil {
			t.Fatalf("parse permission request query %q: %s", permissionRequests[i], err)
		}
		if !reflect.DeepEqual(gotQuery, wantQuery) {
			t.Fatalf("unexpected permission request query %d\nwant: %#v\n got: %#v", i, wantQuery, gotQuery)
		}
	}
}
