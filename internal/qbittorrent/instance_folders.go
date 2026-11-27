// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
)

const categoryVersionTTL = time.Minute

type InstanceFolders struct {
	DefaultSavePath     string
	DownloadPath        string
	CategorySaveFolders []string
	DownloadFolders     []string
	MonitoredFolders    []string
}

type categoryVersion struct {
	rule     CategoryPathRule
	optional bool
	at       time.Time
}

func (sm *SyncManager) InstanceFolders(ctx context.Context, d fsops.PathDialect, instanceID int) (InstanceFolders, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return InstanceFolders{}, fmt.Errorf("failed to get client: %w", err)
	}
	prefs, err := client.refreshAppPreferences(ctx)
	if err != nil {
		return InstanceFolders{}, fmt.Errorf("failed to read qBittorrent preferences: %w", err)
	}
	categories, err := client.GetCategoriesCtx(ctx)
	if err != nil {
		return InstanceFolders{}, fmt.Errorf("failed to read qBittorrent categories: %w", err)
	}
	version, err := sm.categoryVersion(ctx, client, instanceID)
	if err != nil {
		return InstanceFolders{}, err
	}
	return instanceFolders(d, prefs, categories, version.ruleFor(prefs))
}

func (sm *SyncManager) CategoryPathRule(ctx context.Context, instanceID int, prefs *qbt.AppPreferences) (CategoryPathRule, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return CategoryPathRule{}, fmt.Errorf("failed to get client: %w", err)
	}
	version, err := sm.categoryVersion(ctx, client, instanceID)
	if err != nil {
		return CategoryPathRule{}, err
	}
	return version.ruleFor(prefs), nil
}

func (sm *SyncManager) CategorySaveFolder(ctx context.Context, d fsops.PathDialect, instanceID int, prefs *qbt.AppPreferences, category string) (string, error) {
	client, err := sm.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		return "", fmt.Errorf("failed to get client: %w", err)
	}
	categories, err := client.GetCategoriesCtx(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to read qBittorrent categories: %w", err)
	}
	version, err := sm.categoryVersion(ctx, client, instanceID)
	if err != nil {
		return "", err
	}
	return CategorySavePath(d, category, categories, d.Clean(prefs.SavePath), version.ruleFor(prefs)), nil
}

func (v categoryVersion) ruleFor(prefs *qbt.AppPreferences) CategoryPathRule {
	rule := v.rule
	if v.optional && !prefs.UseSubcategories {
		rule.Nest = false
	}
	return rule
}

func (sm *SyncManager) categoryVersion(ctx context.Context, client *Client, instanceID int) (categoryVersion, error) {
	expired := func(_ int, v categoryVersion) bool { return time.Since(v.at) >= categoryVersionTTL }
	sm.categoryVersionsMu.Lock()
	v, ok := sm.categoryVersions[instanceID]
	if ok && !expired(instanceID, v) {
		sm.categoryVersionsMu.Unlock()
		return v, nil
	}
	maps.DeleteFunc(sm.categoryVersions, expired)
	sm.categoryVersionsMu.Unlock()

	if err := client.RefreshCapabilities(ctx); err != nil {
		return categoryVersion{}, fmt.Errorf("failed to refresh qBittorrent capabilities: %w", err)
	}
	nests := client.Supports(CapCategorySavePathNesting)
	fileNames := client.Supports(CapCategoryFileNames)
	optional := nests && !client.Supports(CapSubcategoriesAlwaysEnabled)
	platform, err := categoryFolderPlatform(ctx, client, fileNames)
	if err != nil {
		return categoryVersion{}, err
	}
	v = categoryVersion{
		rule:     CategoryPathRule{Nest: nests, FileName: fileNames, Platform: platform, InheritsDownloadPath: nests},
		optional: optional,
		at:       time.Now(),
	}

	sm.categoryVersionsMu.Lock()
	defer sm.categoryVersionsMu.Unlock()
	if sm.categoryVersions == nil {
		sm.categoryVersions = make(map[int]categoryVersion)
	}
	sm.categoryVersions[instanceID] = v
	return v, nil
}

func categoryFolderPlatform(ctx context.Context, client *Client, fileNames bool) (string, error) {
	if !fileNames {
		return "", nil
	}
	info, err := client.GetAppInfo(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get qBittorrent build info: %w", err)
	}
	return info.BuildInfo.Platform, nil
}

func instanceFolders(d fsops.PathDialect, prefs *qbt.AppPreferences, categories map[string]qbt.Category, rule CategoryPathRule) (InstanceFolders, error) {
	f := InstanceFolders{DefaultSavePath: d.Clean(prefs.SavePath)}
	if f.DefaultSavePath == "." {
		return InstanceFolders{}, errors.New("qBittorrent reported an empty default save path")
	}
	if !d.IsAbs(f.DefaultSavePath) {
		return InstanceFolders{}, fmt.Errorf("qBittorrent default save path %q is not absolute", prefs.SavePath)
	}
	if prefs.TempPathEnabled {
		f.DownloadPath = d.Clean(prefs.TempPath)
		if !d.IsAbs(f.DownloadPath) {
			return InstanceFolders{}, fmt.Errorf("qBittorrent download path %q is not absolute", prefs.TempPath)
		}
	}
	for name := range categories {
		f.CategorySaveFolders = append(f.CategorySaveFolders, CategorySavePath(d, name, categories, f.DefaultSavePath, rule))
		p, ok := CategoryDownloadPath(d, name, categories, prefs.TempPath, prefs.TempPathEnabled, rule)
		if !ok {
			continue
		}
		if p == "" {
			return InstanceFolders{}, fmt.Errorf("download path of category %q rests on the qBittorrent download path %q, which is not absolute", name, prefs.TempPath)
		}
		f.DownloadFolders = append(f.DownloadFolders, p)
	}
	for dir, target := range prefs.ScanDirs {
		if dir = d.Clean(dir); d.IsAbs(dir) {
			f.MonitoredFolders = append(f.MonitoredFolders, dir)
		}
		if target.Mode() == qbt.MonitoredFolderModeCustomPath {
			p := d.Clean(target.CustomPath())
			if !d.IsAbs(p) {
				p = d.Join(f.DefaultSavePath, p)
			}
			f.MonitoredFolders = append(f.MonitoredFolders, p)
		}
	}
	for _, list := range []*[]string{&f.CategorySaveFolders, &f.DownloadFolders, &f.MonitoredFolders} {
		slices.Sort(*list)
		*list = slices.Compact(*list)
	}
	return f, nil
}
