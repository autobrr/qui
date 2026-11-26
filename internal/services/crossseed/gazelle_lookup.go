// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"maps"
	"slices"
	"strings"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/systemnotice"
)

type GazelleLookupStatus string

const (
	GazelleWillAsk  GazelleLookupStatus = "will_ask"
	GazelleMatch    GazelleLookupStatus = "match"
	GazelleNoMatch  GazelleLookupStatus = "no_match"
	GazelleNotAsked GazelleLookupStatus = "not_asked"
	GazelleFailed   GazelleLookupStatus = "failed"
)

type GazelleLookupReason string

const (
	GazelleReasonCooldown      GazelleLookupReason = "cooldown"
	GazelleReasonNotSelected   GazelleLookupReason = "not_selected"
	GazelleReasonSourceSite    GazelleLookupReason = "source_site"
	GazelleReasonAlreadySeeded GazelleLookupReason = "already_seeded"
	GazelleReasonNotMusic      GazelleLookupReason = "not_music"
	GazelleReasonKeyRejected   GazelleLookupReason = "key_rejected"
	GazelleReasonTimedOut      GazelleLookupReason = "timed_out"
	GazelleReasonError         GazelleLookupReason = "error"
)

type GazelleLookup struct {
	Site               string              `json:"site"`
	Name               string              `json:"name"`
	ShortName          string              `json:"shortName"`
	IndexerID          int                 `json:"indexerId"`
	Status             GazelleLookupStatus `json:"status"`
	Reason             GazelleLookupReason `json:"reason,omitempty"`
	Detail             string              `json:"detail,omitempty"`
	ReplacedIndexerIDs []int               `json:"replacedIndexerIds,omitempty"`

	seededByCopy bool
	sent         bool
}

func (l GazelleLookup) notAsked(reason GazelleLookupReason) GazelleLookup {
	l.Status, l.Reason = GazelleNotAsked, reason
	return l
}

func (l GazelleLookup) failed(reason GazelleLookupReason, detail string) GazelleLookup {
	l.Status, l.Reason, l.Detail = GazelleFailed, reason, detail
	return l
}

func gazelleCooldownDue(lookups []GazelleLookup, keyMissing bool) bool {
	lookedUp := false
	for _, l := range lookups {
		if l.Status == GazelleFailed || l.Reason == GazelleReasonCooldown || l.Reason == GazelleReasonNotSelected {
			return false
		}
		lookedUp = lookedUp || !l.seededByCopy
	}
	return lookedUp || (len(lookups) > 0 && !keyMissing)
}

func gazelleRequestsSent(lookups []GazelleLookup) bool {
	return slices.ContainsFunc(lookups, func(l GazelleLookup) bool { return l.sent })
}

func gazelleLookupSite(l GazelleLookup) gazellemusic.Site {
	site, _ := gazellemusic.SiteByHost(l.Site)
	return site
}

func gazelleOutcomeText(l GazelleLookup) string {
	switch l.Status {
	case GazelleMatch:
		return "match"
	case GazelleNoMatch:
		return "no match"
	case GazelleWillAsk:
		return "will ask"
	case GazelleNotAsked:
		return "not asked (" + gazelleReasonText(l) + ")"
	default:
		return "failed (" + gazelleReasonText(l) + ")"
	}
}

func gazelleReasonText(l GazelleLookup) string {
	switch l.Reason {
	case GazelleReasonCooldown:
		return "searched recently"
	case GazelleReasonNotSelected:
		return "not selected"
	case GazelleReasonSourceSite:
		return "the torrent comes from " + l.ShortName
	case GazelleReasonAlreadySeeded:
		return "you already seed it there"
	case GazelleReasonNotMusic:
		return "not music"
	case GazelleReasonKeyRejected:
		return "the site rejected the API key"
	case GazelleReasonTimedOut:
		return "timed out"
	default:
		return l.Detail
	}
}

func gazelleLookupsLine(lookups []GazelleLookup) string {
	parts := make([]string, 0, len(lookups))
	for _, l := range lookups {
		parts = append(parts, l.ShortName+" "+gazelleOutcomeText(l))
	}
	return "RED/OPS: " + strings.Join(parts, ", ")
}

func keyRejectedNotice(site gazellemusic.Site) systemnotice.Kind {
	return systemnotice.Kind(site.SettingsName + "_key_rejected")
}

func (s *Service) gazelleKeyGensNow() map[string]uint64 {
	s.gazelleNoticeMu.Lock()
	defer s.gazelleNoticeMu.Unlock()
	return maps.Clone(s.gazelleKeyGens)
}

func (s *Service) setKeyRejectedNotice(site gazellemusic.Site, gen uint64, open bool, cause string) {
	if s.systemNotices == nil {
		return
	}
	s.gazelleNoticeMu.Lock()
	defer s.gazelleNoticeMu.Unlock()
	if s.gazelleKeyGens[site.Host] != gen {
		return
	}
	if open {
		s.systemNotices.Open(keyRejectedNotice(site), cause)
		return
	}
	s.systemNotices.Close(keyRejectedNotice(site))
}

func (s *Service) closeKeyRejectedNotices(lookups []GazelleLookup, clients *gazelleClientSet) {
	for _, l := range lookups {
		if l.Status == GazelleMatch || l.Status == GazelleNoMatch {
			s.setKeyRejectedNotice(gazelleLookupSite(l), clients.keyGens[l.Site], false, "")
		}
	}
}

func (s *Service) GazelleKeysSaved(settings *models.CrossSeedAutomationSettings, checked []string) {
	if settings == nil {
		return
	}
	s.gazelleNoticeMu.Lock()
	defer s.gazelleNoticeMu.Unlock()
	for _, site := range gazellemusic.Sites() {
		key := strings.TrimSpace(settings.GazelleAPIKey(site.SettingsName))
		if !settings.GazelleEnabled || key == "" || !domain.IsRedactedString(key) {
			if s.gazelleKeyGens == nil {
				s.gazelleKeyGens = map[string]uint64{}
			}
			s.gazelleKeyGens[site.Host]++
		}
		if s.systemNotices != nil && (!settings.GazelleEnabled || key == "" || slices.Contains(checked, site.Host)) {
			s.systemNotices.Close(keyRejectedNotice(site))
		}
	}
}

func (s *Service) keyRejectedOpen(site gazellemusic.Site) bool {
	if s.systemNotices == nil {
		return false
	}
	kind := keyRejectedNotice(site)
	return slices.ContainsFunc(s.systemNotices.List(), func(n systemnotice.Notice) bool { return n.Kind == kind })
}
