// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/cover"
)

type selection struct {
	now       snapshot
	touched   []bench
	changed   map[bench]string
	uncovered []string
	stopped   bool
	noCode    bool
	otherGo   bool
}

type probe struct {
	bench   bench
	took    time.Duration
	blocks  map[block]bool
	reached map[block]bool
	blind   bool
}

type block struct {
	file string
	span
}

func selectBenchmarks(ctx context.Context, b *builder, baseCommit, headCommit string) (selection, error) {
	now, err := takeSnapshot(ctx, commitTree{b.repo, headCommit}, nil)
	if err != nil {
		return selection{}, err
	}
	was, err := takeSnapshot(ctx, commitTree{b.repo, baseCommit}, &now.config)
	if err != nil {
		return selection{}, err
	}
	s := selection{now: now, changed: changedWorkloads(was, now)}
	code, err := changedCode(ctx, b, baseCommit, headCommit, now)
	if err != nil {
		return s, err
	}
	s.otherGo = code.otherGo
	if s.noCode = code.empty() && len(s.changed) == 0; s.noCode {
		return s, nil
	}
	s.probeAll(ctx, b, headCommit, code)
	if s.otherGo {
		s.touched = s.comparable(now.benches)
	}
	return s, nil
}

func (s *selection) comparable(benches []bench) []bench {
	return slices.DeleteFunc(slices.Clone(benches), func(one bench) bool { return s.changed[one] != "" })
}

func changedWorkloads(was, now snapshot) map[bench]string {
	changed := map[bench]string{}
	for _, b := range now.benches {
		switch was.fingerprints[b] {
		case now.fingerprints[b]:
		case "":
			changed[b] = "the benchmark is new on this branch, so there is nothing to compare with"
		default:
			changed[b] = "its workload changes on this branch, so the two sides do not measure the same work"
		}
	}
	return changed
}

func (s *selection) probeAll(ctx context.Context, b *builder, commit string, code changes) {
	var candidates []bench
	for _, one := range s.now.benches {
		if len(code.reachedBy(one.dir)) > 0 {
			candidates = append(candidates, one)
		}
	}
	probes := make([]probe, 0, len(candidates))
	for i, one := range candidates {
		fmt.Fprintf(b.progress, "[%d/%d] %s, checking which changed code it runs\n", i+1, len(candidates), one)
		p := b.probe(ctx, commit, one, code.reachedBy(one.dir))
		if ctx.Err() != nil {
			s.stopped = true
			s.touched = s.comparable(candidates)
			return
		}
		probes = append(probes, p)
	}
	slices.SortStableFunc(probes, func(a, b probe) int { return cmp.Compare(a.took, b.took) })
	for _, p := range probes {
		if s.changed[p.bench] == "" && (p.blind || slices.ContainsFunc(code.units, func(u unit) bool { return code.touchedBy(p, u) })) {
			s.touched = append(s.touched, p.bench)
		}
	}
	s.uncovered = code.uncovered(probes)
}

func (c changes) uncovered(probes []probe) []string {
	all := map[block]bool{}
	for _, p := range probes {
		for b, runs := range p.blocks {
			all[b] = all[b] || runs
		}
	}
	gaps := map[string][]string{}
	for _, u := range c.units {
		if u.file == "" && c.coveredBy(probes, u) {
			continue
		}
		if gap := u.gap(all); gap != "" {
			place := cmp.Or(u.file, c.folder(u.pkg))
			gaps[place] = append(gaps[place], gap)
		}
	}
	lines := make([]string, 0, len(gaps))
	for _, place := range sortedKeys(gaps) {
		lines = append(lines, place+": "+strings.Join(gaps[place], ", "))
	}
	return lines
}

func (b *builder) probe(ctx context.Context, commit string, one bench, pkgs []string) (p probe) {
	p = probe{bench: one}
	started := time.Now()
	defer func() { p.took = time.Since(started) }()
	binary, err := b.binary(ctx, commit, one.dir, "-cover", "-covermode=atomic", "-coverpkg="+strings.Join(pkgs, ","))
	if err != nil {
		p.blind = true
		return p
	}
	started = time.Now()
	twice, failedTwice, err := b.profile(ctx, binary, commit, one, 2)
	thrice, failedThrice, errThrice := b.profile(ctx, binary, commit, one, 3)
	if err != nil || errThrice != nil {
		p.blind = true
		return p
	}
	p.blocks = grown(twice, thrice)
	p.reached = p.blocks
	if failedTwice || failedThrice {
		p.reached = grown(nil, thrice)
	}
	return p
}

type counts map[string]map[cover.ProfileBlock]int

func (b *builder) profile(ctx context.Context, binary, commit string, one bench, times int) (found counts, failed bool, err error) {
	file := filepath.Join(b.scratch, "cover-"+strconv.Itoa(times)+".out")
	if err := os.RemoveAll(file); err != nil {
		return nil, false, err
	}
	_, runErr := b.exec(ctx, binary, commit, one.dir, "-test.bench="+exactly(one.name), "-test.benchtime="+strconv.Itoa(times)+"x", "-test.coverprofile="+file)
	profiles, err := cover.ParseProfiles(file)
	if err != nil {
		return nil, false, err
	}
	found = counts{}
	for _, profile := range profiles {
		found[profile.FileName] = map[cover.ProfileBlock]int{}
		for _, place := range profile.Blocks {
			count := place.Count
			place.Count = 0
			found[profile.FileName][place] = count
		}
	}
	return found, runErr != nil, nil
}

func grown(fewer, more counts) map[block]bool {
	blocks := map[block]bool{}
	for file, places := range more {
		for place, count := range places {
			b := block{file, span{place.StartLine, place.EndLine}}
			blocks[b] = blocks[b] || count > fewer[file][place]
		}
	}
	return blocks
}

func (c changes) touchedBy(p probe, u unit) bool {
	if u.file == "" {
		return c.deps[p.bench.dir][u.pkg]
	}
	return p.touches(u)
}

func (c changes) coveredBy(probes []probe, u unit) bool {
	return slices.ContainsFunc(probes, func(p probe) bool { return c.touchedBy(p, u) })
}

func (p probe) touches(u unit) bool {
	for _, b := range u.within(p.reached) {
		if p.reached[b] {
			return true
		}
	}
	return false
}

func (u unit) within(blocks map[block]bool) []block {
	var whole, changed []block
	for b := range blocks {
		if b.file != path.Join(u.pkg, path.Base(u.file)) || !b.overlaps(u.span) {
			continue
		}
		whole = append(whole, b)
		if slices.ContainsFunc(u.parts, b.overlaps) {
			changed = append(changed, b)
		}
	}
	if len(changed) > 0 {
		return changed
	}
	return whole
}

func (u unit) gap(blocks map[block]bool) string {
	if u.file == "" {
		return u.name
	}
	within := u.within(blocks)
	run, missing := map[int]bool{}, map[int]bool{}
	for _, b := range within {
		for line := max(b.from, u.from); line <= min(b.to, u.to); line++ {
			if blocks[b] {
				run[line] = true
			} else if len(u.parts) == 0 || u.changes(line) {
				missing[line] = true
			}
		}
	}
	if len(within) == 0 {
		return fmt.Sprintf("%s (%s)", u.name, u.span)
	}
	maps.DeleteFunc(missing, func(line int, _ bool) bool { return run[line] })
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("%s (%s)", u.name, spans(slices.Sorted(maps.Keys(missing))))
}

func (u unit) changes(line int) bool {
	return slices.ContainsFunc(u.parts, func(s span) bool { return s.from <= line && line <= s.to })
}

func spans(lines []int) string {
	var parts []string
	for i := 0; i < len(lines); {
		s := span{lines[i], lines[i]}
		for i++; i < len(lines) && lines[i] == s.to+1; i++ {
			s.to = lines[i]
		}
		parts = append(parts, s.String())
	}
	return strings.Join(parts, ",")
}

type span struct{ from, to int }

func (s span) overlaps(other span) bool { return s.from <= other.to && other.from <= s.to }

func (s span) String() string {
	if s.from == s.to {
		return strconv.Itoa(s.from)
	}
	return fmt.Sprintf("%d-%d", s.from, s.to)
}
