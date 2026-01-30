// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/nicholas-fedor/shoutrrr/pkg/router"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/models"
)

const (
	defaultQueueSize = 100
	defaultWorkers   = 2
)

type Notifier interface {
	Notify(event Event)
}

type Event struct {
	Type                     EventType
	Title                    string
	Message                  string
	InstanceID               int
	InstanceName             string
	TorrentName              string
	TorrentHash              string
	TrackerDomain            string
	Category                 string
	Tags                     []string
	BackupKind               models.BackupRunKind
	BackupRunID              int64
	BackupTorrentCount       int
	DirScanRunID             int64
	DirScanMatchesFound      int
	DirScanTorrentsAdded     int
	OrphanScanRunID          int64
	OrphanScanFilesDeleted   int
	OrphanScanFoldersDeleted int
	ErrorMessage             string
}

type Service struct {
	store         *models.NotificationTargetStore
	instanceStore *models.InstanceStore
	logger        zerolog.Logger
	queue         chan Event
	startOnce     sync.Once
}

func NewService(store *models.NotificationTargetStore, instanceStore *models.InstanceStore, logger zerolog.Logger) *Service {
	if store == nil {
		return nil
	}

	return &Service{
		store:         store,
		instanceStore: instanceStore,
		logger:        logger,
		queue:         make(chan Event, defaultQueueSize),
	}
}

func ValidateURL(rawURL string) error {
	_, err := router.New(nil, rawURL)
	return err
}

func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}

	s.startOnce.Do(func() {
		for range defaultWorkers {
			go s.worker(ctx)
		}
	})
}

func (s *Service) Notify(event Event) {
	if s == nil || s.store == nil {
		return
	}

	if s.queue == nil {
		go s.dispatch(context.Background(), event)
		return
	}

	select {
	case s.queue <- event:
	default:
		s.logger.Warn().Str("event", string(event.Type)).Msg("notifications: queue full, dropping event")
	}
}

func (s *Service) SendTest(ctx context.Context, target *models.NotificationTarget, title, message string) error {
	if target == nil {
		return errors.New("notification target required")
	}

	return s.send(ctx, target, title, message)
}

func (s *Service) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.queue:
			s.dispatch(ctx, event)
		}
	}
}

func (s *Service) dispatch(ctx context.Context, event Event) {
	if s == nil || s.store == nil {
		return
	}

	targets, err := s.store.ListEnabled(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("notifications: failed to list targets")
		return
	}
	if len(targets) == 0 {
		return
	}

	title, message := s.formatEvent(ctx, event)
	if strings.TrimSpace(message) == "" {
		return
	}

	for _, target := range targets {
		if !allowsEvent(target.EventTypes, event.Type) {
			continue
		}

		if err := s.send(ctx, target, title, message); err != nil {
			s.logger.Error().Err(err).Str("target", target.Name).Str("event", string(event.Type)).Msg("notifications: send failed")
		}
	}
}

func (s *Service) send(_ context.Context, target *models.NotificationTarget, title, message string) error {
	sender, err := router.New(nil, target.URL)
	if err != nil {
		return err
	}

	params := types.Params{}
	if trimmed := strings.TrimSpace(title); trimmed != "" {
		params.SetTitle(truncateMessage(trimmed, maxTitleLength))
	}

	trimmedMessage := truncateMessage(message, maxMessageLength)
	results := sender.Send(trimmedMessage, &params)
	var errs []error
	for _, sendErr := range results {
		if sendErr != nil {
			errs = append(errs, sendErr)
		}
	}
	if len(errs) == 0 {
		return nil
	}

	return errors.Join(errs...)
}

func (s *Service) formatEvent(ctx context.Context, event Event) (string, string) {
	instanceLabel := s.resolveInstanceLabel(ctx, event)
	customMessage := strings.TrimSpace(event.Message)

	switch event.Type {
	case EventTorrentCompleted:
		title := "Torrent completed"
		lines := []string{
			formatLine("Torrent", fmt.Sprintf("%s%s", event.TorrentName, formatHashSuffix(event.TorrentHash))),
		}
		if tracker := strings.TrimSpace(event.TrackerDomain); tracker != "" {
			lines = append(lines, formatLine("Tracker", tracker))
		}
		if category := strings.TrimSpace(event.Category); category != "" {
			lines = append(lines, formatLine("Category", category))
		}
		if len(event.Tags) > 0 {
			tags := append([]string(nil), event.Tags...)
			slices.Sort(tags)
			lines = append(lines, formatLine("Tags", strings.Join(tags, ", ")))
		}
		return title, buildMessage(instanceLabel, lines)
	case EventBackupSucceeded:
		title := "Backup completed"
		lines := []string{
			formatLine("Backup", formatKind(event.BackupKind)),
			formatLine("Run", fmt.Sprintf("%d", event.BackupRunID)),
			formatLine("Torrents", fmt.Sprintf("%d", event.BackupTorrentCount)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventBackupFailed:
		title := "Backup failed"
		lines := []string{
			formatLine("Backup", formatKind(event.BackupKind)),
			formatLine("Run", fmt.Sprintf("%d", event.BackupRunID)),
			formatLine("Error", formatErrorMessage(event.ErrorMessage)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventDirScanCompleted:
		title := "Directory scan completed"
		lines := []string{
			formatLine("Run", fmt.Sprintf("%d", event.DirScanRunID)),
			formatLine("Matches", fmt.Sprintf("%d", event.DirScanMatchesFound)),
			formatLine("Torrents added", fmt.Sprintf("%d", event.DirScanTorrentsAdded)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventDirScanFailed:
		title := "Directory scan failed"
		lines := []string{
			formatLine("Run", fmt.Sprintf("%d", event.DirScanRunID)),
			formatLine("Error", formatErrorMessage(event.ErrorMessage)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventOrphanScanCompleted:
		title := "Orphan scan completed"
		lines := []string{
			formatLine("Run", fmt.Sprintf("%d", event.OrphanScanRunID)),
			formatLine("Files deleted", fmt.Sprintf("%d", event.OrphanScanFilesDeleted)),
			formatLine("Folders deleted", fmt.Sprintf("%d", event.OrphanScanFoldersDeleted)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventOrphanScanFailed:
		title := "Orphan scan failed"
		lines := []string{
			formatLine("Run", fmt.Sprintf("%d", event.OrphanScanRunID)),
			formatLine("Error", formatErrorMessage(event.ErrorMessage)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventCrossSeedAutomationSucceeded:
		title := "Cross-seed automation completed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventCrossSeedAutomationFailed:
		title := "Cross-seed automation failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventCrossSeedSearchSucceeded:
		title := "Cross-seed search completed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventCrossSeedSearchFailed:
		title := "Cross-seed search failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventAutomationsActionsApplied:
		title := "Automations actions applied"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventAutomationsRunFailed:
		title := "Automations run failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	default:
		return "", ""
	}
}

func (s *Service) resolveInstanceLabel(ctx context.Context, event Event) string {
	if strings.TrimSpace(event.InstanceName) != "" {
		return event.InstanceName
	}
	if event.InstanceID <= 0 || s.instanceStore == nil {
		return "Instance"
	}

	instance, err := s.instanceStore.Get(ctx, event.InstanceID)
	if err != nil || instance == nil {
		return fmt.Sprintf("Instance %d", event.InstanceID)
	}
	if strings.TrimSpace(instance.Name) == "" {
		return fmt.Sprintf("Instance %d", event.InstanceID)
	}

	return instance.Name
}

func allowsEvent(eventTypes []string, eventType EventType) bool {
	if len(eventTypes) == 0 {
		return true
	}

	return slices.Contains(eventTypes, string(eventType))
}

func formatHashSuffix(hash string) string {
	trimmed := strings.TrimSpace(hash)
	if len(trimmed) < 8 {
		return ""
	}
	return fmt.Sprintf(" [%s]", trimmed[:8])
}

func formatLine(label, value string) string {
	trimmedLabel := strings.TrimSpace(label)
	trimmedValue := strings.TrimSpace(value)
	if trimmedLabel == "" || trimmedValue == "" {
		return ""
	}
	return fmt.Sprintf("%s: %s", trimmedLabel, trimmedValue)
}

func buildMessage(instanceLabel string, lines []string) string {
	payload := make([]string, 0, len(lines)+1)
	if trimmed := strings.TrimSpace(instanceLabel); trimmed != "" {
		payload = append(payload, formatLine("Instance", trimmed))
	}
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			payload = append(payload, trimmed)
		}
	}
	return strings.Join(payload, "\n")
}

func splitMessageLines(message string) []string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		if line := strings.TrimSpace(part); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func formatCustomEvent(instanceLabel, defaultTitle, overrideTitle, message string) (string, string) {
	title := defaultTitle
	if strings.TrimSpace(overrideTitle) != "" {
		title = strings.TrimSpace(overrideTitle)
	}
	if strings.TrimSpace(message) == "" {
		return title, ""
	}
	return title, buildMessage(instanceLabel, splitMessageLines(message))
}

const (
	maxMessageLength = 420
	maxTitleLength   = 80
)

func truncateMessage(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if utf8.RuneCountInString(trimmed) <= limit {
		return trimmed
	}
	runes := []rune(trimmed)
	if limit <= 1 {
		return string(runes[:limit])
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func formatKind(kind models.BackupRunKind) string {
	raw := strings.TrimSpace(string(kind))
	if raw == "" {
		return "backup"
	}
	return raw
}

func formatErrorMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return "Unknown error"
	}
	return trimmed
}
