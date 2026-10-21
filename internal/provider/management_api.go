// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

type ManagementAPI struct {
	*management_api.Client

	httpClient *http.Client
	baseURL    string
}

func (m *ManagementAPI) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(m.baseURL, "/")+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	res, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("GET %s: failed to read response: %w", path, err)
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
			return fmt.Errorf("GET %s failed (status %d): %s", path, res.StatusCode, strings.Join(messages, "; "))
		}
		return fmt.Errorf("GET %s failed (status %d): %s", path, res.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("GET %s: failed to decode response: %w", path, err)
	}
	return nil
}
