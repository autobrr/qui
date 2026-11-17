// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share

import (
	"context"
	"errors"
	"net/netip"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/sharetracker"
	"github.com/autobrr/qui/pkg/torrentname"
)

// List returns every Share, the newest first, with the latest reports of the
// tracker.
func (s *Service) List(ctx context.Context) ([]Share, error) {
	if !s.on() {
		return nil, ErrOff
	}
	list, err := s.deps.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range list {
		if l := s.live[list[i].ID]; l != nil {
			list[i].Activity = l.activity()
		}
	}
	return list, nil
}

// TorrentFile returns the .torrent of the recipient of a Share and its file
// name.
func (s *Service) TorrentFile(ctx context.Context, id int64) ([]byte, string, error) {
	if !s.on() {
		return nil, "", ErrOff
	}
	sh, err := s.deps.Store.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return sh.TorrentFile, torrentname.SanitizeExportFilename(sh.Name, "", "", sh.HelperHash), nil
}

// Stop ends an active Share. A Share that ends already stays as it is.
func (s *Service) Stop(ctx context.Context, id int64) error {
	if !s.on() {
		return ErrOff
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	sh, err := s.deps.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if sh.State != StateActive {
		return nil
	}
	return s.end(ctx, sh, reasonStopped, "the user")
}

// Unlock lets the next downloader of an active Share with "Only the first
// downloader" in, also after a restart of qui.
func (s *Service) Unlock(ctx context.Context, id int64) error {
	if !s.on() {
		return ErrOff
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	sh, err := s.deps.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	switch {
	case sh.State != StateActive:
		return refuse("The Share has ended.")
	case sh.Tracker != TrackerOwn:
		return refuse("Unlock needs qui's own tracker. With an outside tracker, every downloader gets the address.")
	case !sh.FirstDownloaderOnly:
		return refuse("The Share lets every downloader in already.")
	}
	if err := s.deps.Store.SetHolder(ctx, id, netip.Addr{}); err != nil {
		return err
	}
	if err := s.refresh(ctx); err != nil {
		log.Error().Err(err).Msg("share: cannot update the Share tracker")
	}
	s.tracker.Unlock(id)
	return nil
}

// Remove removes the row of an ended Share, or returns ErrNotEnded.
func (s *Service) Remove(ctx context.Context, id int64) error {
	if !s.on() {
		return ErrOff
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.removeRow(ctx, id)
}

// Settings are the settings of Settings → Shares: the Share tracker settings
// and the Share options.
type Settings struct {
	Tracker sharetracker.Settings
	Options Options
}

// TrackerInUse is the tracker that a new Share gets, or "" while no tracker
// is set.
func (s Settings) TrackerInUse() TrackerKind {
	kind, _, err := trackerOf(s.Tracker, s.Options)
	if err != nil {
		return ""
	}
	return kind
}

// Settings returns the settings of Settings → Shares.
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	if !s.on() {
		return Settings{}, ErrOff
	}
	trackerSettings, err := s.deps.TrackerSettings.Settings(ctx)
	if err != nil {
		return Settings{}, err
	}
	options, err := s.deps.Settings.Settings(ctx)
	if err != nil {
		return Settings{}, err
	}
	return Settings{Tracker: trackerSettings, Options: options}, nil
}

// SaveSettings saves the settings and applies them to the running tracker.
// It returns an error that wraps ErrInvalidSettings or
// sharetracker.ErrInvalidSettings for a value that qui does not take, and
// sharetracker.ErrPortBusy when another program listens on the port of the
// tracker. Then it saves nothing.
func (s *Service) SaveSettings(ctx context.Context, settings Settings) error {
	if !s.on() {
		return ErrOff
	}
	if err := settings.Options.validate(); err != nil {
		return err
	}
	if err := settings.Tracker.Validate(); err != nil {
		return err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := s.tracker.CheckPort(ctx, settings.Tracker.Port); err != nil {
		return err
	}
	if err := s.deps.TrackerSettings.SaveSettings(ctx, settings.Tracker); err != nil {
		return err
	}
	if err := s.deps.Settings.SaveSettings(ctx, settings.Options); err != nil {
		return err
	}
	return s.refresh(ctx)
}

// IsRefusal reports whether err is a refusal for the user.
func IsRefusal(err error) bool {
	_, ok := errors.AsType[*RefusalError](err)
	return ok
}
