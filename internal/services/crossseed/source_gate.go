// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
)

// sourceFilter limits which local torrents a cross-seed can match. The zero
// value passes every torrent.
type sourceFilter struct {
	categories        []string
	tags              []string
	excludeCategories []string
	excludeTags       []string
}

func (f sourceFilter) hasRules() bool {
	return len(f.categories) > 0 || len(f.tags) > 0 || len(f.excludeCategories) > 0 || len(f.excludeTags) > 0
}

// match reports whether a torrent is inside the filter. Matches are exact and
// case-sensitive, and exclude rules come ahead of include rules. The webhook
// check and the RSS loop call it for each torrent of an instance, so it reads
// the tags only when the filter has a tag rule, and it allocates nothing.
func (f sourceFilter) match(torrent *qbt.Torrent) bool {
	if slices.Contains(f.excludeCategories, torrent.Category) {
		return false
	}
	if len(f.categories) > 0 && !slices.Contains(f.categories, torrent.Category) {
		return false
	}
	if len(f.tags) == 0 && len(f.excludeTags) == 0 {
		return true
	}

	included := len(f.tags) == 0
	for tag := range strings.SplitSeq(torrent.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if slices.Contains(f.excludeTags, tag) {
			return false
		}
		included = included || slices.Contains(f.tags, tag)
	}
	return included
}

// sourceAnswer is what the Source gate says about one torrent.
type sourceAnswer int

const (
	sourcePass sourceAnswer = iota
	sourceOutsideFilter
	sourceErrored
	sourceMovingOrChecking
	sourceIncomplete
)

// lasting reports a refusal that does not end when qBittorrent finishes its
// current work. An errored source needs the user, so it is no match at any
// progress.
func (a sourceAnswer) lasting() bool {
	return a == sourceOutsideFilter || a == sourceErrored
}

// sourceGate is the one rule that says if a torrent can give files to a
// cross-seed now.
type sourceGate struct {
	recoverErrored bool
}

func (s *Service) sourceGate() sourceGate {
	return sourceGate{recoverErrored: s.recoverErroredTorrentsEnabled}
}

// check answers for one torrent. A moving torrent still reports the content
// path of its download path, so a cross-seed added from it misses its files.
func (g sourceGate) check(torrent *qbt.Torrent, filter sourceFilter) sourceAnswer {
	switch {
	case !filter.match(torrent):
		return sourceOutsideFilter
	case !g.recoverErrored && isSourceErrored(torrent.State):
		return sourceErrored
	case isSourceMovingOrChecking(torrent.State):
		return sourceMovingOrChecking
	case torrent.Progress < 1:
		return sourceIncomplete
	default:
		return sourcePass
	}
}

// requireSource returns why one named torrent cannot be a search source now.
func (s *Service) requireSource(torrent *qbt.Torrent) error {
	switch s.sourceGate().check(torrent, sourceFilter{}) {
	case sourceErrored:
		return ErrSourceErrored
	case sourceMovingOrChecking:
		return ErrSourceNotSettled
	case sourceIncomplete:
		return fmt.Errorf("%w: torrent %s is not fully downloaded (progress %.2f)", ErrTorrentNotComplete, torrent.Name, torrent.Progress)
	default:
		return nil
	}
}

// sourceTurnRefusal returns the refusal in err that a Library Scan run reports
// as a Skipped row with no cooldown, so the next run can search the torrent. It
// returns nil for any other err.
func sourceTurnRefusal(err error) error {
	switch {
	case errors.Is(err, ErrSourceNotSettled):
		return ErrSourceNotSettled
	case errors.Is(err, ErrSourceErrored):
		return ErrSourceErrored
	case errors.Is(err, ErrTorrentNotComplete):
		return ErrTorrentNotComplete
	default:
		return nil
	}
}

func isSourceErrored(state qbt.TorrentState) bool {
	return state == qbt.TorrentStateError || state == qbt.TorrentStateMissingFiles
}

func isSourceMovingOrChecking(state qbt.TorrentState) bool {
	return state == qbt.TorrentStateCheckingDl ||
		state == qbt.TorrentStateCheckingUp ||
		state == qbt.TorrentStateCheckingResumeData ||
		state == qbt.TorrentStateMoving
}

// sourceFilterCounts counts, by category, what a source filter kept and
// dropped. The RSS and webhook debug lines report it.
type sourceFilterCounts struct {
	kept     int
	included map[string]int
	excluded map[string]int
}

func (c *sourceFilterCounts) add(category string, kept bool) {
	if c.included == nil {
		c.included, c.excluded = make(map[string]int), make(map[string]int)
	}
	if kept {
		c.kept++
		c.included[category]++
		return
	}
	c.excluded[category]++
}

func (c *sourceFilterCounts) empty() bool {
	return c.included == nil
}

func categoryCountSummary(counts map[string]int) []string {
	summary := make([]string, 0, len(counts))
	for category, count := range counts {
		summary = append(summary, fmt.Sprintf("%s(%d)", category, count))
	}
	return summary
}
