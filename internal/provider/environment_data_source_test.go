// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	golangjwt "github.com/golang-jwt/jwt/v5"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

func TestAccEnvironmentDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "kinde_environment" "test" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.kinde_environment.test", "code"),
					resource.TestCheckResourceAttrSet("data.kinde_environment.test", "name"),
					resource.TestCheckResourceAttrSet("data.kinde_environment.test", "is_default"),
					resource.TestCheckResourceAttrSet("data.kinde_environment.test", "kinde_domain"),
				),
			},
		},
	})
}

func TestEnvironmentDataSourceAgainstStubServer(t *testing.T) {
	server := newStubManagementServer(t, map[string]http.HandlerFunc{
		"/api/v1/environment": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    "OK",
				"message": "success_response",
				"environment": map[string]any{
					"code":          "production",
					"name":          "Production",
					"is_default":    true,
					"is_live":       true,
					"kinde_domain":  "example.kinde.com",
					"custom_domain": nil,
					"theme_code":    "user_preference",
					"created_on":    "2026-01-01T00:00:00+00:00",
				},
			})
		},
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "kinde_environment" "test" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.kinde_environment.test", "code", "production"),
					resource.TestCheckResourceAttr("data.kinde_environment.test", "name", "Production"),
					resource.TestCheckResourceAttr("data.kinde_environment.test", "is_default", "true"),
					resource.TestCheckResourceAttr("data.kinde_environment.test", "is_live", "true"),
					resource.TestCheckResourceAttr("data.kinde_environment.test", "kinde_domain", "example.kinde.com"),
					resource.TestCheckResourceAttr("data.kinde_environment.test", "theme_code", "user_preference"),
					resource.TestCheckNoResourceAttr("data.kinde_environment.test", "custom_domain"),
				),
			},
		},
	})

	if got := server.requests("/api/v1/environment"); got == 0 {
		t.Fatal("expected the data source to call /api/v1/environment")
	}
}

func TestEnvironmentDataSourceReportsAPIErrors(t *testing.T) {
	newStubManagementServer(t, map[string]http.HandlerFunc{
		"/api/v1/environment": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{{"code": "FORBIDDEN", "message": "Missing scope read:environments"}},
			})
		},
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      `data "kinde_environment" "test" {}`,
				ExpectError: regexp.MustCompile(`FORBIDDEN:\s+Missing scope`),
			},
		},
	})
}

func TestManagementAPIError(t *testing.T) {
	res := &management_api.GetEnvironmentBadRequest{
		Errors: []management_api.Error{
			{Code: management_api.NewOptString("INVALID"), Message: management_api.NewOptString("Bad input")},
		},
	}

	err := managementAPIError("get environment", res)
	if !strings.Contains(err.Error(), "INVALID: Bad input") {
		t.Fatalf("expected error to include the API error, got %q", err)
	}

	err = managementAPIError("get environment", &management_api.GetEnvironmentResponse{})
	if !strings.Contains(err.Error(), "unexpected response") {
		t.Fatalf("expected an unexpected response error, got %q", err)
	}
}

type stubManagementServer struct {
	*httptest.Server
	mu     sync.Mutex
	counts map[string]int
}

func (s *stubManagementServer) requests(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[path]
}

func newStubManagementServer(t *testing.T, handlers map[string]http.HandlerFunc) *stubManagementServer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %s", err)
	}
	const keyID = "test-key"

	stub := &stubManagementServer{counts: map[string]int{}}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stub.mu.Lock()
		stub.counts[r.URL.Path]++
		stub.mu.Unlock()

		switch r.URL.Path {
		case "/oauth2/token":
			token := golangjwt.NewWithClaims(golangjwt.SigningMethodRS256, golangjwt.MapClaims{
				"aud": []string{stub.URL + "/api"},
				"iss": stub.URL,
				"exp": time.Now().Add(time.Hour).Unix(),
			})
			token.Header["kid"] = keyID
			signed, err := token.SignedString(key)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": signed,
				"expires_in":   3600,
				"token_type":   "bearer",
			})
		case "/.well-known/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]string{{
					"kty": "RSA",
					"kid": keyID,
					"alg": "RS256",
					"use": "sig",
					"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
				}},
			})
		default:
			handler, ok := handlers[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			handler(w, r)
		}
	}))
	t.Cleanup(stub.Close)

	t.Setenv("KINDE_DOMAIN", stub.URL)
	t.Setenv("KINDE_AUDIENCE", stub.URL+"/api")
	t.Setenv("KINDE_CLIENT_ID", "test-client-id")
	t.Setenv("KINDE_CLIENT_SECRET", "test-client-secret")

	return stub
}
