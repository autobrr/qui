// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
)

var errStopAfterDownload = errors.New("stop after the download")

// regression: #4473
func TestApplyTorrentSearchResultsFindsSelectionByHiddenGUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cached         TorrentSearchResult
		wantError      string
		wantDownloaded []string
	}{
		// regression: #4473
		{
			name: "torznab result",
			cached: TorrentSearchResult{
				Indexer:     "Indexer",
				IndexerID:   7,
				Title:       "Show.S01.1080p.WEB-DL-GRP",
				DownloadURL: "https://indexer.example.invalid/dl/1?passkey=made-up-passkey",
				GUID:        "https://indexer.example.invalid/t/1?key=made-up-key",
				Size:        100,
			},
			wantError:      "download torrent: stop after the download",
			wantDownloaded: []string{"https://indexer.example.invalid/dl/1?passkey=made-up-passkey"},
		},
		// regression: #4473
		{
			name: "RED or OPS result",
			cached: TorrentSearchResult{
				Indexer:     "RED",
				IndexerID:   gazellemusic.Sites()[0].IndexerID,
				Title:       "Artist - Album (2020) [FLAC]",
				DownloadURL: gazellemusic.BuildDownloadURL("api.example.invalid", 42),
				Size:        100,
			},
			wantError: "download torrent: gazelle API key not configured for api.example.invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			const instanceID = 1
			sourceHash := strings.Repeat("c", 40)
			var downloaded []string
			svc := &Service{
				instanceStore: &duplicateFilteringInstanceStore{
					instances: map[int]*models.Instance{instanceID: {ID: instanceID, Name: "Main"}},
				},
				syncManager: &duplicateFilteringSyncManager{
					torrents: map[int][]qbt.Torrent{instanceID: {{Hash: sourceHash, Name: tt.cached.Title, Progress: 1}}},
				},
				searchResultCache: ttlcache.New[string, cachedTorrentSearchResults](),
				torrentDownloadFunc: func(_ context.Context, req jackett.TorrentDownloadRequest) ([]byte, error) {
					downloaded = append(downloaded, req.DownloadURL)
					return nil, errStopAfterDownload
				},
			}
			svc.cacheSearchResults(instanceID, sourceHash, []TorrentSearchResult{tt.cached})

			resp, err := svc.ApplyTorrentSearchResults(t.Context(), instanceID, sourceHash, &ApplyTorrentSearchRequest{
				Selections: []TorrentSearchSelection{{
					IndexerID: tt.cached.IndexerID,
					Title:     tt.cached.Title,
					GUID:      jackett.HiddenGUID(tt.cached.IndexerID, tt.cached.GUID, tt.cached.DownloadURL),
				}},
			})
			require.NoError(t, err)
			require.Len(t, resp.Results, 1)
			require.Equal(t, tt.wantError, resp.Results[0].Error)
			require.Equal(t, tt.wantDownloaded, downloaded)
		})
	}
}
