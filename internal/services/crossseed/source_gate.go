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

type sourceFilter struct {
	categories        []string
	tags              []string
	excludeCategories []string
	excludeTags       []string
}

func (f sourceFilter) hasRules() bool {
	return len(f.categories) > 0 || len(f.tags) > 0 || len(f.excludeCategories) > 0 || len(f.excludeTags) > 0
}

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

type sourceAnswer int

const (
	sourcePass sourceAnswer = iota
	sourceOutsideFilter
	sourceErrored
	sourceMovingOrChecking
	sourceIncomplete
)

func (a sourceAnswer) lasting() bool {
	return a == sourceOutsideFilter || a == sourceErrored
}

type sourceGate struct {
	recoverErrored bool
}

func (s *Service) sourceGate() sourceGate {
	return sourceGate{recoverErrored: s.recoverErroredTorrentsEnabled}
}

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
