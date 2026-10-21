// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/kinde-go"
)

func newTestManagementAPI(t *testing.T, handlers map[string]http.HandlerFunc) *ManagementAPI {
	t.Helper()
	newStubManagementServer(t, handlers)

	client, err := newManagementClient(context.Background(), kinde.NewClientOptions())
	if err != nil {
		t.Fatalf("failed to create management client: %s", err)
	}
	return client
}

func TestIsNotFoundErrorWithManagementAPI(t *testing.T) {
	client := newTestManagementAPI(t, map[string]http.HandlerFunc{
		"/api/v1/permissions/missing": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{{"code": "PERMISSION_NOT_FOUND", "message": "Permission not found"}},
			})
		},
		"/api/v1/permissions/bad": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{{"code": "INVALID_REQUEST", "message": "Bad request"}},
			})
		},
	})
	ctx := context.Background()

	res, err := client.DeletePermission(ctx, management_api.DeletePermissionParams{PermissionID: "missing"})
	_, err = expectResponse[management_api.SuccessResponse]("delete permission", res, err)
	if !isNotFoundError(err) {
		t.Fatalf("expected an undeclared 404 from the SDK to be a not found error, got %v", err)
	}

	res, err = client.DeletePermission(ctx, management_api.DeletePermissionParams{PermissionID: "bad"})
	_, err = expectResponse[management_api.SuccessResponse]("delete permission", res, err)
	if err == nil || isNotFoundError(err) {
		t.Fatalf("expected a 400 not to be a not found error, got %v", err)
	}
	if !hasStatusCode(err, http.StatusBadRequest) || !containsAnyErrorCode(err, "INVALID_REQUEST") {
		t.Fatalf("expected a 400 with code INVALID_REQUEST, got %v", err)
	}

	err = client.doJSON(ctx, http.MethodDelete, "/api/v1/permissions/missing", nil, nil)
	if !isNotFoundError(err) || !containsAnyErrorCode(err, "PERMISSION_NOT_FOUND") {
		t.Fatalf("expected a raw 404 to be a not found error with its code, got %v", err)
	}

	err = managementAPIError("delete directory", &management_api.DeleteDirectoryNotFound{})
	if !isNotFoundError(err) {
		t.Fatalf("expected a typed NotFound response to be a not found error, got %v", err)
	}
}
