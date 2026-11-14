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

// GazelleLookupStatus is the state of one RED/OPS lookup.
type GazelleLookupStatus string

const (
	// GazelleWillAsk is the plan of a lookup that the search sends.
	GazelleWillAsk  GazelleLookupStatus = "will_ask"
	GazelleMatch    GazelleLookupStatus = "match"
	GazelleNoMatch  GazelleLookupStatus = "no_match"
	GazelleNotAsked GazelleLookupStatus = "not_asked"
	GazelleFailed   GazelleLookupStatus = "failed"
)

// GazelleLookupReason says why the search did not ask a site, or why its
// lookup failed.
type GazelleLookupReason string

const (
	GazelleReasonDisabled      GazelleLookupReason = "disabled"
	GazelleReasonNoAPIKey      GazelleLookupReason = "no_api_key"
	GazelleReasonCooldown      GazelleLookupReason = "cooldown"
	GazelleReasonNotSelected   GazelleLookupReason = "not_selected"
	GazelleReasonSourceSite    GazelleLookupReason = "source_site"
	GazelleReasonAlreadySeeded GazelleLookupReason = "already_seeded"
	GazelleReasonNotMusic      GazelleLookupReason = "not_music"
	GazelleReasonKeyRejected   GazelleLookupReason = "key_rejected"
	GazelleReasonTimedOut      GazelleLookupReason = "timed_out"
	GazelleReasonError         GazelleLookupReason = "error"
)

// GazelleLookup is the RED/OPS lookup of one site for one search: the plan
// before the search, or the outcome after it.
type GazelleLookup struct {
	// Site is the API host of the site.
	Site      string              `json:"site"`
	Name      string              `json:"name"`
	ShortName string              `json:"shortName"`
	IndexerID int                 `json:"indexerId"`
	Status    GazelleLookupStatus `json:"status"`
	Reason    GazelleLookupReason `json:"reason,omitempty"`
	// Detail is the error text of a failed lookup.
	Detail string `json:"detail,omitempty"`
	// ReplacedIndexerIDs are the Torznab indexers of the site that the search
	// leaves out, because it asks the site through its API.
	ReplacedIndexerIDs []int `json:"replacedIndexerIds,omitempty"`

	// seededByCopy reports that the torrent or a duplicate copy of it comes
	// from the site, whatever the reason that the lookup shows.
	seededByCopy bool
	// sent reports that the lookup sent a request to the site.
	sent bool
}

func (l GazelleLookup) notAsked(reason GazelleLookupReason) GazelleLookup {
	l.Status, l.Reason = GazelleNotAsked, reason
	return l
}

func (l GazelleLookup) failed(reason GazelleLookupReason, detail string) GazelleLookup {
	l.Status, l.Reason, l.Detail = GazelleFailed, reason, detail
	return l
}

// gazelleCooldownDue reports whether the lookups of one torrent need no retry,
// so the Library Scan stamps the RED/OPS cooldown of the torrent. A failed
// lookup needs a retry. A search that asks no site stamps the cooldown for a
// failed music check, and when a copy seeds on each site that has a key. A
// missing API key stamps none, so a key that the user adds later works at the
// next run.
func gazelleCooldownDue(lookups []GazelleLookup) bool {
	var lookedUp, keyed, missingKey bool
	for _, l := range lookups {
		switch {
		case l.Status == GazelleFailed, l.Reason == GazelleReasonCooldown, l.Reason == GazelleReasonNotSelected:
			return false
		case l.Reason == GazelleReasonDisabled:
		case l.Reason == GazelleReasonNoAPIKey:
			missingKey = missingKey || !l.seededByCopy
		default:
			keyed = true
			lookedUp = lookedUp || !l.seededByCopy
		}
	}
	return lookedUp || (keyed && !missingKey)
}

// gazelleRequestsSent reports whether a lookup sent a request to a site.
func gazelleRequestsSent(lookups []GazelleLookup) bool {
	return slices.ContainsFunc(lookups, func(l GazelleLookup) bool { return l.sent })
}

func gazelleLookupSite(l GazelleLookup) gazellemusic.Site {
	site, _ := gazellemusic.SiteByHost(l.Site)
	return site
}

// gazelleOutcomeText describes one outcome in plain text, for a log line, a
// notification, and a Library Scan row.
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
	case GazelleReasonDisabled:
		return "off"
	case GazelleReasonNoAPIKey:
		return "no API key"
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

// gazelleLookupsLine is the RED/OPS line of a completion notification.
func gazelleLookupsLine(lookups []GazelleLookup) string {
	parts := make([]string, 0, len(lookups))
	for _, l := range lookups {
		parts = append(parts, l.ShortName+" "+gazelleOutcomeText(l))
	}
	return "RED/OPS: " + strings.Join(parts, ", ")
}

// keyRejectedNotice is the System notice that opens when the site rejects the
// API key.
func keyRejectedNotice(site gazellemusic.Site) systemnotice.Kind {
	return systemnotice.Kind(site.SettingsName + "_key_rejected")
}

// gazelleKeyGensNow returns the key generation of each site.
func (s *Service) gazelleKeyGensNow() map[string]uint64 {
	s.gazelleNoticeMu.Lock()
	defer s.gazelleNoticeMu.Unlock()
	return maps.Clone(s.gazelleKeyGens)
}

// setKeyRejectedNotice opens or closes the key rejected notice of the site,
// unless a settings save changed the key of the site after generation gen.
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

// closeKeyRejectedNotices closes the key rejected notice of each site that
// answered.
func (s *Service) closeKeyRejectedNotices(lookups []GazelleLookup, clients *gazelleClientSet) {
	for _, l := range lookups {
		if l.Status == GazelleMatch || l.Status == GazelleNoMatch {
			s.setKeyRejectedNotice(gazelleLookupSite(l), clients.keyGens[l.Site], false, "")
		}
	}
}

// GazelleKeysSaved takes the settings that a save stored, as the request sent
// them (a typed key, the placeholder of a stored key, or "" for no key), and
// the API hosts whose key the save checked with the site. It closes the key
// rejected notice of each site that the save fixes: the check passed, the key
// is cleared, or RED/OPS is off. A site whose key or switch changed gets a new
// key generation, so a client from before the save leaves its notice alone.
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

// keyRejectedOpen reports that the key rejected notice of the site is open.
func (s *Service) keyRejectedOpen(site gazellemusic.Site) bool {
	if s.systemNotices == nil {
		return false
	}
	kind := keyRejectedNotice(site)
	return slices.ContainsFunc(s.systemNotices.List(), func(n systemnotice.Notice) bool { return n.Kind == kind })
}
