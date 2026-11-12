// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// scopeAnswer says if a torrent is in the Rule scope of a rule, and if not, why.
type scopeAnswer uint8

const (
	scopeIn scopeAnswer = iota
	// scopeOtherTracker: the tracker pattern of the rule does not match the torrent.
	scopeOtherTracker
	// scopeSkipWindow: a live run selected the torrent for a delete a short time ago. No rule evaluates it.
	scopeSkipWindow
	// scopeNoValue: the torrent has no value for a field that the rule reads (ADR 0039).
	scopeNoValue
)

// ruleScope answers which torrents each rule evaluates in one run, dry run, or preview. Every
// pass over the torrents of a rule asks it. It also gives the tracker domains of a torrent, which
// it collects one time.
type ruleScope struct {
	skipped  map[string]struct{}
	trackers qbittorrent.TrackerLookup
	// noValue holds, for each rule ID, the field table tests of the fields that the rule reads.
	noValue   map[int][]func(qbt.Torrent) bool
	keyByHash map[string]string          // torrent hash -> domain set key
	domains   map[string][]string        // domain set key -> sorted domains
	matches   map[string]map[string]bool // tracker pattern -> domain set key -> match
}

// newRuleScope opens the Rule scope for the rules as saved. trackers holds the tracker lists of
// the torrents that the run loaded. skipped holds the torrents in the Skip window. A dry run and a
// preview pass nil for skipped. A preview that plans one Action of a rule passes the whole rule, so
// that a field in another Action still counts.
func newRuleScope(rules []*models.Automation, skipped map[string]struct{}, trackers qbittorrent.TrackerLookup) *ruleScope {
	r := &ruleScope{
		skipped:   skipped,
		trackers:  trackers,
		noValue:   make(map[int][]func(qbt.Torrent) bool),
		keyByHash: make(map[string]string),
		domains:   make(map[string][]string),
		matches:   make(map[string]map[string]bool),
	}
	for _, rule := range rules {
		for _, field := range noValueFields {
			if ruleUsesCondition(rule, field.field) {
				r.noValue[rule.ID] = append(r.noValue[rule.ID], field.noValue)
			}
		}
	}
	return r
}

// answer says if the torrent is in the Rule scope of the rule. When more than one test fails, the
// order is the Skip window, the tracker pattern, and the value.
func (r *ruleScope) answer(rule *models.Automation, torrent *qbt.Torrent) scopeAnswer {
	if _, skipped := r.skipped[torrent.Hash]; skipped {
		return scopeSkipWindow
	}
	if !r.matchesPattern(rule.TrackerPattern, torrent) {
		return scopeOtherTracker
	}
	for _, noValue := range r.noValue[rule.ID] {
		if noValue(*torrent) {
			return scopeNoValue
		}
	}
	return scopeIn
}

// groupDropsMember reports whether a member of the group drops out of the rule, so that a grouped
// Action skips the group. A member drops out only when it has no value for a field that the rule
// reads. A member on another tracker or in the Skip window stays a member.
func (r *ruleScope) groupDropsMember(rule *models.Automation, members []string, torrentByHash map[string]*qbt.Torrent) bool {
	tests := r.noValue[rule.ID]
	if len(tests) == 0 {
		return false
	}
	return slices.ContainsFunc(members, func(hash string) bool {
		member := *torrentByHash[hash]
		return slices.ContainsFunc(tests, func(noValue func(qbt.Torrent) bool) bool { return noValue(member) })
	})
}

// trackerDomains returns the sorted tracker domains of the torrent: the tracker in use, and the
// full tracker list when the run loaded it. The caller must not change the result.
func (r *ruleScope) trackerDomains(torrent *qbt.Torrent) []string {
	return r.domains[r.domainKey(torrent)]
}

// tracker returns the tracker domain that a preview row or an activity row shows.
func (r *ruleScope) tracker(torrent *qbt.Torrent) string {
	if domains := r.trackerDomains(torrent); len(domains) > 0 {
		return domains[0]
	}
	return ""
}

func (r *ruleScope) domainKey(torrent *qbt.Torrent) string {
	key, ok := r.keyByHash[torrent.Hash]
	if !ok {
		domains := collectTrackerDomains(torrent, r.trackers.Trackers(torrent.Hash))
		key = strings.Join(domains, ",")
		r.keyByHash[torrent.Hash] = key
		r.domains[key] = domains
	}
	return key
}

// Most torrents share their trackers, so each pattern is matched one time for each set of domains.
func (r *ruleScope) matchesPattern(pattern string, torrent *qbt.Torrent) bool {
	if pattern == "*" {
		return true
	}
	key := r.domainKey(torrent)
	byKey := r.matches[pattern]
	if byKey == nil {
		byKey = make(map[string]bool)
		r.matches[pattern] = byKey
	}
	matched, ok := byKey[key]
	if !ok {
		matched = matchesTracker(pattern, r.domains[key])
		byKey[key] = matched
	}
	return matched
}

func matchesTracker(pattern string, domains []string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == "" {
		return false
	}
	include, exclude := trackerPatternTokens(pattern)
	matches := func(token string) bool { return tokenMatchesDomain(token, domains) }
	if slices.ContainsFunc(exclude, matches) {
		return false
	}
	if len(include) == 0 {
		return len(exclude) > 0
	}
	return slices.ContainsFunc(include, matches)
}

// trackerPatternTokens splits a tracker pattern into its tokens, lowercase. A token with "!" excludes.
func trackerPatternTokens(pattern string) (include, exclude []string) {
	for token := range strings.FieldsFuncSeq(pattern, func(r rune) bool { return r == ',' || r == ';' || r == '|' }) {
		normalized := normalizeLowerTrim(token)
		if normalized == "" {
			continue
		}
		after, negated := strings.CutPrefix(normalized, "!")
		if !negated {
			include = append(include, normalized)
			continue
		}
		if excluded := normalizeLowerTrim(after); excluded != "" {
			exclude = append(exclude, excluded)
		}
	}
	return include, exclude
}

// A token is a domain, a domain suffix that starts with ".", or a glob.
func tokenMatchesDomain(token string, domains []string) bool {
	isGlob := strings.ContainsAny(token, "*?")
	for _, domain := range domains {
		d := normalizeLower(domain)
		if isGlob {
			ok, err := path.Match(token, d)
			if err != nil {
				log.Error().Err(err).Str("pattern", token).Msg("automations: invalid glob pattern")
				continue
			}
			if ok {
				return true
			}
			continue
		}
		if d == token || (strings.HasPrefix(token, ".") && strings.HasSuffix(d, token)) {
			return true
		}
	}
	return false
}

func collectTrackerDomains(t *qbt.Torrent, trackers []qbt.TorrentTracker) []string {
	domainSet := make(map[string]struct{})

	if t.Tracker != "" {
		if domain := qbittorrent.ExtractDomainFromURL(t.Tracker); domain != "" && domain != "Unknown" {
			domainSet[domain] = struct{}{}
		}
	}

	for _, tr := range trackers {
		if tr.Url == "" {
			continue
		}
		if domain := qbittorrent.ExtractDomainFromURL(tr.Url); domain != "" && domain != "Unknown" {
			domainSet[domain] = struct{}{}
		}
	}

	if len(domainSet) == 0 && t.Tracker != "" {
		if domain := sanitizeTrackerHost(t.Tracker); domain != "" {
			domainSet[domain] = struct{}{}
		}
	}

	domains := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domains = append(domains, d)
	}
	slices.Sort(domains)
	return domains
}

// skipWindow returns the torrents of the instance in the Skip window at now: a live run selected
// them for a delete less than SkipWithin ago.
func (s *Service) skipWindow(instanceID int, now time.Time) map[string]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	skipped := make(map[string]struct{})
	for hash, stamp := range s.lastApplied[instanceID] {
		if now.Sub(stamp) < s.cfg.SkipWithin {
			skipped[hash] = struct{}{}
		}
	}
	return skipped
}

// startSkipWindow stamps the torrents that a live run selected for a delete at now.
func (s *Service) startSkipWindow(instanceID int, hashes []string, now time.Time) {
	if len(hashes) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastApplied == nil {
		s.lastApplied = make(map[int]map[string]time.Time)
	}
	stamps := s.lastApplied[instanceID]
	if stamps == nil {
		stamps = make(map[string]time.Time, len(hashes))
		s.lastApplied[instanceID] = stamps
	}
	for _, hash := range hashes {
		stamps[hash] = now
	}
}

// dropOldSkipStamps removes the stamps from before cutoff. The caller holds s.mu.
func (s *Service) dropOldSkipStamps(cutoff time.Time) {
	for _, stamps := range s.lastApplied {
		maps.DeleteFunc(stamps, func(_ string, stamp time.Time) bool { return stamp.Before(cutoff) })
	}
}
