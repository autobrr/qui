// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linktree

import (
	"context"
	"strings"

	"github.com/autobrr/qui/internal/models"
)

// LinkKind is how a Link tree links its files to the source files.
type LinkKind int

const (
	Hardlink LinkKind = iota + 1
	Reflink
)

func (k LinkKind) String() string {
	if k == Reflink {
		return "reflink"
	}
	return "hardlink"
}

// The Directory presets, as the instance row stores them.
const (
	PresetFlat       = "flat"
	PresetByTracker  = "by-tracker"
	PresetByInstance = "by-instance"
)

// Config is the link configuration of one instance.
type Config struct {
	BaseDirs []string
	// Preset is the Directory preset. An unknown value acts as PresetFlat.
	Preset string
	Kind   LinkKind
}

// ConfigOf reads the link configuration from the instance row. It reports
// false when the instance has no link mode on.
func ConfigOf(instance *models.Instance) (Config, bool) {
	cfg := Config{BaseDirs: ParseBaseDirs(instance.HardlinkBaseDir), Preset: instance.HardlinkDirPreset}
	switch {
	case instance.UseReflinks:
		cfg.Kind = Reflink
	case instance.UseHardlinks:
		cfg.Kind = Hardlink
	default:
		return Config{}, false
	}
	return cfg, true
}

// ParseBaseDirs splits the comma-separated Link base directories list of an
// instance.
func ParseBaseDirs(list string) []string {
	var dirs []string
	for dir := range strings.SplitSeq(list, ",") {
		if dir = strings.TrimSpace(dir); dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// TrackerCustomizations lists the Tracker Customizations.
type TrackerCustomizations interface {
	List(ctx context.Context) ([]*models.TrackerCustomization, error)
}

// TrackerFolderName is the folder name of the By Tracker preset: the display
// name of a Tracker Customization for the announce domain, then the indexer
// name, then the announce domain. A failed list read counts as no
// customization.
func TrackerFolderName(ctx context.Context, customizations TrackerCustomizations, announceDomain, indexerName string) string {
	var list []*models.TrackerCustomization
	if customizations != nil {
		if customs, err := customizations.List(ctx); err == nil {
			list = customs
		}
	}
	return models.ResolveTrackerDisplayName(announceDomain, indexerName, list)
}
