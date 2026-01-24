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
			name: "url.Error with token",
			err: &url.Error{
				Op:  "Get",
				URL: "http://example.com?token=TOKENVALUE",
				Err: errors.New("timeout"),
			},
			wantContain: []string{"REDACTED", "timeout"},
			wantNotHave: []string{"TOKENVALUE"},
		},
		{
			name: "url.Error with multiple sensitive params",
			err: &url.Error{
				Op:  "Get",
				URL: "http://x.com?apikey=KEY1&passkey=KEY2&token=KEY3",
				Err: errors.New("error"),
			},
			wantContain: []string{"apikey=REDACTED", "passkey=REDACTED", "token=REDACTED"},
			wantNotHave: []string{"KEY1", "KEY2", "KEY3"},
		},
		{
			name: "url.Error with password",
			err: &url.Error{
				Op:  "Post",
				URL: "http://tracker.example.com?password=MYPASS",
				Err: errors.New("denied"),
			},
			wantContain: []string{"REDACTED"},
			wantNotHave: []string{"MYPASS"},
		},
		{
			name: "url.Error with api_key",
			err: &url.Error{
				Op:  "Get",
				URL: "http://api.example.com?api_key=SECRETKEY",
				Err: errors.New("failed"),
			},
			wantContain: []string{"api_key=REDACTED"},
			wantNotHave: []string{"SECRETKEY"},
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
				assert.Nil(t, result)
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
	require.True(t, errors.As(result, &urlErr), "URLError() should preserve url.Error type")
	assert.Equal(t, "Get", urlErr.Op)
	assert.NotContains(t, urlErr.URL, "SECRET")
}
