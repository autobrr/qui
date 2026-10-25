// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/services/crossseed"
)

// settleFake is qBittorrent for the settle module that the injector calls.
// Each worker poll lists the next torrent of polls; after the last one the
// torrent is gone.
type settleFake struct {
	mu      sync.Mutex
	polls   []*qbt.Torrent
	next    int
	current *qbt.Torrent
	// qbitHash, when set, is the only hash the file and piece reads accept,
	// like qBittorrent's exact-ID lookup for a hybrid torrent.
	qbitHash string
	files    qbt.TorrentFiles
	pieces   []qbt.PieceState
	// The first pieceFailures piece state reads fail.
	pieceFailures   int
	pieceReads      int
	recheckFailures int
	// recheckCtxErr is the context error the first recheck call saw.
	recheckCtxErr error
	actions       []string
}

func (f *settleFake) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(filter.Hashes) > 0 {
		f.current = nil
		if f.next < len(f.polls) {
			f.current = f.polls[f.next]
			f.next++
		}
	}
	if f.current == nil {
		return nil, nil
	}
	return []qbt.Torrent{*f.current}, nil
}

func (f *settleFake) BulkAction(ctx context.Context, _ int, hashes []string, action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if action == "recheck" && len(f.actions) == 0 {
		f.recheckCtxErr = ctx.Err()
	}
	for _, hash := range hashes {
		f.actions = append(f.actions, action+":"+hash)
	}
	if action == "recheck" && f.recheckFailures > 0 {
		f.recheckFailures--
		return errors.New("instance temporarily unreachable")
	}
	return nil
}

func (f *settleFake) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.qbitHash != "" && hashes[0] != f.qbitHash {
		return nil, errors.New("torrent not found")
	}
	return map[string]qbt.TorrentFiles{strings.ToLower(hashes[0]): f.files}, nil
}

func (f *settleFake) GetTorrentPieceStates(_ context.Context, _ int, hash string) ([]qbt.PieceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.qbitHash != "" && hash != f.qbitHash {
		return nil, errors.New("torrent not found")
	}
	f.pieceReads++
	if f.pieceFailures > 0 {
		f.pieceFailures--
		return nil, errors.New("piece states unavailable")
	}
	return f.pieces, nil
}

// calls returns the actions the settle module sent, without the hashes.
func (f *settleFake) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, len(f.actions))
	for i, action := range f.actions {
		names[i], _, _ = strings.Cut(action, ":")
	}
	return names
}

// hashedCalls returns the actions the settle module sent, as "action:hash".
func (f *settleFake) hashedCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.actions)
}

func (f *settleFake) reads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pieceReads
}

// idle returns a settle module with no worker, for a test that does not
// watch the settle.
func (f *settleFake) idle() *crossseed.Settler {
	return crossseed.NewSettler(f, nil)
}

// start returns a running settle module. Call it inside a synctest bubble.
func (f *settleFake) start(t *testing.T) *crossseed.Settler {
	t.Helper()
	settler := crossseed.NewSettler(f, nil)
	settler.Start(t.Context())
	return settler
}
