// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"errors"
	"fmt"
	"slices"
)

type PermissionArea string

type PermissionLevel string

const (
	LevelRead  PermissionLevel = "read"
	LevelRun   PermissionLevel = "run"
	LevelWrite PermissionLevel = "write"
)

const (
	AreaInstances        PermissionArea = "instances"
	AreaTorrents         PermissionArea = "torrents"
	AreaCrossSeed        PermissionArea = "crossSeed"
	AreaDirScan          PermissionArea = "dirScan"
	AreaAutomations      PermissionArea = "automations"
	AreaRSS              PermissionArea = "rss"
	AreaBackups          PermissionArea = "backups"
	AreaOrphanScan       PermissionArea = "orphanScan"
	AreaExternalPrograms PermissionArea = "externalPrograms"
	AreaIndexers         PermissionArea = "indexers"
	AreaArr              PermissionArea = "arr"
	AreaNotifications    PermissionArea = "notifications"
	AreaSettings         PermissionArea = "settings"
	AreaLogs             PermissionArea = "logs"
	AreaSystem           PermissionArea = "system"
)

type Permission struct {
	Area  PermissionArea  `json:"area"`
	Level PermissionLevel `json:"level"`
}

type PermissionAreaInfo struct {
	Area   PermissionArea
	Label  string
	Levels []PermissionLevel
}

var (
	readWrite       = []PermissionLevel{LevelRead, LevelWrite}
	readRunWrite    = []PermissionLevel{LevelRead, LevelRun, LevelWrite}
	PermissionAreas = []PermissionAreaInfo{
		{AreaInstances, "Instances", readWrite},
		{AreaTorrents, "Torrents", readWrite},
		{AreaCrossSeed, "Cross-seed", readRunWrite},
		{AreaDirScan, "Dir Scan", readRunWrite},
		{AreaAutomations, "Automations", readRunWrite},
		{AreaRSS, "RSS", readRunWrite},
		{AreaBackups, "Backups", readRunWrite},
		{AreaOrphanScan, "Orphan scan", readRunWrite},
		{AreaExternalPrograms, "External programs", []PermissionLevel{LevelRead, LevelRun}},
		{AreaIndexers, "Indexers", readWrite},
		{AreaArr, "Arr", readWrite},
		{AreaNotifications, "Notifications", readWrite},
		{AreaSettings, "Settings", readWrite},
		{AreaLogs, "Logs", readWrite},
		{AreaSystem, "System", []PermissionLevel{LevelRead}},
	}
)

func lookupPermissionArea(area PermissionArea) (PermissionAreaInfo, bool) {
	i := slices.IndexFunc(PermissionAreas, func(a PermissionAreaInfo) bool { return a.Area == area })
	if i < 0 {
		return PermissionAreaInfo{}, false
	}
	return PermissionAreas[i], true
}

func (p Permission) String() string {
	label := string(p.Area)
	if info, ok := lookupPermissionArea(p.Area); ok {
		label = info.Label
	}
	return label + ": " + string(p.Level)
}

func AllPermissions() []Permission {
	perms := make([]Permission, 0, len(PermissionAreas)+1)
	for _, info := range PermissionAreas {
		if slices.Contains(info.Levels, LevelWrite) {
			perms = append(perms, Permission{info.Area, LevelWrite})
			continue
		}
		for _, level := range info.Levels {
			perms = append(perms, Permission{info.Area, level})
		}
	}
	return perms
}

func ValidatePermissions(perms []Permission) error {
	if len(perms) == 0 {
		return errors.New("API key needs at least one permission")
	}
	for _, p := range perms {
		info, ok := lookupPermissionArea(p.Area)
		if !ok {
			return fmt.Errorf("unknown permission area %q", p.Area)
		}
		if !slices.Contains(info.Levels, p.Level) {
			return fmt.Errorf("permission area %q does not allow level %q", p.Area, p.Level)
		}
	}
	return nil
}

func (k *APIKey) Allows(area PermissionArea, level PermissionLevel) bool {
	return slices.ContainsFunc(k.Permissions, func(p Permission) bool {
		return p.Area == area && (p.Level == level || p.Level == LevelWrite)
	})
}

func (k *APIKey) ReachesInstance(instanceID int) bool {
	return len(k.InstanceIDs) == 0 || slices.Contains(k.InstanceIDs, instanceID)
}
