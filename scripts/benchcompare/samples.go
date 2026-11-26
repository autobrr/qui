// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/perf/benchfmt"
)

type series map[string][]float64

type samples struct {
	names  []string
	byName map[string]series
	cpu    string
}

func readSamples(out []byte, procs int) (samples, error) {
	var s samples
	suffix := "-" + strconv.Itoa(procs)
	reader := benchfmt.NewReader(bytes.NewReader(out), "benchmark output")
	for reader.Scan() {
		res, ok := reader.Result().(*benchfmt.Result)
		if !ok {
			continue
		}
		name := "Benchmark" + string(res.Name.Full())
		if procs > 1 {
			name = strings.TrimSuffix(name, suffix)
		}
		for _, v := range res.Values {
			s.add(name, v.Unit, v.Value)
		}
		s.cpu = strings.TrimSpace(res.GetConfig("cpu"))
	}
	return s, reader.Err()
}

func (s *samples) add(name, unit string, values ...float64) {
	if s.byName == nil {
		s.byName = map[string]series{}
	}
	if s.byName[name] == nil {
		s.byName[name] = series{}
		s.names = append(s.names, name)
	}
	s.byName[name][unit] = append(s.byName[name][unit], values...)
}

func (s *samples) merge(more samples) {
	for _, name := range more.names {
		for unit, values := range more.byName[name] {
			s.add(name, unit, values...)
		}
	}
	if more.cpu != "" {
		s.cpu = more.cpu
	}
}

func (s *samples) values(name, unit string) []float64 {
	return slices.Clone(s.byName[name][unit])
}
