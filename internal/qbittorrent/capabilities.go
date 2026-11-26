// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"

	"github.com/Masterminds/semver/v3"
)

type Capability int

const (
	CapSetTags Capability = iota
	CapSetComment
	CapTorrentCreation
	CapTorrentExport
	CapTrackerEditing
	CapTrackerHealth
	CapAnnounceFlags
	CapFilePriority
	CapRenameTorrent
	CapRenameFile
	CapRenameFolder
	CapSubcategories
	CapSubcategoriesAlwaysEnabled
	CapCategorySavePathNesting
	CapCategoryFileNames
	CapTorrentTmpPath
	CapPathAutocomplete
	CapTestEmail
	CapSetRSSFeedURL
	CapShareLimitsAction
	CapShareLimitsMode
	CapLiveReannounce
	CapListFieldsV5
	CapListCreatedBy
	CapPartialCategoryEdit
	CapProcessInfo
	CapFreeSpaceAtPath
	CapExportOmitsTrackers
	CapTransferAndShare
)

type versionRange struct {
	lowest, highest *semver.Version
}

func since(lowest string) versionRange {
	return versionRange{lowest: semver.MustParse(lowest)}
}

var capabilityVersions = map[Capability]versionRange{
	CapSetTags:                    since("2.11.4"),
	CapSetComment:                 since("2.12.1"),
	CapTorrentCreation:            since("2.11.2"),
	CapTorrentExport:              since("2.8.11"),
	CapTrackerEditing:             since("2.2.0"),
	CapTrackerHealth:              since("2.11.4"),
	CapAnnounceFlags:              since("2.11.7"),
	CapFilePriority:               since("2.2.0"),
	CapRenameTorrent:              since("2.0.0"),
	CapRenameFile:                 since("2.4.0"),
	CapRenameFolder:               since("2.7.0"),
	CapSubcategories:              since("2.9.0"),
	CapSubcategoriesAlwaysEnabled: since("2.15.0"),
	CapCategorySavePathNesting:    since("2.10.0"),
	CapCategoryFileNames:          since("2.15.4"),
	CapTorrentTmpPath:             since("2.8.4"),
	CapPathAutocomplete:           since("2.11.2"),
	CapTestEmail:                  since("2.11.2"),
	CapSetRSSFeedURL:              since("2.9.1"),
	CapShareLimitsAction:          since("2.15.1"),
	CapShareLimitsMode:            since("2.16.0"),
	CapLiveReannounce:             since("2.10.0"),
	CapListFieldsV5:               since("2.11.2"),
	CapListCreatedBy:              since("2.15.1"),
	CapPartialCategoryEdit:        since("2.16.1"),
	CapProcessInfo:                since("2.15.1"),
	CapFreeSpaceAtPath:            since("2.15.2"),
	CapTransferAndShare:           since("2.11.3"),
	CapExportOmitsTrackers:        {lowest: semver.MustParse("2.9.1"), highest: semver.MustParse("2.9.3")},
}

func versionSupports(version *semver.Version, capability Capability) bool {
	r, ok := capabilityVersions[capability]
	if version == nil || !ok {
		return false
	}
	return !version.LessThan(r.lowest) && (r.highest == nil || !version.GreaterThan(r.highest))
}

func (sm *SyncManager) WebAPIVersionsChanged(ctx context.Context, versions map[int]string) bool {
	for id, version := range versions {
		if client, err := sm.clientPool.GetClientOffline(ctx, id); err == nil && client.GetWebAPIVersion() != version {
			return true
		}
	}
	return false
}
