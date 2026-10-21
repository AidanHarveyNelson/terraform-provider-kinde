// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
)

const defaultPageSize = 100

// getAllPages walks every page of a paginated Kinde Management API endpoint,
// following next_token until the API stops returning one. The Kinde
// Management API silently returns only the first page when next_token is
// not followed, so callers must not assume a single request is sufficient
// for any listing endpoint.
func getAllPages[T any](ctx context.Context, fetchPage func(ctx context.Context, nextToken string) ([]T, string, error)) ([]T, error) {
	var results []T
	nextToken := ""
	for {
		page, next, err := fetchPage(ctx, nextToken)
		if err != nil {
			return nil, err
		}

		results = append(results, page...)

		if next == "" {
			return results, nil
		}
		nextToken = next
	}
}
