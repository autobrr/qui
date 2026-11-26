// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"

	qbt "github.com/autobrr/go-qbittorrent"
)

type webhookCopyKey struct {
	group       uint64
	name        string
	size        int64
	contentType string
	readByFiles bool
}

type webhookCopy struct {
	instanceID int
	torrent    *qbt.Torrent
	settled    bool
	readsFiles bool
	decision   *searchCandidateDecision
}

type webhookCopies struct {
	svc         *Service
	input       webhookCheckInput
	sourceReads map[int]localReads
	alternates  map[*qbt.Torrent][]string
	compared    map[*qbt.Torrent]*webhookCopy
}

func (s *Service) webhookCopies(input webhookCheckInput, candidates map[int][]*qbt.Torrent, sourceReads map[int]localReads, alternates map[*qbt.Torrent][]string) webhookCopies {
	copies := webhookCopies{svc: s, input: input, sourceReads: sourceReads, alternates: alternates, compared: map[*qbt.Torrent]*webhookCopy{}}
	groups := s.heldTitles.sync.CopyGroups()
	gate, filter := s.sourceGate(), defaultsFor(triggerWebhook, input.settings).sourceFilter
	byKey := map[webhookCopyKey]*webhookCopy{}
	for _, instance := range input.instances {
		for _, torrent := range candidates[instance.ID] {
			group, grouped := groups.GroupID(instance.ID, torrent.Hash)
			answer := gate.check(torrent, filter)
			if !grouped || answer.lasting() {
				continue
			}
			rule, _ := categoryRuleContentType(input.settings.CategoryMappingRules, torrent.Category)
			_, readByFiles := sourceReads[instance.ID].entry(instance.ID, torrent)
			key := webhookCopyKey{group: group, name: torrent.Name, size: searchSourceSize(torrent), contentType: rule.ContentType, readByFiles: readByFiles}
			compared := byKey[key]
			if compared == nil {
				compared = &webhookCopy{instanceID: instance.ID, torrent: torrent, readsFiles: readByFiles || positiveExactSize(key.size, int64(input.size))}
				byKey[key] = compared
			}
			if !compared.settled && answer == sourcePass {
				compared.instanceID, compared.torrent, compared.settled = instance.ID, torrent, true
			}
			copies.compared[torrent] = compared
		}
	}
	return copies
}

func (c webhookCopies) decide(ctx context.Context, instanceID int, torrent *qbt.Torrent) searchCandidateDecision {
	compared := c.compared[torrent]
	if compared == nil {
		return c.compare(ctx, instanceID, torrent)
	}
	if compared.decision == nil {
		decision := c.compare(ctx, compared.instanceID, compared.torrent)
		compared.decision = &decision
	}
	if compared.readsFiles && !compared.decision.Accepted && torrent != compared.torrent {
		return c.compare(ctx, instanceID, torrent)
	}
	return *compared.decision
}

func (c webhookCopies) compare(ctx context.Context, instanceID int, torrent *qbt.Torrent) searchCandidateDecision {
	input := c.input
	input.policy.sourceTitles = c.alternates[torrent]
	return c.svc.webhookDecision(ctx, input, instanceID, torrent, c.sourceReads[instanceID])
}
