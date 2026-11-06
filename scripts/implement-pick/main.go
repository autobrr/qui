// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Command implement-pick picks the next issue for the Implementer workflow.
// The workflow fetches the open ready-for-agent issues with GraphQL and gives
// them to the command, so the command needs no network.
//
//	go run ./scripts/implement-pick refs <issues.json>
//	go run ./scripts/implement-pick pick <issues.json> <states.json>
//
// refs prints each blocker that an issue body names, one per line, as
// owner/repo#N. The workflow reads the state of each one and writes
// states.json, a map from the blocker to its state.
//
// pick prints the number of the oldest eligible issue, or nothing when no
// issue is eligible. It writes the reason for each skipped issue to stderr.
package main

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type issue struct {
	Number    int          `json:"number"`
	CreatedAt time.Time    `json:"createdAt"`
	Body      string       `json:"body"`
	Labels    nodes[label] `json:"labels"`
	Assignees struct {
		TotalCount int `json:"totalCount"`
	} `json:"assignees"`
	Parent           *link `json:"parent"`
	SubIssuesSummary struct {
		Total int `json:"total"`
	} `json:"subIssuesSummary"`
	BlockedBy                      nodes[link] `json:"blockedBy"`
	ClosedByPullRequestsReferences nodes[link] `json:"closedByPullRequestsReferences"`
}

type nodes[T any] struct {
	Nodes []T `json:"nodes"`
}

type label struct {
	Name string `json:"name"`
}

// link is a linked issue or pull request. Repository is empty for a pull
// request.
type link struct {
	Number     int    `json:"number"`
	State      string `json:"state"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

var (
	bugCategory    = regexp.MustCompile(`(?im)^\s*\**category:?\**:?\s*bug\b`)
	blockedByLine  = regexp.MustCompile(`(?im)^\s*\**blocked by:?\**:?(.*)$`)
	issueReference = regexp.MustCompile(`(?:https://github\.com/)?([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?(?:/(?:issues|pull)/|#)([0-9]+)`)
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "implement-pick:", err)
		os.Exit(2)
	}
}

func run(args []string) error {
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return errors.New("GITHUB_REPOSITORY is not set")
	}
	switch {
	case len(args) == 2 && args[0] == "refs":
		issues, err := readJSON[[]issue](args[1])
		if err != nil {
			return err
		}
		var refs []string
		for _, is := range issues {
			refs = append(refs, bodyBlockers(repo, is.Body)...)
		}
		slices.Sort(refs)
		for _, r := range slices.Compact(refs) {
			fmt.Println(r)
		}
	case len(args) == 3 && args[0] == "pick":
		issues, err := readJSON[[]issue](args[1])
		if err != nil {
			return err
		}
		states, err := readJSON[map[string]string](args[2])
		if err != nil {
			return err
		}
		picks, skips := eligible(repo, issues, states)
		for _, s := range skips {
			fmt.Fprintln(os.Stderr, s)
		}
		if len(picks) > 0 {
			fmt.Println(picks[0])
		}
	default:
		return errors.New("usage: implement-pick refs <issues.json> | pick <issues.json> <states.json>")
	}
	return nil
}

func readJSON[T any](path string) (T, error) {
	var v T
	raw, err := os.ReadFile(path) //nolint:gosec // the workflow passes the path
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("%s: %w", path, err)
	}
	return v, nil
}

// eligible returns the issues oldest first, and a reason for each skip.
func eligible(repo string, issues []issue, states map[string]string) ([]int, []string) {
	var picks []issue
	var skips []string
	for _, is := range issues {
		if reason := skipReason(repo, is, states); reason != "" {
			skips = append(skips, fmt.Sprintf("#%d: %s", is.Number, reason))
			continue
		}
		picks = append(picks, is)
	}
	slices.SortFunc(picks, func(a, b issue) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.Number, b.Number))
	})
	numbers := make([]int, 0, len(picks))
	for _, is := range picks {
		numbers = append(numbers, is.Number)
	}
	return numbers, skips
}

func skipReason(repo string, is issue, states map[string]string) string {
	labels := make(map[string]bool)
	for _, l := range is.Labels.Nodes {
		labels[l.Name] = true
	}
	switch {
	case !labels["ready-for-agent"]:
		return "no ready-for-agent label"
	case !labels["bug"] && !bugCategory.MatchString(is.Body):
		return "not a bug"
	case is.Parent != nil:
		return fmt.Sprintf("part of #%d", is.Parent.Number)
	case is.SubIssuesSummary.Total > 0:
		return "has sub-issues"
	case is.Assignees.TotalCount > 0:
		return "assigned"
	case labels["rls"]:
		return "waits for autobrr/rls"
	case labels["implementer-failed"]:
		return "the Implementer failed on it"
	}
	for _, pr := range is.ClosedByPullRequestsReferences.Nodes {
		if pr.State == "OPEN" {
			return fmt.Sprintf("pull request #%d closes it", pr.Number)
		}
	}
	for _, b := range is.BlockedBy.Nodes {
		if !strings.EqualFold(b.State, "closed") {
			return fmt.Sprintf("blocked by %s#%d", b.Repository.NameWithOwner, b.Number)
		}
	}
	for _, ref := range bodyBlockers(repo, is.Body) {
		// A blocker that the workflow could not read counts as open.
		if !strings.EqualFold(states[ref], "closed") {
			return "blocked by " + ref
		}
	}
	return ""
}

// bodyBlockers returns the blockers as owner/repo#N.
func bodyBlockers(repo, body string) []string {
	parts := blockedBySections(body)
	for _, m := range blockedByLine.FindAllStringSubmatch(body, -1) {
		parts = append(parts, m[1])
	}
	matches := issueReference.FindAllStringSubmatch(strings.Join(parts, "\n"), -1)
	refs := make([]string, 0, len(matches))
	for _, m := range matches {
		refs = append(refs, cmp.Or(m[1], repo)+"#"+m[2])
	}
	return refs
}

// blockedBySections returns the text under each "Blocked by" heading, up to
// the next heading.
func blockedBySections(body string) []string {
	src := []byte(body)
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))
	var sections []string
	start := -1
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		h, ok := n.(*ast.Heading)
		if !ok || h.Lines().Len() == 0 {
			continue
		}
		if start >= 0 {
			sections = append(sections, body[start:h.Lines().At(0).Start])
			start = -1
		}
		if strings.EqualFold(strings.TrimSpace(string(h.Lines().Value(src))), "blocked by") {
			start = h.Lines().At(h.Lines().Len() - 1).Stop
		}
	}
	if start >= 0 {
		sections = append(sections, body[start:])
	}
	return sections
}
