// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/jackett"
)

func TestApplyTorrentSearchResultsFindsSelectionByHiddenGUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cached         TorrentSearchResult
		torznabPath    string
		cachedFor      string
		noTorznab      bool
		wantErr        error
		wantError      string
		wantRetryTime  bool
		wantDownloaded []string
	}{
		{
			name: "torznab result",
			cached: TorrentSearchResult{
				Indexer:   "Indexer",
				IndexerID: 7,
				Title:     "Show.S01.1080p.WEB-DL-GRP",
				GUID:      "https://indexer.example.invalid/t/1?key=made-up-key",
				Size:      100,
			},
			torznabPath:    "dl/1?passkey=made-up-passkey",
			wantError:      "download torrent: indexer Indexer 7 grab rate-limited until ",
			wantRetryTime:  true,
			wantDownloaded: []string{"dl/1?passkey=made-up-passkey"},
		},
		{
			name: "torznab result without a Torznab client",
			cached: TorrentSearchResult{
				Indexer:   "Indexer",
				IndexerID: 7,
				Title:     "Show.S01.1080p.WEB-DL-GRP",
				Size:      100,
			},
			torznabPath: "dl/1?passkey=made-up-passkey",
			noTorznab:   true,
			wantError:   "download torrent: torznab search is not configured",
		},
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
		{
			name: "results of another torrent on the instance",
			cached: TorrentSearchResult{
				Indexer:     "Indexer",
				IndexerID:   7,
				Title:       "Show.S01.1080p.WEB-DL-GRP",
				DownloadURL: "https://indexer.example.invalid/dl/1?passkey=made-up-passkey",
				Size:        100,
			},
			cachedFor: strings.Repeat("d", 40),
			wantErr:   ErrInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			const instanceID = 1
			sourceHash := strings.Repeat("c", 40)
			torznab := newTorznabServer(t, 7)
			torznab.refuse("dl/1", http.StatusTooManyRequests)
			if tt.torznabPath != "" {
				tt.cached.DownloadURL = torznab.url(tt.torznabPath)
			}
			svc := &Service{
				instanceStore: &duplicateFilteringInstanceStore{
					instances: map[int]*models.Instance{instanceID: {ID: instanceID, Name: "Main"}},
				},
				syncManager: &duplicateFilteringSyncManager{
					torrents: map[int][]qbt.Torrent{instanceID: {{Hash: sourceHash, Name: tt.cached.Title, Progress: 1}}},
				},
				searchResultCache: ttlcache.New[string, cachedTorrentSearchResults](),
				jackettService:    torznab.jackett,
			}
			if tt.noTorznab {
				svc.jackettService = nil
			}
			svc.cacheSearchResults(instanceID, cmp.Or(tt.cachedFor, sourceHash), []TorrentSearchResult{tt.cached})

			resp, err := svc.ApplyTorrentSearchResults(t.Context(), instanceID, mustParseHash(t, sourceHash), &ApplyTorrentSearchRequest{
				Selections: []TorrentSearchSelection{{
					IndexerID: tt.cached.IndexerID,
					Title:     tt.cached.Title,
					GUID:      jackett.HiddenGUID(tt.cached.IndexerID, tt.cached.GUID, tt.cached.DownloadURL),
				}},
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Empty(t, torznab.downloads())
				return
			}
			require.NoError(t, err)
			require.Len(t, resp.Results, 1)
			if tt.wantRetryTime {
				retryTime, found := strings.CutPrefix(resp.Results[0].Error, tt.wantError)
				require.True(t, found, resp.Results[0].Error)
				_, err := time.Parse(time.RFC3339, retryTime)
				require.NoError(t, err)
			} else {
				require.Equal(t, tt.wantError, resp.Results[0].Error)
			}
			require.Equal(t, tt.wantDownloaded, torznab.downloads())
		})
	}
}
