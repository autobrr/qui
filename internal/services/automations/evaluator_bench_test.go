// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

// A rule shaped like a real one: a top-level AND of an OR group plus a few
// leaves, mixing string, tag and numeric fields.
func benchRule() *models.RuleCondition {
	return &models.RuleCondition{
		Operator: models.OperatorAnd,
		Conditions: []*models.RuleCondition{
			{
				Operator: models.OperatorOr,
				Conditions: []*models.RuleCondition{
					{Field: models.FieldCategory, Operator: models.OperatorEqual, Value: "movies"},
					{Field: models.FieldCategory, Operator: models.OperatorEqual, Value: "tv"},
					{Field: models.FieldName, Operator: models.OperatorContains, Value: "2160P"},
				},
			},
			{Field: models.FieldTags, Operator: models.OperatorNotContains, Value: "permaseed"},
			{Field: models.FieldRatio, Operator: models.OperatorGreaterThan, Value: "1.5"},
			{Field: models.FieldName, Operator: models.OperatorEndsWith, Value: "-GRPA"},
		},
	}
}

func benchEvalTorrents(count int) []qbt.Torrent {
	groups := []string{"GRPA", "GrpB", "grpc"}
	categories := []string{"", "movies", "tv", "music"}
	tagSets := []string{"", "cross-seed", "hardlinked, permaseed"}

	torrents := make([]qbt.Torrent, count)
	for i := range torrents {
		torrents[i] = qbt.Torrent{
			Hash: fmt.Sprintf("%040x", i),
			Name: fmt.Sprintf("Some.Release.Title.%d.S%02dE%02d.2160p.WEB-DL.DDP5.1-%s",
				i%400, i%12+1, i%24+1, groups[i%len(groups)]),
			Category: categories[i%len(categories)],
			Tags:     tagSets[i%len(tagSets)],
			Ratio:    float64(i%50) * 0.1,
			State:    qbt.TorrentStateUploading,
		}
	}
	return torrents
}

// BenchmarkEvaluateRule walks a rule over a library the way a scheduled
// automation run does.
func BenchmarkEvaluateRule(b *testing.B) {
	cond := benchRule()
	torrents := benchEvalTorrents(10000)
	ctx := &EvalContext{}

	for b.Loop() {
		matches := 0
		for i := range torrents {
			if EvaluateConditionWithContext(cond, torrents[i], ctx, 0) {
				matches++
			}
		}
		if matches == 0 {
			b.Fatal("expected some matches")
		}
	}
}
