// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// The transfer-info contract is consumed by the dashboard, the title bar and by
// external clients that poll it instead of requesting a full torrent list for the
// all-time totals. Every key below is load-bearing for one of them.
func TestTransferInfoResponseFromServerState(t *testing.T) {
	state := &qbt.ServerState{
		AlltimeDl:        111,
		AlltimeUl:        222,
		ConnectionStatus: "connected",
		DhtNodes:         333,
		DlInfoData:       444,
		DlInfoSpeed:      555,
		DlRateLimit:      666,
		UpInfoData:       777,
		UpInfoSpeed:      888,
		UpRateLimit:      999,
		FreeSpaceOnDisk:  1234,
	}

	payload, err := json.Marshal(newTransferInfoResponse(state))
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))

	require.Equal(t, map[string]any{
		"alltime_dl":        float64(111),
		"alltime_ul":        float64(222),
		"connection_status": "connected",
		"dht_nodes":         float64(333),
		"dl_info_data":      float64(444),
		"dl_info_speed":     float64(555),
		"dl_rate_limit":     float64(666),
		"up_info_data":      float64(777),
		"up_info_speed":     float64(888),
		"up_rate_limit":     float64(999),
	}, decoded, "server state must project onto the transfer-info keys and nothing else")
}

// qBittorrent's /transfer/info carries no all-time totals. Emitting zeros there
// would overwrite a client's last known totals with 0, so the keys must be absent
// and let the client keep the value it already has.
func TestTransferInfoResponseFallbackOmitsAlltimeTotals(t *testing.T) {
	payload, err := json.Marshal(transferInfoResponse{
		ConnectionStatus: "connected", DlInfoSpeed: 42,
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))

	require.NotContains(t, decoded, "alltime_dl")
	require.NotContains(t, decoded, "alltime_ul")
	require.InDelta(t, float64(42), decoded["dl_info_speed"], 0)
}
