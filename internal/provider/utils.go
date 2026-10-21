// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/kinde-oss/kinde-go/kinde/management_api"
)

// Package provider contains the provider implementation.

// splitID splits a colon-separated ID into its parts and validates the number of parts.
func splitID(id string, expectedParts int, format string) ([]string, error) {
	parts := strings.Split(id, ":")
	if len(parts) != expectedParts {
		return nil, fmt.Errorf("invalid ID format. Expected format: %s", format)
	}

	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("invalid ID format. Expected format: %s", format)
		}
	}

	return parts, nil
}

func isNotFoundError(err error) bool {
	return hasStatusCode(err, http.StatusNotFound)
}

func isUserNotInOrganizationError(err error) bool {
	return containsAnyErrorCode(err, "USER_NOT_IN_ORGANIZATION")
}

func containsAnyErrorCode(err error, codes ...string) bool {
	if err == nil {
		return false
	}

	message := strings.ToUpper(err.Error())
	for _, code := range codes {
		if strings.Contains(message, strings.ToUpper(code)) {
			return true
		}
	}

	return false
}

func hasStatusCode(err error, code int) bool {
	for current := err; current != nil; current = errors.Unwrap(current) {
		status, ok := statusCodeFromError(current)
		if ok && status == code {
			return true
		}
	}
	return false
}

func statusCodeFromError(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	val := reflect.ValueOf(err)
	if !val.IsValid() {
		return 0, false
	}
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return 0, false
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return 0, false
	}
	field := val.FieldByName("StatusCode")
	if !field.IsValid() {
		return 0, false
	}
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(field.Int()), true
	default:
		return 0, false
	}
}

func managementAPIError(operation string, res any) error {
	val := reflect.ValueOf(res)
	if val.Kind() == reflect.Pointer && !val.IsNil() {
		val = val.Elem()
	}

	if val.Kind() == reflect.Struct {
		if field := val.FieldByName("Errors"); field.IsValid() {
			if apiErrs, ok := field.Interface().([]management_api.Error); ok && len(apiErrs) > 0 {
				messages := make([]string, 0, len(apiErrs))
				for _, apiErr := range apiErrs {
					messages = append(messages, fmt.Sprintf("%s: %s", apiErr.Code.Or("UNKNOWN"), apiErr.Message.Or("")))
				}
				return fmt.Errorf("%s failed (%T): %s", operation, res, strings.Join(messages, "; "))
			}
		}
	}

	return fmt.Errorf("%s failed: unexpected response %T", operation, res)
}
