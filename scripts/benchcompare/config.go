// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const configPath = ".bench.json"

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

type limit struct {
	Percent float64 `json:"percent"`
	Min     float64 `json:"min"`
	Any     float64 `json:"any"`
}

type limits map[string]limit

type config struct {
	Limits       limits            `json:"limits"`
	Benchmarks   map[string]limits `json:"benchmarks"`
	Starts       map[string]string `json:"starts"`
	WorkloadDirs []string          `json:"workload_dirs"`
}

type bench struct{ dir, name string }

func (b bench) String() string { return b.dir + "." + b.name }

func parseBench(key string) (bench, bool) {
	dir, name, found := strings.Cut(key, ".Benchmark")
	return bench{dir, "Benchmark" + name}, found && dir != ""
}

func parseConfig(raw []byte) (config, error) {
	var c config
	if err := json.Unmarshal(raw, &c, json.RejectUnknownMembers(true)); err != nil {
		return c, err
	}
	if len(c.Limits) == 0 {
		return c, errors.New("limits has no unit")
	}
	if err := c.Limits.validate("limits"); err != nil {
		return c, err
	}
	for key, own := range c.Benchmarks {
		if err := own.validate(key); err != nil {
			return c, err
		}
		for unit := range own {
			if _, ok := c.Limits[unit]; !ok {
				return c, fmt.Errorf("%s: limits has no unit %s", key, unit)
			}
		}
	}
	for key, commit := range c.Starts {
		if _, ok := parseBench(key); !ok {
			return c, fmt.Errorf("starts: %s is not <package folder>.Benchmark<name>", key)
		}
		if !fullCommit.MatchString(commit) {
			return c, fmt.Errorf("starts: %s: %q is not a full commit hash", key, commit)
		}
	}
	return c, nil
}

func (l limits) validate(key string) error {
	for unit, one := range l {
		if one.Percent <= 0 || one.Min <= 0 || one.Any <= 0 {
			return fmt.Errorf("%s: %s: percent, min, and any must be above 0", key, unit)
		}
	}
	return nil
}

func (c config) limitFor(b bench, name, unit string) limit {
	for {
		if own, ok := c.Benchmarks[b.dir+"."+name][unit]; ok {
			return own
		}
		parent, _, found := strings.CutLast(name, "/")
		if !found {
			return c.Limits[unit]
		}
		name = parent
	}
}
