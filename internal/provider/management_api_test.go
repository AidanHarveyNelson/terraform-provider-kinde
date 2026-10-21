// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

func newTestManagementAPI(t *testing.T, handlers map[string]http.HandlerFunc) *ManagementAPI {
	t.Helper()
	newStubManagementServer(t, handlers)

	client, err := newManagementClient(context.Background(), managementConfigFromEnv())
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

func TestIsNotFoundErrorWithKindeNotFoundCodes(t *testing.T) {
	client := newTestManagementAPI(t, map[string]http.HandlerFunc{
		"/api/v1/roles/missing": func(w http.ResponseWriter, r *http.Request) {
			fakeError(w, http.StatusBadRequest, "INVALID_ROLE_ID", "Role was not found")
		},
		"/api/v1/roles/forbidden": func(w http.ResponseWriter, r *http.Request) {
			fakeError(w, http.StatusForbidden, "ROLE_ID_INVALID", "Not allowed")
		},
	})
	ctx := context.Background()

	res, err := client.DeleteRole(ctx, management_api.DeleteRoleParams{RoleID: "missing"})
	_, err = expectResponse[management_api.SuccessResponse]("delete role", res, err)
	if !isNotFoundError(err) {
		t.Fatalf("expected a typed 400 with a not found code to be a not found error, got %v", err)
	}

	err = client.getJSON(ctx, "/api/v1/roles/missing", &struct{}{})
	if !isNotFoundError(err) {
		t.Fatalf("expected a raw 400 with a not found code to be a not found error, got %v", err)
	}

	err = client.getJSON(ctx, "/api/v1/roles/forbidden", &struct{}{})
	if isNotFoundError(err) {
		t.Fatalf("expected a 403 not to be a not found error even with a not found code, got %v", err)
	}
}

func TestManagementClientConcurrentRequests(t *testing.T) {
	client := newTestManagementAPI(t, map[string]http.HandlerFunc{
		"/api/v1/roles/role_1": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"role": map[string]string{"id": "role_1"}})
		},
	})
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 2000)
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				errs <- client.getJSON(ctx, "/api/v1/roles/role_1", &struct{}{})
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				res, err := client.DeleteRole(ctx, management_api.DeleteRoleParams{RoleID: "role_1"})
				if err == nil {
					_, err = expectResponse[management_api.SuccessResponse]("delete role", res, err)
				}
				if err != nil && !strings.Contains(err.Error(), "unexpected status code: 405") {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent request failed: %s", err)
		}
	}
}

func TestManagementConfigValidate(t *testing.T) {
	err := managementConfig{Domain: "https://example.kinde.com"}.validate()
	if err == nil || err.Error() != "missing required Kinde client options: audience, client_id, client_secret" {
		t.Fatalf("unexpected validation error: %v", err)
	}

	if err := (managementConfig{Domain: "d", Audience: "a", ClientID: "i", ClientSecret: "s"}).validate(); err != nil {
		t.Fatalf("expected a complete config to be valid, got %s", err)
	}
}

func TestManagementClientRequestsConfiguredScopes(t *testing.T) {
	stub := newStubManagementServer(t, map[string]http.HandlerFunc{
		"/api/v1/roles/role_1": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"role": map[string]string{"id": "role_1"}})
		},
	})
	t.Setenv("KINDE_SCOPES", "read:roles delete:roles")

	client, err := newManagementClient(context.Background(), managementConfigFromEnv())
	if err != nil {
		t.Fatalf("failed to create management client: %s", err)
	}
	if err := client.getJSON(context.Background(), "/api/v1/roles/role_1", &struct{}{}); err != nil {
		t.Fatalf("request failed: %s", err)
	}

	forms := stub.tokenRequests()
	if len(forms) == 0 {
		t.Fatal("expected a token request")
	}
	if got := forms[0].Get("scope"); got != "read:roles delete:roles" {
		t.Fatalf("expected scope %q in the token request, got %q", "read:roles delete:roles", got)
	}
	if got := forms[0].Get("audience"); got != stub.URL+"/api" {
		t.Fatalf("expected audience %q in the token request, got %q", stub.URL+"/api", got)
	}
}
