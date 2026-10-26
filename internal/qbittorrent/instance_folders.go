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

// categoryVersionTTL is how long an instance's version answer is reused, since
// reading it costs a request.
const categoryVersionTTL = time.Minute

// InstanceFolders are the folders qBittorrent saves and downloads into for one
// instance, in the dialect of its backend. Each list is sorted.
type InstanceFolders struct {
	DefaultSavePath string
	// DownloadPath is "" when it is turned off.
	DownloadPath        string
	CategorySaveFolders []string
	// DownloadFolders are each category's download folder.
	DownloadFolders []string
	// MonitoredFolders are each monitored folder and its custom save path.
	MonitoredFolders []string
}

// categoryVersion is what an instance's qBittorrent version says about
// category folders.
type categoryVersion struct {
	// rule is the rule with subcategories on.
	rule CategoryPathRule
	// optional: the use_subcategories preference can turn nesting off
	// (qBittorrent 5.0 and 5.1). 5.2 dropped it and always nests.
	optional bool
	at       time.Time
}

// InstanceFolders reads the preferences and the categories from qBittorrent,
// not from the cache, so a change in its WebUI is seen on the next call. The
// cache can also name a folder qBittorrent no longer uses: before WebAPI
// 2.11.10 a partial maindata update never reports a category going back to
// inherit its download path. It fails rather than return a short list, which
// would leave a folder qBittorrent saves into to be removed.
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
	rule := version.rule
	if version.optional && !prefs.UseSubcategories {
		rule.Nest = false
	}
	return instanceFolders(d, prefs, categories, rule)
}

// categoryVersion reuses an instance's version answer for categoryVersionTTL.
// An error is not kept, so the next call asks again.
func (sm *SyncManager) categoryVersion(ctx context.Context, client *Client, instanceID int) (categoryVersion, error) {
	expired := func(_ int, v categoryVersion) bool { return time.Since(v.at) >= categoryVersionTTL }
	sm.categoryVersionsMu.Lock()
	v, ok := sm.categoryVersions[instanceID]
	if ok && !expired(instanceID, v) {
		sm.categoryVersionsMu.Unlock()
		return v, nil
	}
	// An expired entry is never served, so drop them all, which also drops
	// the entries of instances that no longer ask.
	maps.DeleteFunc(sm.categoryVersions, expired)
	sm.categoryVersionsMu.Unlock()

	// A client that keeps syncing skips health checks, so after an in-place
	// upgrade from 4.6 its cached capabilities would still say "flat".
	if err := client.RefreshCapabilities(ctx); err != nil {
		return categoryVersion{}, fmt.Errorf("failed to refresh qBittorrent capabilities: %w", err)
	}
	platform, err := categoryFolderPlatform(ctx, client)
	if err != nil {
		return categoryVersion{}, err
	}
	nests := client.NestsCategorySavePaths()
	v = categoryVersion{
		rule:     CategoryPathRule{Nest: nests, FileName: client.CategoryFileNames(), Platform: platform, InheritsDownloadPath: nests},
		optional: nests && !client.SubcategoriesAlwaysEnabled(),
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

// categoryFolderPlatform returns the build platform when it changes the folder
// name of a category (qBittorrent 5.3+), and "" before that.
func categoryFolderPlatform(ctx context.Context, client *Client) (string, error) {
	if !client.CategoryFileNames() {
		return "", nil
	}
	info, err := client.GetAppInfo(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get qBittorrent build info: %w", err)
	}
	return info.BuildInfo.Platform, nil
}

func instanceFolders(d fsops.PathDialect, prefs *qbt.AppPreferences, categories map[string]qbt.Category, rule CategoryPathRule) (InstanceFolders, error) {
	// Without the default save path no inheriting category resolves.
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
		// A category can have its own download path, or one under the
		// global download path even when that is turned off.
		p, ok := CategoryDownloadPath(d, name, categories, prefs.TempPath, prefs.TempPathEnabled, rule)
		if !ok {
			continue
		}
		if p == "" {
			return InstanceFolders{}, fmt.Errorf("download path of category %q rests on the qBittorrent download path %q, which is not absolute", name, prefs.TempPath)
		}
		f.DownloadFolders = append(f.DownloadFolders, p)
	}
	// qBittorrent refuses a relative monitored folder and resolves a relative
	// custom save path under the default save path.
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
