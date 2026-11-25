// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import "context"

type InstanceReportFacts struct {
	QBittorrentVersion string `json:"qbittorrentVersion,omitempty"`
	LibtorrentVersion  string `json:"libtorrentVersion,omitempty"`
	State              string `json:"state"`
	TorrentCount       *int   `json:"torrentCount,omitempty"`
}

func (cp *ClientPool) ReportFacts(instanceID int, active bool) InstanceReportFacts {
	facts := InstanceReportFacts{State: "disabled"}
	if !active {
		return facts
	}
	facts.State = "offline"
	client, err := cp.GetClientOffline(context.Background(), instanceID)
	if err != nil {
		return facts
	}
	if client.IsHealthy() {
		facts.State = "connected"
	}
	if info, _ := client.cachedAppInfo(); info != nil {
		facts.QBittorrentVersion = info.Version
		if info.BuildInfo != nil {
			facts.LibtorrentVersion = info.BuildInfo.Libtorrent
		}
	}
	if snapshot := client.fullList.Load(); snapshot != nil {
		count := snapshot.Len()
		facts.TorrentCount = &count
	}
	return facts
}
