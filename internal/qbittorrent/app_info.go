// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const appInfoCacheTTL = 5 * time.Minute
const appInfoRequestTimeout = 10 * time.Second

// AppBuildInfo represents the qBittorrent build information reported by the API.
type AppBuildInfo struct {
	Qt         string `json:"qt"`
	Libtorrent string `json:"libtorrent"`
	Boost      string `json:"boost"`
	OpenSSL    string `json:"openssl"`
	Zlib       string `json:"zlib"`
	Bitness    int    `json:"bitness"`
	Platform   string `json:"platform,omitempty"`
}

// AppInfo captures the qBittorrent application metadata exposed via the API.
type AppInfo struct {
	Version       string        `json:"version"`
	WebAPIVersion string        `json:"webAPIVersion,omitempty"`
	BuildInfo     *AppBuildInfo `json:"buildInfo,omitempty"`
}

func cloneAppInfo(info *AppInfo) *AppInfo {
	if info == nil {
		return nil
	}

	clone := *info
	if info.BuildInfo != nil {
		buildClone := *info.BuildInfo
		clone.BuildInfo = &buildClone
	}
	return &clone
}

// GetAppInfo returns cached qBittorrent app information, refreshing it when stale.
func (c *Client) GetAppInfo(ctx context.Context) (*AppInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	c.appInfoMu.RLock()
	if c.appInfoCache != nil && time.Since(c.appInfoFetchedAt) < appInfoCacheTTL {
		cached := cloneAppInfo(c.appInfoCache)
		c.appInfoMu.RUnlock()
		return cached, nil
	}
	c.appInfoMu.RUnlock()

	return c.refreshAppInfo(ctx)
}

func (c *Client) refreshAppInfo(ctx context.Context) (*AppInfo, error) {
	requestCtx, cancel := context.WithTimeout(ctx, appInfoRequestTimeout)
	defer cancel()

	version, err := c.GetAppVersionCtx(requestCtx)
	if err != nil {
		return nil, fmt.Errorf("get app version: %w", err)
	}

	webAPIVersion, err := c.GetWebAPIVersionCtx(requestCtx)
	if err != nil {
		return nil, fmt.Errorf("get web API version: %w", err)
	}

	webAPIVersion = strings.TrimSpace(webAPIVersion)
	if webAPIVersion == "" {
		return nil, errors.New("web API version is empty")
	}

	buildInfo, err := c.GetBuildInfoCtx(requestCtx)
	if err != nil {
		return nil, fmt.Errorf("get build info: %w", err)
	}

	info := &AppInfo{
		Version:       strings.TrimSpace(version),
		WebAPIVersion: webAPIVersion,
		BuildInfo: &AppBuildInfo{
			Qt:         buildInfo.Qt,
			Libtorrent: buildInfo.Libtorrent,
			Boost:      buildInfo.Boost,
			OpenSSL:    buildInfo.Openssl,
			Zlib:       buildInfo.Zlib,
			Bitness:    buildInfo.Bitness,
			Platform:   buildInfo.Platform,
		},
	}

	c.mu.Lock()
	previousVersion := c.webAPIVersion
	c.applyCapabilitiesLocked(webAPIVersion)
	c.mu.Unlock()

	if previousVersion != webAPIVersion {
		log.Trace().
			Int("instanceID", c.instanceID).
			Str("previousWebAPIVersion", previousVersion).
			Str("webAPIVersion", webAPIVersion).
			Msg("Updated qBittorrent capabilities from app info refresh")
	}

	c.appInfoMu.Lock()
	c.appInfoCache = info
	c.appInfoFetchedAt = time.Now()
	c.appInfoMu.Unlock()

	return cloneAppInfo(info), nil
}
