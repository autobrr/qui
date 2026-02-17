// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nicholas-fedor/shoutrrr/pkg/router"
	shoutrrrdiscord "github.com/nicholas-fedor/shoutrrr/pkg/services/chat/discord"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/models"
)

const (
	defaultQueueSize = 100
	defaultWorkers   = 2
)

type Notifier interface {
	Notify(ctx context.Context, event Event)
}

type Event struct {
	Type                     EventType
	Title                    string
	Message                  string
	StartedAt                *time.Time
	CompletedAt              *time.Time
	CrossSeed                *CrossSeedEventData
	Automations              *AutomationsEventData
	InstanceID               int
	InstanceName             string
	TorrentName              string
	TorrentHash              string
	TorrentAddedOn           int64
	TorrentETASeconds        int64
	TorrentState             string
	TorrentProgress          float64
	TorrentRatio             float64
	TorrentTotalSizeBytes    int64
	TorrentDownloadedBytes   int64
	TorrentAmountLeftBytes   int64
	TorrentDlSpeedBps        int64
	TorrentUpSpeedBps        int64
	TorrentNumSeeds          int64
	TorrentNumLeechs         int64
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
	ErrorMessages            []string
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
	if targetScheme(rawURL) == "notifiarrapi" {
		_, err := parseNotifiarrAPIConfig(rawURL)
		return err
	}
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

func (s *Service) Notify(ctx context.Context, event Event) {
	if s == nil || s.store == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if s.queue == nil {
		go s.dispatch(ctx, event)
		return
	}

	select {
	case <-ctx.Done():
		return
	case s.queue <- event:
	default:
		s.logger.Warn().Str("event", string(event.Type)).Msg("notifications: queue full, dropping event")
	}
}

func (s *Service) SendTest(ctx context.Context, target *models.NotificationTarget, title, message string) error {
	if target == nil {
		return errors.New("notification target required")
	}

	return s.send(ctx, target, Event{}, title, message)
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

		if err := s.send(ctx, target, event, title, message); err != nil {
			s.logger.Error().Err(err).Str("target", target.Name).Str("event", string(event.Type)).Msg("notifications: send failed")
		}
	}
}

func (s *Service) send(ctx context.Context, target *models.NotificationTarget, event Event, title, message string) error {
	if target == nil {
		return errors.New("notification target required")
	}

	switch targetScheme(target.URL) {
	case "discord":
		return s.sendDiscord(target.URL, event, title, message)
	case "notifiarr":
		return s.sendNotifiarr(target.URL, event, title, message)
	case "notifiarrapi":
		return s.sendNotifiarrAPI(ctx, target.URL, event, title, message)
	default:
		return s.sendDefault(target.URL, title, message)
	}
}

func (s *Service) sendDefault(rawURL, title, message string) error {
	sender, err := router.New(nil, rawURL)
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

func (s *Service) sendDiscord(rawURL string, event Event, title, message string) error {
	configURL, err := url.Parse(rawURL)
	if err != nil {
		return err
	}

	service := &shoutrrrdiscord.Service{}
	if err := service.Initialize(configURL, nil); err != nil {
		return err
	}
	service.Config.JSON = true

	payload, err := buildDiscordPayload(service.Config, event, title, message)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return service.Send(string(encoded), nil)
}

func (s *Service) sendNotifiarr(rawURL string, _ Event, title, message string) error {
	sender, err := router.New(nil, rawURL)
	if err != nil {
		return err
	}

	description, fields := buildStructuredMessage(message)
	if description == "" {
		description = message
	}
	description = truncateMessage(description, notifiarrDescriptionLimit)

	params := types.Params{}
	if trimmed := strings.TrimSpace(title); trimmed != "" {
		params.SetTitle(truncateMessage(trimmed, notifiarrTitleLimit))
	}
	if len(fields) > 0 {
		encoded, err := json.Marshal(buildNotifiarrFields(fields))
		if err != nil {
			return err
		}
		params["fields"] = string(encoded)
	}

	results := sender.Send(description, &params)
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
	case EventTorrentAdded:
		title := "Torrent added"
		lines := []string{
			formatLine("Torrent", fmt.Sprintf("%s%s", event.TorrentName, formatHashSuffix(event.TorrentHash))),
		}
		if eta := formatETA(event.TorrentETASeconds); eta != "" {
			lines = append(lines, formatLine("ETA", eta))
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
			formatLine("Run", strconv.FormatInt(event.BackupRunID, 10)),
			formatLine("Torrents", strconv.Itoa(event.BackupTorrentCount)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventBackupFailed:
		title := "Backup failed"
		lines := []string{
			formatLine("Backup", formatKind(event.BackupKind)),
			formatLine("Run", strconv.FormatInt(event.BackupRunID, 10)),
			formatLine("Error", formatErrorMessage(event.ErrorMessage)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventDirScanCompleted:
		title := "Directory scan completed"
		lines := []string{
			formatLine("Run", strconv.FormatInt(event.DirScanRunID, 10)),
			formatLine("Matches", strconv.Itoa(event.DirScanMatchesFound)),
			formatLine("Torrents added", strconv.Itoa(event.DirScanTorrentsAdded)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventDirScanFailed:
		title := "Directory scan failed"
		lines := []string{
			formatLine("Run", strconv.FormatInt(event.DirScanRunID, 10)),
			formatLine("Error", formatErrorMessage(event.ErrorMessage)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventOrphanScanCompleted:
		title := "Orphan scan completed"
		lines := []string{
			formatLine("Run", strconv.FormatInt(event.OrphanScanRunID, 10)),
			formatLine("Files deleted", strconv.Itoa(event.OrphanScanFilesDeleted)),
			formatLine("Folders deleted", strconv.Itoa(event.OrphanScanFoldersDeleted)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventOrphanScanFailed:
		title := "Orphan scan failed"
		lines := []string{
			formatLine("Run", strconv.FormatInt(event.OrphanScanRunID, 10)),
			formatLine("Error", formatErrorMessage(event.ErrorMessage)),
		}
		return title, buildMessage(instanceLabel, lines)
	case EventCrossSeedAutomationSucceeded:
		title := "Cross-seed RSS automation completed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventCrossSeedAutomationFailed:
		title := "Cross-seed RSS automation failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventCrossSeedSearchSucceeded:
		title := "Cross-seed seeded search completed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventCrossSeedSearchFailed:
		title := "Cross-seed seeded search failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventCrossSeedCompletionSucceeded:
		title := "Cross-seed completion search completed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventCrossSeedCompletionFailed:
		title := "Cross-seed completion search failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventCrossSeedWebhookSucceeded:
		title := "Cross-seed webhook check completed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case EventCrossSeedWebhookFailed:
		title := "Cross-seed webhook check failed"
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

func formatETA(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	return (time.Duration(seconds) * time.Second).Round(time.Second).String()
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

type messageField struct {
	Label  string
	Value  string
	Inline bool
}

type discordWebhookPayload struct {
	Content   string         `json:"content,omitempty"`
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Embeds    []discordEmbed `json:"embeds,omitempty"`
}

type discordEmbed struct {
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Color       int                 `json:"color,omitempty"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type notifiarrField struct {
	Title  string `json:"title"`
	Text   string `json:"text"`
	Inline bool   `json:"inline"`
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
	maxMessageLength          = 420
	maxTitleLength            = 80
	discordTitleLimit         = 256
	discordDescriptionLimit   = 4096
	discordFieldNameLimit     = 256
	discordFieldValueLimit    = 1024
	discordFieldsLimit        = 25
	notifiarrTitleLimit       = 256
	notifiarrDescriptionLimit = 4096
)

const (
	discordColorInfo    = 0x58b9ff
	discordColorSuccess = 0x57f287
	discordColorError   = 0xed4245
)

func targetScheme(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme == "" {
		return ""
	}
	if strings.Contains(scheme, "+") {
		parts := strings.SplitN(scheme, "+", 2)
		return parts[0]
	}
	return scheme
}

func buildStructuredMessage(message string) (string, []messageField) {
	lines := splitMessageLines(message)
	if len(lines) == 0 {
		return "", nil
	}

	var description string
	fields := make([]messageField, 0, len(lines))

	for _, line := range lines {
		label, value, ok := splitLine(line)
		if !ok {
			if description == "" {
				description = line
			} else {
				fields = append(fields, messageField{
					Label:  "Details",
					Value:  normalizeField("Details", line).Value,
					Inline: false,
				})
			}
			continue
		}
		lowerLabel := strings.ToLower(label)
		if lowerLabel == "instance" {
			fields = append(fields, normalizeField(label, value))
			continue
		}
		if description == "" {
			switch lowerLabel {
			case "torrent":
				description = value
			case "run":
				description = "Run " + value
			default:
				description = fmt.Sprintf("%s: %s", label, value)
			}
			continue
		}
		fields = append(fields, normalizeField(label, value))
	}

	if description == "" && len(fields) > 0 {
		description = fmt.Sprintf("%s: %s", fields[0].Label, fields[0].Value)
		fields = fields[1:]
	}

	return description, fields
}

func splitLine(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	label := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if label == "" || value == "" {
		return "", "", false
	}
	return label, value, true
}

func normalizeField(label, value string) messageField {
	trimmedLabel := truncateMessage(label, discordFieldNameLimit)
	trimmedValue := truncateMessage(value, discordFieldValueLimit)
	return messageField{
		Label:  trimmedLabel,
		Value:  trimmedValue,
		Inline: shouldInlineField(trimmedLabel, trimmedValue),
	}
}

func shouldInlineField(label, value string) bool {
	if label == "" || value == "" {
		return false
	}
	switch strings.ToLower(label) {
	case "torrent", "samples", "errors", "error", "message", "tags":
		return false
	}
	return utf8.RuneCountInString(value) <= 48
}

func buildDiscordPayload(config *shoutrrrdiscord.Config, event Event, title, message string) (discordWebhookPayload, error) {
	description, fields := buildStructuredMessage(message)
	if description == "" {
		description = message
	}

	title = truncateMessage(strings.TrimSpace(title), discordTitleLimit)
	description = truncateMessage(description, discordDescriptionLimit)

	embedFields := make([]discordEmbedField, 0, min(len(fields), discordFieldsLimit))
	for _, field := range fields {
		if len(embedFields) >= discordFieldsLimit {
			break
		}
		embedFields = append(embedFields, discordEmbedField{
			Name:   field.Label,
			Value:  field.Value,
			Inline: field.Inline,
		})
	}

	embed := discordEmbed{
		Title:       title,
		Description: description,
		Color:       discordEventColor(event.Type),
		Fields:      embedFields,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	payload := discordWebhookPayload{
		Embeds: []discordEmbed{embed},
	}
	if config != nil {
		if strings.TrimSpace(config.Username) != "" {
			payload.Username = strings.TrimSpace(config.Username)
		}
		if strings.TrimSpace(config.Avatar) != "" {
			payload.AvatarURL = strings.TrimSpace(config.Avatar)
		}
	}

	if payload.Embeds[0].Title == "" && payload.Embeds[0].Description == "" && len(payload.Embeds[0].Fields) == 0 {
		return discordWebhookPayload{}, errors.New("notification has no content to send")
	}

	return payload, nil
}

func discordEventColor(eventType EventType) int {
	switch eventType {
	case EventBackupFailed,
		EventDirScanFailed,
		EventOrphanScanFailed,
		EventCrossSeedAutomationFailed,
		EventCrossSeedSearchFailed,
		EventCrossSeedCompletionFailed,
		EventCrossSeedWebhookFailed,
		EventAutomationsRunFailed:
		return discordColorError
	case EventTorrentCompleted,
		EventBackupSucceeded,
		EventDirScanCompleted,
		EventOrphanScanCompleted,
		EventCrossSeedAutomationSucceeded,
		EventCrossSeedSearchSucceeded,
		EventCrossSeedCompletionSucceeded,
		EventCrossSeedWebhookSucceeded,
		EventAutomationsActionsApplied:
		return discordColorSuccess
	default:
		return discordColorInfo
	}
}

func buildNotifiarrFields(fields []messageField) []notifiarrField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]notifiarrField, 0, min(len(fields), discordFieldsLimit))
	for _, field := range fields {
		if len(out) >= discordFieldsLimit {
			break
		}
		out = append(out, notifiarrField{
			Title:  truncateMessage(field.Label, discordFieldNameLimit),
			Text:   truncateMessage(field.Value, discordFieldValueLimit),
			Inline: field.Inline,
		})
	}
	return out
}

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
