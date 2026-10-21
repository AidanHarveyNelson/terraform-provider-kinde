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
	"reflect"
	"strings"

	"github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/kinde-oss/kinde-go/oauth2/client_credentials"
	"github.com/nxt-fwd/kinde-go"
)

type ManagementAPI struct {
	*management_api.Client

	httpClient *http.Client
	baseURL    string
}

func newManagementClient(ctx context.Context, opts *kinde.ClientOptions) (*ManagementAPI, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	audience := client_credentials.WithKindeManagementAPI(opts.Domain)
	if opts.Audience != "" {
		audience = client_credentials.WithAudience(opts.Audience)
	}

	flow, err := client_credentials.NewClientCredentialsFlow(
		opts.Domain, opts.ClientID, opts.ClientSecret,
		audience,
		client_credentials.WithTokenValidation(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client credentials flow: %w", err)
	}

	httpClient, err := flow.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create management API HTTP client: %w", err)
	}

	client, err := management_api.NewClient(opts.Domain, managementSecuritySource{flow: flow}, management_api.WithClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create management API client: %w", err)
	}

	return &ManagementAPI{Client: client, httpClient: httpClient, baseURL: opts.Domain}, nil
}

type managementSecuritySource struct {
	flow client_credentials.IClientCredentialsFlow
}

func (s managementSecuritySource) KindeBearerAuth(ctx context.Context, _ management_api.OperationName) (management_api.KindeBearerAuth, error) {
	token, err := s.flow.GetToken(ctx)
	if err != nil {
		return management_api.KindeBearerAuth{}, err
	}

	rawToken := token.GetRawToken()
	if rawToken == nil {
		return management_api.KindeBearerAuth{}, fmt.Errorf("access token is empty")
	}

	return management_api.KindeBearerAuth{Token: rawToken.AccessToken}, nil
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
	"BadRequest":      http.StatusBadRequest,
	"Unauthorized":    http.StatusUnauthorized,
	"Forbidden":       http.StatusForbidden,
	"NotFound":        http.StatusNotFound,
	"Conflict":        http.StatusConflict,
	"TooManyRequests": http.StatusTooManyRequests,
	"ErrorResponse":   http.StatusBadRequest,
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
