// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"fmt"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/logbudget"
)

func sinceRigWithUnregistered(t *testing.T, webAPI string, count int) *sinceRig {
	t.Helper()
	rig := newSinceRig(t, webAPI)
	rig.set(func() {
		added := rig.torrents[flagsHashDown].addedOn
		for i := range count {
			hash := fmt.Sprintf("%040x", 0xa000+i)
			announce := fmt.Sprintf("https://t%d.example.invalid/announce/key%04d", i, i)
			rig.torrents[hash] = &flagsTorrent{flags: [3]bool{false, false, true}, addedOn: added, trackers: []qbt.TorrentTracker{
				{Url: announce, Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"},
			}}
			rig.trackers[announce] = []string{hash}
		}
	})
	return rig
}

func setUnregisteredTrackerStatus(rig *sinceRig, status qbt.TrackerStatus, message string) {
	rig.set(func() {
		for hash, torrent := range rig.torrents {
			if hash == flagsHashHealthy || hash == flagsHashError {
				continue
			}
			for i := range torrent.trackers {
				torrent.trackers[i].Status, torrent.trackers[i].Message = status, message
			}
		}
	})
}

func TestUnregisteredSinceLogsDoNotGrowWithTheTorrents(t *testing.T) {
	for _, webAPI := range []string{"2.15.1", "2.11.4"} {
		t.Run(webAPI, func(t *testing.T) {
			passes := func(count int) logbudget.Lines {
				rig := sinceRigWithUnregistered(t, webAPI, count)
				return logbudget.Count(t, func() {
					rig.pass(t)
					require.Len(t, rig.store.stored(), count+1, "every unregistered torrent has a time")
					for _, step := range []struct {
						status  qbt.TrackerStatus
						message string
					}{
						{qbt.TrackerStatusNotWorking, "timed out"},
						{qbt.TrackerStatusOK, ""},
						{qbt.TrackerStatusNotWorking, "Unregistered torrent"},
					} {
						rig.now = rig.now.Add(time.Hour)
						rig.sm.trackerHealthDetailMaxAge = 0
						setUnregisteredTrackerStatus(rig, step.status, step.message)
						rig.pass(t)
					}
					require.Len(t, rig.store.stored(), count+1, "the times start again on the last answer")
					rig.requireSince(t, rig.now, "the working tracker started the time over")
				})
			}
			logbudget.AssertFlat(t, passes(2), passes(40))
		})
	}
}
