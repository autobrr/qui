// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

type PermissionTemplate struct {
	ID                  string
	Permissions         []Permission
	AllowsInstanceLimit bool
}

var PermissionTemplates = []PermissionTemplate{
	{"autobrr", []Permission{{AreaCrossSeed, LevelRun}}, false},
	{"dashbrr", []Permission{{AreaInstances, LevelRead}}, true},
	{"homepage", []Permission{{AreaTorrents, LevelRead}}, true},
	{"arrDirScan", []Permission{{AreaDirScan, LevelRun}}, false},
	{"quiExtension", []Permission{{AreaInstances, LevelRead}, {AreaTorrents, LevelWrite}}, true},
	{"quiExtensionCrossSeed", []Permission{{AreaInstances, LevelRead}, {AreaTorrents, LevelWrite}, {AreaCrossSeed, LevelWrite}}, false},
}
