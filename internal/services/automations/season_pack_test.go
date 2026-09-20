// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/releases"
)

// newSeasonPackInstanceStore creates one instance per entry, active as given.
func newSeasonPackInstanceStore(t *testing.T, active []bool) *models.InstanceStore {
	t.Helper()
	store, err := models.NewInstanceStore(testdb.NewMigratedSQLite(t, "season-pack"), make([]byte, 32))
	require.NoError(t, err)
	for i, isActive := range active {
		inst, err := store.Create(t.Context(), fmt.Sprintf("inst-%d", i+1), "http://127.0.0.1:1", "", "", nil, nil, false, nil)
		require.NoError(t, err)
		if !isActive {
			_, err = store.SetActiveState(t.Context(), inst.ID, false)
			require.NoError(t, err)
		}
	}
	return store
}

func TestSeasonPackStatus(t *testing.T) {
	parser := releases.NewDefaultParser()
	packs := buildSeasonPackSet(parser, []qbt.Torrent{
		{Name: "Show.Name.S01.1080p.WEB-DL.DDP5.1.H.264-GRP"},
		{Name: "Show.Name.S01.Extended.Cut.1080p.WEB-DL.DDP5.1.H.264-GRP"},
		{Name: "Other.Show.S01-S03.1080p.WEB-DL.DDP5.1.H.264-GRP"},
		{Name: "Special.Show.S00.1080p.WEB-DL.DDP5.1.H.264-GRP"},
		{Name: "Show.Name.Complete.Series.1080p.WEB-DL.DDP5.1.H.264-GRP"},
		{Name: "Show.Name.S01E05E06.1080p.WEB-DL.DDP5.1.H.264-GRP"},
	})

	tests := []struct {
		name string
		want string
	}{
		{"Show.Name.S01.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusPack},
		{"Show.Name.S01E03.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusPacked},
		{"Show.Name.S01E03.The.Long.Night.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusPacked},
		{"Show.Name.S01E03.with.Audio.Description.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusUnpacked},
		{"Show.Name.S01E03.REPACK.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusUnpacked},
		{"Show.Name.S01E03.720p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusUnpacked},
		{"Show.Name.S01E03.1080p.WEB-DL.DDP5.1.H.264-OTHER", SeasonPackStatusUnpacked},
		{"Show.Name.S01E03.1080p.BluRay.DDP5.1.H.264-GRP", SeasonPackStatusUnpacked},
		{"Show.Name.S01E03.Extended.Cut.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusPacked},
		{"Show.Name.S02E01.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusUnpacked},
		{"Other.Show.S01-S03.1080p.WEB-DL.DDP5.1.H.264-GRP", ""},
		{"Other.Show.S02E04.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusUnpacked},
		{"Show.Name.S01E05E06.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusPacked},
		{"Show.Name.S02E05E06.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusUnpacked},
		{"Special.Show.S00E02.1080p.WEB-DL.DDP5.1.H.264-GRP", SeasonPackStatusPacked},
		{"Show.Name.Complete.Series.1080p.WEB-DL.DDP5.1.H.264-GRP", ""},
		{"Some.Movie.2021.1080p.BluRay.x264-GRP", ""},
		{"Daily.Show.2024.01.05.1080p.WEB.h264-GRP", ""},
		{"[SubGroup] Anime Show - 01 [1080p][HEVC]", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, seasonPackStatus(parser.Parse(tt.name), packs))
		})
	}
}

func TestSeasonPackStatus_EditionPackDoesNotCoverPlainEpisode(t *testing.T) {
	parser := releases.NewDefaultParser()
	packs := buildSeasonPackSet(parser, []qbt.Torrent{{Name: "Show.Name.S01.Extended.Cut.1080p.WEB-DL.DDP5.1.H.264-GRP"}})
	require.Equal(t, SeasonPackStatusUnpacked, seasonPackStatus(parser.Parse("Show.Name.S01E03.1080p.WEB-DL.DDP5.1.H.264-GRP"), packs))
	require.Equal(t, SeasonPackStatusPacked, seasonPackStatus(parser.Parse("Show.Name.S01E03.Extended.Cut.1080p.WEB-DL.DDP5.1.H.264-GRP"), packs))
}

// The run path builds a pack set only when an eligible rule uses the field.
func TestSeasonPackStatus_RuleGate(t *testing.T) {
	rule := func(field ConditionField) *models.Automation {
		return &models.Automation{Enabled: true, Conditions: &models.ActionConditions{Tags: []*models.TagAction{{
			Enabled:   true,
			Tags:      []string{"packed"},
			Condition: &RuleCondition{Field: field, Operator: OperatorEqual, Value: SeasonPackStatusPacked},
		}}}}
	}
	require.False(t, rulesUseCondition([]*models.Automation{rule(FieldName)}, FieldSeasonPackStatus))
	require.True(t, rulesUseCondition([]*models.Automation{rule(FieldSeasonPackStatus)}, FieldSeasonPackStatus))
	require.False(t, rulesUseCondition([]*models.Automation{rule(FieldSeasonPackStatus)}, FieldSeasonPackStatusAnyInstance))
	require.True(t, rulesUseCondition([]*models.Automation{rule(FieldSeasonPackStatusAnyInstance)}, FieldSeasonPackStatusAnyInstance))
}

func TestSeasonPackStatus_EpisodeRangeWithoutPack(t *testing.T) {
	parser := releases.NewDefaultParser()
	packs := buildSeasonPackSet(parser, []qbt.Torrent{{Name: "Show.Name.S01E05E06.1080p.WEB-DL.DDP5.1.H.264-GRP"}})
	require.Empty(t, packs, "an episode range is not a pack")
	require.Equal(t, SeasonPackStatusUnpacked, seasonPackStatus(parser.Parse("Show.Name.S01E05E06.1080p.WEB-DL.DDP5.1.H.264-GRP"), packs))
}

type seasonPackViewsReader struct {
	fakeFilesReader
	byInstance map[int][]qbittorrent.CrossInstanceTorrentView
	read       []int
}

func (r *seasonPackViewsReader) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	r.read = append(r.read, instanceID)
	return r.byInstance[instanceID], nil
}

func seasonPackView(name string) qbittorrent.CrossInstanceTorrentView {
	return qbittorrent.CrossInstanceTorrentView{TorrentView: &qbittorrent.TorrentView{Torrent: &qbt.Torrent{Name: name}}}
}

func TestBuildAnyInstanceSeasonPackSet_ReadsEveryActiveInstance(t *testing.T) {
	reader := &seasonPackViewsReader{byInstance: map[int][]qbittorrent.CrossInstanceTorrentView{
		1: {seasonPackView("Show.Name.S01E03.1080p.WEB-DL.DDP5.1.H.264-GRP")},
		2: {seasonPackView("Show.Name.S01.1080p.WEB-DL.DDP5.1.H.264-GRP")},
		3: {seasonPackView("Show.Name.S02.1080p.WEB-DL.DDP5.1.H.264-GRP")},
	}}
	s := &Service{
		instanceStore: newSeasonPackInstanceStore(t, []bool{true, true, false}),
		filesReader:   reader,
		releaseParser: releases.NewDefaultParser(),
	}

	packs := s.buildAnyInstanceSeasonPackSet(t.Context())

	require.Equal(t, []int{1, 2}, reader.read, "reads every active instance, skips the inactive one")
	require.Equal(t, SeasonPackStatusPacked, seasonPackStatus(s.releaseParser.Parse("Show.Name.S01E03.1080p.WEB-DL.DDP5.1.H.264-GRP"), packs))
	require.Equal(t, SeasonPackStatusUnpacked, seasonPackStatus(s.releaseParser.Parse("Show.Name.S02E03.1080p.WEB-DL.DDP5.1.H.264-GRP"), packs))
}

// The preview builds a pack set when only a score rule uses the field, as the run does.
func TestSeasonPackStatus_PreviewScoreRuleGate(t *testing.T) {
	rule := &models.Automation{SortingConfig: &models.SortingConfig{
		Type: models.SortingTypeScore,
		ScoreRules: []models.ScoreRule{{
			Type: models.ScoreRuleTypeConditional,
			Conditional: &models.ConditionalScoreRule{
				Score:     1,
				Condition: &RuleCondition{Field: FieldSeasonPackStatus, Operator: OperatorEqual, Value: SeasonPackStatusPacked},
			},
		}},
	}}
	evalCtx := &EvalContext{ReleaseParser: releases.NewDefaultParser()}
	(&Service{}).setupPreviewSeasonPackContext(t.Context(), rule, nil, []qbt.Torrent{{Name: "Show.Name.S01.1080p.WEB-DL.DDP5.1.H.264-GRP"}}, evalCtx)
	require.Len(t, evalCtx.SeasonPackSet, 1)
}
