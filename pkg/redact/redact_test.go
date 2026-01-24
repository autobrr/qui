// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestURLString(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantContain []string
		wantNotHave []string
	}{
		{
			name:        "empty string",
			input:       "",
			wantContain: nil,
			wantNotHave: nil,
		},
		{
			name:        "URL with apikey",
			input:       "http://example.com/api?apikey=SECRET123&other=value",
			wantContain: []string{"apikey=REDACTED", "other=value"},
			wantNotHave: []string{"SECRET123"},
		},
		{
			name:        "URL with api_key",
			input:       "http://example.com/api?api_key=SECRET456",
			wantContain: []string{"api_key=REDACTED"},
			wantNotHave: []string{"SECRET456"},
		},
		{
			name:        "URL with passkey",
			input:       "http://example.com/api?passkey=MYPASSKEY",
			wantContain: []string{"passkey=REDACTED"},
			wantNotHave: []string{"MYPASSKEY"},
		},
		{
			name:        "URL with token",
			input:       "http://example.com/api?token=MYTOKEN",
			wantContain: []string{"token=REDACTED"},
			wantNotHave: []string{"MYTOKEN"},
		},
		{
			name:        "URL with password",
			input:       "http://example.com/api?password=MYPASSWORD",
			wantContain: []string{"password=REDACTED"},
			wantNotHave: []string{"MYPASSWORD"},
		},
		{
			name:        "case insensitive - APIKEY",
			input:       "http://example.com/api?APIKEY=SECRET",
			wantContain: []string{"APIKEY=REDACTED"},
			wantNotHave: []string{"SECRET"},
		},
		{
			name:        "case insensitive - ApiKey",
			input:       "http://example.com/api?ApiKey=SECRET",
			wantContain: []string{"ApiKey=REDACTED"},
			wantNotHave: []string{"SECRET"},
		},
		{
			name:        "multiple sensitive params",
			input:       "http://example.com/api?apikey=KEY1&token=TOKEN1&passkey=PASS1",
			wantContain: []string{"apikey=REDACTED", "token=REDACTED", "passkey=REDACTED"},
			wantNotHave: []string{"KEY1", "TOKEN1", "PASS1"},
		},
		{
			name:        "non-sensitive params unchanged",
			input:       "http://example.com/api?format=json&limit=100&query=test",
			wantContain: []string{"format=json", "limit=100", "query=test"},
			wantNotHave: nil,
		},
		{
			name:        "mixed sensitive and non-sensitive",
			input:       "http://example.com/api?apikey=SECRET&format=json&t=search",
			wantContain: []string{"apikey=REDACTED", "format=json", "t=search"},
			wantNotHave: []string{"SECRET"},
		},
		{
			name:        "URL without query params",
			input:       "http://example.com/api",
			wantContain: []string{"http://example.com/api"},
			wantNotHave: nil,
		},
		{
			name:        "URL with userinfo password",
			input:       "http://admin:secretpass@example.com/api",
			wantContain: []string{"admin:REDACTED@example.com"},
			wantNotHave: []string{"secretpass"},
		},
		{
			name:        "URL with userinfo password and query params",
			input:       "http://user:hunter2@example.com/api?apikey=SECRET",
			wantContain: []string{"user:REDACTED@", "apikey=REDACTED"},
			wantNotHave: []string{"hunter2", "SECRET"},
		},
		{
			name:        "proxy path with API key",
			input:       "http://localhost:8080/proxy/abc123def456/api/v2/torrents/info",
			wantContain: []string{"/proxy/REDACTED/api/v2/torrents/info"},
			wantNotHave: []string{"abc123def456"},
		},
		{
			name:        "multi-valued apikey params",
			input:       "http://example.com/api?apikey=SECRET1&apikey=SECRET2",
			wantContain: []string{"REDACTED"},
			wantNotHave: []string{"SECRET1", "SECRET2"},
		},
		{
			name:        "empty apikey value",
			input:       "http://example.com/api?apikey=",
			wantContain: []string{"apikey=REDACTED"},
			wantNotHave: nil,
		},
		{
			name:        "empty apikey with other params",
			input:       "http://example.com/api?apikey=&t=caps",
			wantContain: []string{"apikey=REDACTED", "t=caps"},
			wantNotHave: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := URLString(tt.input)
			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}
			for _, notWant := range tt.wantNotHave {
				assert.NotContains(t, got, notWant)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantContain []string
		wantNotHave []string
	}{
		{
			name:        "empty string",
			input:       "",
			wantContain: nil,
			wantNotHave: nil,
		},
		{
			name:        "error message with apikey",
			input:       "error making request to http://example.com?apikey=SECRET123",
			wantContain: []string{"apikey=REDACTED"},
			wantNotHave: []string{"SECRET123"},
		},
		{
			name:        "error message with api_key",
			input:       "failed: http://x.com?api_key=ABC123&t=caps",
			wantContain: []string{"api_key=REDACTED", "t=caps"},
			wantNotHave: []string{"ABC123"},
		},
		{
			name:        "error message with token",
			input:       "connection failed token=SECRETTOKEN",
			wantContain: []string{"token=REDACTED"},
			wantNotHave: []string{"SECRETTOKEN"},
		},
		{
			name:        "error message with passkey",
			input:       "GET http://tracker.example.com/download?passkey=MYPASSKEY failed",
			wantContain: []string{"passkey=REDACTED"},
			wantNotHave: []string{"MYPASSKEY"},
		},
		{
			name:        "error message with password",
			input:       "auth failed: password=hunter2",
			wantContain: []string{"password=REDACTED"},
			wantNotHave: []string{"hunter2"},
		},
		{
			name:        "case insensitive in error",
			input:       "error: APIKEY=SECRET123 TOKEN=ABC",
			wantContain: []string{"APIKEY=REDACTED", "TOKEN=REDACTED"},
			wantNotHave: []string{"SECRET123", "ABC"},
		},
		{
			name:        "multiple params in error string",
			input:       "apikey=A token=B passkey=C password=D api_key=E",
			wantContain: []string{"apikey=REDACTED", "token=REDACTED", "passkey=REDACTED", "password=REDACTED", "api_key=REDACTED"},
			wantNotHave: []string{"=A", "=B", "=C", "=D", "=E"},
		},
		{
			name:        "unparseable URL fragment still redacted",
			input:       "error: ://broken-url?apikey=LEAK",
			wantContain: []string{"apikey=REDACTED"},
			wantNotHave: []string{"LEAK"},
		},
		{
			name:        "no sensitive params - unchanged",
			input:       "connection reset by peer",
			wantContain: []string{"connection reset by peer"},
			wantNotHave: nil,
		},
		{
			name:        "error with userinfo password",
			input:       "dial tcp http://user:secret@host.com:8080: connection refused",
			wantContain: []string{"user:REDACTED@host.com"},
			wantNotHave: []string{"secret"},
		},
		{
			name:        "error with proxy path",
			input:       "request to /proxy/mysecretkey/api/v2/torrents failed",
			wantContain: []string{"/proxy/REDACTED/api/v2/torrents"},
			wantNotHave: []string{"mysecretkey"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := String(tt.input)
			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}
			for _, notWant := range tt.wantNotHave {
				assert.NotContains(t, got, notWant)
			}
		})
	}
}

func TestProxyPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no proxy path",
			input: "/api/v2/torrents/info",
			want:  "/api/v2/torrents/info",
		},
		{
			name:  "proxy path with key",
			input: "/proxy/abc123def456/api/v2/torrents/info",
			want:  "/proxy/REDACTED/api/v2/torrents/info",
		},
		{
			name:  "proxy path at end",
			input: "/proxy/secretkey",
			want:  "/proxy/REDACTED",
		},
		{
			name:  "proxy path with trailing slash",
			input: "/proxy/secretkey/",
			want:  "/proxy/REDACTED/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ProxyPath(tt.input))
		})
	}
}

func TestBasicAuthUser(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "user:password",
			input: "admin:hunter2",
			want:  "admin:REDACTED",
		},
		{
			name:  "user only (no colon)",
			input: "admin",
			want:  "admin",
		},
		{
			name:  "user with empty password",
			input: "admin:",
			want:  "admin:REDACTED",
		},
		{
			name:  "user with complex password",
			input: "admin:p@ssw0rd!#$%",
			want:  "admin:REDACTED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BasicAuthUser(tt.input))
		})
	}
}
