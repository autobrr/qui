// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package tray shows the Tray: qui's icon in the Windows notification area.
// See docs/adr/0012-the-tray-lives-in-the-served-child-of-a-second-windows-binary.md.
package tray

import (
	"net"
	"strconv"

	"github.com/autobrr/qui/pkg/httphelpers"
)

// URL is the address that Open qui opens. A browser cannot open an address
// that means "all addresses", so those map to localhost. qui has no built-in
// TLS, so the scheme is always http.
func URL(host string, port int, baseURL string) string {
	switch host {
	case "", "0.0.0.0", "::":
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + httphelpers.NormalizeBasePath(baseURL) + "/"
}
