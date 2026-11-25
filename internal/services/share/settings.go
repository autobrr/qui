// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	"github.com/autobrr/qui/internal/dbinterface"
)

var ErrInvalidSettings = errors.New("invalid Share settings")

type Options struct {
	MaxActive     int
	PeersPerShare int
	TrackerURL    string
	Defaults      Defaults
}

type Defaults struct {
	Days                int
	EndAtFirstDownload  bool
	FirstDownloaderOnly bool
}

func defaultOptions() Options {
	return Options{MaxActive: 10, PeersPerShare: 20, Defaults: Defaults{Days: 7, EndAtFirstDownload: true, FirstDownloaderOnly: true}}
}

const maxDays = 365

func (o Options) validate() error {
	switch {
	case o.MaxActive < 1:
		return fmt.Errorf("%w: allow at least one active Share", ErrInvalidSettings)
	case o.PeersPerShare < 1:
		return fmt.Errorf("%w: allow at least one peer for each Share", ErrInvalidSettings)
	case o.Defaults.Days < 1 || o.Defaults.Days > maxDays:
		return fmt.Errorf("%w: a Share lasts 1 to %d days", ErrInvalidSettings, maxDays)
	}
	if o.TrackerURL != "" {
		u, err := url.Parse(o.TrackerURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "udp") || u.Host == "" {
			return fmt.Errorf("%w: the tracker URL is not an http, https, or udp URL", ErrInvalidSettings)
		}
	}
	return nil
}

type SettingsStore struct {
	db dbinterface.Querier
}

func NewSettingsStore(db dbinterface.Querier) *SettingsStore {
	return &SettingsStore{db: db}
}

func (s *SettingsStore) Settings(ctx context.Context) (Options, error) {
	var o Options
	err := s.db.QueryRowContext(ctx, `SELECT max_active, peers_per_share, tracker_url, default_days,
			default_end_at_first_download, default_first_downloader_only
		FROM share_settings WHERE id = 1`).
		Scan(&o.MaxActive, &o.PeersPerShare, &o.TrackerURL, &o.Defaults.Days, &o.Defaults.EndAtFirstDownload, &o.Defaults.FirstDownloaderOnly)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultOptions(), nil
	}
	if err != nil {
		return Options{}, fmt.Errorf("read share settings: %w", err)
	}
	return o, nil
}

func (s *SettingsStore) SaveSettings(ctx context.Context, o Options) error {
	if err := o.validate(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO share_settings (id, max_active, peers_per_share, tracker_url, default_days,
			default_end_at_first_download, default_first_downloader_only)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			max_active = excluded.max_active,
			peers_per_share = excluded.peers_per_share,
			tracker_url = excluded.tracker_url,
			default_days = excluded.default_days,
			default_end_at_first_download = excluded.default_end_at_first_download,
			default_first_downloader_only = excluded.default_first_downloader_only`,
		o.MaxActive, o.PeersPerShare, o.TrackerURL, o.Defaults.Days, o.Defaults.EndAtFirstDownload, o.Defaults.FirstDownloaderOnly); err != nil {
		return fmt.Errorf("save share settings: %w", err)
	}
	return nil
}
