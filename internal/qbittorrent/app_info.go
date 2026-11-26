// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

const appInfoCacheTTL = 5 * time.Minute
const appInfoRequestTimeout = 10 * time.Second

type AppBuildInfo struct {
	Qt         string `json:"qt"`
	Libtorrent string `json:"libtorrent"`
	Boost      string `json:"boost"`
	OpenSSL    string `json:"openssl"`
	Zlib       string `json:"zlib"`
	Bitness    int    `json:"bitness"`
	Platform   string `json:"platform,omitempty"`
}

type AppProcessInfo struct {
	LaunchTime int64 `json:"launchTime"`
}

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

func buildProcessInfo(supported bool, fetch func() (qbt.ProcessInfo, error)) *AppProcessInfo {
	if !supported {
		return nil
	}

	processInfo, err := fetch()
	if err != nil {
		log.Debug().Err(err).Msg("Failed to get qBittorrent process info")
		return nil
	}

	return &AppProcessInfo{LaunchTime: processInfo.LaunchTime}
}

func (c *Client) GetAppInfo(ctx context.Context) (*AppInfo, error) {
	if c == nil || c.Client == nil {
		return nil, errors.New("qbittorrent client unavailable")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	cached, fresh := c.cachedAppInfo()
	if fresh {
		return cloneAppInfo(cached), nil
	}

	result, err, _ := c.appInfoGroup.Do("refresh", func() (any, error) {
		if info, fresh := c.cachedAppInfo(); fresh {
			return info, nil
		}
		return c.refreshAppInfo(context.WithoutCancel(ctx))
	})
	if err != nil {
		if cached != nil {
			log.Debug().
				Err(err).
				Int("instanceID", c.instanceID).
				Msg("Serving stale qBittorrent app info after refresh failure")
			return cloneAppInfo(cached), nil
		}
		return nil, err
	}

	cached, _ = result.(*AppInfo)
	return cloneAppInfo(cached), nil
}

func (c *Client) cachedAppInfo() (*AppInfo, bool) {
	c.appInfoMu.RLock()
	defer c.appInfoMu.RUnlock()
	return c.appInfoCache, c.appInfoCache != nil && time.Since(c.appInfoFetchedAt) < appInfoCacheTTL
}

func (c *Client) refreshAppInfo(ctx context.Context) (*AppInfo, error) {
	requestCtx, cancel := context.WithTimeout(ctx, appInfoRequestTimeout)
	defer cancel()

	version, err := c.GetAppVersionCtx(requestCtx)
	if err != nil {
		return nil, fmt.Errorf("get app version: %w", err)
	}

	if err := c.RefreshCapabilities(requestCtx); err != nil {
		return nil, fmt.Errorf("get web API version: %w", err)
	}

	buildInfo, err := c.GetBuildInfoCtx(requestCtx)
	if err != nil {
		return nil, fmt.Errorf("get build info: %w", err)
	}

	info := &AppInfo{
		Version:       strings.TrimSpace(version),
		WebAPIVersion: c.GetWebAPIVersion(),
		BuildInfo: &AppBuildInfo{
			Qt:         buildInfo.Qt,
			Libtorrent: buildInfo.Libtorrent,
			Boost:      buildInfo.Boost,
			OpenSSL:    buildInfo.Openssl,
			Zlib:       buildInfo.Zlib,
			Bitness:    buildInfo.Bitness,
			Platform:   buildInfo.Platform,
		},
		ProcessInfo: buildProcessInfo(c.Supports(CapProcessInfo), func() (qbt.ProcessInfo, error) {
			return c.GetProcessInfoCtx(requestCtx)
		}),
	}

	c.appInfoMu.Lock()
	c.appInfoCache = info
	c.appInfoFetchedAt = time.Now()
	c.appInfoMu.Unlock()

	return info, nil
}
