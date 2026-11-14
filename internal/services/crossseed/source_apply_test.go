// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

// searchWithHistory runs one Torznab search over the indexers against a local
// server, so that Search history has an entry for each indexer under the
// returned job ID. The server also serves "/<title>.torrent" as a torrent
// named <title>, and a 404 for a title that starts with "download-fail".
func searchWithHistory(t *testing.T, indexerIDs []int) (*jackett.Service, uint64, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if title, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".torrent"); ok {
			if strings.HasPrefix(title, "download-fail") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/x-bittorrent")
			_, _ = w.Write(createTestTorrent(t, title, []string{"payload.mkv"}, 256*1024))
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel></channel></rss>`))
	}))
	t.Cleanup(server.Close)

	indexers := make([]*models.TorznabIndexer, 0, len(indexerIDs))
	for _, id := range indexerIDs {
		indexers = append(indexers, &models.TorznabIndexer{ID: id, Name: "Indexer", Enabled: true, BaseURL: server.URL, Backend: models.TorznabBackendNative, Capabilities: []string{"search"}})
	}
	service := jackett.NewService(&failingEnabledIndexerStore{indexers: indexers},
		jackett.WithMinRequestInterval(time.Millisecond),
		jackett.WithSearchHistory(100),
		jackett.WithIndexerOutcomes(100),
	)

	type searchDone struct {
		resp *jackett.SearchResponse
		err  error
	}
	done := make(chan searchDone, 1)
	require.NoError(t, service.Search(t.Context(), &jackett.TorznabSearchRequest{
		Query:      "source",
		IndexerIDs: indexerIDs,
		OnAllComplete: func(resp *jackett.SearchResponse, err error) {
			done <- searchDone{resp, err}
		},
	}))
	select {
	case result := <-done:
		require.NoError(t, result.err)
		resp := result.resp
		require.NotZero(t, resp.JobID)
		return service, resp.JobID, server.URL
	case <-time.After(10 * time.Second):
		t.Fatal("search did not complete")
		return nil, 0, ""
	}
}

// historyBadges returns the outcome of each indexer in Search history for one job.
func historyBadges(t *testing.T, service *jackett.Service, jobID uint64) map[int]string {
	t.Helper()
	history, err := service.GetSearchHistory(t.Context(), 100)
	require.NoError(t, err)
	badges := make(map[int]string)
	for _, entry := range history.Entries {
		if entry.JobID == jobID && entry.Outcome != "" {
			badges[entry.IndexerID] = entry.Outcome
		}
	}
	return badges
}

func TestApplySourceSearchResults(t *testing.T) {
	const (
		instanceID      = 1
		downloadFail404 = "torrent download failed after 4 attempts: torrent download from SERVER/download-fail.torrent returned status 404"
	)
	rescue := searchDecisionProvenance{Class: searchCandidateClassTitleRescue}
	result := func(title string, indexerID int, hash string, decision searchDecisionProvenance) TorrentSearchResult {
		return TorrentSearchResult{
			Indexer:        "Indexer" + strconv.Itoa(indexerID),
			IndexerID:      indexerID,
			Title:          title,
			InfoHashV1:     hash,
			SearchDecision: decision,
		}
	}

	tests := []struct {
		name    string
		results []TorrentSearchResult
		// cancelAfter cancels the context after the add of this title.
		cancelAfter string
		wantEntries []string
		wantCounts  sourceApplyCounts
		wantErr     error
		wantBadges  map[int]string
	}{
		{
			name: "normal adds only",
			results: []TorrentSearchResult{
				result("add-a", 1, "", searchDecisionProvenance{}),
				result("add-b", 2, "", searchDecisionProvenance{}),
			},
			wantEntries: []string{"add-a=added", "add-b=added"},
			wantCounts:  sourceApplyCounts{added: 2},
			wantBadges:  map[int]string{1: "added", 2: "added"},
		},
		{
			name: "download failure",
			results: []TorrentSearchResult{
				result("download-fail", 1, "", searchDecisionProvenance{}),
				result("add-b", 2, "", searchDecisionProvenance{}),
			},
			wantEntries: []string{"download-fail=failed", "add-b=added"},
			wantCounts: sourceApplyCounts{
				added: 1, failed: 1,
				firstErr:     errors.New("Indexer1: download failed: " + downloadFail404),
				errorSamples: []string{"download failed: " + downloadFail404},
			},
			wantBadges: map[int]string{1: "failed", 2: "added"},
		},
		{
			name: "classified failure and exists",
			results: []TorrentSearchResult{
				result("exists-a", 1, "", searchDecisionProvenance{}),
				result("reject-b", 2, "", searchDecisionProvenance{}),
			},
			wantEntries: []string{"exists-a=skipped", "reject-b=failed"},
			wantCounts: sourceApplyCounts{
				failed: 1, skipped: 1,
				firstErr:     errors.New(`Indexer2 "reject-b": size mismatch`),
				errorSamples: []string{"size mismatch"},
			},
			wantBadges: map[int]string{2: "failed"},
		},
		{
			name: "title rescue skips an indexer with a normal add",
			results: []TorrentSearchResult{
				result("add-a", 1, "", searchDecisionProvenance{}),
				result("rescue-a", 1, "hash-ra", rescue),
				result("rescue-b", 2, "hash-rb", rescue),
			},
			wantEntries: []string{"add-a=added", "rescue-b=added"},
			wantCounts:  sourceApplyCounts{added: 2},
			wantBadges:  map[int]string{1: "added", 2: "added"},
		},
		{
			name: "at most three title rescue attempts",
			results: []TorrentSearchResult{
				result("rescue-a", 1, "hash-ra", rescue),
				result("rescue-b", 2, "hash-rb", rescue),
				result("rescue-c", 3, "hash-rc", rescue),
				result("rescue-d", 4, "hash-rd", rescue),
			},
			wantEntries: []string{"rescue-a=added", "rescue-b=added", "rescue-c=added"},
			wantCounts:  sourceApplyCounts{added: 3},
			wantBadges:  map[int]string{1: "added", 2: "added", 3: "added"},
		},
		{
			name: "title rescue skips a duplicate info hash",
			results: []TorrentSearchResult{
				result("add-a", 1, "hash-a", searchDecisionProvenance{}),
				result("rescue-same-as-add", 2, "hash-a", rescue),
				result("rescue-b", 3, "hash-rb", rescue),
				result("rescue-same-as-b", 4, "hash-rb", rescue),
			},
			wantEntries: []string{"add-a=added", "rescue-b=added"},
			wantCounts:  sourceApplyCounts{added: 2},
			wantBadges:  map[int]string{1: "added", 3: "added"},
		},
		{
			name: "title rescue skips a result already in the client",
			results: []TorrentSearchResult{
				result("rescue-present", 1, "already-present", rescue),
				result("rescue-b", 2, "hash-rb", rescue),
			},
			wantEntries: []string{"rescue-b=added"},
			wantCounts:  sourceApplyCounts{added: 1},
			wantBadges:  map[int]string{2: "added"},
		},
		{
			name: "cancel in the middle keeps the adds made",
			results: []TorrentSearchResult{
				result("add-a", 1, "", searchDecisionProvenance{}),
				result("add-b", 2, "", searchDecisionProvenance{}),
				result("rescue-c", 3, "hash-rc", rescue),
			},
			cancelAfter: "add-a",
			wantEntries: []string{"add-a=added"},
			wantCounts:  sourceApplyCounts{added: 1},
			wantErr:     context.Canceled,
			wantBadges:  map[int]string{1: "added"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jackettService, jobID, serverURL := searchWithHistory(t, []int{1, 2, 3, 4})
			for i := range tt.results {
				tt.results[i].DownloadURL = serverURL + "/" + tt.results[i].Title + ".torrent"
			}
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			instance := &models.Instance{ID: instanceID, Name: "main"}
			service := &Service{
				jackettService: jackettService,
				syncManager: newFakeSyncManager(instance, []qbt.Torrent{
					{Hash: "source"},
					{Hash: "already-present"},
				}, nil),
				crossSeedInvoker: func(_ context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
					data, err := base64.StdEncoding.DecodeString(req.TorrentData)
					require.NoError(t, err)
					mi, err := metainfo.Load(bytes.NewReader(data))
					require.NoError(t, err)
					info, err := mi.UnmarshalInfo()
					require.NoError(t, err)
					title := info.Name
					var resp *CrossSeedResponse
					switch {
					case strings.HasPrefix(title, "exists"):
						resp = &CrossSeedResponse{Results: []InstanceCrossSeedResult{{Status: "exists"}}}
					case strings.HasPrefix(title, "reject"):
						resp = &CrossSeedResponse{Results: []InstanceCrossSeedResult{{Status: "error", Message: "size mismatch"}}}
					default:
						resp = &CrossSeedResponse{Success: true}
					}
					if title == tt.cancelAfter {
						cancel()
					}
					return resp, nil
				},
			}

			entries, err := service.applySourceSearchResults(ctx, instanceID, &qbt.Torrent{Hash: "source", Name: "source"},
				&TorrentSearchResponse{Results: tt.results, JobID: jobID}, triggerDefaults{}, time.Now())

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			got := make([]string, 0, len(entries))
			for _, entry := range entries {
				got = append(got, entry.result.ReleaseTitle+"="+string(entry.result.Status))
			}
			require.Equal(t, tt.wantEntries, got)

			counts := countSourceApplyEntries(entries)
			if counts.firstErr != nil {
				counts.firstErr = errors.New(strings.ReplaceAll(counts.firstErr.Error(), serverURL, "SERVER"))
			}
			for i, sample := range counts.errorSamples {
				counts.errorSamples[i] = strings.ReplaceAll(sample, serverURL, "SERVER")
			}
			if tt.wantCounts.firstErr == nil {
				require.NoError(t, counts.firstErr)
			} else {
				require.EqualError(t, counts.firstErr, tt.wantCounts.firstErr.Error())
			}
			counts.firstErr, tt.wantCounts.firstErr = nil, nil
			require.Equal(t, tt.wantCounts, counts)

			require.Equal(t, tt.wantBadges, historyBadges(t, jackettService, jobID))
		})
	}
}
