// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

func TestParseStreamRequests_GroupCopies(t *testing.T) {
	parse := func(entry map[string]any) ([]streamRequest, error) {
		raw, err := json.Marshal([]map[string]any{entry})
		require.NoError(t, err)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream?streams="+url.QueryEscape(string(raw)), nil)
		return parseStreamRequests(req)
	}

	grouped, err := parse(map[string]any{"instanceId": 1, "groupCopies": true})
	require.NoError(t, err)
	require.True(t, grouped[0].options.View.CopyGroups)

	flat, err := parse(map[string]any{"instanceId": 1})
	require.NoError(t, err)
	require.False(t, flat[0].options.View.CopyGroups)
	require.NotEqual(t, streamOptionsKey(flat[0].options), streamOptionsKey(grouped[0].options),
		"a grouped stream must not share a payload with a flat stream")

	cross, err := parse(map[string]any{"instanceIds": []int{1, 2}, "groupCopies": true})
	require.NoError(t, err)
	require.True(t, cross[0].options.View.CopyGroups)

	_, err = parse(map[string]any{"instanceId": 1, "groupCopies": true, "summary": true})
	require.Error(t, err, "a summary has no rows to group")
}

// A Copy group change with no torrent change sends the row again in each view.
func TestCopyGroupChangeResendsRow(t *testing.T) {
	groups := map[string]*qbittorrent.CopyGroupRow{
		"plain":                nil,
		"member":               {ID: "00000000000000aa"},
		"other group":          {ID: "00000000000000bb"},
		"representative":       {ID: "00000000000000aa", FirstSorted: &qbittorrent.CopyGroupMember{InstanceID: 1, Hash: "a"}},
		"other first hash":     {ID: "00000000000000aa", FirstSorted: &qbittorrent.CopyGroupMember{InstanceID: 1, Hash: "b"}},
		"other first instance": {ID: "00000000000000aa", FirstSorted: &qbittorrent.CopyGroupMember{InstanceID: 2, Hash: "a"}},
	}
	row := func(group *qbittorrent.CopyGroupRow) qbittorrent.TorrentView {
		return qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "a", Name: "Alpha"}, CopyGroup: group}
	}
	fingerprints := map[string]func(*qbittorrent.CopyGroupRow) uint64{
		"single": func(g *qbittorrent.CopyGroupRow) uint64 { return singleRowFingerprint(new(fpBuf), row(g)) },
		"one":    func(g *qbittorrent.CopyGroupRow) uint64 { return oneRowFingerprint(new(fpBuf), row(g)) },
		"cross": func(g *qbittorrent.CopyGroupRow) uint64 {
			tv := row(g)
			return crossRowFingerprint(new(fpBuf), qbittorrent.CrossInstanceTorrentView{TorrentView: &tv, InstanceID: 1})
		},
	}
	for name, fp := range fingerprints {
		t.Run(name, func(t *testing.T) {
			seen := map[uint64]string{}
			for groupName, group := range groups {
				sum := fp(group)
				require.NotContains(t, seen, sum, "%s and %s must not share a fingerprint", groupName, seen[sum])
				seen[sum] = groupName
			}
		})
	}
}

func copyGroupRows(ready bool) *qbittorrent.TorrentResponse {
	a := qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "a", Name: "Show.S01.1080p.WEB-DL-GRP", AddedOn: 1}}
	b := qbittorrent.TorrentView{Torrent: &qbt.Torrent{Hash: "b", Name: "Show.S01.1080p.WEB-DL-GRP", AddedOn: 2}}
	if ready {
		a.CopyGroup = &qbittorrent.CopyGroupRow{ID: "00000000000000aa", FirstSorted: &qbittorrent.CopyGroupMember{InstanceID: 1, Hash: "b"}}
		b.CopyGroup = &qbittorrent.CopyGroupRow{ID: "00000000000000aa"}
	}
	return &qbittorrent.TorrentResponse{Torrents: []qbittorrent.TorrentView{a, b}, Total: 2, CopyGroupsReady: new(ready)}
}

func flatRows() *qbittorrent.TorrentResponse {
	resp := copyGroupRows(false)
	resp.CopyGroupsReady = nil
	return resp
}

func groupedStreamPayload(instanceID int, key string) []map[string]any {
	payload := streamPayload(instanceID, key)
	payload[0]["groupCopies"] = true
	return payload
}

func TestFlatAndGroupedSubscribersGetTheirOwnPayload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{torrentsResponse: flatRows(), groupedResponse: copyGroupRows(true)}
		manager := newIdleTestManager(t, provider)

		payload := append(streamPayload(1, "flat"), groupedStreamPayload(1, "grouped")...)
		reader := connectPipeStream(t, manager, payload)
		inits := map[string]*StreamPayload{}
		for range 2 {
			init := decodeStreamPayloadData(t, reader.waitForEvent(t, streamEventInit, time.Second).data)
			inits[init.Meta.StreamKey] = init
		}

		flat := inits["flat"].Data
		require.Nil(t, flat.CopyGroupsReady)
		for _, row := range flat.Torrents {
			require.Nil(t, row.CopyGroup)
		}

		grouped := inits["grouped"].Data
		require.NotNil(t, grouped.CopyGroupsReady)
		require.True(t, *grouped.CopyGroupsReady)
		require.Len(t, grouped.Torrents, 2)
		require.Equal(t, "00000000000000aa", grouped.Torrents[0].CopyGroup.ID)
		require.Equal(t, "b", grouped.Torrents[0].CopyGroup.FirstSorted.Hash)
	})
}

// The browser sends no list request while a grouped stream works, so the
// stream keeps the job, which stops 5 minutes after the last demand, running.
func TestOpenGroupedStreamKeepsDemandingCopyGroups(t *testing.T) {
	for name, tt := range map[string]struct {
		payload []map[string]any
		demands bool
	}{
		"flat":    {payload: streamPayload(1, "flat")},
		"grouped": {payload: groupedStreamPayload(1, "grouped"), demands: true},
		"unified": {payload: []map[string]any{{"key": "unified", "instanceIds": []int{1, 2}, "groupCopies": true}}, demands: true},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				provider := &fakeSyncProvider{
					torrentsResponse:      flatRows(),
					groupedResponse:       copyGroupRows(true),
					crossInstanceResponse: &qbittorrent.TorrentResponse{},
				}
				manager := newIdleTestManager(t, provider)
				reader := connectPipeStream(t, manager, tt.payload)
				reader.waitForEvent(t, streamEventInit, time.Second)
				openedAt := time.Now()

				reader.requireNoTick(t, 10*time.Minute)

				demands := provider.demandTimes()
				if !tt.demands {
					require.Empty(t, demands, "a flat stream must not wake the job")
					return
				}
				require.NotEmpty(t, demands)
				last := openedAt
				for _, at := range demands {
					require.LessOrEqual(t, at.Sub(last), 2*heartbeatInterval, "the stream left a gap between demands")
					last = at
				}
				require.LessOrEqual(t, time.Since(last), 2*heartbeatInterval)
			})
		})
	}
}

// The setter wakes the stream when no torrent changed: the frame carries the
// ready field and the rows that joined a group.
func TestStreamFrameAfterCopyGroupsSetter(t *testing.T) {
	// Its caches start goroutines that do not stop, so it stays out of the bubble.
	sm := qbittorrent.NewSyncManager(nil, nil)
	synctest.Test(t, func(t *testing.T) {
		provider := &fakeSyncProvider{torrentsResponse: flatRows(), groupedResponse: copyGroupRows(false)}
		manager := newIdleTestManager(t, provider)
		sm.SetSyncEventSink(manager)

		reader := connectPipeStream(t, manager, groupedStreamPayload(1, "grouped"))
		init := decodeStreamPayloadData(t, reader.waitForEvent(t, streamEventInit, time.Second).data)
		require.False(t, *init.Data.CopyGroupsReady)
		synctest.Wait()

		provider.mu.Lock()
		provider.groupedResponse = copyGroupRows(true)
		provider.mu.Unlock()
		sm.SetCopyGroups(qbittorrent.NewCopyGroups(map[int]map[string]uint64{1: {"a": 0xaa, "b": 0xaa}}, []int{1}))

		frame := decodeStreamPayloadData(t, reader.waitForTick(t, time.Second).data)
		require.Equal(t, streamEventDelta, frame.Type)
		require.True(t, *frame.Data.CopyGroupsReady)
		require.Equal(t, []string{"a", "b"}, changedHashes(frame))
	})
}
