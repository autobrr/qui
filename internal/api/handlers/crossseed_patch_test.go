package handlers

import (
	"testing"

	"github.com/autobrr/qui/internal/models"
)

func ptrBool(v bool) *bool        { return &v }
func ptrInt(v int) *int           { return &v }
func ptrFloat(v float64) *float64 { return &v }

func TestApplyAutomationSettingsPatch_MergesFields(t *testing.T) {
	existing := models.CrossSeedAutomationSettings{
		Enabled:                      false,
		RunIntervalMinutes:           120,
		StartPaused:                  true,
		Category:                     stringPtr("tv"),
		Tags:                         []string{"old"},
		IgnorePatterns:               []string{".nfo"},
		TargetInstanceIDs:            []int{1},
		TargetIndexerIDs:             []int{2},
		MaxResultsPerRun:             10,
		FindIndividualEpisodes:       false,
		SizeMismatchTolerancePercent: 5.0,
		UseCategoryFromIndexer:       false,
		RunExternalProgramID:         ptrInt(42),
		PreventReaddPreviouslyAdded:  false,
		Completion: models.CrossSeedCompletionSettings{
			Enabled:           false,
			Categories:        []string{"tv"},
			Tags:              []string{"cross-seed"},
			ExcludeCategories: []string{"anime"},
			ExcludeTags:       []string{"skip"},
		},
	}

	newCategory := " movies "
	patch := automationSettingsPatchRequest{
		Enabled:                      ptrBool(true),
		RunIntervalMinutes:           ptrInt(45),
		StartPaused:                  ptrBool(false),
		Category:                     optionalString{Set: true, Value: &newCategory},
		Tags:                         &[]string{"new"},
		IgnorePatterns:               &[]string{"*.srr"},
		TargetInstanceIDs:            &[]int{3, 4},
		TargetIndexerIDs:             &[]int{7},
		MaxResultsPerRun:             ptrInt(25),
		FindIndividualEpisodes:       ptrBool(true),
		SizeMismatchTolerancePercent: ptrFloat(12.5),
		UseCategoryFromIndexer:       ptrBool(true),
		RunExternalProgramID:         optionalInt{Set: true, Value: nil},
		PreventReaddPreviouslyAdded:  ptrBool(true),
		Completion: &completionSettingsPatchRequest{
			Enabled:           ptrBool(true),
			Categories:        &[]string{"movies"},
			Tags:              &[]string{"cross"},
			ExcludeCategories: &[]string{"music"},
			ExcludeTags:       &[]string{"x265"},
		},
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
	if len(existing.Tags) != 1 || existing.Tags[0] != "new" {
		t.Fatalf("unexpected tags: %#v", existing.Tags)
	}
	if len(existing.IgnorePatterns) != 1 || existing.IgnorePatterns[0] != "*.srr" {
		t.Fatalf("unexpected ignore patterns: %#v", existing.IgnorePatterns)
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
	if !existing.PreventReaddPreviouslyAdded {
		t.Fatalf("expected prevent re-add flag to be true")
	}
	if existing.Completion.Enabled != true ||
		len(existing.Completion.Categories) != 1 ||
		existing.Completion.Categories[0] != "movies" {
		t.Fatalf("unexpected completion categories: %#v", existing.Completion)
	}
	if existing.Completion.Tags[0] != "cross" {
		t.Fatalf("unexpected completion tags: %#v", existing.Completion.Tags)
	}
	if existing.Completion.ExcludeCategories[0] != "music" {
		t.Fatalf("unexpected completion exclude categories: %#v", existing.Completion.ExcludeCategories)
	}
	if existing.Completion.ExcludeTags[0] != "x265" {
		t.Fatalf("unexpected completion exclude tags: %#v", existing.Completion.ExcludeTags)
	}
}

func TestApplyAutomationSettingsPatch_PreservesUnspecifiedFields(t *testing.T) {
	existing := models.CrossSeedAutomationSettings{
		Enabled:                     true,
		RunIntervalMinutes:          60,
		Category:                    stringPtr("tv"),
		Tags:                        []string{"keep"},
		PreventReaddPreviouslyAdded: true,
		Completion: models.CrossSeedCompletionSettings{
			Enabled: true,
			Tags:    []string{"keep-tag"},
		},
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
	if len(existing.Tags) != 1 || existing.Tags[0] != "keep" {
		t.Fatalf("expected tags to stay unchanged, got %#v", existing.Tags)
	}
	if !existing.Completion.Enabled || existing.Completion.Tags[0] != "keep-tag" {
		t.Fatalf("expected completion to stay unchanged, got %#v", existing.Completion)
	}
	if existing.SizeMismatchTolerancePercent != 20 {
		t.Fatalf("expected updated tolerance to be 20, got %.2f", existing.SizeMismatchTolerancePercent)
	}
	if !existing.PreventReaddPreviouslyAdded {
		t.Fatalf("expected prevent re-add flag to remain true")
	}
}

func stringPtr(value string) *string { return &value }
