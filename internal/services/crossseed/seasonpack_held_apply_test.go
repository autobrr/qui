// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type packApplyService struct {
	packService
	adds  *adderFake
	links []*hardlinktree.TreePlan
}

func (r *packRig) applying(svc packService) *packApplyService {
	apply := &packApplyService{packService: svc}
	apply.seasonPackLinkCreator = func(_ context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
		apply.links = append(apply.links, plan)
		return &fsops.TreeCreateResult{}, nil
	}
	apply.SetBackendPool(fsops.NewPool(r.store, local.NewBackend()))
	apply.adds = withLinkAdder(apply.Service)
	return apply
}

func (p *packApplyService) apply(t *testing.T, name, torrentData string) *SeasonPackApplyResponse {
	t.Helper()
	resp, err := p.ApplySeasonPackWebhook(t.Context(), &SeasonPackApplyRequest{TorrentName: name, TorrentData: torrentData})
	require.NoError(t, err)
	return resp
}

func (p *packApplyService) linked() []string {
	var sources []string
	for _, plan := range p.links {
		for _, file := range plan.Files {
			sources = append(sources, file.SourcePath)
		}
	}
	return sources
}

func packLibraryOnDisk(t *testing.T, root string, torrents []qbt.Torrent) *heldLibrary {
	t.Helper()
	media := filepath.Join(root, "media")
	files := map[string]qbt.TorrentFiles{}
	for i := range torrents {
		file := torrents[i].Name + ".mkv"
		torrents[i].SavePath = media
		torrents[i].ContentPath = filepath.Join(media, file)
		files[torrents[i].Hash] = qbt.TorrentFiles{{Name: file, Size: packLocalFileSize}}
	}
	return serveHeldLibrary(t, torrents, files)
}

func TestSeasonPackApplyEndsAsWithTheFullScan(t *testing.T) {
	pack := packOf(t, packShow, packShowEpisode, 1, 2, 3, 4)
	torrents := packTorrents('1', packShowEpisode, 1, 2, 3, 4)
	for _, tt := range []struct {
		name    string
		held    []qbt.Torrent
		torrent string
		set     func(*testing.T, *packRig, *packApplyService)
		reason  string
		applied bool
	}{
		{name: "applied", held: torrents, applied: true},
		{name: "invalid torrent", held: torrents, torrent: undecodableTorrent(), reason: "invalid_torrent"},
		{name: "blocked", held: torrents, reason: "blocked", set: func(t *testing.T, _ *packRig, svc *packApplyService) {
			svc.blocklistStore = blocklistWithPack(t, pack)
			svc.adds = withLinkAdder(svc.Service)
		}},
		{name: "already exists", held: torrents, reason: "already_exists", set: func(_ *testing.T, _ *packRig, svc *packApplyService) {
			svc.adds.existing = &qbt.Torrent{Hash: heldHash('9', 1)}
		}},
		{name: "coverage check failed", held: torrents, reason: "coverage_check_failed", set: func(_ *testing.T, _ *packRig, svc *packApplyService) {
			svc.syncManager = unreachableInstance{qbittorrentSync: svc.syncManager, id: 1}
		}},
		{name: "drifted", held: torrents[:2], reason: "drifted"},
		{name: "drifted with nothing held", reason: "drifted"},
		{name: "layout mismatch", held: torrents, reason: "layout_mismatch", set: func(t *testing.T, r *packRig, _ *packApplyService) {
			r.links = filepath.Join(t.TempDir(), "file")
			require.NoError(t, os.WriteFile(r.links, nil, 0o600))
			r.linkTo(1)
		}},
		{name: "link failed", held: torrents, reason: "link_failed", set: func(_ *testing.T, _ *packRig, svc *packApplyService) {
			svc.seasonPackLinkCreator = func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
				return &fsops.TreeCreateResult{}, errors.New("no space left")
			}
		}},
		{name: "skipped recheck", held: torrents[:3], reason: "skipped_recheck", set: func(_ *testing.T, r *packRig, _ *packApplyService) {
			r.settings.SkipRecheck = true
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			rig := newPackRig(t, root, packLibraryOnDisk(t, root, append(packTorrents('2', packNotHeldFile, 1), tt.held...)))
			full, held := rig.applying(rig.full()), rig.applying(rig.held())
			torrent := pack
			if tt.torrent != "" {
				torrent = tt.torrent
			}
			if tt.set != nil {
				tt.set(t, rig, full)
				tt.set(t, rig, held)
			}

			want, got := full.apply(t, packShow, torrent), held.apply(t, packShow, torrent)

			require.Equal(t, tt.applied, got.Applied, got.Message)
			require.Equal(t, tt.reason, got.Reason, got.Message)
			require.Equal(t, want, got)
			require.Equal(t, full.takeRuns(), held.takeRuns())
			require.ElementsMatch(t, full.linked(), held.linked())
			require.Equal(t, full.adds.added, held.adds.added)
			require.Equal(t, full.sonarr.seasonCalls, held.sonarr.seasonCalls)
		})
	}
}

func packAliasSources() []string {
	names := make([]string, 4)
	for i := range names {
		names[i] = fmt.Sprintf(packAliasedLocal, i+1) + ".mkv"
	}
	return names
}

func requireLinked(t *testing.T, svc *packApplyService, files []string) {
	t.Helper()
	linked := svc.linked()
	for i := range linked {
		linked[i] = filepath.Base(linked[i])
	}
	require.ElementsMatch(t, files, linked)
}

func TestSeasonPackApplyLinksWhatTheCheckCountsThroughAnAlternateTitle(t *testing.T) {
	pack := packOf(t, packAliased, packAliasedFile, 1, 2, 3, 4)
	for name, set := range map[string]func(*spyARRLookupService){
		"Sonarr up": func(s *spyARRLookupService) {
			s.seasonResult = &arr.SeasonEpisodeTotalResult{TotalEpisodes: 4, Titles: []string{"Vinterhagen", "Winter Orchard"}}
		},
		"Sonarr down": func(s *spyARRLookupService) { s.err = errors.New("Sonarr does not answer") },
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			rig := newPackRig(t, root, packLibraryOnDisk(t, root, packTorrents('4', packAliasedLocal, 1, 2, 3, 4)))
			held := rig.applying(rig.held(packAliasSource()))
			requireAlternateTitles(t, held.Service, packAliasedKey)
			set(held.sonarr)

			require.True(t, held.check(t, packAliased, pack).Ready)
			resp := held.apply(t, packAliased, pack)

			require.True(t, resp.Applied, resp.Message)
			require.Equal(t, 4, resp.MatchedEpisodes)
			requireLinked(t, held, packAliasSources())
		})
	}
}

type rebuildsBeforeAssembly struct {
	qbittorrentSync

	svc   *Service
	calls *int
}

func (r rebuildsBeforeAssembly) GetSyncOwner(ctx context.Context, instanceID int) (*syncowner.Owner, error) {
	if *r.calls++; *r.calls == 2 {
		h := r.svc.heldTitles
		h.mu.Lock()
		h.instances[instanceID].needsBuild = true
		h.mu.Unlock()
	}
	return r.qbittorrentSync.GetSyncOwner(ctx, instanceID)
}

func TestSeasonPackApplySurvivesARebuildBetweenCoverageAndAssembly(t *testing.T) {
	for _, tt := range []struct{ name, local, pack, file string }{
		{"own title", packShowEpisode, packShow, packShowEpisode},
		{"alternate title", packAliasedLocal, packAliased, packAliasedFile},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			rig := newPackRig(t, root, packLibraryOnDisk(t, root, packTorrents('4', tt.local, 1, 2, 3, 4)))
			held := rig.applying(rig.held(packAliasSource()))
			if tt.local == packAliasedLocal {
				requireAlternateTitles(t, held.Service, packAliasedKey)
			}
			calls := 0
			held.syncManager = rebuildsBeforeAssembly{held.syncManager, held.Service, &calls}

			resp := held.apply(t, tt.pack, packOf(t, tt.pack, tt.file, 1, 2, 3, 4))

			require.True(t, resp.Applied, "%s: %s", resp.Reason, resp.Message)
			require.Equal(t, 2, calls)
			require.Len(t, held.linked(), 4)
		})
	}
}

func TestSeasonPackApplyAssemblesWhileABlockedInstanceRebuilds(t *testing.T) {
	root := t.TempDir()
	rig := newPackRig(t, root,
		packLibraryOnDisk(t, root, packTorrents('4', packAliasedLocal, 1, 2, 3, 4)),
		packLibraryOnDisk(t, root, packTorrents('2', packNotHeldFile, 1)))
	held := rig.applying(rig.held(packAliasSource()))
	requireAlternateTitles(t, held.Service, packAliasedKey)
	held.sonarr.err = errors.New("Sonarr does not answer")
	pack := packOf(t, packAliased, packAliasedFile, 1, 2, 3, 4)
	torrentBytes, err := base64.StdEncoding.DecodeString(pack)
	require.NoError(t, err)
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)
	held.blocklistStore = models.NewCrossSeedBlocklistStore(rig.db)
	_, err = held.blocklistStore.Upsert(t.Context(), &models.CrossSeedBlocklistEntry{InstanceID: 2, InfoHash: meta.HashV1})
	require.NoError(t, err)
	held.adds = withLinkAdder(held.Service)
	held.heldTitles.mu.Lock()
	held.heldTitles.instances[2].needsBuild = true
	held.heldTitles.mu.Unlock()

	resp := held.apply(t, packAliased, pack)

	require.True(t, resp.Applied, "%s: %s", resp.Reason, resp.Message)
	require.Equal(t, 1, resp.InstanceID)
	requireLinked(t, held, packAliasSources())
}
