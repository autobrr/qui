// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import "context"

// InstanceReportFacts is what a Problem report says about one instance. It
// holds no address, user name or path.
type InstanceReportFacts struct {
	QBittorrentVersion string `json:"qbittorrentVersion,omitempty"`
	LibtorrentVersion  string `json:"libtorrentVersion,omitempty"`
	// State is connected, offline or disabled.
	State        string `json:"state"`
	TorrentCount *int   `json:"torrentCount,omitempty"`
}

// ReportFacts returns the Problem report facts of an instance from what the
// pool already holds: the last app info and the torrent list of the last full
// sync. It sends no request to qBittorrent, and leaves out what it does not hold.
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
