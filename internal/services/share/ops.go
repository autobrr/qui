// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share

import (
	"context"
	"errors"
	"net/netip"
	"slices"

	"github.com/autobrr/qui/internal/services/sharetracker"
	"github.com/autobrr/qui/pkg/torrentname"
)

func (s *Service) List(ctx context.Context) ([]Share, error) {
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

func (s *Service) TorrentFile(ctx context.Context, id int64) ([]byte, string, error) {
	sh, err := s.deps.Store.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return sh.TorrentFile, torrentname.SanitizeExportFilename(sh.Name, "", "", sh.HelperHash), nil
}

func (s *Service) Stop(ctx context.Context, id int64) error {
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

func (s *Service) Unlock(ctx context.Context, id int64) error {
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
	s.holderMu.Lock()
	defer s.holderMu.Unlock()
	if err := s.deps.Store.SetHolder(ctx, id, netip.Addr{}); err != nil {
		return err
	}
	s.tracker.Unlock(id)
	return nil
}

func (s *Service) Remove(ctx context.Context, id int64) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.removeRow(ctx, id)
}

type Settings struct {
	Tracker sharetracker.Settings
	Options Options
}

func (s Settings) TrackerInUse() TrackerKind {
	kind, _, err := trackerOf(s.Tracker, s.Options)
	if err != nil {
		return ""
	}
	return kind
}

func (s *Service) Settings(ctx context.Context) (Settings, error) {
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

func (s *Service) SaveSettings(ctx context.Context, settings Settings) error {
	if err := settings.Options.validate(); err != nil {
		return err
	}
	if err := settings.Tracker.Validate(); err != nil {
		return err
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := s.keepTrackerOfActiveShares(ctx, settings.Tracker); err != nil {
		return err
	}
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

func (s *Service) keepTrackerOfActiveShares(ctx context.Context, next sharetracker.Settings) error {
	saved, err := s.deps.TrackerSettings.Settings(ctx)
	if err != nil {
		return err
	}
	if next.Port == saved.Port && next.PublicAddress == saved.PublicAddress {
		return nil
	}
	unfinished, err := s.deps.Store.Unfinished(ctx)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(unfinished, func(sh Share) bool { return sh.State == StateActive && sh.Tracker == TrackerOwn }) {
		return refuse("End the active Shares on qui's tracker first: their .torrent files carry the old address.")
	}
	return nil
}

func IsRefusal(err error) bool {
	_, ok := errors.AsType[*RefusalError](err)
	return ok
}
