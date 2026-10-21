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
	"net/url"
	"sync"
	"testing"
	"time"

	golangjwt "github.com/golang-jwt/jwt/v5"
)

const stubFallbackRoute = "*"

type stubManagementServer struct {
	*httptest.Server
	mu         sync.Mutex
	counts     map[string]int
	tokenForms []url.Values
}

func (s *stubManagementServer) requests(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[path]
}

func (s *stubManagementServer) tokenRequests() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.tokenForms...)
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
			if err := r.ParseForm(); err == nil {
				stub.mu.Lock()
				stub.tokenForms = append(stub.tokenForms, r.PostForm)
				stub.mu.Unlock()
			}
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
				handler, ok = handlers[stubFallbackRoute]
			}
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
	t.Setenv("KINDE_SCOPES", "")

	return stub
}
