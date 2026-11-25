// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package tray

import (
	"net"
	"strconv"

	"github.com/autobrr/qui/pkg/httphelpers"
)

func URL(host string, port int, baseURL string) string {
	switch host {
	case "", "0.0.0.0", "::":
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + httphelpers.NormalizeBasePath(baseURL) + "/"
}
