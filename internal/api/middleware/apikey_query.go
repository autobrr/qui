// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import "net/http"

// APIKeyFromQuery promotes an API key query param into the X-API-Key header.
// Use this only on routes that explicitly allow query param auth.
func APIKeyFromQuery(param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-API-Key") == "" {
				if apiKey := r.URL.Query().Get(param); apiKey != "" {
					r.Header.Set("X-API-Key", apiKey)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
