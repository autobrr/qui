// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const repo = "autobrr/qui"

func newIssue(number int, day int, labels ...string) issue {
	var is issue
	is.Number = number
	is.CreatedAt = time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC)
	for _, l := range append([]string{"ready-for-agent"}, labels...) {
		is.Labels.Nodes = append(is.Labels.Nodes, label{l})
	}
	return is
}

func bug(number int, day int, labels ...string) issue {
	return newIssue(number, day, append([]string{"bug"}, labels...)...)
}

func withBody(is issue, body string) issue {
	is.Body = body
	return is
}

func TestEligible(t *testing.T) {
	assigned := bug(1, 1)
	assigned.Assignees.TotalCount = 1
	child := bug(1, 1)
	child.Parent = &link{Number: 9}
	parent := bug(1, 1)
	parent.SubIssuesSummary.Total = 2
	linkedOpen := bug(1, 1)
	linkedOpen.BlockedBy.Nodes = []link{{Number: 5, State: "OPEN"}}
	linkedOpen.BlockedBy.Nodes[0].Repository.NameWithOwner = "autobrr/go-qbittorrent"
	linkedClosed := bug(1, 1)
	linkedClosed.BlockedBy.Nodes = []link{{Number: 5, State: "CLOSED"}}
	withPR := bug(1, 1)
	withPR.ClosedByPullRequestsReferences.Nodes = []link{{Number: 7, State: "OPEN"}}

	tests := []struct {
		name   string
		issues []issue
		states map[string]string
		want   []int
		reason string
	}{
		{name: "bug label", issues: []issue{bug(1, 1)}, want: []int{1}},
		{name: "category bug in the brief", issues: []issue{withBody(newIssue(1, 1), "## Agent Brief\n\n**Category:** bug\n")}, want: []int{1}},
		{name: "enhancement", issues: []issue{withBody(newIssue(1, 1, "enhancement"), "**Category:** enhancement")}, reason: "#1: not a bug"},
		{name: "no ready-for-agent label", issues: []issue{{Number: 1}}, reason: "#1: no ready-for-agent label"},
		{name: "has a parent", issues: []issue{child}, reason: "#1: part of #9"},
		{name: "has sub-issues", issues: []issue{parent}, reason: "#1: has sub-issues"},
		{name: "assigned", issues: []issue{assigned}, reason: "#1: assigned"},
		{name: "rls", issues: []issue{bug(1, 1, "rls")}, reason: "#1: waits for autobrr/rls"},
		{name: "implementer-failed", issues: []issue{bug(1, 1, "implementer-failed")}, reason: "#1: the Implementer failed on it"},
		{name: "issue link to an open blocker", issues: []issue{linkedOpen}, reason: "#1: blocked by autobrr/go-qbittorrent#5"},
		{name: "issue link to a closed blocker", issues: []issue{linkedClosed}, want: []int{1}},
		{name: "blocked by line in this repo", issues: []issue{withBody(bug(1, 1), "Blocked by #5.")},
			states: map[string]string{"autobrr/qui#5": "open"}, reason: "#1: blocked by autobrr/qui#5"},
		{name: "blocked by line in another repo", issues: []issue{withBody(bug(1, 1), "Blocked by: autobrr/go-qbittorrent#150")},
			states: map[string]string{"autobrr/go-qbittorrent#150": "open"}, reason: "#1: blocked by autobrr/go-qbittorrent#150"},
		{name: "bold blocked by line", issues: []issue{withBody(bug(1, 1), "**Blocked by:** #5")},
			states: map[string]string{"autobrr/qui#5": "open"}, reason: "#1: blocked by autobrr/qui#5"},
		{name: "blocked by section", issues: []issue{withBody(bug(1, 1), "## Blocked by\n\n- #4: done\n- autobrr/rls#8: open\n\n## Notes\n\n- #6")},
			states: map[string]string{"autobrr/qui#4": "closed", "autobrr/rls#8": "open"}, reason: "#1: blocked by autobrr/rls#8"},
		{name: "heading in a code block does not end a blocked by section", issues: []issue{withBody(bug(1, 1), "## Blocked by\n\n```sh\n# note\n```\n\n- #5\n")},
			states: map[string]string{"autobrr/qui#5": "open"}, reason: "#1: blocked by autobrr/qui#5"},
		{name: "heading after a blocked by section ends it", issues: []issue{withBody(bug(1, 1), "## Blocked by\n\n- #4\n\n## Notes\n\n- #6")},
			states: map[string]string{"autobrr/qui#4": "closed"}, want: []int{1}},
		{name: "closed blocker", issues: []issue{withBody(bug(1, 1), "Blocked by #5")},
			states: map[string]string{"autobrr/qui#5": "closed"}, want: []int{1}},
		{name: "blocker that the workflow could not read", issues: []issue{withBody(bug(1, 1), "Blocked by #5")},
			reason: "#1: blocked by autobrr/qui#5"},
		{name: "open pull request from a person", issues: []issue{withPR}, reason: "#1: pull request #7 closes it"},
		{name: "oldest first", issues: []issue{bug(3, 9), bug(1, 4), bug(2, 4)}, want: []int{1, 2, 3}},
		{name: "no eligible issue", issues: []issue{bug(1, 1, "rls"), assigned}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, skips := eligible(repo, tt.issues, tt.states)
			if tt.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
			if tt.reason != "" {
				assert.Equal(t, []string{tt.reason}, skips)
			}
		})
	}
}

// The backlog of 2026-11-06, with the issue bodies cut to the lines that the
// pick reads, after the cleanup in #3847.
func TestEligibleBacklog(t *testing.T) {
	issues, err := readJSON[[]issue]("testdata/backlog-2026-11-06.json")
	require.NoError(t, err)
	states, err := readJSON[map[string]string]("testdata/states-2026-11-06.json")
	require.NoError(t, err)

	closed := []int{2719, 3117}
	toHuman := []int{2496, 2552, 3692, 2735, 2736, 3664, 2705, 3165, 3454, 2801, 3181}
	issues = slices.DeleteFunc(issues, func(is issue) bool {
		return slices.Contains(closed, is.Number) || slices.Contains(toHuman, is.Number)
	})
	i := slices.IndexFunc(issues, func(is issue) bool { return is.Number == 2495 })
	require.GreaterOrEqual(t, i, 0)
	issues[i].Body = "Blocked by autobrr/go-qbittorrent#150\n" + issues[i].Body

	got, _ := eligible(repo, issues, states)
	assert.Equal(t, []int{
		2512, 2523, 2524, 2538, 2539, 2550, 2551, 2553, 2564, 2664,
		2712, 3012, 3288, 3351, 3453, 3577, 3578, 3629, 3691, 3784,
	}, got)
}
