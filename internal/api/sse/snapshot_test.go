// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sse

import (
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// Each page row points into the full filtered library returned by qBittorrent.
func snapshotPage(librarySize, pageSize int, cross bool) *qbittorrent.TorrentResponse {
	library := make([]qbt.Torrent, librarySize)
	for i := range library {
		library[i] = qbt.Torrent{Hash: fmt.Sprintf("%040x", i), Name: "Synthetic torrent", State: qbt.TorrentStateStalledUp, Size: 1 << 30}
	}
	response := &qbittorrent.TorrentResponse{Total: librarySize}
	if cross {
		all := make([]qbittorrent.CrossInstanceTorrentView, librarySize)
		views := make([]qbittorrent.TorrentView, librarySize)
		for i := range library {
			views[i].Torrent = &library[i]
			all[i] = qbittorrent.CrossInstanceTorrentView{TorrentView: &views[i], InstanceID: 1}
		}
		response.CrossInstanceTorrents = all[:pageSize]
	} else {
		response.Torrents = make([]qbittorrent.TorrentView, pageSize)
		for i := range response.Torrents {
			response.Torrents[i].Torrent = &library[i]
		}
	}
	return response
}

func TestBaselineOwnsPageRows(t *testing.T) {
	for _, cross := range []bool{false, true} {
		for _, init := range []bool{false, true} {
			t.Run(fmt.Sprintf("cross=%t/init=%t", cross, init), func(t *testing.T) {
				response := snapshotPage(1000, 1, cross)
				opts := StreamOptions{InstanceID: 1}
				if cross {
					opts = StreamOptions{InstanceIDs: []int{1}}
				}
				group := &subscriptionGroup{}
				if init {
					group.buildInitPayload(opts, response, &StreamMeta{})
				} else {
					group.buildUpdatePayload(opts, response, &StreamMeta{})
				}
				retained := group.buildInitPayload(opts, nil, &StreamMeta{}).Data
				require.Equal(t, response, retained)
				if cross {
					require.NotSame(t, &response.CrossInstanceTorrents[0], &retained.CrossInstanceTorrents[0])
					require.NotSame(t, response.CrossInstanceTorrents[0].TorrentView, retained.CrossInstanceTorrents[0].TorrentView)
					require.NotSame(t, response.CrossInstanceTorrents[0].Torrent, retained.CrossInstanceTorrents[0].Torrent)
				} else {
					require.NotSame(t, response.Torrents[0].Torrent, retained.Torrents[0].Torrent)
				}
			})
		}
	}
}

func BenchmarkBuildUpdatePayload(b *testing.B) {
	for _, cross := range []bool{false, true} {
		for _, pageSize := range []int{1, 300, 2000} {
			for _, changed := range []int{0, min(10, pageSize)} {
				b.Run(fmt.Sprintf("cross=%t/rows=%d/changed=%d", cross, pageSize, changed), func(b *testing.B) {
					response := snapshotPage(20000, pageSize, cross)
					opts := StreamOptions{InstanceID: 1}
					if cross {
						opts = StreamOptions{InstanceIDs: []int{1}}
					}
					group := &subscriptionGroup{}
					group.buildInitPayload(opts, response, &StreamMeta{})
					b.ReportAllocs()
					for b.Loop() {
						for i := range changed {
							if cross {
								response.CrossInstanceTorrents[i].UploadedSession++
							} else {
								response.Torrents[i].UploadedSession++
							}
						}
						group.buildUpdatePayload(opts, response, &StreamMeta{})
					}
				})
			}
		}
	}
}
