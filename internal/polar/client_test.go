// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package polar

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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
	testOrgID := "test-org-123"
	client := NewClient(WithOrganizationID(testOrgID))

	assert.Equal(t, testOrgID, client.organizationID)
}

func TestIsClientConfigured(t *testing.T) {
	tests := []struct {
		name     string
		orgID    string
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
			client := NewClient(WithOrganizationID(tt.orgID))

			result := client.IsClientConfigured()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateLicense_NoOrgID(t *testing.T) {
	client := NewClient()
	// Don't set organization ID

	result, err := client.Validate(context.Background(), ValidateRequest{})
	assert.ErrorIs(t, err, ErrBadRequestData)
	assert.Nil(t, result)
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
			assert.Equal(t, tt.expected, result)
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
			assert.Equal(t, tt.expected, result)
		})
	}
}
