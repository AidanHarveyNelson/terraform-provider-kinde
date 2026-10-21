// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

type terraformState = terraform.State

type fakePermission struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type fakeAPI struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Audience        string   `json:"audience"`
	IsManagementAPI bool     `json:"is_management_api"`
	Scopes          []string `json:"scopes"`
	Applications    []string `json:"applications"`
}

type fakeApplication struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	LoginURI     string   `json:"login_uri"`
	HomepageURI  string   `json:"homepage_uri"`
	LogoutURIs   []string `json:"logout_uris"`
	RedirectURIs []string `json:"redirect_uris"`
}

type fakeConnection struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	Strategy    string         `json:"strategy"`
	Options     map[string]any `json:"-"`
}

type fakeColor struct {
	Raw string `json:"raw"`
	Hex string `json:"hex"`
	Hsl string `json:"hsl"`
}

type fakeOrganization struct {
	Code            string     `json:"code"`
	Name            string     `json:"name"`
	Handle          *string    `json:"handle"`
	CreatedOn       string     `json:"created_on"`
	ExternalID      *string    `json:"external_id"`
	ColorScheme     string     `json:"color_scheme"`
	BackgroundColor *fakeColor `json:"background_color"`
	ButtonColor     *fakeColor `json:"button_color"`
	ButtonTextColor *fakeColor `json:"button_text_color"`
	LinkColor       *fakeColor `json:"link_color"`
}

type fakeRole struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Key         string   `json:"key"`
	Description string   `json:"description"`
	Permissions []string `json:"-"`
}

type fakeUser struct {
	ID          string `json:"id"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	IsSuspended bool   `json:"is_suspended"`
	CreatedOn   string `json:"created_on"`
	UpdatedOn   string `json:"updated_on"`
}

type fakeIdentity struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	UserID string `json:"-"`
}

type fakeRoute struct {
	method  string
	pattern *regexp.Regexp
	handle  func(w http.ResponseWriter, r *http.Request, params []string)
}

type fakeKinde struct {
	mu             sync.Mutex
	nextID         int
	permissions    map[string]*fakePermission
	apis           map[string]*fakeAPI
	applications   map[string]*fakeApplication
	appConnections map[string]map[string]bool
	connections    map[string]*fakeConnection
	organizations  map[string]*fakeOrganization
	orgMembers     map[string]map[string][]string
	roles          map[string]*fakeRole
	users          map[string]*fakeUser
	identities     map[string]*fakeIdentity
	routes         []fakeRoute
	server         *stubManagementServer
}

func newFakeKinde(t *testing.T) *fakeKinde {
	t.Helper()

	f := &fakeKinde{
		permissions:    map[string]*fakePermission{},
		apis:           map[string]*fakeAPI{},
		applications:   map[string]*fakeApplication{},
		appConnections: map[string]map[string]bool{},
		connections:    map[string]*fakeConnection{},
		organizations:  map[string]*fakeOrganization{},
		orgMembers:     map[string]map[string][]string{},
		roles:          map[string]*fakeRole{},
		users:          map[string]*fakeUser{},
		identities:     map[string]*fakeIdentity{},
	}
	for _, strategy := range []string{strategyEmailPassword, strategyEmailOTP, strategyPhoneOTP, strategyUsernamePassword, strategyUsernameOTP} {
		id := f.newID("conn")
		f.connections[id] = &fakeConnection{ID: id, Name: strategy, DisplayName: strategy, Strategy: strategy}
	}
	f.registerRoutes()
	f.server = newStubManagementServer(t, map[string]http.HandlerFunc{stubFallbackRoute: f.serveHTTP})
	return f
}

func (f *fakeKinde) newID(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s_%04d", prefix, f.nextID)
}

func (f *fakeKinde) route(method, pattern string, handle func(w http.ResponseWriter, r *http.Request, params []string)) {
	f.routes = append(f.routes, fakeRoute{method: method, pattern: regexp.MustCompile("^" + pattern + "$"), handle: handle})
}

func (f *fakeKinde) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, route := range f.routes {
		if route.method != r.Method {
			continue
		}
		if params := route.pattern.FindStringSubmatch(r.URL.Path); params != nil {
			route.handle(w, r, params[1:])
			return
		}
	}
	fakeError(w, http.StatusNotFound, "ROUTE_NOT_FOUND", r.Method+" "+r.URL.Path)
}

func fakeJSON(w http.ResponseWriter, status int, body any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func fakeOK(w http.ResponseWriter, status int) {
	fakeJSON(w, status, map[string]string{"code": "OK", "message": "Success"})
}

func fakeError(w http.ResponseWriter, status int, code, message string) {
	fakeJSON(w, status, map[string]any{"errors": []map[string]string{{"code": code, "message": message}}})
}

func fakeBody(r *http.Request, out any) {
	_ = json.NewDecoder(r.Body).Decode(out)
}

func fakePage[T any](r *http.Request, items []T) ([]T, string) {
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize <= 0 {
		pageSize = len(items) + 1
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("next_token"))
	if offset >= len(items) {
		return nil, ""
	}
	end := offset + pageSize
	if end >= len(items) {
		return items[offset:], ""
	}
	return items[offset:end], strconv.Itoa(end)
}

func sortedValues[T any](m map[string]*T) []T {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]T, 0, len(keys))
	for _, key := range keys {
		values = append(values, *m[key])
	}
	return values
}

func fakeColorValue(value string) *fakeColor {
	if value == "" {
		return nil
	}
	return &fakeColor{Raw: value, Hex: value, Hsl: value}
}

func fakeTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func (f *fakeKinde) registerRoutes() {
	f.route(http.MethodGet, "/api/v1/permissions", func(w http.ResponseWriter, r *http.Request, _ []string) {
		page, next := fakePage(r, sortedValues(f.permissions))
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "permissions": page, "next_token": next})
	})
	f.route(http.MethodPost, "/api/v1/permissions", func(w http.ResponseWriter, r *http.Request, _ []string) {
		var body fakePermission
		fakeBody(r, &body)
		body.ID = f.newID("perm")
		f.permissions[body.ID] = &body
		fakeJSON(w, http.StatusCreated, map[string]any{"code": "PERMISSION_CREATED", "permission": map[string]string{"id": body.ID}})
	})
	f.route(http.MethodPatch, "/api/v1/permissions/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		permission, ok := f.permissions[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "INVALID_PERMISSION_ID", "Permission not found")
			return
		}
		var body fakePermission
		fakeBody(r, &body)
		permission.Name, permission.Key, permission.Description = body.Name, body.Key, body.Description
		fakeOK(w, http.StatusOK)
	})
	f.route(http.MethodDelete, "/api/v1/permissions/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		if _, ok := f.permissions[p[0]]; !ok {
			fakeError(w, http.StatusBadRequest, "INVALID_PERMISSION_ID", "Permission not found")
			return
		}
		delete(f.permissions, p[0])
		fakeOK(w, http.StatusOK)
	})

	f.route(http.MethodPost, "/api/v1/apis", func(w http.ResponseWriter, r *http.Request, _ []string) {
		var body fakeAPI
		fakeBody(r, &body)
		body.ID = f.newID("api")
		f.apis[body.ID] = &body
		fakeJSON(w, http.StatusOK, map[string]any{"code": "API_CREATED", "api": map[string]string{"id": body.ID}})
	})
	f.route(http.MethodGet, "/api/v1/apis/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		api, ok := f.apis[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "API_INVALID", "API not found")
			return
		}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "api": api})
	})
	f.route(http.MethodDelete, "/api/v1/apis/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		if _, ok := f.apis[p[0]]; !ok {
			fakeError(w, http.StatusBadRequest, "INVALID_API", "API not found")
			return
		}
		delete(f.apis, p[0])
		fakeOK(w, http.StatusOK)
	})

	f.route(http.MethodPost, "/api/v1/applications", func(w http.ResponseWriter, r *http.Request, _ []string) {
		var body fakeApplication
		fakeBody(r, &body)
		body.ID = f.newID("app")
		body.ClientID = body.ID + "_client"
		body.ClientSecret = body.ID + "_secret"
		f.applications[body.ID] = &body
		fakeJSON(w, http.StatusCreated, map[string]any{"code": "APPLICATION_CREATED", "application": map[string]string{
			"id": body.ID, "client_id": body.ClientID, "client_secret": body.ClientSecret,
		}})
	})
	f.route(http.MethodGet, "/api/v1/applications/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		app, ok := f.applications[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "INVALID_APP_ID", "Application not found")
			return
		}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "application": app})
	})
	f.route(http.MethodPatch, "/api/v1/applications/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		app, ok := f.applications[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "INVALID_APP_ID", "Application not found")
			return
		}
		var body fakeApplication
		fakeBody(r, &body)
		if body.LoginURI != "" {
			app.LoginURI = body.LoginURI
		}
		if body.HomepageURI != "" {
			app.HomepageURI = body.HomepageURI
		}
		if body.LogoutURIs != nil {
			app.LogoutURIs = body.LogoutURIs
		}
		if body.RedirectURIs != nil {
			app.RedirectURIs = body.RedirectURIs
		}
		w.WriteHeader(http.StatusOK)
	})
	f.route(http.MethodDelete, "/api/v1/applications/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		if _, ok := f.applications[p[0]]; !ok {
			fakeError(w, http.StatusBadRequest, "APPLICATION_ID_INVALID", "Application not found")
			return
		}
		delete(f.applications, p[0])
		delete(f.appConnections, p[0])
		fakeOK(w, http.StatusOK)
	})
	f.route(http.MethodGet, "/api/v1/applications/([^/]+)/connections", func(w http.ResponseWriter, r *http.Request, p []string) {
		var connections []fakeConnection
		for _, connection := range sortedValues(f.connections) {
			if f.appConnections[p[0]][connection.ID] {
				connections = append(connections, connection)
			}
		}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "connections": connections})
	})
	f.route(http.MethodPost, "/api/v1/applications/([^/]+)/connections/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		if f.appConnections[p[0]] == nil {
			f.appConnections[p[0]] = map[string]bool{}
		}
		f.appConnections[p[0]][p[1]] = true
		w.WriteHeader(http.StatusOK)
	})
	f.route(http.MethodDelete, "/api/v1/applications/([^/]+)/connections/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		if !f.appConnections[p[0]][p[1]] {
			fakeError(w, http.StatusBadRequest, "APPLICATION_NOT_FOUND", "Connection not enabled")
			return
		}
		delete(f.appConnections[p[0]], p[1])
		fakeOK(w, http.StatusOK)
	})

	f.route(http.MethodGet, "/api/v1/connections", func(w http.ResponseWriter, r *http.Request, _ []string) {
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "connections": sortedValues(f.connections)})
	})
	f.route(http.MethodPost, "/api/v1/connections", func(w http.ResponseWriter, r *http.Request, _ []string) {
		var body struct {
			fakeConnection
			Options map[string]any `json:"options"`
		}
		fakeBody(r, &body)
		connection := body.fakeConnection
		connection.ID = f.newID("conn")
		connection.Options = body.Options
		f.connections[connection.ID] = &connection
		fakeJSON(w, http.StatusCreated, map[string]any{"code": "CONNECTION_CREATED", "connection": map[string]string{"id": connection.ID}})
	})
	f.route(http.MethodGet, "/api/v1/connections/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		connection, ok := f.connections[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "CONNECTION_NOT_FOUND", "Connection not found")
			return
		}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "connection": connection})
	})
	f.route(http.MethodPatch, "/api/v1/connections/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		connection, ok := f.connections[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "CONNECTION_NOT_FOUND", "Connection not found")
			return
		}
		var body struct {
			Name        string         `json:"name"`
			DisplayName string         `json:"display_name"`
			Options     map[string]any `json:"options"`
		}
		fakeBody(r, &body)
		if body.Name != "" {
			connection.Name = body.Name
		}
		if body.DisplayName != "" {
			connection.DisplayName = body.DisplayName
		}
		if body.Options != nil {
			connection.Options = body.Options
		}
		fakeOK(w, http.StatusOK)
	})
	f.route(http.MethodDelete, "/api/v1/connections/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		if _, ok := f.connections[p[0]]; !ok {
			fakeError(w, http.StatusBadRequest, "CONNECTION_NOT_FOUND", "Connection not found")
			return
		}
		delete(f.connections, p[0])
		fakeOK(w, http.StatusOK)
	})

	f.route(http.MethodPost, "/api/v1/organization", func(w http.ResponseWriter, r *http.Request, _ []string) {
		var body struct {
			Name   string  `json:"name"`
			Handle *string `json:"handle"`
		}
		fakeBody(r, &body)
		code := f.newID("org")
		f.organizations[code] = &fakeOrganization{Code: code, Name: body.Name, Handle: body.Handle, CreatedOn: fakeTimestamp(), ColorScheme: "light"}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "organization": map[string]string{"code": code}})
	})
	f.route(http.MethodGet, "/api/v1/organization", func(w http.ResponseWriter, r *http.Request, _ []string) {
		organization, ok := f.organizations[r.URL.Query().Get("code")]
		if !ok {
			fakeError(w, http.StatusBadRequest, "ORGANIZATION_INVALID", "Organization not found")
			return
		}
		fakeJSON(w, http.StatusOK, organization)
	})
	f.route(http.MethodPatch, "/api/v1/organization/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		organization, ok := f.organizations[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "ORGANIZATION_INVALID", "Organization not found")
			return
		}
		var body map[string]string
		fakeBody(r, &body)
		if value, ok := body["name"]; ok {
			organization.Name = value
		}
		if value, ok := body["external_id"]; ok {
			organization.ExternalID = &value
		}
		if value, ok := body["handle"]; ok {
			organization.Handle = &value
		}
		if value, ok := body["theme_code"]; ok {
			organization.ColorScheme = value
		}
		if value, ok := body["background_color"]; ok {
			organization.BackgroundColor = fakeColorValue(value)
		}
		if value, ok := body["button_color"]; ok {
			organization.ButtonColor = fakeColorValue(value)
		}
		if value, ok := body["button_text_color"]; ok {
			organization.ButtonTextColor = fakeColorValue(value)
		}
		if value, ok := body["link_color"]; ok {
			organization.LinkColor = fakeColorValue(value)
		}
		fakeOK(w, http.StatusOK)
	})
	f.route(http.MethodDelete, "/api/v1/organization/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		if _, ok := f.organizations[p[0]]; !ok {
			fakeError(w, http.StatusBadRequest, "ORGANIZATION_INVALID", "Invalid organization")
			return
		}
		delete(f.organizations, p[0])
		delete(f.orgMembers, p[0])
		fakeOK(w, http.StatusOK)
	})

	f.route(http.MethodPost, "/api/v1/organizations/([^/]+)/users", func(w http.ResponseWriter, r *http.Request, p []string) {
		if _, ok := f.organizations[p[0]]; !ok {
			fakeError(w, http.StatusBadRequest, "ORGANIZATION_INVALID", "Organization not found")
			return
		}
		var body struct {
			Users []struct {
				ID string `json:"id"`
			} `json:"users"`
		}
		fakeBody(r, &body)
		if f.orgMembers[p[0]] == nil {
			f.orgMembers[p[0]] = map[string][]string{}
		}
		var added []string
		for _, user := range body.Users {
			if _, ok := f.orgMembers[p[0]][user.ID]; !ok {
				f.orgMembers[p[0]][user.ID] = nil
				added = append(added, user.ID)
			}
		}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "users_added": added})
	})
	f.route(http.MethodDelete, "/api/v1/organizations/([^/]+)/users/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		if _, ok := f.orgMembers[p[0]][p[1]]; !ok {
			fakeError(w, http.StatusBadRequest, "USER_NOT_IN_ORGANIZATION", "User is not in the organization")
			return
		}
		delete(f.orgMembers[p[0]], p[1])
		fakeOK(w, http.StatusOK)
	})
	f.route(http.MethodGet, "/api/v1/organizations/([^/]+)/users/([^/]+)/roles", func(w http.ResponseWriter, r *http.Request, p []string) {
		if _, ok := f.users[p[1]]; !ok {
			fakeError(w, http.StatusBadRequest, "USER_INVALID", "Invalid user id")
			return
		}
		roleIDs, ok := f.orgMembers[p[0]][p[1]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "USER_NOT_IN_ORGANIZATION", "User is not in the organization")
			return
		}
		var roles []fakeRole
		for _, roleID := range roleIDs {
			if role, ok := f.roles[roleID]; ok {
				roles = append(roles, *role)
			}
		}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "roles": roles})
	})
	f.route(http.MethodPost, "/api/v1/organizations/([^/]+)/users/([^/]+)/roles", func(w http.ResponseWriter, r *http.Request, p []string) {
		roleIDs, ok := f.orgMembers[p[0]][p[1]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "USER_NOT_IN_ORGANIZATION", "User is not in the organization")
			return
		}
		var body struct {
			RoleID string `json:"role_id"`
		}
		fakeBody(r, &body)
		if _, ok := f.roles[body.RoleID]; !ok {
			fakeError(w, http.StatusBadRequest, "ROLE_INVALID", "Role not found")
			return
		}
		f.orgMembers[p[0]][p[1]] = append(roleIDs, body.RoleID)
		fakeOK(w, http.StatusOK)
	})
	f.route(http.MethodDelete, "/api/v1/organizations/([^/]+)/users/([^/]+)/roles/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		roleIDs, ok := f.orgMembers[p[0]][p[1]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "USER_NOT_IN_ORGANIZATION", "User is not in the organization")
			return
		}
		var kept []string
		for _, roleID := range roleIDs {
			if roleID != p[2] {
				kept = append(kept, roleID)
			}
		}
		f.orgMembers[p[0]][p[1]] = kept
		fakeOK(w, http.StatusOK)
	})

	f.route(http.MethodPost, "/api/v1/roles", func(w http.ResponseWriter, r *http.Request, _ []string) {
		var body fakeRole
		fakeBody(r, &body)
		body.ID = f.newID("role")
		f.roles[body.ID] = &body
		fakeJSON(w, http.StatusCreated, map[string]any{"code": "ROLE_CREATED", "role": map[string]string{"id": body.ID}})
	})
	f.route(http.MethodGet, "/api/v1/roles/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		role, ok := f.roles[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "ROLE_ID_INVALID", "Role not found")
			return
		}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "role": role})
	})
	f.route(http.MethodPatch, "/api/v1/roles/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		role, ok := f.roles[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "ROLE_ID_INVALID", "Role not found")
			return
		}
		var body fakeRole
		fakeBody(r, &body)
		role.Name, role.Key, role.Description = body.Name, body.Key, body.Description
		fakeOK(w, http.StatusCreated)
	})
	f.route(http.MethodDelete, "/api/v1/roles/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		if _, ok := f.roles[p[0]]; !ok {
			fakeError(w, http.StatusBadRequest, "INVALID_ROLE_ID", "Role not found")
			return
		}
		delete(f.roles, p[0])
		fakeOK(w, http.StatusOK)
	})
	f.route(http.MethodGet, "/api/v1/roles/([^/]+)/permissions", func(w http.ResponseWriter, r *http.Request, p []string) {
		role, ok := f.roles[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "ROLE_ID_INVALID", "Role not found")
			return
		}
		var permissions []fakePermission
		for _, permissionID := range role.Permissions {
			permissions = append(permissions, fakePermission{ID: permissionID})
		}
		page, next := fakePage(r, permissions)
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "permissions": page, "next_token": next})
	})
	f.route(http.MethodPatch, "/api/v1/roles/([^/]+)/permissions", func(w http.ResponseWriter, r *http.Request, p []string) {
		role, ok := f.roles[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "ROLE_ID_INVALID", "Role not found")
			return
		}
		var body struct {
			Permissions []struct {
				ID string `json:"id"`
			} `json:"permissions"`
		}
		fakeBody(r, &body)
		for _, permission := range body.Permissions {
			if _, ok := f.permissions[permission.ID]; !ok {
				fakeError(w, http.StatusBadRequest, "INVALID_PERMISSION_ID", "Permission not found")
				return
			}
			role.Permissions = append(role.Permissions, permission.ID)
		}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "permissions_added": nil, "permissions_removed": nil})
	})
	f.route(http.MethodDelete, "/api/v1/roles/([^/]+)/permissions/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		role, ok := f.roles[p[0]]
		if !ok {
			fakeError(w, http.StatusBadRequest, "INVALID_ROLE_ID", "Role not found")
			return
		}
		var kept []string
		for _, permissionID := range role.Permissions {
			if permissionID != p[1] {
				kept = append(kept, permissionID)
			}
		}
		role.Permissions = kept
		fakeOK(w, http.StatusOK)
	})

	f.route(http.MethodPost, "/api/v1/user", func(w http.ResponseWriter, r *http.Request, _ []string) {
		var body struct {
			Profile struct {
				GivenName  string `json:"given_name"`
				FamilyName string `json:"family_name"`
			} `json:"profile"`
			Identities []struct {
				Type    string            `json:"type"`
				Details map[string]string `json:"details"`
			} `json:"identities"`
		}
		fakeBody(r, &body)
		now := fakeTimestamp()
		user := &fakeUser{ID: f.newID("kp"), FirstName: body.Profile.GivenName, LastName: body.Profile.FamilyName, CreatedOn: now, UpdatedOn: now}
		f.users[user.ID] = user
		for _, identity := range body.Identities {
			id := f.newID("identity")
			f.identities[id] = &fakeIdentity{ID: id, Type: identity.Type, Name: identity.Details[identity.Type], UserID: user.ID}
		}
		fakeJSON(w, http.StatusOK, map[string]any{"id": user.ID, "created": true, "identities": nil})
	})
	f.route(http.MethodGet, "/api/v1/user", func(w http.ResponseWriter, r *http.Request, _ []string) {
		user, ok := f.users[r.URL.Query().Get("id")]
		if !ok {
			fakeError(w, http.StatusBadRequest, "USER_INVALID", "User not found")
			return
		}
		fakeJSON(w, http.StatusOK, user)
	})
	f.route(http.MethodPatch, "/api/v1/user", func(w http.ResponseWriter, r *http.Request, _ []string) {
		user, ok := f.users[r.URL.Query().Get("id")]
		if !ok {
			fakeError(w, http.StatusBadRequest, "USER_INVALID", "User not found")
			return
		}
		var body struct {
			GivenName   string `json:"given_name"`
			FamilyName  string `json:"family_name"`
			IsSuspended *bool  `json:"is_suspended"`
		}
		fakeBody(r, &body)
		if body.GivenName != "" {
			user.FirstName = body.GivenName
		}
		if body.FamilyName != "" {
			user.LastName = body.FamilyName
		}
		if body.IsSuspended != nil {
			user.IsSuspended = *body.IsSuspended
		}
		user.UpdatedOn = fakeTimestamp()
		fakeJSON(w, http.StatusOK, map[string]any{"id": user.ID, "given_name": user.FirstName, "family_name": user.LastName})
	})
	f.route(http.MethodDelete, "/api/v1/user", func(w http.ResponseWriter, r *http.Request, _ []string) {
		id := r.URL.Query().Get("id")
		if _, ok := f.users[id]; !ok {
			fakeError(w, http.StatusBadRequest, "USER_INVALID", "User not found")
			return
		}
		delete(f.users, id)
		for identityID, identity := range f.identities {
			if identity.UserID == id {
				delete(f.identities, identityID)
			}
		}
		fakeOK(w, http.StatusOK)
	})
	f.route(http.MethodGet, "/api/v1/users/([^/]+)/identities", func(w http.ResponseWriter, r *http.Request, p []string) {
		var identities []fakeIdentity
		for _, identity := range sortedValues(f.identities) {
			if identity.UserID == p[0] {
				identities = append(identities, identity)
			}
		}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "identities": identities})
	})
	f.route(http.MethodPost, "/api/v1/users/([^/]+)/identities", func(w http.ResponseWriter, r *http.Request, p []string) {
		var body struct {
			Value          string `json:"value"`
			Type           string `json:"type"`
			PhoneCountryID string `json:"phone_country_id"`
		}
		fakeBody(r, &body)
		name := body.Value
		if body.Type == identityTypePhone {
			formatted, err := formatPhoneNumber(body.Value, body.PhoneCountryID)
			if err != nil {
				fakeError(w, http.StatusBadRequest, "INVALID_PHONE", err.Error())
				return
			}
			name = formatted
		}
		id := f.newID("identity")
		f.identities[id] = &fakeIdentity{ID: id, Type: body.Type, Name: name, UserID: p[0]}
		fakeJSON(w, http.StatusOK, map[string]any{"code": "OK", "identity": map[string]string{"id": id}})
	})
	f.route(http.MethodDelete, "/api/v1/identities/([^/]+)", func(w http.ResponseWriter, r *http.Request, p []string) {
		if _, ok := f.identities[p[0]]; !ok {
			fakeError(w, http.StatusBadRequest, "IDENTITY_NOT_FOUND", "Identity not found")
			return
		}
		delete(f.identities, p[0])
		fakeOK(w, http.StatusOK)
	})
}

func (f *fakeKinde) do(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fakeKinde) check(fn func() error) func(*terraformState) error {
	return func(*terraformState) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		return fn()
	}
}

func offlineNotFoundStep(config string, remove func()) resource.TestStep {
	return resource.TestStep{
		PreConfig:          remove,
		Config:             config,
		PlanOnly:           true,
		ExpectNonEmptyPlan: true,
	}
}

func stateAttr(state *terraformState, address, attribute string) (string, error) {
	rs, ok := state.RootModule().Resources[address]
	if !ok {
		return "", fmt.Errorf("%s not found in state", address)
	}
	return rs.Primary.Attributes[attribute], nil
}
