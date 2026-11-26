// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/transfer"
)

type exportRunner interface {
	Export(ctx context.Context, req transfer.ExportRequest) (int64, <-chan transfer.ExportResult, error)
}

func (s *Service) SetExportRunner(runner exportRunner) {
	s.exportRunner = runner
}

func (s *Service) exportOnTransfers(ctx context.Context, sourceInstanceID int, exec pendingExportToInstance, ch chan<- *models.AutomationActivity) {
	ctx = context.WithoutCancel(ctx)
	defer func() {
		s.mu.Lock()
		delete(s.inFlightExports, fmt.Sprintf("%d:%s", exec.action.TargetInstanceID, exec.hash))
		s.mu.Unlock()
	}()
	if _, exists, err := s.targetLookup.HasTorrentByAnyHash(ctx, exec.action.TargetInstanceID, []string{exec.hash}); err == nil && exists {
		return
	}
	_, done, err := s.exportRunner.Export(ctx, transfer.ExportRequest{
		SourceInstanceID: sourceInstanceID,
		TargetInstanceID: exec.action.TargetInstanceID,
		Hash:             exec.hash,
		Name:             exec.torrent.Name,
		Size:             exec.torrent.Size,
		Placement: transfer.Placement{SavePath: exec.resolvedSavePath, Category: exec.action.Category, Tags: exec.action.Tags,
			ContentLayout: exec.action.ContentLayout},
		Paused:       exec.action.Paused,
		SkipChecking: exec.action.SkipCheckingEnabled(),
		RuleID:       exec.ruleID,
		RuleName:     exec.ruleName,
	})
	if err != nil {
		s.sendExportActivity(ctx, ch, exportActivity(sourceInstanceID, exec, exec.trackerDomain, models.ActivityOutcomeFailed, "Export failed: "+err.Error()))
		return
	}
	res, ended := <-done
	if !ended || res.AlreadyOnTarget {
		return
	}
	outcome := models.ActivityOutcomeSuccess
	if res.Failure != "" {
		outcome = models.ActivityOutcomeFailed
	}
	s.sendExportActivity(ctx, ch, exportActivity(sourceInstanceID, exec, cmp.Or(res.TrackerDomain, exec.trackerDomain), outcome, res.Failure))
}

func exportActivity(sourceInstanceID int, exec pendingExportToInstance, trackerDomain, outcome, reason string) *models.AutomationActivity {
	details, _ := json.Marshal(struct { //nolint:errchkjson // int and string fields cannot fail to encode
		Count            int    `json:"count"`
		SavePath         string `json:"savePath"`
		TargetInstanceID int    `json:"targetInstanceId"`
	}{1, exec.resolvedSavePath, exec.action.TargetInstanceID})
	return &models.AutomationActivity{
		InstanceID:    sourceInstanceID,
		Hash:          exec.hash,
		TorrentName:   exec.torrent.Name,
		TrackerDomain: trackerDomain,
		Action:        models.ActivityActionExportedToInstance,
		RuleID:        new(exec.ruleID),
		RuleName:      exec.ruleName,
		Outcome:       outcome,
		Reason:        reason,
		Details:       details,
	}
}

func (s *Service) sendExportActivity(ctx context.Context, ch chan<- *models.AutomationActivity, activity *models.AutomationActivity) {
	if s.activityStore != nil {
		if err := s.activityStore.Create(ctx, activity); err != nil {
			log.Warn().Err(err).Str("hash", activity.Hash).Msg("automations: failed to log export activity")
		}
	}
	ch <- activity
}
