// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

type ManagementAPI struct {
	*management_api.Client

	httpClient *http.Client
	baseURL    string
}

func (m *ManagementAPI) getJSON(ctx context.Context, path string, out any) error {
	return m.requestJSON(ctx, http.MethodGet, path, nil, out)
}

func (m *ManagementAPI) requestJSON(ctx context.Context, method, path string, query url.Values, out any) error {
	target := strings.TrimSuffix(m.baseURL, "/") + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	res, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("%s %s: failed to read response: %w", method, path, err)
	}

	if res.StatusCode >= http.StatusBadRequest {
		var apiErr struct {
			Errors []struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if json.Unmarshal(body, &apiErr) == nil && len(apiErr.Errors) > 0 {
			messages := make([]string, 0, len(apiErr.Errors))
			for _, e := range apiErr.Errors {
				messages = append(messages, fmt.Sprintf("%s: %s", e.Code, e.Message))
			}
			return fmt.Errorf("%s %s failed (status %d): %s", method, path, res.StatusCode, strings.Join(messages, "; "))
		}
		return fmt.Errorf("%s %s failed (status %d): %s", method, path, res.StatusCode, string(body))
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s %s: failed to decode response: %w", method, path, err)
	}
	return nil
}
