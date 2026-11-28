// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations/verdict"
)

type unregisteredFacts struct {
	Since   time.Time         `json:"since,omitzero"`
	Host    string            `json:"host,omitempty"`
	Status  qbt.TrackerStatus `json:"status,omitzero"`
	Message string            `json:"message,omitempty"`
}

func (d *deletionSelection) recordUnregistered(claim deletionClaim, waited verdict.Waited) {
	entry, queued := d.byHash[claim.hash]
	if !queued || entry.rule.id != claim.rule.ID {
		return
	}
	entry.unregistered = unregisteredFactsOf(claim.rule, claim.hash, waited)
	d.byHash[claim.hash] = entry
}

func unregisteredFactsOf(rule *models.Automation, hash string, waited verdict.Waited) *unregisteredFacts {
	if !conditionReadsVerdict(rule.Conditions.Delete.Condition) || !waited.UnregisteredNow(hash) {
		return nil
	}
	since, timed := waited.Since(hash)
	tracker, found := waited.UnregisteredBy(hash)
	if !timed && !found {
		return nil
	}
	return &unregisteredFacts{Since: since, Host: tracker.Host, Status: tracker.Status, Message: tracker.Message}
}

func (d selectedDeletion) logUnregistered(event *zerolog.Event) {
	facts := d.unregistered
	if facts == nil {
		return
	}
	if !facts.Since.IsZero() {
		event.Time("unregisteredSince", facts.Since)
	}
	if facts.Host != "" {
		event.Str("trackerHost", facts.Host).Int("trackerStatus", int(facts.Status)).Str("trackerMessage", facts.Message)
	}
}
