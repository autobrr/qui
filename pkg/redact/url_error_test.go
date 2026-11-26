// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package redact

import (
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURLError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantContain []string
		wantNotHave []string
	}{
		{
			name:        "nil error",
			err:         nil,
			wantContain: nil,
			wantNotHave: nil,
		},
		{
			name: "url.Error with apikey",
			err: &url.Error{
				Op:  "Get",
				URL: "http://example.com/api?apikey=SECRET123&t=caps",
				Err: errors.New("connection refused"),
			},
			wantContain: []string{"REDACTED", "connection refused"},
			wantNotHave: []string{"SECRET123"},
		},
		{
			name:        "non-url.Error unchanged",
			err:         errors.New("simple error"),
			wantContain: []string{"simple error"},
			wantNotHave: nil,
		},
		{
			name:        "wrapped url.Error",
			err:         fmt.Errorf("wrapped: %w", &url.Error{Op: "Get", URL: "http://x.com?apikey=SECRET", Err: errors.New("fail")}),
			wantContain: []string{"REDACTED"},
			wantNotHave: []string{"SECRET"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := URLError(tt.err)

			if tt.err == nil {
				assert.NoError(t, result)
				return
			}

			gotStr := result.Error()
			for _, want := range tt.wantContain {
				assert.Contains(t, gotStr, want)
			}
			for _, notWant := range tt.wantNotHave {
				assert.NotContains(t, gotStr, notWant)
			}
		})
	}
}

func TestURLError_PreservesErrorType(t *testing.T) {
	original := &url.Error{
		Op:  "Get",
		URL: "http://x.com?apikey=SECRET",
		Err: errors.New("connection refused"),
	}

	result := URLError(original)

	var urlErr *url.Error
	require.ErrorAs(t, result, &urlErr, "URLError() should preserve url.Error type")
	assert.Equal(t, "Get", urlErr.Op)
	assert.NotContains(t, urlErr.URL, "SECRET")
}
