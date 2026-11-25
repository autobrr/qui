// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package linktree

import (
	"context"
	"strings"

	"github.com/autobrr/qui/internal/models"
)

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

const (
	PresetFlat       = "flat"
	PresetByTracker  = "by-tracker"
	PresetByInstance = "by-instance"
)

type Config struct {
	BaseDirs []string
	Preset   string
	Kind     LinkKind
}

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

func ParseBaseDirs(list string) []string {
	var dirs []string
	for dir := range strings.SplitSeq(list, ",") {
		if dir = strings.TrimSpace(dir); dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

type TrackerCustomizations interface {
	List(ctx context.Context) ([]*models.TrackerCustomization, error)
}

func TrackerFolderName(ctx context.Context, customizations TrackerCustomizations, announceDomain, indexerName string) string {
	var list []*models.TrackerCustomization
	if customizations != nil {
		if customs, err := customizations.List(ctx); err == nil {
			list = customs
		}
	}
	return models.ResolveTrackerDisplayName(announceDomain, indexerName, list)
}
