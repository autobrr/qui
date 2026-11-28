// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/infohash"
)

type PendingDeleteList struct {
	Rules []PendingDeleteGroup `json:"rules"`
}

type PendingDeleteGroup struct {
	RuleID   int                    `json:"ruleId"`
	RuleName string                 `json:"ruleName"`
	Torrents []PendingDeleteTorrent `json:"torrents"`
}

type PendingDeleteTorrent struct {
	Hash              string `json:"hash"`
	Name              string `json:"name"`
	Tracker           string `json:"tracker"`
	Message           string `json:"message"`
	UnregisteredSince int64  `json:"unregisteredSince"`
	DeletesAt         int64  `json:"deletesAt"`
	AwaitsTracker     bool   `json:"awaitsTracker"`
}

func (s *Service) ListPendingDeletes(ctx context.Context, instanceID int) (*PendingDeleteList, error) {
	list := &PendingDeleteList{Rules: []PendingDeleteGroup{}}
	if s.pendingDeletes == nil || s.ruleStore == nil {
		return list, nil
	}
	rows, err := s.pendingDeletes.ListPendingDeletes(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list pending deletes: %w", err)
	}
	if len(rows) == 0 {
		return list, nil
	}
	rules, err := s.ruleStore.ListByInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}

	byRule := make(map[int][]models.PendingDelete)
	for _, row := range rows {
		byRule[row.RuleID] = append(byRule[row.RuleID], row)
	}
	torrents := s.pendingTorrents(ctx, instanceID, rows)
	waited := s.waitedNow(instanceID)
	now := s.now()
	for _, rule := range rules {
		ruleRows := byRule[rule.ID]
		if len(ruleRows) == 0 {
			continue
		}
		wait := deleteWaitOf(rule, waited).wait
		group := PendingDeleteGroup{RuleID: rule.ID, RuleName: rule.Name, Torrents: make([]PendingDeleteTorrent, 0, len(ruleRows))}
		for _, row := range ruleRows {
			since := row.UnregisteredSince
			if live, timed := waited.Since(row.Hash); timed {
				since = live
			} else if waited.Loaded() && !waited.UnregisteredNow(row.Hash) {
				continue
			}
			deletesAt := since.Add(wait)
			entry := PendingDeleteTorrent{
				Hash:              row.Hash,
				UnregisteredSince: since.Unix(),
				DeletesAt:         deletesAt.Unix(),
				AwaitsTracker:     !now.Before(deletesAt) && !waited.UnregisteredNow(row.Hash),
			}
			if torrent := torrents[row.Hash]; torrent != nil {
				entry.Name = torrent.Name
				entry.Tracker = trackerHost(torrent.Tracker)
			}
			if by, found := waited.UnregisteredBy(row.Hash); found {
				entry.Tracker = cmp.Or(by.Host, entry.Tracker)
				entry.Message = by.Message
			}
			group.Torrents = append(group.Torrents, entry)
		}
		slices.SortFunc(group.Torrents, func(a, b PendingDeleteTorrent) int {
			return cmp.Or(cmp.Compare(a.DeletesAt, b.DeletesAt), cmp.Compare(a.Hash, b.Hash))
		})
		list.Rules = append(list.Rules, group)
	}
	return list, nil
}

func (s *Service) pendingTorrents(ctx context.Context, instanceID int, rows []models.PendingDelete) map[string]*qbt.Torrent {
	byHash := make(map[string]*qbt.Torrent, len(rows))
	if s.syncManager == nil {
		return byHash
	}
	hashes := make([]string, len(rows))
	for i, row := range rows {
		hashes[i] = row.Hash
	}
	refs, err := s.syncManager.GetTorrentRefs(ctx, instanceID, qbittorrent.TorrentRefFilter{Hashes: infohash.ParseAll(infohash.UniqueStrings(hashes))})
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("automations: the torrents that wait for a delete are listed without their names")
		return byHash
	}
	for _, torrent := range refs {
		byHash[torrent.Hash] = torrent
	}
	return byHash
}

func trackerHost(tracker string) string {
	u, err := url.Parse(tracker)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
