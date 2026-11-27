// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"time"
)

type verdict string

const (
	verdictSame        verdict = "same"
	verdictExtraCopies verdict = "extra copies"
	verdictSeasonPack  verdict = "season pack"
	verdictDiffers     verdict = "differs"
	verdictFailed      verdict = "failed"
)

var verdicts = []verdict{verdictSame, verdictExtraCopies, verdictSeasonPack, verdictDiffers, verdictFailed}

type matchKey struct {
	instanceID int
	hash       string
}

func compare(a announce, old, now answer) result {
	r := result{Announce: a, Old: old, New: now}
	switch {
	case old.Error != "" || now.Error != "":
		r.Verdict = verdictFailed
	case old.Status == http.StatusNotFound && len(old.Matches) == 0 && now.Status == http.StatusOK && now.SeasonPack != nil && len(now.Matches) == 0:
		r.Verdict = verdictSeasonPack
	case old.Status != now.Status || old.CanCrossSeed != now.CanCrossSeed || old.Recommendation != now.Recommendation || !samePack(old.SeasonPack, now.SeasonPack):
		r.Verdict = verdictDiffers
	default:
		r.Verdict, r.Extra = compareMatches(old.Matches, now.Matches)
	}
	return r
}

func samePack(old, now *seasonPack) bool {
	if old == nil || now == nil {
		return old == now
	}
	return *old == *now
}

func compareMatches(old, now []match) (verdict, []match) {
	held := map[matchKey]string{}
	names := map[string]bool{}
	for _, m := range old {
		held[matchKey{m.InstanceID, m.TorrentHash}] = m.MatchType
		names[m.TorrentName] = true
	}
	var extra []match
	retyped := false
	for _, m := range now {
		key := matchKey{m.InstanceID, m.TorrentHash}
		matchType, ok := held[key]
		switch {
		case !ok:
			extra = append(extra, m)
		case matchType != m.MatchType:
			retyped = true
		}
		delete(held, key)
	}
	switch {
	case len(held) > 0, retyped, slices.ContainsFunc(extra, func(m match) bool { return !names[m.TorrentName] }):
		return verdictDiffers, nil
	case len(extra) > 0:
		return verdictExtraCopies, extra
	}
	return verdictSame, nil
}

type latency struct {
	count          int
	p50, p95, most time.Duration
}

type summary struct {
	total   int
	counts  map[verdict]int
	latency map[string]map[string]latency
	listed  []result
}

func summarize(results []result) summary {
	s := summary{total: len(results), counts: map[verdict]int{}, latency: map[string]map[string]latency{}}
	times := map[string]map[string][]time.Duration{"old": {}, "new": {}}
	for _, r := range results {
		s.counts[r.Verdict]++
		if r.Verdict != verdictSame {
			s.listed = append(s.listed, r)
		}
		for build, a := range map[string]answer{"old": r.Old, "new": r.New} {
			if a.Error == "" {
				group := statusGroup(a.Status)
				times[build][group] = append(times[build][group], time.Duration(a.Millis*float64(time.Millisecond)))
			}
		}
	}
	for build, groups := range times {
		s.latency[build] = map[string]latency{}
		for group, durations := range groups {
			slices.Sort(durations)
			s.latency[build][group] = latency{
				count: len(durations),
				p50:   durations[len(durations)/2],
				p95:   durations[min(len(durations)-1, len(durations)*95/100)],
				most:  durations[len(durations)-1],
			}
		}
	}
	return s
}

func statusGroup(status int) string {
	if status == http.StatusNotFound {
		return "404"
	}
	return "200 or 202"
}

func (s summary) agrees() bool {
	return s.counts[verdictDiffers] == 0 && s.counts[verdictFailed] == 0
}

func (s summary) write(w io.Writer) {
	fmt.Fprintf(w, "announces: %d\n", s.total)
	for _, v := range verdicts {
		fmt.Fprintf(w, "  %-13s %d\n", v, s.counts[v])
	}
	fmt.Fprintln(w, "latency of the webhook check:")
	for _, build := range []string{"old", "new"} {
		for _, group := range slices.Sorted(maps.Keys(s.latency[build])) {
			l := s.latency[build][group]
			fmt.Fprintf(w, "  %s %-10s n=%-6d p50=%-10s p95=%-10s max=%s\n", build, group, l.count, l.p50.Round(10*time.Microsecond), l.p95.Round(10*time.Microsecond), l.most.Round(10*time.Microsecond))
		}
	}
	slices.SortStableFunc(s.listed, func(a, b result) int {
		return cmp.Compare(slices.Index(verdicts, b.Verdict), slices.Index(verdicts, a.Verdict))
	})
	for _, r := range s.listed {
		fmt.Fprintf(w, "%s: %q size %d: old %s, new %s\n", r.Verdict, r.Announce.TorrentName, r.Announce.Size, r.Old.brief(), r.New.brief())
		for _, m := range r.Extra {
			fmt.Fprintf(w, "    extra match on instance %d: %s %q\n", m.InstanceID, m.TorrentHash, m.TorrentName)
		}
	}
	if s.agrees() {
		fmt.Fprintln(w, "PASS: no answer differs")
		return
	}
	fmt.Fprintf(w, "FAIL: %d answers differ and %d checks failed\n", s.counts[verdictDiffers], s.counts[verdictFailed])
}
