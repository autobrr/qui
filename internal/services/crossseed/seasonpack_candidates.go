// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"iter"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/releases"
)

func (s *Service) seasonPackLookup(ctx context.Context, prep *seasonPackPrep, instances []*models.Instance) *heldLookup {
	lookup := s.heldTitles.lookup(ctx, heldTitlesQuery{
		announced:     namedRelease{release: prep.packRelease, rawName: prep.packName},
		titlesOnly:    true,
		everyInstance: true,
	}, instances)
	if lookup == nil {
		return nil
	}
	lookup.addTitles(prep.aliasTitles)
	return lookup
}

func seasonPackNotHeld(ctx context.Context, lookup *heldLookup) bool {
	if lookup == nil {
		return false
	}
	return lookup.holdsNothing(ctx, releases.ContentTypeTV) && (!lookup.awaitDueSyncs(ctx) || lookup.holdsNothing(ctx, releases.ContentTypeTV))
}

func (s *Service) seasonPackTorrents(ctx context.Context, prep *seasonPackPrep, lookup *heldLookup, matchedTitles map[string][]string) func(instanceID int) (iter.Seq2[*qbt.Torrent, []string], error) {
	answers := lookup != nil && lookup.answers(ctx)
	if prep.heldOnly && !answers {
		return noSeasonPackTorrents
	}
	if !answers {
		return func(instanceID int) (iter.Seq2[*qbt.Torrent, []string], error) {
			return s.seasonPackAllTorrents(ctx, instanceID, matchedTitles)
		}
	}
	found := lookup.candidates()
	return func(instanceID int) (iter.Seq2[*qbt.Torrent, []string], error) {
		candidates := found[instanceID]
		if prep.sonarrAnswers {
			candidates.alternateTitles = nil
		}
		return candidates.withAlternateTitles(), nil
	}
}

func noSeasonPackTorrents(int) (iter.Seq2[*qbt.Torrent, []string], error) {
	return func(func(*qbt.Torrent, []string) bool) {}, nil
}

func (c heldCandidates) withAlternateTitles() iter.Seq2[*qbt.Torrent, []string] {
	return func(yield func(*qbt.Torrent, []string) bool) {
		for _, torrent := range c.sorted() {
			if !yield(torrent, c.alternateTitles[torrent]) {
				return
			}
		}
	}
}
