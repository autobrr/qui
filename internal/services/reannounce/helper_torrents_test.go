// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package reannounce

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

type fakeHelpers struct {
	instanceID int
	hashes     map[string]bool
}

func (f fakeHelpers) IsHelper(instanceID int, hash string) bool {
	return instanceID == f.instanceID && f.hashes[hash]
}

func (f fakeHelpers) HelperKinds(instanceID int) map[string]string {
	if instanceID != f.instanceID {
		return nil
	}
	var kinds map[string]string
	for hash, helper := range f.hashes {
		if !helper {
			continue
		}
		if kinds == nil {
			kinds = make(map[string]string)
		}
		kinds[hash] = "transfer"
	}
	return kinds
}

func TestReannounceSkipsHelperTorrents(t *testing.T) {
	now := time.Now()
	helper, own := strings.Repeat("c", 40), strings.Repeat("d", 40)
	torrents := map[string]qbt.Torrent{}
	for _, hash := range []string{helper, own} {
		torrents[hash] = qbt.Torrent{
			Hash: hash, Name: "Quiet.Harbor.2024.1080p.WEB-DL-GRP", AddedOn: now.Unix() - 60,
			State: qbt.TorrentStateStalledUp, Tags: "qui-transfer",
			Trackers: []qbt.TorrentTracker{{Url: "https://tracker.test/announce", Status: qbt.TrackerStatusNotWorking}},
		}
	}

	for _, tc := range []struct {
		name    string
		helpers map[string]bool
		want    []string
	}{
		{name: "a helper torrent is left out", helpers: map[string]bool{helper: true}, want: []string{strings.ToUpper(own)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newService := func() (*Service, int, *[]string) {
				svc, instanceID, _ := newScanTestService(t, "2.11.4", torrents)
				svc.now = func() time.Time { return now }
				helpers := fakeHelpers{instanceID: instanceID, hashes: tc.helpers}
				svc.syncManager.SetHelperTorrents(helpers)
				svc.SetHelperTorrents(helpers)
				svc.settingsCache = NewSettingsCache(nil)
				svc.settingsCache.Replace(&models.InstanceReannounceSettings{InstanceID: instanceID, Enabled: true, MonitorAll: true})
				var jobs []string
				svc.runJob = func(_ context.Context, _ int, hash, _, _ string) { jobs = append(jobs, hash) }
				return svc, instanceID, &jobs
			}

			svc, instanceID, jobs := newService()
			svc.scanInstance(t.Context(), instanceID, svc.getSettings(t.Context(), instanceID))
			require.ElementsMatch(t, tc.want, *jobs, "scan")

			svc, instanceID, _ = newService()
			requested := slices.Collect(maps.Keys(svc.lookupTorrents(t.Context(), instanceID, []string{helper, own})))
			require.ElementsMatch(t, tc.want, requested, "request")
		})
	}
}
