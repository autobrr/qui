// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"net/http"

	"github.com/autobrr/qui/pkg/httphelpers"
)

// RequireAllowedHosts takes a startup snapshot of allowedHosts.
// Install it before RealIP so only direct loopback health probes bypass the list.
func RequireAllowedHosts(entries []string) (func(http.Handler) http.Handler, error) {
	list, err := httphelpers.NewHostAllowlist(entries)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if (r.Method == http.MethodGet || r.Method == http.MethodHead) && isBuiltInHealthEndpoint(r.URL.EscapedPath()) {
				if addr, err := parseRemoteAddrIP(r.RemoteAddr); err == nil && addr.IsLoopback() {
					next.ServeHTTP(w, r)
					return
				}
			}
			if !list.Allows(r.Host) {
				http.Error(w, "Host is not permitted by allowedHosts", http.StatusBadRequest)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}
