package handlers

import (
	"context"
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

func TestGetQBittorrentAppInfoIncludesProcessInfoForSupportedWebAPI(t *testing.T) {
	client := &stubQBittorrentAppInfoClient{
		version:       "v5.2.0",
		webAPIVersion: "2.15.1",
		buildInfo: qbt.BuildInfo{
			Libtorrent: "2.0.11",
			Platform:   "linux",
		},
		processInfo: qbt.ProcessInfo{LaunchTime: 1769331513},
	}

	info, err := getQBittorrentAppInfo(context.Background(), client)

	require.NoError(t, err)
	require.True(t, client.processInfoCalled)
	require.NotNil(t, info.ProcessInfo)
	require.Equal(t, int64(1769331513), info.ProcessInfo.LaunchTime)
}

func TestGetQBittorrentAppInfoSkipsProcessInfoForOlderWebAPI(t *testing.T) {
	client := &stubQBittorrentAppInfoClient{
		version:       "v5.1.4",
		webAPIVersion: "2.11.4",
		buildInfo: qbt.BuildInfo{
			Libtorrent: "2.0.11",
			Platform:   "linux",
		},
		processInfo: qbt.ProcessInfo{LaunchTime: 1769331513},
	}

	info, err := getQBittorrentAppInfo(context.Background(), client)

	require.NoError(t, err)
	require.False(t, client.processInfoCalled)
	require.Nil(t, info.ProcessInfo)
}
