// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"errors"
	"fmt"
	"slices"
)

// PermissionArea is a part of the qui API that an API key Permission covers.
type PermissionArea string

// PermissionLevel is what a Permission lets a key do in its area. Write
// includes read and run. Run does not include read, because settings hold
// credentials.
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

type permissionAreaInfo struct {
	area   PermissionArea
	label  string
	levels []PermissionLevel
}

var (
	readWrite       = []PermissionLevel{LevelRead, LevelWrite}
	readRunWrite    = []PermissionLevel{LevelRead, LevelRun, LevelWrite}
	permissionAreas = []permissionAreaInfo{
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

func lookupPermissionArea(area PermissionArea) (permissionAreaInfo, bool) {
	i := slices.IndexFunc(permissionAreas, func(a permissionAreaInfo) bool { return a.area == area })
	if i < 0 {
		return permissionAreaInfo{}, false
	}
	return permissionAreas[i], true
}

// String names the Permission as a user reads it, for example "Torrents: write".
func (p Permission) String() string {
	label := string(p.Area)
	if info, ok := lookupPermissionArea(p.Area); ok {
		label = info.label
	}
	return label + ": " + string(p.Level)
}

// AllPermissions returns the list that grants every route a key can reach: the
// highest level of each area. Existing keys got this list on upgrade.
func AllPermissions() []Permission {
	perms := make([]Permission, 0, len(permissionAreas)+1)
	for _, info := range permissionAreas {
		if slices.Contains(info.levels, LevelWrite) {
			perms = append(perms, Permission{info.area, LevelWrite})
			continue
		}
		for _, level := range info.levels {
			perms = append(perms, Permission{info.area, level})
		}
	}
	return perms
}

// ValidatePermissions refuses an empty list, an unknown area, and a level that
// the area does not allow.
func ValidatePermissions(perms []Permission) error {
	if len(perms) == 0 {
		return errors.New("API key needs at least one permission")
	}
	for _, p := range perms {
		info, ok := lookupPermissionArea(p.Area)
		if !ok {
			return fmt.Errorf("unknown permission area %q", p.Area)
		}
		if !slices.Contains(info.levels, p.Level) {
			return fmt.Errorf("permission area %q does not allow level %q", p.Area, p.Level)
		}
	}
	return nil
}

// Allows reports whether the key holds level in area. Write includes read and run.
func (k *APIKey) Allows(area PermissionArea, level PermissionLevel) bool {
	return slices.ContainsFunc(k.Permissions, func(p Permission) bool {
		return p.Area == area && (p.Level == level || p.Level == LevelWrite)
	})
}

// ReachesInstance reports whether the Instance limit of the key holds the
// instance. A key with no Instance limit reaches every instance.
func (k *APIKey) ReachesInstance(instanceID int) bool {
	return len(k.InstanceIDs) == 0 || slices.Contains(k.InstanceIDs, instanceID)
}
