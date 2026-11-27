// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
)

var errHeldUnreachable = errors.New("instance does not answer")

type unreachableInstance struct {
	qbittorrentSync

	id int
}

func (u unreachableInstance) GetSyncOwner(ctx context.Context, instanceID int) (*syncowner.Owner, error) {
	if instanceID == u.id {
		return nil, errHeldUnreachable
	}
	return u.qbittorrentSync.GetSyncOwner(ctx, instanceID)
}

func (u unreachableInstance) GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	if instanceID == u.id {
		return nil, errHeldUnreachable
	}
	return u.qbittorrentSync.GetCachedInstanceTorrents(ctx, instanceID)
}

func TestCheckLogsAnInstanceThatHeldTitlesCannotReadAndAnswersFromTheOthers(t *testing.T) {
	held := qbt.Torrent{Hash: heldHash('a', 1), Name: "Lone.Harbor.S02E03.1080p.WEB-DL.DDP5.1.H.264-GRP7", Size: heldSingleSize, TotalSize: heldSingleSize, Progress: 1, AddedOn: 1}
	rig := newHeldRig(t, serveHeldLibrary(t, []qbt.Torrent{held}, nil), serveHeldLibrary(t, []qbt.Torrent{held}, nil))
	svc, _ := rig.heldTitles(t.Context())
	rig.waitReady(svc)
	svc.syncManager = unreachableInstance{qbittorrentSync: svc.syncManager, id: 2}
	previousLogger := log.Logger
	var logs bytes.Buffer
	log.Logger = zerolog.New(&logs).Level(zerolog.WarnLevel)
	t.Cleanup(func() { log.Logger = previousLogger })

	resp := checkAnnounce(t, svc, held.Name, uint64(held.Size))

	require.True(t, resp.CanCrossSeed)
	require.Len(t, resp.Matches, 1)
	require.Equal(t, 1, resp.Matches[0].InstanceID)
	require.Equal(t, 1, strings.Count(logs.String(), "\n"), logs.String())
	require.Contains(t, logs.String(), `"instanceID":2,"message":"Failed to get torrents from instance"`)
	require.Contains(t, logs.String(), errHeldUnreachable.Error())
}
