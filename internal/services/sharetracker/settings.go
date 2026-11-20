// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sharetracker

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/autobrr/qui/internal/dbinterface"
)

var ErrInvalidSettings = errors.New("invalid Share tracker settings")

// Settings are the Share tracker settings. Port 0 means that the user set no
// port, and the tracker does not listen. PublicAddress is the http or https
// URL at which recipients reach the tracker. TrustedProxies are the addresses
// and CIDR prefixes whose X-Forwarded-For the tracker trusts.
type Settings struct {
	Port           int      `json:"port"`
	PublicAddress  string   `json:"publicAddress"`
	TrustedProxies []string `json:"trustedProxies"`
}

func (s Settings) Validate() error {
	if s.Port < 0 || s.Port > 65535 {
		return fmt.Errorf("%w: the port is not between 0 and 65535", ErrInvalidSettings)
	}
	if s.PublicAddress != "" {
		u, err := url.Parse(s.PublicAddress)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || strings.ContainsAny(s.PublicAddress, "?#") {
			return fmt.Errorf("%w: the public address is not an http or https URL", ErrInvalidSettings)
		}
	}
	_, err := s.trustedProxies()
	return err
}

func (s Settings) trustedProxies() ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(s.TrustedProxies))
	for _, entry := range s.TrustedProxies {
		var prefix netip.Prefix
		var err error
		if strings.Contains(entry, "/") {
			prefix, err = netip.ParsePrefix(entry)
		} else {
			var addr netip.Addr
			addr, err = netip.ParseAddr(entry)
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		if err != nil {
			return nil, fmt.Errorf("%w: trusted proxy %q is not an address or a CIDR prefix", ErrInvalidSettings, entry)
		}
		// chi compares an IPv4-mapped client address in its IPv4 form.
		if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

type SettingsStore struct {
	db dbinterface.Querier
}

func NewSettingsStore(db dbinterface.Querier) *SettingsStore {
	return &SettingsStore{db: db}
}

// Settings returns the saved settings. Before the first save, they have no
// port.
func (s *SettingsStore) Settings(ctx context.Context) (Settings, error) {
	var settings Settings
	var proxies string
	err := s.db.QueryRowContext(ctx, `
		SELECT port, public_address, trusted_proxies FROM share_tracker_settings WHERE id = 1`).
		Scan(&settings.Port, &settings.PublicAddress, &proxies)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read share tracker settings: %w", err)
	}
	if err := json.Unmarshal([]byte(proxies), &settings.TrustedProxies); err != nil {
		return Settings{}, fmt.Errorf("read share tracker trusted proxies: %w", err)
	}
	return settings, nil
}

func (s *SettingsStore) SaveSettings(ctx context.Context, settings Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	proxies, err := json.Marshal(settings.TrustedProxies)
	if err != nil {
		return fmt.Errorf("save share tracker trusted proxies: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO share_tracker_settings (id, port, public_address, trusted_proxies)
		VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			port = excluded.port,
			public_address = excluded.public_address,
			trusted_proxies = excluded.trusted_proxies`,
		settings.Port, settings.PublicAddress, string(proxies)); err != nil {
		return fmt.Errorf("save share tracker settings: %w", err)
	}
	return nil
}
