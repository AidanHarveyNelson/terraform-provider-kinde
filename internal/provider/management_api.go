// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/kinde-oss/kinde-go/oauth2/client_credentials"
	"golang.org/x/oauth2"
)

type ManagementAPI struct {
	*management_api.Client

	httpClient *http.Client
	baseURL    string
}

type managementConfig struct {
	Domain       string
	Audience     string
	ClientID     string
	ClientSecret string
	Scopes       string
}

func managementConfigFromEnv() managementConfig {
	return managementConfig{
		Domain:       os.Getenv("KINDE_DOMAIN"),
		Audience:     os.Getenv("KINDE_AUDIENCE"),
		ClientID:     os.Getenv("KINDE_CLIENT_ID"),
		ClientSecret: os.Getenv("KINDE_CLIENT_SECRET"),
		Scopes:       os.Getenv("KINDE_SCOPES"),
	}
}

func (c managementConfig) validate() error {
	var missing []string
	if c.Domain == "" {
		missing = append(missing, "domain")
	}
	if c.Audience == "" {
		missing = append(missing, "audience")
	}
	if c.ClientID == "" {
		missing = append(missing, "client_id")
	}
	if c.ClientSecret == "" {
		missing = append(missing, "client_secret")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required Kinde client options: %s", strings.Join(missing, ", "))
	}
	return nil
}

func newManagementClient(ctx context.Context, opts managementConfig) (*ManagementAPI, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	options := []client_credentials.Option{
		client_credentials.WithAudience(opts.Audience),
		client_credentials.WithTokenValidation(true),
	}
	if scopes := strings.TrimSpace(opts.Scopes); scopes != "" {
		options = append(options, client_credentials.WithAuthParameter("scope", scopes))
	}

	flow, err := client_credentials.NewClientCredentialsFlow(opts.Domain, opts.ClientID, opts.ClientSecret, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create client credentials flow: %w", err)
	}

	tokens := &managementTokenSource{ctx: ctx, flow: flow}

	client, err := management_api.NewClient(opts.Domain, tokens, management_api.WithClient(&http.Client{}))
	if err != nil {
		return nil, fmt.Errorf("failed to create management API client: %w", err)
	}

	return &ManagementAPI{Client: client, httpClient: oauth2.NewClient(ctx, tokens), baseURL: opts.Domain}, nil
}

type managementTokenSource struct {
	mu   sync.Mutex
	ctx  context.Context
	flow client_credentials.IClientCredentialsFlow
}

func (s *managementTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, err := s.flow.GetToken(s.ctx)
	if err != nil {
		return nil, err
	}

	rawToken := token.GetRawToken()
	if rawToken == nil {
		return nil, fmt.Errorf("access token is empty")
	}
	tokenCopy := *rawToken
	return &tokenCopy, nil
}

func (s *managementTokenSource) KindeBearerAuth(_ context.Context, _ management_api.OperationName) (management_api.KindeBearerAuth, error) {
	token, err := s.Token()
	if err != nil {
		return management_api.KindeBearerAuth{}, err
	}
	return management_api.KindeBearerAuth{Token: token.AccessToken}, nil
}

func (m *ManagementAPI) getJSON(ctx context.Context, path string, out any) error {
	return m.doJSON(ctx, http.MethodGet, path, nil, out)
}

func (m *ManagementAPI) doJSON(ctx context.Context, method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(m.baseURL, "/")+path, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("%s %s: failed to read response: %w", method, path, err)
	}

	var apiErr struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &apiErr) == nil && len(apiErr.Errors) > 0 {
		messages := make([]string, 0, len(apiErr.Errors))
		for _, e := range apiErr.Errors {
			messages = append(messages, fmt.Sprintf("%s: %s", e.Code, e.Message))
		}
		return &managementRequestError{
			StatusCode: res.StatusCode,
			Message:    fmt.Sprintf("%s %s failed (status %d): %s", method, path, res.StatusCode, strings.Join(messages, "; ")),
		}
	}

	if res.StatusCode >= http.StatusBadRequest {
		return &managementRequestError{
			StatusCode: res.StatusCode,
			Message:    fmt.Sprintf("%s %s failed (status %d): %s", method, path, res.StatusCode, string(raw)),
		}
	}

	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s: failed to decode response: %w", method, path, err)
	}
	return nil
}

type managementRequestError struct {
	StatusCode int
	Message    string
}

func (e *managementRequestError) Error() string {
	return e.Message
}

var responseStatusSuffixes = map[string]int{
	"BadRequest":       http.StatusBadRequest,
	"Unauthorized":     http.StatusUnauthorized,
	"Forbidden":        http.StatusForbidden,
	"NotFound":         http.StatusNotFound,
	"NotFoundResponse": http.StatusNotFound,
	"Conflict":         http.StatusConflict,
	"TooManyRequests":  http.StatusTooManyRequests,
	"ErrorResponse":    http.StatusBadRequest,
}

func responseStatusCode(res any) int {
	name := reflect.TypeOf(res).String()
	for suffix, status := range responseStatusSuffixes {
		if strings.HasSuffix(name, suffix) {
			return status
		}
	}
	return 0
}

func expectResponse[T any](operation string, res any, err error) (*T, error) {
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	if typed, ok := res.(*T); ok {
		return typed, nil
	}
	return nil, managementAPIError(operation, res)
}
