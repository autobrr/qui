// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// RestoreOptions control restore execution behaviour.
type RestoreOptions struct {
	DryRun             bool
	StartPaused        bool
	SkipHashCheck      bool
	AutoResumeVerified bool
	ExcludeHashes      []string
}

// RestoreError captures an operation failure during restore execution.
type RestoreError struct {
	Operation string `json:"operation"`
	Target    string `json:"target"`
	Message   string `json:"message"`
}

// RestoreApplied summarises the actions that were successfully applied.
type RestoreApplied struct {
	Categories CategoryApplied `json:"categories"`
	Tags       TagApplied      `json:"tags"`
	Torrents   TorrentApplied  `json:"torrents"`
}

// CategoryApplied summarises category operations.
type CategoryApplied struct {
	Created []string `json:"created,omitempty"`
	Updated []string `json:"updated,omitempty"`
	Deleted []string `json:"deleted,omitempty"`
}

// TagApplied summarises tag operations.
type TagApplied struct {
	Created []string `json:"created,omitempty"`
	Deleted []string `json:"deleted,omitempty"`
}

// TorrentApplied summarises torrent operations.
type TorrentApplied struct {
	Added   []string `json:"added,omitempty"`
	Updated []string `json:"updated,omitempty"`
	Deleted []string `json:"deleted,omitempty"`
}

// RestoreResult describes the outcome of a restore execution.
type RestoreResult struct {
	Mode       RestoreMode    `json:"mode"`
	RunID      int64          `json:"runId"`
	InstanceID int            `json:"instanceId"`
	DryRun     bool           `json:"dryRun"`
	Plan       *RestorePlan   `json:"plan"`
	Applied    RestoreApplied `json:"applied"`
	Warnings   []string       `json:"warnings,omitempty"`
	Errors     []RestoreError `json:"errors,omitempty"`
}

// PreviewRestore returns the diff plan without executing any mutations.
func (s *Service) PreviewRestore(ctx context.Context, runID int64, mode RestoreMode, opts *RestorePlanOptions) (*RestorePlan, error) {
	return s.PlanRestoreDiff(ctx, runID, mode, opts)
}

// ExecuteRestore executes the restore plan for the given run and mode.
func (s *Service) ExecuteRestore(ctx context.Context, runID int64, mode RestoreMode, opts RestoreOptions) (*RestoreResult, error) {
	var planOpts *RestorePlanOptions
	if len(opts.ExcludeHashes) > 0 {
		planOpts = &RestorePlanOptions{ExcludeHashes: opts.ExcludeHashes}
	}

	plan, err := s.PlanRestoreDiff(ctx, runID, mode, planOpts)
	if err != nil {
		return nil, err
	}

	result := &RestoreResult{
		Mode:       plan.Mode,
		RunID:      plan.RunID,
		InstanceID: plan.InstanceID,
		DryRun:     opts.DryRun,
		Plan:       plan,
	}

	if opts.DryRun {
		return result, nil
	}

	if s.syncManager == nil {
		return result, errors.New("sync manager unavailable")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	result.Applied = RestoreApplied{}

	if err := s.applyCategoryPlan(ctx, plan, &result.Applied, &result.Errors); err != nil {
		return result, err
	}

	if err := s.applyTagPlan(ctx, plan, &result.Applied, &result.Errors); err != nil {
		return result, err
	}

	excludeSet := buildHashSet(opts.ExcludeHashes)

	warnings, err := s.applyTorrentPlan(ctx, plan, &result.Applied, &result.Errors, excludeSet, opts)
	if len(warnings) > 0 {
		result.Warnings = append(result.Warnings, warnings...)
	}
	if err != nil {
		return result, err
	}

	return result, nil
}

func (s *Service) applyCategoryPlan(ctx context.Context, plan *RestorePlan, applied *RestoreApplied, errs *[]RestoreError) error {
	instanceID := plan.InstanceID

	for _, spec := range plan.Categories.Create {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.syncManager.CreateCategory(ctx, instanceID, spec.Name, spec.SavePath); err != nil {
			appendRestoreError(errs, "create_category", spec.Name, err)
			log.Warn().Err(err).Int("instanceID", instanceID).Str("category", spec.Name).Msg("Restore: create category failed")
			continue
		}
		applied.Categories.Created = append(applied.Categories.Created, spec.Name)
	}

	for _, update := range plan.Categories.Update {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.syncManager.EditCategory(ctx, instanceID, update.Name, update.DesiredPath); err != nil {
			appendRestoreError(errs, "update_category", update.Name, err)
			log.Warn().Err(err).Int("instanceID", instanceID).Str("category", update.Name).Msg("Restore: update category failed")
			continue
		}
		applied.Categories.Updated = append(applied.Categories.Updated, update.Name)
	}

	if len(plan.Categories.Delete) == 0 {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.syncManager.RemoveCategories(ctx, instanceID, plan.Categories.Delete); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Strs("categories", plan.Categories.Delete).Msg("Restore: batch category removal failed, retry individually")
		for _, name := range plan.Categories.Delete {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.syncManager.RemoveCategories(ctx, instanceID, []string{name}); err != nil {
				appendRestoreError(errs, "delete_category", name, err)
				continue
			}
			applied.Categories.Deleted = append(applied.Categories.Deleted, name)
		}
	} else {
		applied.Categories.Deleted = append(applied.Categories.Deleted, plan.Categories.Delete...)
	}

	return nil
}

func (s *Service) applyTagPlan(ctx context.Context, plan *RestorePlan, applied *RestoreApplied, errs *[]RestoreError) error {
	instanceID := plan.InstanceID

	if len(plan.Tags.Create) > 0 {
		tags := make([]string, 0, len(plan.Tags.Create))
		for _, spec := range plan.Tags.Create {
			tags = append(tags, spec.Name)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.syncManager.CreateTags(ctx, instanceID, tags); err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Strs("tags", tags).Msg("Restore: batch tag creation failed, retry individually")
			for _, tag := range tags {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := s.syncManager.CreateTags(ctx, instanceID, []string{tag}); err != nil {
					appendRestoreError(errs, "create_tag", tag, err)
					continue
				}
				applied.Tags.Created = append(applied.Tags.Created, tag)
			}
		} else {
			applied.Tags.Created = append(applied.Tags.Created, tags...)
		}
	}

	if len(plan.Tags.Delete) == 0 {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.syncManager.DeleteTags(ctx, instanceID, plan.Tags.Delete); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Strs("tags", plan.Tags.Delete).Msg("Restore: batch tag deletion failed, retry individually")
		for _, tag := range plan.Tags.Delete {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.syncManager.DeleteTags(ctx, instanceID, []string{tag}); err != nil {
				appendRestoreError(errs, "delete_tag", tag, err)
				continue
			}
			applied.Tags.Deleted = append(applied.Tags.Deleted, tag)
		}
	} else {
		applied.Tags.Deleted = append(applied.Tags.Deleted, plan.Tags.Delete...)
	}

	return nil
}

func (s *Service) applyTorrentPlan(ctx context.Context, plan *RestorePlan, applied *RestoreApplied, errs *[]RestoreError, exclude map[string]struct{}, opts RestoreOptions) ([]string, error) {
	instanceID := plan.InstanceID
	var warnings []string
	var pendingResume []string

	for _, spec := range plan.Torrents.Add {
		if err := ctx.Err(); err != nil {
			return warnings, err
		}

		if shouldSkipTorrent(spec.Manifest.Hash, exclude) {
			continue
		}

		blobPath := strings.TrimSpace(spec.Manifest.TorrentBlob)
		if blobPath == "" {
			msg := fmt.Sprintf("Torrent %s missing cached blob reference", spec.Manifest.Hash)
			warnings = append(warnings, msg)
			appendRestoreError(errs, "add_torrent", spec.Manifest.Hash, errors.New("missing torrent blob"))
			continue
		}

		payload, err := s.loadTorrentBlobData(blobPath)
		if err != nil {
			appendRestoreError(errs, "add_torrent", spec.Manifest.Hash, err)
			log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", spec.Manifest.Hash).Msg("Restore: failed to load torrent blob")
			continue
		}

		options := map[string]string{}
		paused := "false"
		stopped := "false"
		if opts.StartPaused || opts.SkipHashCheck {
			paused = "true"
			stopped = "true"
		}
		options["paused"] = paused
		options["stopped"] = stopped
		if opts.SkipHashCheck {
			options["skip_checking"] = "true"
		}
		if spec.Manifest.Category != nil {
			category := strings.TrimSpace(*spec.Manifest.Category)
			if category != "" {
				options["category"] = category
			}
		}
		if len(spec.Manifest.Tags) > 0 {
			options["tags"] = strings.Join(spec.Manifest.Tags, ",")
		}

		if err := s.syncManager.AddTorrent(ctx, instanceID, payload, options); err != nil {
			appendRestoreError(errs, "add_torrent", spec.Manifest.Hash, err)
			log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", spec.Manifest.Hash).Msg("Restore: add torrent failed")
			continue
		}

		desiredCategory := normalizeCategory(spec.Manifest.Category)
		if desiredCategory != "" {
			if err := s.syncManager.SetCategory(ctx, instanceID, []string{spec.Manifest.Hash}, desiredCategory); err != nil {
				appendRestoreError(errs, "set_category", spec.Manifest.Hash, err)
			}
		}

		if len(spec.Manifest.Tags) > 0 {
			tagPayload := strings.Join(spec.Manifest.Tags, ",")
			if err := s.syncManager.SetTags(ctx, instanceID, []string{spec.Manifest.Hash}, tagPayload); err != nil {
				appendRestoreError(errs, "set_tags", spec.Manifest.Hash, err)
			}
		}

		applied.Torrents.Added = append(applied.Torrents.Added, spec.Manifest.Hash)

		if opts.SkipHashCheck && opts.AutoResumeVerified {
			pendingResume = append(pendingResume, spec.Manifest.Hash)
		}
	}

	for _, update := range plan.Torrents.Update {
		if err := ctx.Err(); err != nil {
			return warnings, err
		}

		if shouldSkipTorrent(update.Hash, exclude) {
			continue
		}

		supportedApplied := false

		for _, change := range update.Changes {
			if !change.Supported {
				message := change.Message
				if message == "" {
					message = "manual intervention required"
				}
				warnings = append(warnings, fmt.Sprintf("%s: %s (%s)", update.Hash, change.Field, message))
				continue
			}

			switch change.Field {
			case "category":
				desired := normalizeCategoryPtr(asString(change.Desired))
				if err := s.syncManager.SetCategory(ctx, instanceID, []string{update.Hash}, desired); err != nil {
					appendRestoreError(errs, "set_category", update.Hash, err)
				} else {
					supportedApplied = true
				}
			case "tags":
				desiredTags := asStringSlice(change.Desired)
				tagPayload := strings.Join(desiredTags, ",")
				if err := s.syncManager.SetTags(ctx, instanceID, []string{update.Hash}, tagPayload); err != nil {
					appendRestoreError(errs, "set_tags", update.Hash, err)
				} else {
					supportedApplied = true
				}
			}
		}

		if supportedApplied {
			applied.Torrents.Updated = append(applied.Torrents.Updated, update.Hash)
		}
	}

	if len(pendingResume) > 0 && s.syncManager != nil {
		s.syncManager.ResumeWhenComplete(instanceID, pendingResume, qbittorrent.ResumeWhenCompleteOptions{})
	}

	if len(plan.Torrents.Delete) == 0 {
		return warnings, nil
	}

	if err := ctx.Err(); err != nil {
		return warnings, err
	}

	deleteTargets := make([]string, 0, len(plan.Torrents.Delete))
	for _, hash := range plan.Torrents.Delete {
		if shouldSkipTorrent(hash, exclude) {
			continue
		}
		deleteTargets = append(deleteTargets, hash)
	}

	if len(deleteTargets) == 0 {
		return warnings, nil
	}

	if err := s.syncManager.BulkAction(ctx, instanceID, deleteTargets, "delete"); err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Strs("hashes", deleteTargets).Msg("Restore: bulk torrent delete failed, retry individually")
		for _, hash := range deleteTargets {
			if err := ctx.Err(); err != nil {
				return warnings, err
			}
			if err := s.syncManager.BulkAction(ctx, instanceID, []string{hash}, "delete"); err != nil {
				appendRestoreError(errs, "delete_torrent", hash, err)
				continue
			}
			applied.Torrents.Deleted = append(applied.Torrents.Deleted, hash)
		}
	} else {
		applied.Torrents.Deleted = append(applied.Torrents.Deleted, deleteTargets...)
	}

	return warnings, nil
}

func buildHashSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(items))
	for _, hash := range items {
		normalized := strings.TrimSpace(strings.ToLower(hash))
		if normalized == "" {
			continue
		}
		set[normalized] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func shouldSkipTorrent(hash string, exclude map[string]struct{}) bool {
	if len(exclude) == 0 {
		return false
	}
	normalized := strings.TrimSpace(strings.ToLower(hash))
	_, skip := exclude[normalized]
	return skip
}

func (s *Service) loadTorrentBlobData(blobPath string) ([]byte, error) {
	dataDir := strings.TrimSpace(s.cfg.DataDir)
	if dataDir == "" {
		return nil, errors.New("backup data directory not configured")
	}
	cleanRel := filepath.Clean(blobPath)
	cleanRel = strings.TrimPrefix(cleanRel, string(filepath.Separator))

	baseAbs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}

	resolve := func(rel string) ([]byte, error) {
		abs, err := filepath.Abs(filepath.Join(baseAbs, rel))
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(abs, baseAbs+string(filepath.Separator)) && abs != baseAbs {
			return nil, fmt.Errorf("invalid blob path %q", rel)
		}
		return os.ReadFile(abs)
	}

	data, err := resolve(cleanRel)
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read torrent blob %q: %w", blobPath, err)
	}

	altRel := filepath.ToSlash(filepath.Join("backups", cleanRel))
	data, altErr := resolve(altRel)
	if altErr == nil {
		return data, nil
	}

	return nil, fmt.Errorf("read torrent blob %q: %w", blobPath, err)
}

func appendRestoreError(errs *[]RestoreError, operation, target string, err error) {
	if err == nil {
		return
	}
	*errs = append(*errs, RestoreError{
		Operation: operation,
		Target:    target,
		Message:   err.Error(),
	})
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case *string:
		if v == nil {
			return ""
		}
		return *v
	default:
		return fmt.Sprintf("%v", value)
	}
}

func asStringSlice(value any) []string {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []string:
		return cloneStringSlice(v)
	case *[]string:
		if v == nil {
			return nil
		}
		return cloneStringSlice(*v)
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	default:
		return nil
	}
}
