// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import "github.com/go-chi/chi/v5/middleware"

var RequestID = middleware.RequestID

var Recoverer = middleware.Recoverer

var RealIP = middleware.RealIP //nolint:staticcheck // SA1019: spoofable by design, which is why the auth-disabled CIDR allowlist runs before it in server.go; RemoteAddr past this point is for logs and throttling

var ThrottleBacklog = middleware.ThrottleBacklog
