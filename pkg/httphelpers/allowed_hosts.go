// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package httphelpers

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// HostAllowlist matches the received HTTP Host against a fixed list.
type HostAllowlist struct {
	hosts []string
}

// NewHostAllowlist accepts exact hosts and leading *. subdomain wildcards.
// An empty list permits all hosts. Invalid entries return an error.
func NewHostAllowlist(entries []string) (*HostAllowlist, error) {
	list := &HostAllowlist{hosts: make([]string, 0, len(entries))}
	for _, entry := range entries {
		host, wildcard := strings.CutPrefix(strings.TrimSpace(entry), "*.")
		host, err := normalizeHost(host)
		if err != nil {
			return nil, fmt.Errorf("invalid allowedHosts entry %q: %w", entry, err)
		}
		if wildcard {
			if _, err := netip.ParseAddr(host); err == nil {
				return nil, fmt.Errorf("invalid allowedHosts entry %q: an IP address cannot use a wildcard", entry)
			}
			host = "*." + host
		}
		list.hosts = append(list.hosts, host)
	}
	return list, nil
}

// Allows reports whether host, ignoring any port, matches a listed host name or IP.
func (list *HostAllowlist) Allows(host string) bool {
	if len(list.hosts) == 0 {
		return true
	}
	// HTTP IPv6 literals use brackets, with or without a port.
	if strings.Contains(host, ":") && (!strings.HasPrefix(host, "[") || !strings.HasSuffix(host, "]")) {
		name, port, err := net.SplitHostPort(host)
		if err != nil {
			return false
		}
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return false
		}
		// Preserve brackets so malformed bracketed DNS names remain invalid.
		if strings.HasPrefix(host, "[") {
			name = "[" + name + "]"
		}
		host = name
	}
	host, err := normalizeHost(host)
	if err != nil {
		return false
	}
	for _, allowed := range list.hosts {
		if host == allowed {
			return true
		}
		if suffix, wildcard := strings.CutPrefix(allowed, "*"); wildcard && strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func normalizeHost(host string) (string, error) {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		addr, err := netip.ParseAddr(host[1 : len(host)-1])
		if err != nil || !addr.Is6() || addr.Zone() != "" {
			return "", errors.New("invalid bracketed IPv6 address")
		}
		return addr.Unmap().String(), nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Zone() != "" {
			return "", errors.New("IP zones are not allowed")
		}
		return addr.Unmap().String(), nil
	}
	host, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return "", err
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if len(host) > 253 {
		return "", errors.New("hostname is too long")
	}
	// Check lengths after IDNA maps all DNS dot forms and removes one final dot.
	for label := range strings.SplitSeq(host, ".") {
		if len(label) == 0 || len(label) > 63 {
			return "", errors.New("invalid hostname label length")
		}
	}
	return host, nil
}
