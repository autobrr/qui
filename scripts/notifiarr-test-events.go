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
	"strings"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/notifications"
)

const (
	defaultEndpoint = "https://notifiarr.com/api/v1/notification/test?event=qui"
	defaultTimeout  = 15 * time.Second
)

type fixture struct {
	Name  string
	Event notifications.Event
}

func main() {
	endpoint := flag.String("endpoint", defaultEndpoint, "notifiarr endpoint")
	eventFilter := flag.String("event", "", "send only these events (comma-separated)")
	dryRun := flag.Bool("dry-run", false, "print payloads without sending")
	format := flag.String("format", "pretty", "dry-run output format: pretty|json|jsonl")
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
	var payloads []json.RawMessage
	for _, item := range fixtures {
		payload, err := notifications.BuildNotifiarrTestPayload(item.Event)
		if err != nil {
			fmt.Fprintf(os.Stderr, "build %s failed: %v\n", item.Name, err)
			continue
		}
		if len(payload) == 0 {
			fmt.Printf("skip %s: empty payload\n", item.Name)
			continue
		}

		if *dryRun {
			switch strings.ToLower(strings.TrimSpace(*format)) {
			case "", "pretty":
				printPayload(item.Name, payload)
			case "json":
				payloads = append(payloads, payload)
			case "jsonl":
				fmt.Println(string(payload))
			default:
				fmt.Fprintf(os.Stderr, "invalid -format %q (expected: pretty|json|jsonl)\n", *format)
				os.Exit(2)
			}
			continue
		}

		if err := sendPayload(client, *endpoint, payload); err != nil {
			fmt.Fprintf(os.Stderr, "send %s failed: %v\n", item.Name, err)
			continue
		}
		fmt.Printf("sent %s\n", item.Name)
	}

	if *dryRun && strings.EqualFold(strings.TrimSpace(*format), "json") {
		encoded, err := json.MarshalIndent(payloads, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshal payloads failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(encoded))
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

func printPayload(name string, payload json.RawMessage) {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal %s failed: %v\n", name, err)
		return
	}
	fmt.Printf("payload %s:\n%s\n", name, string(encoded))
}

func sendPayload(client *http.Client, endpoint string, payload json.RawMessage) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(payload))
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
			Name: "torrent_added",
			Event: notifications.Event{
				Type:                   notifications.EventTorrentAdded,
				InstanceName:           instanceLabel,
				TorrentName:            "Some.Movie.2025.1080p.BluRay.x264",
				TorrentHash:            "abcdef0123456789abcdef0123456789abcdef01",
				TorrentAddedOn:         baseTime.Add(-10 * time.Second).Unix(),
				TorrentETASeconds:      int64((90 * time.Minute).Seconds()),
				TorrentState:           "downloading",
				TorrentProgress:        0.25,
				TorrentRatio:           0.0,
				TorrentTotalSizeBytes:  20_000_000_000,
				TorrentDownloadedBytes: 5_000_000_000,
				TorrentAmountLeftBytes: 15_000_000_000,
				TorrentDlSpeedBps:      25_000_000,
				TorrentUpSpeedBps:      1_000_000,
				TorrentNumSeeds:        120,
				TorrentNumLeechs:       35,
				TrackerDomain:          "tracker.example",
				Category:               "movies",
				Tags:                   []string{"seed", "bluray"},
			},
		},
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
					Added:   2,
					Samples: []string{"Primary", "Archive"},
				},
				StartedAt:   webhookStart,
				CompletedAt: webhookEnd,
				Message: strings.Join([]string{
					"Torrent: Example.Show.S01E01",
					"Added: 2",
					"Instances: Primary; Archive",
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
					Rules: []notifications.AutomationRuleSummary{
						{
							RuleID:   12,
							RuleName: "Ratio rule",
							Applied:  2,
							Failed:   1,
							Actions: []notifications.AutomationActionSummary{
								{Action: models.ActivityActionDeletedRatio, Label: "Deleted torrent (ratio rule)", Applied: 2, Failed: 0},
								{Action: models.ActivityActionDeleteFailed, Label: "Delete failed", Applied: 0, Failed: 1},
							},
						},
						{
							RuleID:   13,
							RuleName: "Tagger",
							Applied:  2,
							Failed:   0,
							Actions: []notifications.AutomationActionSummary{
								{Action: models.ActivityActionTagsChanged, Label: "Tags updated", Applied: 2, Failed: 0},
							},
						},
					},
					Samples: []string{"Example.Movie.2025", "Another.Show.S01E01"},
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
