// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"testing"

	"github.com/autobrr/qui/internal/models"
)

func ptrBool(v bool) *bool        { return &v }
func ptrInt(v int) *int           { return &v }
func ptrFloat(v float64) *float64 { return &v }
func ptrString(v string) *string  { return &v }

func TestApplyAutomationSettingsPatch_MergesFields(t *testing.T) {
	existing := models.CrossSeedAutomationSettings{
		Enabled:                      false,
		RunIntervalMinutes:           120,
		StartPaused:                  true,
		Category:                     stringPtr("tv"),
		RSSAutomationTags:            []string{"old"},
		SeededSearchTags:             []string{"old"},
		CompletionSearchTags:         []string{"old"},
		WebhookTags:                  []string{"old"},
		TargetInstanceIDs:            []int{1},
		TargetIndexerIDs:             []int{2},
		MaxResultsPerRun:             10,
		FindIndividualEpisodes:       false,
		SizeMismatchTolerancePercent: 5.0,
		UseCategoryFromIndexer:       false,
		RunExternalProgramID:         ptrInt(42),
		GazelleEnabled:               false,
		RedactedAPIKey:               "",
		OrpheusAPIKey:                "",
	}

	newCategory := " movies "
	patch := automationSettingsPatchRequest{
		Enabled:                      ptrBool(true),
		RunIntervalMinutes:           ptrInt(45),
		StartPaused:                  ptrBool(false),
		Category:                     optionalString{Set: true, Value: &newCategory},
		RSSAutomationTags:            &[]string{"new"},
		SeededSearchTags:             &[]string{"new-seeded"},
		TargetInstanceIDs:            &[]int{3, 4},
		TargetIndexerIDs:             &[]int{7},
		MaxResultsPerRun:             ptrInt(25),
		FindIndividualEpisodes:       ptrBool(true),
		SizeMismatchTolerancePercent: ptrFloat(12.5),
		UseCategoryFromIndexer:       ptrBool(true),
		RunExternalProgramID:         optionalInt{Set: true, Value: nil},
		GazelleEnabled:               ptrBool(true),
		RedactedAPIKey:               ptrString("red-key"),
		OrpheusAPIKey:                ptrString("ops-key"),
	}

	applyAutomationSettingsPatch(&existing, patch)

	if !existing.Enabled {
		t.Fatalf("expected enabled to be true")
	}
	if existing.RunIntervalMinutes != 45 {
		t.Fatalf("expected run interval 45, got %d", existing.RunIntervalMinutes)
	}
	if existing.StartPaused {
		t.Fatalf("expected startPaused to be false")
	}
	if existing.Category == nil || *existing.Category != "movies" {
		t.Fatalf("expected category 'movies', got %#v", existing.Category)
	}
	if len(existing.RSSAutomationTags) != 1 || existing.RSSAutomationTags[0] != "new" {
		t.Fatalf("unexpected rss automation tags: %#v", existing.RSSAutomationTags)
	}
	if len(existing.SeededSearchTags) != 1 || existing.SeededSearchTags[0] != "new-seeded" {
		t.Fatalf("unexpected seeded search tags: %#v", existing.SeededSearchTags)
	}
	// CompletionSearchTags and WebhookTags were not patched, should remain unchanged
	if len(existing.CompletionSearchTags) != 1 || existing.CompletionSearchTags[0] != "old" {
		t.Fatalf("unexpected completion search tags: %#v", existing.CompletionSearchTags)
	}
	if len(existing.WebhookTags) != 1 || existing.WebhookTags[0] != "old" {
		t.Fatalf("unexpected webhook tags: %#v", existing.WebhookTags)
	}
	if len(existing.TargetInstanceIDs) != 2 || existing.TargetInstanceIDs[0] != 3 || existing.TargetInstanceIDs[1] != 4 {
		t.Fatalf("unexpected target instance ids: %#v", existing.TargetInstanceIDs)
	}
	if len(existing.TargetIndexerIDs) != 1 || existing.TargetIndexerIDs[0] != 7 {
		t.Fatalf("unexpected target indexer ids: %#v", existing.TargetIndexerIDs)
	}
	if existing.MaxResultsPerRun != 25 {
		t.Fatalf("expected maxResultsPerRun 25, got %d", existing.MaxResultsPerRun)
	}
	if !existing.FindIndividualEpisodes {
		t.Fatalf("expected findIndividualEpisodes to be true")
	}
	if existing.SizeMismatchTolerancePercent != 12.5 {
		t.Fatalf("expected size mismatch tolerance 12.5, got %.2f", existing.SizeMismatchTolerancePercent)
	}
	if !existing.UseCategoryFromIndexer {
		t.Fatalf("expected useCategoryFromIndexer to be true")
	}
	if existing.RunExternalProgramID != nil {
		t.Fatalf("expected runExternalProgramID to be nil")
	}
	if !existing.GazelleEnabled {
		t.Fatalf("expected gazelleEnabled to be true")
	}
	if existing.RedactedAPIKey != "red-key" {
		t.Fatalf("expected redacted api key to be set")
	}
	if existing.OrpheusAPIKey != "ops-key" {
		t.Fatalf("expected orpheus api key to be set")
	}
}

func TestApplyAutomationSettingsPatch_PreservesUnspecifiedFields(t *testing.T) {
	existing := models.CrossSeedAutomationSettings{
		Enabled:              true,
		RunIntervalMinutes:   60,
		Category:             stringPtr("tv"),
		RSSAutomationTags:    []string{"keep"},
		SeededSearchTags:     []string{"keep-seeded"},
		CompletionSearchTags: []string{"keep-completion"},
		WebhookTags:          []string{"keep-webhook"},
	}

	patch := automationSettingsPatchRequest{
		SizeMismatchTolerancePercent: ptrFloat(20),
		Category:                     optionalString{Set: true, Value: nil}, // explicit clear
	}

	applyAutomationSettingsPatch(&existing, patch)

	if existing.Enabled != true {
		t.Fatalf("expected enabled to remain true")
	}
	if existing.RunIntervalMinutes != 60 {
		t.Fatalf("expected runIntervalMinutes to remain 60")
	}
	if existing.Category != nil {
		t.Fatalf("expected category to be cleared")
	}
	if len(existing.RSSAutomationTags) != 1 || existing.RSSAutomationTags[0] != "keep" {
		t.Fatalf("expected rss automation tags to stay unchanged, got %#v", existing.RSSAutomationTags)
	}
	if len(existing.SeededSearchTags) != 1 || existing.SeededSearchTags[0] != "keep-seeded" {
		t.Fatalf("expected seeded search tags to stay unchanged, got %#v", existing.SeededSearchTags)
	}
	if existing.SizeMismatchTolerancePercent != 20 {
		t.Fatalf("expected updated tolerance to be 20, got %.2f", existing.SizeMismatchTolerancePercent)
	}
}

func stringPtr(value string) *string { return &value }

func TestApplyAutomationSettingsPatch_CategoryAffix(t *testing.T) {
	existing := models.CrossSeedAutomationSettings{
		UseCrossCategoryAffix:  true,
		CategoryAffixMode:      models.CategoryAffixModeSuffix,
		CategoryAffix:          ".cross",
		UseCategoryFromIndexer: false,
		UseCustomCategory:      false,
		CustomCategory:         "",
	}

	newAffixMode := models.CategoryAffixModePrefix
	newAffix := "cross/"
	patch := automationSettingsPatchRequest{
		UseCrossCategoryAffix: ptrBool(true),
		CategoryAffixMode:     &newAffixMode,
		CategoryAffix:         &newAffix,
	}

	applyAutomationSettingsPatch(&existing, patch)

	if !existing.UseCrossCategoryAffix {
		t.Fatalf("expected useCrossCategoryAffix to be true")
	}
	if existing.CategoryAffixMode != models.CategoryAffixModePrefix {
		t.Fatalf("expected categoryAffixMode to be 'prefix', got %q", existing.CategoryAffixMode)
	}
	if existing.CategoryAffix != "cross/" {
		t.Fatalf("expected categoryAffix to be 'cross/', got %q", existing.CategoryAffix)
	}
}

func TestApplyAutomationSettingsPatch_CustomCategory(t *testing.T) {
	existing := models.CrossSeedAutomationSettings{
		UseCrossCategoryAffix:  true,
		CategoryAffixMode:      models.CategoryAffixModeSuffix,
		CategoryAffix:          ".cross",
		UseCategoryFromIndexer: false,
		UseCustomCategory:      false,
		CustomCategory:         "",
	}

	customCat := "cross-seed"
	patch := automationSettingsPatchRequest{
		UseCrossCategoryAffix: ptrBool(false),
		UseCustomCategory:     ptrBool(true),
		CustomCategory:        &customCat,
	}

	applyAutomationSettingsPatch(&existing, patch)

	if existing.UseCrossCategoryAffix {
		t.Fatalf("expected useCrossCategoryAffix to be false")
	}
	if !existing.UseCustomCategory {
		t.Fatalf("expected useCustomCategory to be true")
	}
	if existing.CustomCategory != "cross-seed" {
		t.Fatalf("expected customCategory to be 'cross-seed', got %q", existing.CustomCategory)
	}
}
