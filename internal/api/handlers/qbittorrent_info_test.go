package handlers

import (
	"context"
	"errors"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

type stubQBittorrentAppInfoClient struct {
	version            string
	webAPIVersion      string
	buildInfo          qbt.BuildInfo
	processInfo        qbt.ProcessInfo
	processInfoCalled  bool
	processInfoCallErr error
}

func (c *stubQBittorrentAppInfoClient) GetAppVersionCtx(context.Context) (string, error) {
	return c.version, nil
}

func (c *stubQBittorrentAppInfoClient) GetWebAPIVersionCtx(context.Context) (string, error) {
	return c.webAPIVersion, nil
}

func (c *stubQBittorrentAppInfoClient) GetBuildInfoCtx(context.Context) (qbt.BuildInfo, error) {
	return c.buildInfo, nil
}

func (c *stubQBittorrentAppInfoClient) GetProcessInfoCtx(context.Context) (qbt.ProcessInfo, error) {
	c.processInfoCalled = true
	return c.processInfo, c.processInfoCallErr
}

func TestGetQBittorrentAppInfoProcessInfo(t *testing.T) {
	processInfoErr := errors.New("process info unavailable")

	tests := []struct {
		name                  string
		version               string
		webAPIVersion         string
		processInfo           qbt.ProcessInfo
		processInfoErr        error
		wantProcessInfoCalled bool
		wantLaunchTime        int64
		wantProcessInfo       bool
	}{
		{
			name:                  "includes process info for supported web api",
			version:               "v5.2.0",
			webAPIVersion:         "2.15.1",
			processInfo:           qbt.ProcessInfo{LaunchTime: 1769331513},
			wantProcessInfoCalled: true,
			wantLaunchTime:        1769331513,
			wantProcessInfo:       true,
		},
		{
			name:                  "skips process info for older web api",
			version:               "v5.1.4",
			webAPIVersion:         "2.11.4",
			processInfo:           qbt.ProcessInfo{LaunchTime: 1769331513},
			wantProcessInfoCalled: false,
			wantProcessInfo:       false,
		},
		{
			name:                  "omits process info when supported call fails",
			version:               "v5.2.0",
			webAPIVersion:         "2.15.1",
			processInfoErr:        processInfoErr,
			wantProcessInfoCalled: true,
			wantProcessInfo:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubQBittorrentAppInfoClient{
				version:       tt.version,
				webAPIVersion: tt.webAPIVersion,
				buildInfo: qbt.BuildInfo{
					Libtorrent: "2.0.11",
					Platform:   "linux",
				},
				processInfo:        tt.processInfo,
				processInfoCallErr: tt.processInfoErr,
			}

			info, err := getQBittorrentAppInfo(context.Background(), client)

			require.NoError(t, err)
			require.Equal(t, tt.wantProcessInfoCalled, client.processInfoCalled)
			if !tt.wantProcessInfo {
				require.Nil(t, info.ProcessInfo)
				return
			}

			require.NotNil(t, info.ProcessInfo)
			require.Equal(t, tt.wantLaunchTime, info.ProcessInfo.LaunchTime)
		})
	}
}
