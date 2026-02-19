// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/domain"
)

// RequireAuthDisabledIPAllowlist enforces authDisabledAllowedCIDRs when
// built-in authentication is disabled.
func RequireAuthDisabledIPAllowlist(cfg *domain.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg == nil || !cfg.IsAuthDisabled() {
				next.ServeHTTP(w, r)
				return
			}

			prefixes, err := cfg.ParseAuthDisabledAllowedCIDRs()
			if err != nil || len(prefixes) == 0 {
				log.Error().Err(err).Msg("auth-disabled mode is misconfigured: authDisabledAllowedCIDRs is invalid or empty")
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			addr, err := parseRemoteAddrIP(r.RemoteAddr)
			if err != nil {
				log.Warn().Err(err).Str("remote_addr", r.RemoteAddr).Msg("Failed to parse remote address for auth-disabled allowlist")
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			for _, prefix := range prefixes {
				if prefix.Contains(addr) {
					next.ServeHTTP(w, r)
					return
				}
			}

			log.Warn().
				Str("remote_addr", r.RemoteAddr).
				Str("ip", addr.String()).
				Msg("Blocked request in auth-disabled mode: client IP not in authDisabledAllowedCIDRs")
			http.Error(w, "Forbidden", http.StatusForbidden)
		})
	}
}

func parseRemoteAddrIP(remoteAddr string) (netip.Addr, error) {
	trimmed := strings.TrimSpace(remoteAddr)
	if addr, err := netip.ParseAddr(strings.Trim(trimmed, "[]")); err == nil {
		return addr.Unmap(), nil
	}

	host, _, err := net.SplitHostPort(trimmed)
	if err != nil {
		return netip.Addr{}, err
	}

	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, err
	}

	return addr.Unmap(), nil
}
