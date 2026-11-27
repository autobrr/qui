// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package httphelpers

import (
	"net/http"
	"strings"
)

const maxLoggedUserAgentBytes = 512

func LoggedUserAgent(r *http.Request) string {
	userAgent := r.UserAgent()
	if len(userAgent) <= maxLoggedUserAgentBytes {
		return userAgent
	}
	return strings.ToValidUTF8(userAgent[:maxLoggedUserAgentBytes], "")
}
