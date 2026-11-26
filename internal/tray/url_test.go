// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package tray

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestURL(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		baseURL string
		want    string
	}{
		{name: "all IPv4 addresses", host: "0.0.0.0", baseURL: "/", want: "http://localhost:7476/"},
		{name: "all IPv6 addresses", host: "::", baseURL: "/", want: "http://localhost:7476/"},
		{name: "empty host", host: "", baseURL: "/", want: "http://localhost:7476/"},
		{name: "named host", host: "qui.lan", baseURL: "/", want: "http://qui.lan:7476/"},
		{name: "IPv6 loopback", host: "::1", baseURL: "/", want: "http://[::1]:7476/"},
		{name: "empty base URL", host: "127.0.0.1", baseURL: "", want: "http://127.0.0.1:7476/"},
		{name: "base URL with both slashes", host: "0.0.0.0", baseURL: "/qui/", want: "http://localhost:7476/qui/"},
		{name: "base URL without slashes", host: "0.0.0.0", baseURL: "qui", want: "http://localhost:7476/qui/"},
		{name: "base URL without trailing slash", host: "0.0.0.0", baseURL: "/qui", want: "http://localhost:7476/qui/"},
		{name: "base URL without leading slash", host: "0.0.0.0", baseURL: "qui/", want: "http://localhost:7476/qui/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, URL(tt.host, 7476, tt.baseURL))
		})
	}
}
