// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/notifications"
)

const (
	defaultEndpoint = "https://notifiarr.com/api/v1/notification/test?event=qui"
	defaultTimeout  = 15 * time.Second

	discordFieldNameLimit  = 256
	discordFieldValueLimit = 1024
	discordFieldsLimit     = 25
)

type notifiarrMessage struct {
	Event string               `json:"event"`
	Data  notifiarrMessageData `json:"data"`
}

type notifiarrMessageData struct {
	Subject                  string                              `json:"subject,omitempty"`
	Message                  string                              `json:"message,omitempty"`
	Event                    string                              `json:"event"`
	Timestamp                time.Time                           `json:"timestamp"`
	CrossSeed                *notifications.CrossSeedEventData   `json:"cross_seed,omitempty"`
	Automations              *notifications.AutomationsEventData `json:"automations,omitempty"`
	InstanceID               *int                                `json:"instance_id,omitempty"`
	InstanceName             *string                             `json:"instance_name,omitempty"`
	TorrentName              *string                             `json:"torrent_name,omitempty"`
	TorrentHash              *string                             `json:"torrent_hash,omitempty"`
	TrackerDomain            *string                             `json:"tracker_domain,omitempty"`
	Category                 *string                             `json:"category,omitempty"`
	Tags                     []string                            `json:"tags,omitempty"`
	BackupKind               *string                             `json:"backup_kind,omitempty"`
	BackupRunID              *int64                              `json:"backup_run_id,omitempty"`
	BackupTorrentCount       *int                                `json:"backup_torrent_count,omitempty"`
	DirScanRunID             *int64                              `json:"dir_scan_run_id,omitempty"`
	DirScanMatchesFound      *int                                `json:"dir_scan_matches_found,omitempty"`
	DirScanTorrentsAdded     *int                                `json:"dir_scan_torrents_added,omitempty"`
	OrphanScanRunID          *int64                              `json:"orphan_scan_run_id,omitempty"`
	OrphanScanFilesDeleted   *int                                `json:"orphan_scan_files_deleted,omitempty"`
	OrphanScanFoldersDeleted *int                                `json:"orphan_scan_folders_deleted,omitempty"`
	ErrorMessage             *string                             `json:"error_message,omitempty"`
	ErrorMessages            []string                            `json:"error_messages,omitempty"`
	StartedAt                *time.Time                          `json:"started_at,omitempty"`
	CompletedAt              *time.Time                          `json:"completed_at,omitempty"`
	DurationMs               *int64                              `json:"duration_ms,omitempty"`
	Description              string                              `json:"description,omitempty"`
	Fields                   []notifiarrField                    `json:"fields,omitempty"`
}

type notifiarrField struct {
	Title  string `json:"title"`
	Text   string `json:"text"`
	Inline bool   `json:"inline"`
}

type fixture struct {
	Name  string
	Event notifications.Event
}

func main() {
	endpoint := flag.String("endpoint", defaultEndpoint, "notifiarr endpoint")
	eventFilter := flag.String("event", "", "send only these events (comma-separated)")
	dryRun := flag.Bool("dry-run", false, "print payloads without sending")
	timeout := flag.Duration("timeout", defaultTimeout, "http timeout")
	flag.Parse()

	filter := buildFilter(*eventFilter)
	fixtures := buildFixtures()
	if len(filter) > 0 {
		fixtures = filterFixtures(fixtures, filter)
		if len(fixtures) == 0 {
			fmt.Fprintf(os.Stderr, "no matching events for filter: %s\n", *eventFilter)
			printAvailableEvents()
			os.Exit(1)
		}
	}

	client := &http.Client{Timeout: *timeout}
	for _, item := range fixtures {
		title, message := formatEvent(item.Event)
		if strings.TrimSpace(title) == "" && strings.TrimSpace(message) == "" {
			fmt.Printf("skip %s: empty payload\n", item.Name)
			continue
		}
		payload := notifiarrMessage{
			Event: buildNotifiarrEventValue(item.Event.Type),
			Data:  buildNotifiarrData(item.Event, title, message),
		}

		if *dryRun {
			printPayload(item.Name, payload)
			continue
		}

		if err := sendPayload(client, *endpoint, payload); err != nil {
			fmt.Fprintf(os.Stderr, "send %s failed: %v\n", item.Name, err)
			continue
		}
		fmt.Printf("sent %s\n", item.Name)
	}
}

func buildFilter(raw string) map[string]struct{} {
	filter := make(map[string]struct{})
	if strings.TrimSpace(raw) == "" {
		return filter
	}
	for part := range strings.SplitSeq(raw, ",") {
		if value := strings.TrimSpace(part); value != "" {
			filter[value] = struct{}{}
		}
	}
	return filter
}

func filterFixtures(fixtures []fixture, filter map[string]struct{}) []fixture {
	if len(filter) == 0 {
		return fixtures
	}
	out := make([]fixture, 0, len(fixtures))
	for _, item := range fixtures {
		if _, ok := filter[string(item.Event.Type)]; ok {
			out = append(out, item)
		}
	}
	return out
}

func printAvailableEvents() {
	fmt.Fprintln(os.Stderr, "available events:")
	for _, def := range notifications.EventDefinitions() {
		fmt.Fprintf(os.Stderr, "- %s\n", def.Type)
	}
}

func printPayload(name string, payload notifiarrMessage) {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal %s failed: %v\n", name, err)
		return
	}
	fmt.Printf("payload %s:\n%s\n", name, string(encoded))
}

func sendPayload(client *http.Client, endpoint string, payload notifiarrMessage) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "qui-notifiarr-test")

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("unexpected status: %d body: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func buildFixtures() []fixture {
	instanceLabel := "qBittorrent"
	baseTime := time.Now().UTC()
	backupStart, backupEnd := runTimes(baseTime, -35*time.Minute, 2*time.Minute)
	dirScanStart, dirScanEnd := runTimes(baseTime, -30*time.Minute, 90*time.Second)
	orphanStart, orphanEnd := runTimes(baseTime, -25*time.Minute, 3*time.Minute)
	crossSeedAutoStart, crossSeedAutoEnd := runTimes(baseTime, -20*time.Minute, 4*time.Minute)
	crossSeedSearchStart, crossSeedSearchEnd := runTimes(baseTime, -15*time.Minute, 5*time.Minute)
	crossSeedCompletionStart, crossSeedCompletionEnd := runTimes(baseTime, -10*time.Minute, 2*time.Minute)
	webhookStart, webhookEnd := runTimes(baseTime, -5*time.Minute, 20*time.Second)

	return []fixture{
		{
			Name: "torrent_completed",
			Event: notifications.Event{
				Type:          notifications.EventTorrentCompleted,
				InstanceName:  instanceLabel,
				TorrentName:   "Some.Movie.2025.1080p.BluRay.x264",
				TorrentHash:   "abcdef0123456789abcdef0123456789abcdef01",
				TrackerDomain: "tracker.example",
				Category:      "movies",
				Tags:          []string{"seed", "bluray"},
			},
		},
		{
			Name: "backup_succeeded",
			Event: notifications.Event{
				Type:               notifications.EventBackupSucceeded,
				InstanceName:       instanceLabel,
				BackupKind:         models.BackupRunKindDaily,
				BackupRunID:        42,
				BackupTorrentCount: 120,
				StartedAt:          backupStart,
				CompletedAt:        backupEnd,
			},
		},
		{
			Name: "backup_failed",
			Event: notifications.Event{
				Type:         notifications.EventBackupFailed,
				InstanceName: instanceLabel,
				BackupKind:   models.BackupRunKindDaily,
				BackupRunID:  43,
				ErrorMessage: "failed to write archive: permission denied",
				StartedAt:    backupStart,
				CompletedAt:  backupEnd,
			},
		},
		{
			Name: "dir_scan_completed",
			Event: notifications.Event{
				Type:                 notifications.EventDirScanCompleted,
				InstanceName:         instanceLabel,
				DirScanRunID:         101,
				DirScanMatchesFound:  12,
				DirScanTorrentsAdded: 8,
				StartedAt:            dirScanStart,
				CompletedAt:          dirScanEnd,
			},
		},
		{
			Name: "dir_scan_failed",
			Event: notifications.Event{
				Type:         notifications.EventDirScanFailed,
				InstanceName: instanceLabel,
				DirScanRunID: 102,
				ErrorMessage: "scan path not found",
				StartedAt:    dirScanStart,
				CompletedAt:  dirScanEnd,
			},
		},
		{
			Name: "orphan_scan_completed",
			Event: notifications.Event{
				Type:                     notifications.EventOrphanScanCompleted,
				InstanceName:             instanceLabel,
				OrphanScanRunID:          77,
				OrphanScanFilesDeleted:   45,
				OrphanScanFoldersDeleted: 3,
				StartedAt:                orphanStart,
				CompletedAt:              orphanEnd,
			},
		},
		{
			Name: "orphan_scan_failed",
			Event: notifications.Event{
				Type:            notifications.EventOrphanScanFailed,
				InstanceName:    instanceLabel,
				OrphanScanRunID: 78,
				ErrorMessage:    "deletion failed for 2 file(s): permission denied",
				StartedAt:       orphanStart,
				CompletedAt:     orphanEnd,
			},
		},
		{
			Name: "cross_seed_automation_succeeded",
			Event: notifications.Event{
				Type:         notifications.EventCrossSeedAutomationSucceeded,
				InstanceName: "Cross-seed RSS",
				CrossSeed: &notifications.CrossSeedEventData{
					RunID:      9,
					Mode:       "rss",
					Status:     "success",
					FeedItems:  120,
					Candidates: 8,
					Added:      3,
					Failed:     0,
					Skipped:    5,
					Samples:    []string{"Some.Movie.2025", "Another.Show.S01E01"},
				},
				StartedAt:   crossSeedAutoStart,
				CompletedAt: crossSeedAutoEnd,
				Message: strings.Join([]string{
					"Run: 9",
					"Mode: rss",
					"Status: success",
					"Feed items: 120",
					"Candidates: 8",
					"Added: 3",
					"Failed: 0",
					"Skipped: 5",
					"Samples: Some.Movie.2025; Another.Show.S01E01",
				}, "\n"),
			},
		},
		{
			Name: "cross_seed_automation_failed",
			Event: notifications.Event{
				Type:         notifications.EventCrossSeedAutomationFailed,
				InstanceName: "Cross-seed RSS",
				CrossSeed: &notifications.CrossSeedEventData{
					RunID:      10,
					Mode:       "rss",
					Status:     "partial",
					FeedItems:  95,
					Candidates: 4,
					Added:      1,
					Failed:     2,
					Skipped:    1,
					Samples:    []string{"Example.Release.2024"},
				},
				ErrorMessage:  "indexer timeout",
				ErrorMessages: []string{"indexer timeout"},
				StartedAt:     crossSeedAutoStart,
				CompletedAt:   crossSeedAutoEnd,
				Message: strings.Join([]string{
					"Run: 10",
					"Mode: rss",
					"Status: partial",
					"Feed items: 95",
					"Candidates: 4",
					"Added: 1",
					"Failed: 2",
					"Skipped: 1",
					"Error: indexer timeout",
					"Samples: Example.Release.2024",
				}, "\n"),
			},
		},
		{
			Name: "cross_seed_search_succeeded",
			Event: notifications.Event{
				Type:         notifications.EventCrossSeedSearchSucceeded,
				InstanceName: instanceLabel,
				CrossSeed: &notifications.CrossSeedEventData{
					RunID:     31,
					Status:    "success",
					Processed: 200,
					Total:     200,
					Added:     4,
					Failed:    0,
					Skipped:   3,
					Samples:   []string{"Movie.One.2025", "Movie.Two.2024"},
				},
				StartedAt:   crossSeedSearchStart,
				CompletedAt: crossSeedSearchEnd,
				Message: strings.Join([]string{
					"Run: 31",
					"Status: success",
					"Processed: 200/200",
					"Added: 4",
					"Failed: 0",
					"Skipped: 3",
					"Samples: Movie.One.2025; Movie.Two.2024",
				}, "\n"),
			},
		},
		{
			Name: "cross_seed_search_failed",
			Event: notifications.Event{
				Type:         notifications.EventCrossSeedSearchFailed,
				InstanceName: instanceLabel,
				CrossSeed: &notifications.CrossSeedEventData{
					RunID:     32,
					Status:    "failed",
					Processed: 40,
					Total:     200,
					Added:     0,
					Failed:    1,
					Skipped:   2,
				},
				ErrorMessage:  "canceled",
				ErrorMessages: []string{"canceled"},
				StartedAt:     crossSeedSearchStart,
				CompletedAt:   crossSeedSearchEnd,
				Message: strings.Join([]string{
					"Run: 32",
					"Status: failed",
					"Processed: 40/200",
					"Added: 0",
					"Failed: 1",
					"Skipped: 2",
					"Error: canceled",
				}, "\n"),
			},
		},
		{
			Name: "cross_seed_completion_succeeded",
			Event: notifications.Event{
				Type:         notifications.EventCrossSeedCompletionSucceeded,
				InstanceName: instanceLabel,
				TorrentName:  "Example.Movie.2025.1080p",
				CrossSeed: &notifications.CrossSeedEventData{
					Matches: 6,
					Added:   2,
					Failed:  0,
					Skipped: 4,
					Samples: []string{"Example.Movie.2025.REMUX", "Example.Movie.2025.BluRay"},
				},
				StartedAt:   crossSeedCompletionStart,
				CompletedAt: crossSeedCompletionEnd,
				Message: strings.Join([]string{
					"Torrent: Example.Movie.2025.1080p",
					"Matches: 6",
					"Added: 2",
					"Failed: 0",
					"Skipped: 4",
					"Samples: Example.Movie.2025.REMUX; Example.Movie.2025.BluRay",
				}, "\n"),
			},
		},
		{
			Name: "cross_seed_completion_failed",
			Event: notifications.Event{
				Type:         notifications.EventCrossSeedCompletionFailed,
				InstanceName: instanceLabel,
				TorrentName:  "Example.Movie.2025.1080p",
				CrossSeed: &notifications.CrossSeedEventData{
					Matches: 2,
					Added:   0,
					Failed:  1,
					Skipped: 1,
					Samples: []string{"Example.Movie.2025.WEB"},
				},
				ErrorMessage:  "cross-seed completion failed",
				ErrorMessages: []string{"cross-seed completion failed"},
				StartedAt:     crossSeedCompletionStart,
				CompletedAt:   crossSeedCompletionEnd,
				Message: strings.Join([]string{
					"Torrent: Example.Movie.2025.1080p",
					"Matches: 2",
					"Added: 0",
					"Failed: 1",
					"Skipped: 1",
					"Samples: Example.Movie.2025.WEB",
				}, "\n"),
			},
		},
		{
			Name: "cross_seed_webhook_succeeded",
			Event: notifications.Event{
				Type:         notifications.EventCrossSeedWebhookSucceeded,
				InstanceName: "Cross-seed webhook",
				TorrentName:  "Example.Show.S01E01",
				CrossSeed: &notifications.CrossSeedEventData{
					Matches:        3,
					Complete:       2,
					Pending:        1,
					Recommendation: "keep current torrent",
					Samples:        []string{"Example.Show.S01E01.1080p", "Example.Show.S01E01.2160p"},
				},
				StartedAt:   webhookStart,
				CompletedAt: webhookEnd,
				Message: strings.Join([]string{
					"Torrent: Example.Show.S01E01",
					"Matches: 3",
					"Complete matches: 2",
					"Pending matches: 1",
					"Recommendation: keep current torrent",
					"Samples: Example.Show.S01E01.1080p; Example.Show.S01E01.2160p",
				}, "\n"),
			},
		},
		{
			Name: "cross_seed_webhook_failed",
			Event: notifications.Event{
				Type:          notifications.EventCrossSeedWebhookFailed,
				InstanceName:  "Cross-seed webhook",
				TorrentName:   "Example.Show.S01E01",
				ErrorMessage:  "invalid request signature",
				ErrorMessages: []string{"invalid request signature"},
				StartedAt:     webhookStart,
				CompletedAt:   webhookEnd,
				Message: strings.Join([]string{
					"Torrent: Example.Show.S01E01",
					"Error: invalid request signature",
				}, "\n"),
			},
		},
		{
			Name: "automations_actions_applied",
			Event: notifications.Event{
				Type:         notifications.EventAutomationsActionsApplied,
				InstanceName: instanceLabel,
				Automations: &notifications.AutomationsEventData{
					Applied: 4,
					Failed:  1,
					TopActions: []notifications.LabelCount{
						{Label: "Deleted torrent (ratio rule)", Count: 2},
						{Label: "Tags updated", Count: 2},
					},
					TopFailures: []notifications.LabelCount{
						{Label: "Delete failed", Count: 1},
					},
					Rules: []notifications.LabelCount{
						{Label: "Ratio rule", Count: 2},
						{Label: "Tagger", Count: 2},
					},
					Samples: []string{"Example.Movie.2025", "Another.Show.S01E01"},
					Errors:  []string{"permission denied", "missing category"},
				},
				ErrorMessage:  "permission denied",
				ErrorMessages: []string{"permission denied", "missing category"},
				Message: strings.Join([]string{
					"Applied: 4",
					"Failed: 1",
					"Top actions: Deleted torrent (ratio rule): 2; Tags updated: 2",
					"Top failures: Delete failed: 1",
					"Rules: Ratio rule: 2; Tagger: 2",
					"Samples: Example.Movie.2025; Another.Show.S01E01",
					"Errors: permission denied; missing category",
				}, "\n"),
			},
		},
		{
			Name: "automations_run_failed",
			Event: notifications.Event{
				Type:         notifications.EventAutomationsRunFailed,
				InstanceName: instanceLabel,
				Message:      "database unavailable",
				ErrorMessage: "database unavailable",
				ErrorMessages: []string{
					"database unavailable",
				},
			},
		},
	}
}

func runTimes(base time.Time, offset, duration time.Duration) (*time.Time, *time.Time) {
	start := base.Add(offset)
	end := start.Add(duration)
	return &start, &end
}

func formatEvent(event notifications.Event) (string, string) {
	instanceLabel := resolveInstanceLabel(event)
	customMessage := strings.TrimSpace(event.Message)

	switch event.Type {
	case notifications.EventTorrentCompleted:
		title := "Torrent completed"
		lines := []string{
			formatLine("Torrent", event.TorrentName+formatHashSuffix(event.TorrentHash)),
		}
		if tracker := strings.TrimSpace(event.TrackerDomain); tracker != "" {
			lines = append(lines, formatLine("Tracker", tracker))
		}
		if category := strings.TrimSpace(event.Category); category != "" {
			lines = append(lines, formatLine("Category", category))
		}
		if len(event.Tags) > 0 {
			tags := append([]string(nil), event.Tags...)
			sort.Strings(tags)
			lines = append(lines, formatLine("Tags", strings.Join(tags, ", ")))
		}
		return title, buildMessage(instanceLabel, lines)
	case notifications.EventBackupSucceeded:
		title := "Backup completed"
		lines := []string{
			formatLine("Backup", formatKind(event.BackupKind)),
			formatLine("Run", strconv.FormatInt(event.BackupRunID, 10)),
			formatLine("Torrents", strconv.Itoa(event.BackupTorrentCount)),
		}
		return title, buildMessage(instanceLabel, lines)
	case notifications.EventBackupFailed:
		title := "Backup failed"
		lines := []string{
			formatLine("Backup", formatKind(event.BackupKind)),
			formatLine("Run", strconv.FormatInt(event.BackupRunID, 10)),
			formatLine("Error", formatErrorMessage(event.ErrorMessage)),
		}
		return title, buildMessage(instanceLabel, lines)
	case notifications.EventDirScanCompleted:
		title := "Directory scan completed"
		lines := []string{
			formatLine("Run", strconv.FormatInt(event.DirScanRunID, 10)),
			formatLine("Matches", strconv.Itoa(event.DirScanMatchesFound)),
			formatLine("Torrents added", strconv.Itoa(event.DirScanTorrentsAdded)),
		}
		return title, buildMessage(instanceLabel, lines)
	case notifications.EventDirScanFailed:
		title := "Directory scan failed"
		lines := []string{
			formatLine("Run", strconv.FormatInt(event.DirScanRunID, 10)),
			formatLine("Error", formatErrorMessage(event.ErrorMessage)),
		}
		return title, buildMessage(instanceLabel, lines)
	case notifications.EventOrphanScanCompleted:
		title := "Orphan scan completed"
		lines := []string{
			formatLine("Run", strconv.FormatInt(event.OrphanScanRunID, 10)),
			formatLine("Files deleted", strconv.Itoa(event.OrphanScanFilesDeleted)),
			formatLine("Folders deleted", strconv.Itoa(event.OrphanScanFoldersDeleted)),
		}
		return title, buildMessage(instanceLabel, lines)
	case notifications.EventOrphanScanFailed:
		title := "Orphan scan failed"
		lines := []string{
			formatLine("Run", strconv.FormatInt(event.OrphanScanRunID, 10)),
			formatLine("Error", formatErrorMessage(event.ErrorMessage)),
		}
		return title, buildMessage(instanceLabel, lines)
	case notifications.EventCrossSeedAutomationSucceeded:
		title := "Cross-seed RSS automation completed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case notifications.EventCrossSeedAutomationFailed:
		title := "Cross-seed RSS automation failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case notifications.EventCrossSeedSearchSucceeded:
		title := "Cross-seed seeded search completed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case notifications.EventCrossSeedSearchFailed:
		title := "Cross-seed seeded search failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case notifications.EventCrossSeedCompletionSucceeded:
		title := "Cross-seed completion search completed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case notifications.EventCrossSeedCompletionFailed:
		title := "Cross-seed completion search failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case notifications.EventCrossSeedWebhookSucceeded:
		title := "Cross-seed webhook check completed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case notifications.EventCrossSeedWebhookFailed:
		title := "Cross-seed webhook check failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case notifications.EventAutomationsActionsApplied:
		title := "Automations actions applied"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	case notifications.EventAutomationsRunFailed:
		title := "Automations run failed"
		return formatCustomEvent(instanceLabel, title, event.Title, customMessage)
	default:
		return "", ""
	}
}

type messageField struct {
	Label  string
	Value  string
	Inline bool
}

func resolveInstanceLabel(event notifications.Event) string {
	if strings.TrimSpace(event.InstanceName) != "" {
		return strings.TrimSpace(event.InstanceName)
	}
	if event.InstanceID > 0 {
		return "Instance " + strconv.Itoa(event.InstanceID)
	}
	return "Instance"
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

func formatLine(label, value string) string {
	trimmedLabel := strings.TrimSpace(label)
	trimmedValue := strings.TrimSpace(value)
	if trimmedLabel == "" || trimmedValue == "" {
		return ""
	}
	return trimmedLabel + ": " + trimmedValue
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

func formatHashSuffix(hash string) string {
	trimmed := strings.TrimSpace(hash)
	if len(trimmed) < 8 {
		return ""
	}
	return " [" + trimmed[:8] + "]"
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

func buildNotifiarrData(event notifications.Event, title, message string) notifiarrMessageData {
	trimmedTitle := strings.TrimSpace(title)
	trimmedMessage := strings.TrimSpace(message)

	data := notifiarrMessageData{
		Event:     buildNotifiarrEventValue(event.Type),
		Timestamp: time.Now().UTC(),
	}
	if trimmedTitle != "" {
		data.Subject = trimmedTitle
	}
	if trimmedMessage != "" {
		data.Message = trimmedMessage
	}

	if event.CrossSeed != nil {
		data.CrossSeed = event.CrossSeed
	}
	if event.Automations != nil {
		data.Automations = event.Automations
	}

	if event.InstanceID > 0 {
		data.InstanceID = intPtr(event.InstanceID)
	}

	instanceName := strings.TrimSpace(event.InstanceName)
	if instanceName == "" && event.InstanceID > 0 {
		instanceName = strings.TrimSpace(resolveInstanceLabel(event))
	}
	if instanceName != "" {
		data.InstanceName = stringPtr(instanceName)
	}

	if strings.TrimSpace(event.TorrentName) != "" {
		data.TorrentName = stringPtr(event.TorrentName)
	}
	if strings.TrimSpace(event.TorrentHash) != "" {
		data.TorrentHash = stringPtr(event.TorrentHash)
	}
	if strings.TrimSpace(event.TrackerDomain) != "" {
		data.TrackerDomain = stringPtr(event.TrackerDomain)
	}
	if strings.TrimSpace(event.Category) != "" {
		data.Category = stringPtr(event.Category)
	}
	if len(event.Tags) > 0 {
		tags := append([]string(nil), event.Tags...)
		sort.Strings(tags)
		data.Tags = tags
	}
	if strings.TrimSpace(string(event.BackupKind)) != "" {
		data.BackupKind = stringPtr(string(event.BackupKind))
	}
	if event.BackupRunID > 0 {
		data.BackupRunID = int64Ptr(event.BackupRunID)
	}
	if event.BackupTorrentCount > 0 {
		data.BackupTorrentCount = intPtr(event.BackupTorrentCount)
	}
	if event.DirScanRunID > 0 {
		data.DirScanRunID = int64Ptr(event.DirScanRunID)
	}
	if event.DirScanMatchesFound > 0 {
		data.DirScanMatchesFound = intPtr(event.DirScanMatchesFound)
	}
	if event.DirScanTorrentsAdded > 0 {
		data.DirScanTorrentsAdded = intPtr(event.DirScanTorrentsAdded)
	}
	if event.OrphanScanRunID > 0 {
		data.OrphanScanRunID = int64Ptr(event.OrphanScanRunID)
	}
	if event.OrphanScanFilesDeleted > 0 {
		data.OrphanScanFilesDeleted = intPtr(event.OrphanScanFilesDeleted)
	}
	if event.OrphanScanFoldersDeleted > 0 {
		data.OrphanScanFoldersDeleted = intPtr(event.OrphanScanFoldersDeleted)
	}
	if strings.TrimSpace(event.ErrorMessage) != "" {
		data.ErrorMessage = stringPtr(event.ErrorMessage)
	}
	if len(event.ErrorMessages) > 0 {
		data.ErrorMessages = normalizeErrorMessages(event.ErrorMessages)
	}
	if data.ErrorMessage != nil {
		if len(data.ErrorMessages) == 0 {
			data.ErrorMessages = []string{*data.ErrorMessage}
		} else if !slices.Contains(data.ErrorMessages, *data.ErrorMessage) {
			data.ErrorMessages = append([]string{*data.ErrorMessage}, data.ErrorMessages...)
		}
	}

	if event.StartedAt != nil && !event.StartedAt.IsZero() {
		data.StartedAt = event.StartedAt
	}
	if event.CompletedAt != nil && !event.CompletedAt.IsZero() {
		data.CompletedAt = event.CompletedAt
	}
	if data.StartedAt != nil && data.CompletedAt != nil {
		durationMs := data.CompletedAt.Sub(*data.StartedAt).Milliseconds()
		if durationMs >= 0 {
			data.DurationMs = &durationMs
		}
	}

	description, fields := buildStructuredMessage(trimmedMessage)
	if description == "" {
		description = trimmedMessage
	}
	if description != "" {
		data.Description = description
	}
	if len(fields) > 0 {
		data.Fields = buildNotifiarrFields(fields)
	}

	return data
}

func normalizeErrorMessages(messages []string) []string {
	if len(messages) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(messages))
	out := make([]string, 0, minInt(len(messages), 10))
	for _, raw := range messages {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
		if len(out) >= 10 {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildNotifiarrEventValue(eventType notifications.EventType) string {
	value := strings.TrimSpace(string(eventType))
	if value == "" {
		return "test"
	}
	return value
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
				description = label + ": " + value
			}
			continue
		}
		fields = append(fields, normalizeField(label, value))
	}

	if description == "" && len(fields) > 0 {
		description = fields[0].Label + ": " + fields[0].Value
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

func buildNotifiarrFields(fields []messageField) []notifiarrField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]notifiarrField, 0, minInt(len(fields), discordFieldsLimit))
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

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func intPtr(value int) *int {
	if value == 0 {
		return nil
	}
	v := value
	return &v
}

func int64Ptr(value int64) *int64 {
	if value == 0 {
		return nil
	}
	v := value
	return &v
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
