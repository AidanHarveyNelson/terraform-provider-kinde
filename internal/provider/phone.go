// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"strings"

	"github.com/nyaruka/phonenumbers"
)

func parsePhoneNumber(fullNumber string) (localNumber, countryID string, err error) {
	num, err := phonenumbers.Parse(fullNumber, "")
	if err != nil {
		return "", "", fmt.Errorf("invalid phone number format: %w", err)
	}

	if !phonenumbers.IsValidNumber(num) {
		return "", "", fmt.Errorf("invalid phone number")
	}

	countryID = strings.ToLower(phonenumbers.GetRegionCodeForNumber(num))
	localNumber = fmt.Sprint(num.GetNationalNumber())

	return localNumber, countryID, nil
}

func formatPhoneNumber(localNumber, countryID string) (string, error) {
	num, err := phonenumbers.Parse(localNumber, strings.ToUpper(countryID))
	if err != nil {
		return "", fmt.Errorf("invalid phone number format: %w", err)
	}

	if !phonenumbers.IsValidNumber(num) {
		return "", fmt.Errorf("invalid phone number")
	}

	formatted := phonenumbers.Format(num, phonenumbers.INTERNATIONAL)
	formatted = strings.ReplaceAll(formatted, " ", "")
	formatted = strings.ReplaceAll(formatted, "-", "")
	if !strings.HasPrefix(formatted, "+") {
		formatted = "+" + formatted
	}

	return formatted, nil
}
