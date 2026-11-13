// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// filterCapture is a file in testdata/filters of go-qbittorrent, which its
// capture command writes from a real qBittorrent release.
type filterCapture struct {
	Version              string                       `json:"version"`
	WebAPIVersion        string                       `json:"webapi_version"`
	ReturnedEveryTorrent []string                     `json:"returned_every_torrent"`
	Verdicts             map[string]map[string]string `json:"verdicts"`
	Categories           []struct {
		Value         string `json:"value"`
		Subcategories bool   `json:"subcategories"`
		Torrents      []struct {
			Category string `json:"category"`
			Returned bool   `json:"returned"`
		} `json:"torrents"`
	} `json:"categories"`
	Tags []struct {
		Value    string `json:"value"`
		Torrents []struct {
			Tags     string `json:"tags"`
			Returned bool   `json:"returned"`
		} `json:"torrents"`
	} `json:"tags"`
}

func (c *filterCapture) since(version string) bool {
	return semver.MustParse(strings.TrimPrefix(c.Version, "v")).GreaterThanEqual(semver.MustParse(version))
}

func (c *filterCapture) unknownToRelease(filter string) bool {
	return slices.Contains(c.ReturnedEveryTorrent, filter)
}

// A statusCase is one recorded verdict that the status rule does not give.
type statusCase struct {
	capture       *filterCapture
	status, state string
	// verdict is the verdict of qBittorrent: yes, no, or mixed.
	verdict string
}

// A statusDifference is a deliberate difference between the status rule and
// qBittorrent, with its reason.
type statusDifference struct {
	name, reason string
	applies      func(statusCase) bool
}

// sometimesShown are the differences that one capture run shows and the next
// can miss.
var sometimesShown = []string{"pausedUP and stoppedUP stay out of Running"}

// statusDifferences are the deliberate differences of ADR 0043. A difference
// that is not listed fails the test, and so does a listed one that no
// recorded result shows.
var statusDifferences = []statusDifference{
	{
		"an unknown status gives no rows",
		"a typo in a bulk action touches nothing; qBittorrent returns every torrent",
		func(c statusCase) bool {
			_, known := lookupStatus(c.status)
			return !known && c.capture.unknownToRelease(c.status)
		},
	},
	{
		"each filter name works on each version",
		"qBittorrent returns every torrent for a filter name that the release does not have",
		func(c statusCase) bool {
			return c.capture.unknownToRelease(c.status) &&
				slices.Contains([]string{"running", "resumed", "stopped", "paused", "checking", "moving"}, c.status)
		},
	},
	{
		"uploading means seeding",
		"the UI and the API agree; qBittorrent treats uploading as unknown",
		func(c statusCase) bool { return c.status == "uploading" && c.capture.unknownToRelease(c.status) },
	},
	{
		"state names, stalled_seeding, and the Tracker health statuses narrow the list",
		"qBittorrent treats them as unknown and returns every torrent",
		func(c statusCase) bool {
			_, isState := statesByLowerName[strings.ToLower(c.status)]
			_, isHealth := namedStatuses[c.status]
			isHealth = isHealth && namedStatuses[c.status].health != ""
			return c.capture.unknownToRelease(c.status) && (isState || isHealth || c.status == "stalled_seeding")
		},
	},
	{
		"a stopped torrent in error, missingFiles, or checkingResumeData counts as running",
		"qBittorrent reads a stopped flag that its API does not return, so one capture run can differ from the next",
		func(c statusCase) bool {
			if !slices.Contains([]string{"error", "missingFiles", "checkingResumeData"}, c.state) {
				return false
			}
			switch c.status {
			case "stopped", "paused":
				return c.verdict == "yes" || c.verdict == "mixed"
			case "running", "resumed":
				return c.verdict == "no" || c.verdict == "mixed"
			}
			return false
		},
	},
	{
		"a stopped torrent that rechecks on 4.3.9 counts as running",
		"4.3.9 shows checkingUP for a stopped torrent that rechecks, and the API does not return the stopped flag",
		func(c statusCase) bool {
			return c.capture.Version == "v4.3.9" && c.state == "checkingUP" &&
				(c.status == "resumed" || c.status == "paused") && c.verdict == "mixed"
		},
	},
	{
		"pausedUP and stoppedUP stay out of Running",
		"a torrent that has just stopped can still count as running in qBittorrent for a moment",
		func(c statusCase) bool {
			return (c.state == "pausedUP" || c.state == "stoppedUP") &&
				(c.status == "running" || c.status == "resumed") && c.verdict == "mixed"
		},
	},
	{
		"stopped torrents stay out of Downloading",
		"qui keeps its Downloading list of running downloads",
		func(c statusCase) bool {
			return c.status == "downloading" && (c.state == "pausedDL" || c.state == "stoppedDL") && c.verdict == "yes"
		},
	},
	{
		"active follows one state list on each version",
		"below 5.2, qBittorrent also counts metaDL, forcedMetaDL, moving, and a stalledDL torrent that uploads; " +
			"from 5.2, it uses the transfer rate",
		func(c statusCase) bool {
			if c.status != "active" && c.status != "inactive" {
				return false
			}
			return c.capture.since("5.2.0") ||
				slices.Contains([]string{"metaDL", "forcedMetaDL", "moving", "stalledDL"}, c.state)
		},
	},
}

func loadFilterCaptures(t *testing.T) map[string]*filterCapture {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(goQbittorrentDir(t), "testdata", "filters", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	captures := make(map[string]*filterCapture, len(files))
	for _, file := range files {
		body, err := os.ReadFile(file)
		require.NoError(t, err)
		var c filterCapture
		require.NoError(t, json.Unmarshal(body, &c))
		captures[c.Version] = &c
	}
	return captures
}

// TestStatusRuleMatchesQBittorrent compares the status rule with the recorded
// results of real qBittorrent releases.
func TestStatusRuleMatchesQBittorrent(t *testing.T) {
	t.Parallel()

	shown := map[string]bool{}
	for version, capture := range loadFilterCaptures(t) {
		for status, states := range capture.Verdicts {
			def, known := lookupStatus(status)
			for state, verdict := range states {
				qui := "no"
				if known && def.matches(&qbt.Torrent{State: qbt.TorrentState(state)}, nil) {
					qui = "yes"
				}
				if qui == verdict {
					continue
				}
				c := statusCase{capture: capture, status: status, state: state, verdict: verdict}
				i := slices.IndexFunc(statusDifferences, func(d statusDifference) bool { return d.applies(c) })
				if i < 0 {
					t.Errorf("%s: status %q, state %q: qBittorrent says %s, qui says %s", version, status, state, verdict, qui)
					continue
				}
				shown[statusDifferences[i].name] = true
			}
		}
	}
	for _, d := range statusDifferences {
		require.True(t, shown[d.name] || slices.Contains(sometimesShown, d.name), "no recorded result shows the difference %q", d.name)
	}
}

// A filterCase is one recorded category or tag result that the rule does not give.
type filterCase struct {
	capture *filterCapture
	webAPI  *semver.Version
	// kind is "category" or "tag".
	kind          string
	subcategories bool
	// returned is what qBittorrent did with the probe torrent.
	returned bool
}

// filterDifferences are the deliberate category and tag differences of ADR 0043.
var filterDifferences = []struct {
	name, reason string
	applies      func(filterCase) bool
}{
	{
		"an exact category match below WebAPI 2.9.0",
		"the API cannot report the subcategories setting, and qBittorrent includes subcategories if the user turned it on",
		func(c filterCase) bool {
			return c.kind == "category" && !versionSupports(c.webAPI, CapSubcategories) && c.subcategories && c.returned
		},
	},
	{
		"a tag narrows the list on 4.3.9",
		"4.3.9 has no tag parameter, so it returns every torrent for tag=x and for tag=",
		func(c filterCase) bool { return c.kind == "tag" && c.capture.Version == "v4.3.9" && c.returned },
	},
}

// TestCategoryAndTagRulesMatchQBittorrent compares the category and tag rules
// with the recorded results. A difference that filterDifferences does not
// list fails the test, and so does a listed one that no recorded result shows.
func TestCategoryAndTagRulesMatchQBittorrent(t *testing.T) {
	t.Parallel()

	shown := map[string]bool{}
	differs := func(c filterCase) bool {
		for _, d := range filterDifferences {
			if d.applies(c) {
				shown[d.name] = true
				return true
			}
		}
		return false
	}
	for version, capture := range loadFilterCaptures(t) {
		webAPI := semver.MustParse(capture.WebAPIVersion)
		require.NotEmpty(t, capture.Categories, version)
		require.NotEmpty(t, capture.Tags, version)
		for _, c := range capture.Categories {
			useSubcategories := resolveUseSubcategories(versionSupports(webAPI, CapSubcategories),
				versionSupports(webAPI, CapSubcategoriesAlwaysEnabled),
				&qbt.MainData{ServerState: qbt.ServerState{UseSubcategories: c.Subcategories}}, nil)
			rule := newTorrentListRule(FilterOptions{Categories: []string{c.Value}}, nil, useSubcategories, trackerSources{})
			for _, probe := range c.Torrents {
				qui := rule.match(&qbt.Torrent{Category: probe.Category})
				if qui == probe.Returned || differs(filterCase{capture, webAPI, "category", c.Subcategories, probe.Returned}) {
					continue
				}
				t.Errorf("%s: category %q (subcategories %t), torrent in %q: qBittorrent returned it: %t, qui: %t",
					version, c.Value, c.Subcategories, probe.Category, probe.Returned, qui)
			}
		}
		for _, c := range capture.Tags {
			rule := newTorrentListRule(FilterOptions{Tags: []string{c.Value}}, nil, false, trackerSources{})
			for _, probe := range c.Torrents {
				qui := rule.match(&qbt.Torrent{Tags: probe.Tags})
				if qui == probe.Returned || differs(filterCase{capture, webAPI, "tag", false, probe.Returned}) {
					continue
				}
				t.Errorf("%s: tag %q, torrent with tags %q: qBittorrent returned it: %t, qui: %t",
					version, c.Value, probe.Tags, probe.Returned, qui)
			}
		}
	}
	for _, d := range filterDifferences {
		require.True(t, shown[d.name], "no recorded result shows the difference %q", d.name)
	}
}
