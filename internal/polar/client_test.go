// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package polar

import (
	"context"
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}

	if client.httpClient == nil {
		t.Error("HTTP client not initialized")
	}

	if client.httpClient.Timeout != requestTimeout {
		t.Errorf("HTTP client timeout = %v, want %v", client.httpClient.Timeout, requestTimeout)
	}

	if client.organizationID != "" {
		t.Error("Organization ID should be empty initially")
	}
}

func TestSetOrganizationID(t *testing.T) {
	client := NewClient()
	testOrgID := "test-org-123"

	client.SetOrganizationID(testOrgID)

	if client.organizationID != testOrgID {
		t.Errorf("Organization ID = %v, want %v", client.organizationID, testOrgID)
	}
}

func TestIsClientConfigured(t *testing.T) {
	tests := []struct {
		name   string
		orgID  string
		expected bool
	}{
		{
			name:     "empty org ID returns false",
			orgID:    "",
			expected: false,
		},
		{
			name:     "non-empty org ID returns true",
			orgID:    "test-org",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient()
			client.SetOrganizationID(tt.orgID)

			result := client.IsClientConfigured()
			if result != tt.expected {
				t.Errorf("IsClientConfigured() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestValidateConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		orgID     string
		wantError bool
	}{
		{
			name:      "empty org ID returns error",
			orgID:     "",
			wantError: true,
		},
		{
			name:      "valid org ID returns no error",
			orgID:     "test-org",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient()
			client.SetOrganizationID(tt.orgID)

			err := client.ValidateConfiguration(context.Background())
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateConfiguration() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestValidateLicense_NoOrgID(t *testing.T) {
	client := NewClient()
	// Don't set organization ID

	result, err := client.ValidateLicense(context.Background(), "test-license")
	if err != nil {
		t.Errorf("ValidateLicense() error = %v, want nil", err)
	}

	if result == nil {
		t.Fatal("ValidateLicense() returned nil result")
	}

	if result.Valid {
		t.Error("License should be invalid when org ID not configured")
	}

	if result.ErrorMessage != orgIDNotConfigMsg {
		t.Errorf("Error message = %v, want %v", result.ErrorMessage, orgIDNotConfigMsg)
	}
}

func TestActivateLicense_NoOrgID(t *testing.T) {
	client := NewClient()
	// Don't set organization ID

	result, err := client.ActivateLicense(context.Background(), "test-license")
	if err != nil {
		t.Errorf("ActivateLicense() error = %v, want nil", err)
	}

	if result == nil {
		t.Fatal("ActivateLicense() returned nil result")
	}

	if result.Valid {
		t.Error("License should be invalid when org ID not configured")
	}

	if result.ErrorMessage != orgIDNotConfigMsg {
		t.Errorf("Error message = %v, want %v", result.ErrorMessage, orgIDNotConfigMsg)
	}
}

func TestMapBenefitToTheme(t *testing.T) {
	tests := []struct {
		name      string
		benefitID string
		operation string
		expected  string
	}{
		{
			name:      "empty benefit ID returns unknown",
			benefitID: "",
			operation: "validation",
			expected:  unknownThemeName,
		},
		{
			name:      "non-empty benefit ID returns premium",
			benefitID: "benefit-123",
			operation: "activation",
			expected:  premiumThemeName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient()
			result := client.mapBenefitToTheme(tt.benefitID, tt.operation)
			if result != tt.expected {
				t.Errorf("mapBenefitToTheme() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMaskLicenseKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "short key returns stars",
			key:      "123",
			expected: "***",
		},
		{
			name:     "8 char key returns stars",
			key:      "12345678",
			expected: "***",
		},
		{
			name:     "long key returns first 8 plus stars",
			key:      "123456789012345",
			expected: "12345678***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskLicenseKey(tt.key)
			if result != tt.expected {
				t.Errorf("maskLicenseKey() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMaskID(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected string
	}{
		{
			name:     "short ID returns stars",
			id:       "abc",
			expected: "***",
		},
		{
			name:     "8 char ID returns stars",
			id:       "abcdefgh",
			expected: "***",
		},
		{
			name:     "long ID returns first 8 plus stars",
			id:       "abcdefghijklmnop",
			expected: "abcdefgh***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskID(tt.id)
			if result != tt.expected {
				t.Errorf("maskID() = %v, want %v", result, tt.expected)
			}
		})
	}
}
