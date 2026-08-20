// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

const appInfoCacheTTL = 5 * time.Minute
const appInfoRequestTimeout = 10 * time.Second

// processInfoMinWebAPIVersion is the lowest Web API version that exposes process info (launch time).
var processInfoMinWebAPIVersion = semver.MustParse("2.15.1")

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

// AppProcessInfo represents qBittorrent process information (e.g. launch time).
type AppProcessInfo struct {
	LaunchTime int64 `json:"launchTime"`
}

// AppInfo captures the qBittorrent application metadata exposed via the API.
type AppInfo struct {
	Version       string          `json:"version"`
	WebAPIVersion string          `json:"webAPIVersion,omitempty"`
	BuildInfo     *AppBuildInfo   `json:"buildInfo,omitempty"`
	ProcessInfo   *AppProcessInfo `json:"processInfo,omitempty"`
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
	if info.ProcessInfo != nil {
		processClone := *info.ProcessInfo
		clone.ProcessInfo = &processClone
	}
	return &clone
}

// supportsProcessInfo reports whether the given Web API version exposes process info.
func supportsProcessInfo(webAPIVersion string) bool {
	version, err := semver.NewVersion(strings.TrimSpace(webAPIVersion))
	if err != nil {
		return false
	}

	return !version.LessThan(processInfoMinWebAPIVersion)
}

// buildProcessInfo fetches process info when the Web API version supports it,
// returning nil when unsupported or when the lookup fails (best-effort metadata).
func buildProcessInfo(webAPIVersion string, fetch func() (qbt.ProcessInfo, error)) *AppProcessInfo {
	if !supportsProcessInfo(webAPIVersion) {
		return nil
	}

	processInfo, err := fetch()
	if err != nil {
		log.Debug().Err(err).Str("webAPIVersion", webAPIVersion).Msg("Failed to get qBittorrent process info")
		return nil
	}

	return &AppProcessInfo{LaunchTime: processInfo.LaunchTime}
}

// GetAppInfo returns cached qBittorrent app information, refreshing it when stale.
func (c *Client) GetAppInfo(ctx context.Context) (*AppInfo, error) {
	if c == nil || c.Client == nil {
		return nil, errors.New("qbittorrent client unavailable")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	c.appInfoMu.RLock()
	cached := cloneAppInfo(c.appInfoCache)
	fresh := c.appInfoCache != nil && time.Since(c.appInfoFetchedAt) < appInfoCacheTTL
	c.appInfoMu.RUnlock()

	if fresh {
		return cached, nil
	}

	info, err := c.refreshAppInfo(ctx)
	if err != nil {
		// A saturated qBittorrent WebUI times out even trivial calls. Serve the
		// last known app info rather than failing the whole torrent stream tick;
		// only surface the error when there is nothing cached to fall back on.
		if cached != nil {
			log.Debug().
				Err(err).
				Int("instanceID", c.instanceID).
				Msg("Serving stale qBittorrent app info after refresh failure")
			return cached, nil
		}
		return nil, err
	}

	return info, nil
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
		ProcessInfo: buildProcessInfo(webAPIVersion, func() (qbt.ProcessInfo, error) {
			return c.GetProcessInfoCtx(requestCtx)
		}),
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
