// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/notifications"
)

func (s *Service) notifyAutomationRun(ctx context.Context, run *models.CrossSeedRun, runErr error) {
	if s == nil || s.notifier == nil || run == nil {
		return
	}

	eventType := notifications.EventCrossSeedAutomationSucceeded
	if run.Status == models.CrossSeedRunStatusFailed || run.Status == models.CrossSeedRunStatusPartial {
		eventType = notifications.EventCrossSeedAutomationFailed
	} else if run.CrossSeedsAdded == 0 && run.CandidatesFailed == 0 {
		log.Info().
			Int64("runID", run.ID).
			Str("status", string(run.Status)).
			Int("feedItems", run.TotalFeedItems).
			Int("candidates", run.CandidatesFound).
			Int("added", run.CrossSeedsAdded).
			Int("failed", run.CandidatesFailed).
			Int("skipped", run.CandidatesSkipped).
			Msg("Skipping cross-seed RSS success notification")
		return
	}

	var errorMessage string
	if run.ErrorMessage != nil && strings.TrimSpace(*run.ErrorMessage) != "" {
		errorMessage = strings.TrimSpace(*run.ErrorMessage)
	} else if runErr != nil {
		errorMessage = runErr.Error()
	}
	if errorMessage == "" && eventType == notifications.EventCrossSeedAutomationFailed {
		errorMessage = fmt.Sprintf("status: %s", run.Status)
	}

	lines := []string{
		fmt.Sprintf("Run: %d", run.ID),
		fmt.Sprintf("Mode: %s", run.Mode),
		fmt.Sprintf("Status: %s", run.Status),
		fmt.Sprintf("Feed items: %d", run.TotalFeedItems),
		fmt.Sprintf("Candidates: %d", run.CandidatesFound),
		fmt.Sprintf("Added: %d", run.CrossSeedsAdded),
		fmt.Sprintf("Failed: %d", run.CandidatesFailed),
		fmt.Sprintf("Skipped: %d", run.CandidatesSkipped),
	}
	if run.Message != nil && strings.TrimSpace(*run.Message) != "" {
		lines = append(lines, "Message: "+strings.TrimSpace(*run.Message))
	}
	if run.ErrorMessage != nil && strings.TrimSpace(*run.ErrorMessage) != "" {
		lines = append(lines, "Error: "+strings.TrimSpace(*run.ErrorMessage))
	} else if runErr != nil {
		lines = append(lines, "Error: "+runErr.Error())
	}
	samples := collectCrossSeedRunSamples(run.Results, 0)
	if sampleText := formatSamplesForMessage(samples, 3); sampleText != "" {
		lines = append(lines, "Samples: "+sampleText)
	}

	s.notifier.Notify(ctx, notifications.Event{
		Type:         eventType,
		InstanceName: "Cross-seed RSS",
		Message:      strings.Join(lines, "\n"),
		CrossSeed: &notifications.CrossSeedEventData{
			RunID:      run.ID,
			Mode:       string(run.Mode),
			Status:     string(run.Status),
			FeedItems:  run.TotalFeedItems,
			Candidates: run.CandidatesFound,
			Added:      run.CrossSeedsAdded,
			Failed:     run.CandidatesFailed,
			Skipped:    run.CandidatesSkipped,
			Samples:    samples,
		},
		ErrorMessage: errorMessage,
		ErrorMessages: func() []string {
			if strings.TrimSpace(errorMessage) == "" {
				return nil
			}
			return []string{errorMessage}
		}(),
		StartedAt:   &run.StartedAt,
		CompletedAt: run.CompletedAt,
	})
}

func (s *Service) notifySearchRun(ctx context.Context, state *searchRunState, canceled bool) {
	if s == nil || s.notifier == nil || state == nil || state.run == nil {
		return
	}

	eventType := notifications.EventCrossSeedSearchSucceeded
	if state.run.Status != models.CrossSeedSearchRunStatusSuccess {
		eventType = notifications.EventCrossSeedSearchFailed
	}

	var errorMessage string
	if state.run.ErrorMessage != nil && strings.TrimSpace(*state.run.ErrorMessage) != "" {
		errorMessage = strings.TrimSpace(*state.run.ErrorMessage)
	} else if canceled {
		errorMessage = "canceled"
	}
	if errorMessage == "" && eventType == notifications.EventCrossSeedSearchFailed {
		errorMessage = fmt.Sprintf("status: %s", state.run.Status)
	}

	lines := []string{
		fmt.Sprintf("Run: %d", state.run.ID),
		fmt.Sprintf("Status: %s", state.run.Status),
		fmt.Sprintf("Processed: %d/%d", state.run.Processed, state.run.TotalTorrents),
		fmt.Sprintf("Cross-seeds added: %d", state.run.CrossSeedsAdded),
		fmt.Sprintf("Torrents with cross-seeds: %d", state.run.TorrentsWithCrossSeeds),
		fmt.Sprintf("Failed: %d", state.run.TorrentsFailed),
		fmt.Sprintf("Skipped: %d", state.run.TorrentsSkipped),
	}
	if state.run.ErrorMessage != nil && strings.TrimSpace(*state.run.ErrorMessage) != "" {
		lines = append(lines, "Error: "+strings.TrimSpace(*state.run.ErrorMessage))
	} else if canceled {
		lines = append(lines, "Error: canceled")
	}

	results := state.recentResults
	if len(results) == 0 && state.run != nil {
		results = state.run.Results
	}
	samples := collectSearchResultSamples(results, 0)
	if sampleText := formatSamplesForMessage(samples, 3); sampleText != "" {
		lines = append(lines, "Samples: "+sampleText)
	}

	s.notifier.Notify(ctx, notifications.Event{
		Type:       eventType,
		InstanceID: state.run.InstanceID,
		Message:    strings.Join(lines, "\n"),
		CrossSeed: &notifications.CrossSeedEventData{
			RunID:                  state.run.ID,
			Status:                 string(state.run.Status),
			Processed:              state.run.Processed,
			Total:                  state.run.TotalTorrents,
			Added:                  state.run.CrossSeedsAdded,
			TorrentsWithCrossSeeds: state.run.TorrentsWithCrossSeeds,
			Failed:                 state.run.TorrentsFailed,
			Skipped:                state.run.TorrentsSkipped,
			Samples:                samples,
		},
		ErrorMessage: errorMessage,
		ErrorMessages: func() []string {
			if strings.TrimSpace(errorMessage) == "" {
				return nil
			}
			return []string{errorMessage}
		}(),
		StartedAt:   &state.run.StartedAt,
		CompletedAt: state.run.CompletedAt,
	})
}

func collectCrossSeedRunSamples(results []models.CrossSeedRunResult, limit int) []string {
	if len(results) == 0 {
		return nil
	}

	capHint := 64
	if limit > 0 {
		capHint = min(limit, len(results))
	} else {
		capHint = min(capHint, len(results))
	}
	seen := make(map[string]struct{}, capHint)
	samples := make([]string, 0, capHint)
	for _, result := range results {
		if !result.Success {
			continue
		}
		label := ""
		if result.MatchedTorrentName != nil && strings.TrimSpace(*result.MatchedTorrentName) != "" {
			label = strings.TrimSpace(*result.MatchedTorrentName)
		} else if result.MatchedTorrentHash != nil && strings.TrimSpace(*result.MatchedTorrentHash) != "" {
			label = strings.TrimSpace(*result.MatchedTorrentHash)
		}
		if label == "" {
			continue
		}
		if strings.TrimSpace(result.InstanceName) != "" {
			label = fmt.Sprintf("%s @ %s", label, result.InstanceName)
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		samples = append(samples, label)
		if limit > 0 && len(samples) >= limit {
			break
		}
	}
	return samples
}

func collectSearchResultSamples(results []models.CrossSeedSearchResult, limit int) []string {
	if len(results) == 0 {
		return nil
	}

	capHint := 64
	if limit > 0 {
		capHint = min(limit, len(results))
	} else {
		capHint = min(capHint, len(results))
	}
	seen := make(map[string]struct{}, capHint)
	samples := make([]string, 0, capHint)
	for _, result := range results {
		if result.Status != models.CrossSeedSearchResultStatusAdded {
			continue
		}
		label := strings.TrimSpace(result.TorrentName)
		if label == "" {
			label = "torrent"
		}
		if strings.TrimSpace(result.ReleaseTitle) != "" {
			label = fmt.Sprintf("%s → %s", label, strings.TrimSpace(result.ReleaseTitle))
		}
		if strings.TrimSpace(result.IndexerName) != "" {
			label = fmt.Sprintf("%s @ %s", label, strings.TrimSpace(result.IndexerName))
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		samples = append(samples, label)
		if limit > 0 && len(samples) >= limit {
			break
		}
	}
	return samples
}

func formatSampleText(samples []string) string {
	if len(samples) == 0 {
		return ""
	}
	return strings.Join(samples, "; ")
}

func formatSamplesForMessage(samples []string, maxCount int) string {
	if len(samples) == 0 || maxCount <= 0 {
		return ""
	}
	shown := samples
	more := 0
	if len(samples) > maxCount {
		shown = samples[:maxCount]
		more = len(samples) - maxCount
	}
	out := formatSampleText(shown)
	if out == "" {
		return ""
	}
	if more > 0 {
		out = fmt.Sprintf("%s (+%d more)", out, more)
	}
	return out
}

func (s *Service) notifyWebhookApply(ctx context.Context, req *AutobrrApplyRequest, resp *CrossSeedResponse, runErr error, startedAt time.Time) {
	if s == nil || s.notifier == nil || req == nil {
		return
	}

	var addedResults, failedResults []InstanceCrossSeedResult
	if resp != nil {
		addedResults = make([]InstanceCrossSeedResult, 0, len(resp.Results))
		failedResults = make([]InstanceCrossSeedResult, 0, len(resp.Results))
		for _, result := range resp.Results {
			if class := result.class(); class == classAdded {
				addedResults = append(addedResults, result)
			} else if class == classStranded || class == classFailed {
				failedResults = append(failedResults, result)
			}
		}
	}
	if len(addedResults) == 0 && len(failedResults) == 0 && runErr == nil {
		return
	}

	torrentName := strings.TrimSpace(req.TorrentName)
	if torrentName == "" && resp != nil && resp.TorrentInfo != nil {
		torrentName = strings.TrimSpace(resp.TorrentInfo.Name)
	}
	failedCount := len(failedResults)
	if failedCount == 0 && runErr != nil {
		failedCount = 1
	}
	lines := make([]string, 0, 3)
	if torrentName != "" {
		lines = append(lines, "Torrent: "+torrentName)
	}
	lines = append(lines, fmt.Sprintf("Added: %d", len(addedResults)))
	if failedCount > 0 {
		lines = append(lines, fmt.Sprintf("Failed: %d", failedCount))
	}

	results := make([]InstanceCrossSeedResult, 0, len(addedResults)+len(failedResults))
	results = append(results, addedResults...)
	results = append(results, failedResults...)
	const maxSamples = 3
	samples := make([]string, 0, min(maxSamples, len(results)))
	seenSamples := make(map[string]struct{}, min(maxSamples, len(results)))
	for _, result := range results {
		if instanceName := strings.TrimSpace(result.InstanceName); instanceName != "" {
			if _, ok := seenSamples[instanceName]; ok {
				continue
			}
			seenSamples[instanceName] = struct{}{}
			samples = append(samples, instanceName)
			if len(samples) == maxSamples {
				break
			}
		}
	}
	if sampleText := formatSamplesForMessage(samples, maxSamples); sampleText != "" {
		lines = append(lines, "Instances: "+sampleText)
	}

	eventType := notifications.EventCrossSeedWebhookSucceeded
	errorMessage := ""
	if len(failedResults) > 0 || runErr != nil {
		eventType = notifications.EventCrossSeedWebhookFailed
		if runErr != nil {
			errorMessage = strings.TrimSpace(runErr.Error())
		}
		if errorMessage == "" && len(failedResults) > 0 {
			errorMessage = strings.TrimSpace(failedResults[0].Message)
		}
		if errorMessage == "" {
			errorMessage = "cross-seed webhook apply failed"
		}
		lines = append(lines, "Error: "+errorMessage)
	}

	completedAt := time.Now().UTC()
	s.notifier.Notify(ctx, notifications.Event{
		Type:         eventType,
		InstanceName: "Cross-seed webhook",
		Message:      strings.Join(lines, "\n"),
		TorrentName:  torrentName,
		CrossSeed: &notifications.CrossSeedEventData{
			Added:   len(addedResults),
			Failed:  failedCount,
			Samples: samples,
		},
		ErrorMessage: errorMessage,
		ErrorMessages: func() []string {
			if errorMessage == "" {
				return nil
			}
			return []string{errorMessage}
		}(),
		StartedAt:   &startedAt,
		CompletedAt: &completedAt,
	})
}

func (s *Service) notifyWebhookCheckFailure(ctx context.Context, req *WebhookCheckRequest, err error, startedAt time.Time) {
	if s == nil || s.notifier == nil || req == nil || err == nil {
		return
	}
	if errors.Is(err, ErrInvalidWebhookRequest) {
		return
	}

	errorMessage := strings.TrimSpace(err.Error())

	lines := []string{
		"Torrent: " + strings.TrimSpace(req.TorrentName),
		"Error: " + err.Error(),
	}

	completedAt := time.Now().UTC()
	s.notifier.Notify(ctx, notifications.Event{
		Type:         notifications.EventCrossSeedWebhookFailed,
		InstanceName: "Cross-seed webhook",
		Message:      strings.Join(lines, "\n"),
		TorrentName:  strings.TrimSpace(req.TorrentName),
		ErrorMessage: errorMessage,
		ErrorMessages: func() []string {
			if errorMessage == "" {
				return nil
			}
			return []string{errorMessage}
		}(),
		StartedAt:   &startedAt,
		CompletedAt: &completedAt,
	})
}
