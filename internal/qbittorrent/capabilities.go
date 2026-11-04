// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"

	"github.com/Masterminds/semver/v3"
)

// Capability is something an instance can do or send because of its
// qBittorrent version. Client.Supports answers for one.
type Capability int

const (
	CapSetTags Capability = iota
	CapSetComment
	CapTorrentCreation
	CapTorrentExport
	CapTrackerEditing
	// CapTrackerHealth is torrents/info?includeTrackers, which tracker health reads.
	CapTrackerHealth
	// CapAnnounceFlags is the has_tracker_* flags in sync/maindata.
	CapAnnounceFlags
	CapFilePriority
	CapRenameTorrent
	CapRenameFile
	CapRenameFolder
	CapSubcategories
	CapSubcategoriesAlwaysEnabled
	// CapCategorySavePathNesting resolves an empty category save path under
	// the parent category. qBittorrent 4.6 shows subcategories but saves to
	// the default save path plus the full name.
	CapCategorySavePathNesting
	// CapCategoryFileNames names the folder of a category with no save path
	// with toValidFileName, not toValidPath.
	CapCategoryFileNames
	CapTorrentTmpPath
	CapPathAutocomplete
	CapTestEmail
	CapSetRSSFeedURL
	// CapShareLimitsAction is setShareLimits with ratio, seeding time,
	// inactive seeding time and the share-limit action.
	CapShareLimitsAction
	// CapShareLimitsMode is setShareLimits with ShareLimitsMode (MatchAny / MatchAll).
	CapShareLimitsMode
	// CapLiveReannounce is the seconds until the next announce in the torrent
	// list. Before that, only torrents/properties has the value.
	CapLiveReannounce
	// CapListFieldsV5 is popularity, private and comment in the torrent list.
	// Before that, qBittorrent leaves them out.
	CapListFieldsV5
	// CapListCreatedBy is created_by in the torrent list.
	CapListCreatedBy
	// CapPartialCategoryEdit is an editCategory that keeps the options it
	// leaves out. Before that, an edit replaces them all.
	CapPartialCategoryEdit
	// CapProcessInfo is app/processInfo, which has the launch time.
	CapProcessInfo
	// CapExportOmitsTrackers marks the qBittorrent 4.6.x builds that export a
	// .torrent file without its trackers. No capture confirms the bug: the
	// 4.6.0 and 4.6.7 feature probes in go-qbittorrent keep the tracker.
	CapExportOmitsTrackers
)

// versionRange holds the Web API versions of a capability. A nil highest
// means no upper bound; both ends are inclusive.
type versionRange struct {
	lowest, highest *semver.Version
}

func since(lowest string) versionRange {
	return versionRange{lowest: semver.MustParse(lowest)}
}

// capabilityVersions is the one table of qBittorrent version cut-offs. A
// capability that it leaves out reads as not supported.
var capabilityVersions = map[Capability]versionRange{
	CapSetTags:                    since("2.11.4"),
	CapSetComment:                 since("2.12.1"),
	CapTorrentCreation:            since("2.11.2"),
	CapTorrentExport:              since("2.8.11"),
	CapTrackerEditing:             since("2.2.0"),
	CapTrackerHealth:              since("2.11.4"),
	CapAnnounceFlags:              since("2.11.7"), // qBittorrent 5.2
	CapFilePriority:               since("2.2.0"),
	CapRenameTorrent:              since("2.0.0"),
	CapRenameFile:                 since("2.4.0"),
	CapRenameFolder:               since("2.7.0"),
	CapSubcategories:              since("2.9.0"),
	CapSubcategoriesAlwaysEnabled: since("2.15.0"),
	CapCategorySavePathNesting:    since("2.10.0"), // qBittorrent 5.0; 4.6.x is 2.9.x
	CapCategoryFileNames:          since("2.15.4"), // qBittorrent 5.3; 5.2.x is 2.15.1, and dev builds at 2.15.3 predate the rule
	CapTorrentTmpPath:             since("2.8.4"),
	CapPathAutocomplete:           since("2.11.2"),
	CapTestEmail:                  since("2.11.2"),
	CapSetRSSFeedURL:              since("2.9.1"),
	CapShareLimitsAction:          since("2.15.1"),
	CapShareLimitsMode:            since("2.16.0"),
	CapLiveReannounce:             since("2.10.0"), // qBittorrent 5.0
	CapListFieldsV5:               since("2.11.2"), // qBittorrent 5.0.0rc1. 5.0.0beta1 (2.10.4) sends comment, but not popularity or private
	CapListCreatedBy:              since("2.15.1"), // qBittorrent 5.2
	CapPartialCategoryEdit:        since("2.16.1"), // qBittorrent 5.3. The partial edit (4b429e4f4) landed at 2.16.0, and 5.3.0 reports 2.16.2
	CapProcessInfo:                since("2.15.1"),
	CapExportOmitsTrackers:        {lowest: semver.MustParse("2.9.1"), highest: semver.MustParse("2.9.3")},
}

func versionSupports(version *semver.Version, capability Capability) bool {
	r, ok := capabilityVersions[capability]
	if version == nil || !ok {
		return false
	}
	return !version.LessThan(r.lowest) && (r.highest == nil || !version.GreaterThan(r.highest))
}

// WebAPIVersionsChanged reports whether an instance in versions now has another
// Web API version, "" while unknown. An instance with no client counts as
// unchanged.
func (sm *SyncManager) WebAPIVersionsChanged(ctx context.Context, versions map[int]string) bool {
	for id, version := range versions {
		if client, err := sm.clientPool.GetClientOffline(ctx, id); err == nil && client.GetWebAPIVersion() != version {
			return true
		}
	}
	return false
}
