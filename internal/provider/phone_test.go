// Copyright (c) Kinde Australia Pty Ltd
// SPDX-License-Identifier: MPL-2.0

package provider

import "testing"

func TestParsePhoneNumber(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantLocal     string
		wantCountryID string
		wantErr       bool
	}{
		{name: "Armenian number", input: "+37455251234", wantLocal: "55251234", wantCountryID: "am"},
		{name: "Australian number", input: "+61412345678", wantLocal: "412345678", wantCountryID: "au"},
		{name: "US number", input: "+12025550123", wantLocal: "2025550123", wantCountryID: "us"},
		{name: "Invalid number", input: "+1234", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLocal, gotCountryID, err := parsePhoneNumber(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %s", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if gotLocal != tt.wantLocal || gotCountryID != tt.wantCountryID {
				t.Fatalf("got (%s, %s), want (%s, %s)", gotLocal, gotCountryID, tt.wantLocal, tt.wantCountryID)
			}
		})
	}
}

func TestFormatPhoneNumber(t *testing.T) {
	tests := []struct {
		name        string
		localNumber string
		countryID   string
		want        string
		wantErr     bool
	}{
		{name: "Armenian number", localNumber: "55251234", countryID: "am", want: "+37455251234"},
		{name: "Australian number", localNumber: "412345678", countryID: "au", want: "+61412345678"},
		{name: "US number", localNumber: "2025550123", countryID: "us", want: "+12025550123"},
		{name: "Full number without a country", localNumber: "+61412345678", want: "+61412345678"},
		{name: "Invalid number", localNumber: "123", countryID: "us", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatPhoneNumber(tt.localNumber, tt.countryID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %s", tt.localNumber)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}
			if got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}
