// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/releases"
)

const (
	packShow          = "Amber.Quay.S01.1080p.WEB.x264-GRP9"
	packShowEpisode   = "Amber.Quay.S01E%02d.1080p.WEB.x264-GRP9"
	packAliased       = "Vinterhagen.S01.1080p.WEB.x264-GRP9"
	packAliasedFile   = "Vinterhagen.S01E%02d.1080p.WEB.x264-GRP9"
	packAliasedLocal  = "Winter.Orchard.S01E%02d.1080p.WEB.x264-GRP9"
	packNumeral       = "Silver.Meadow.S02.1080p.WEB.x264-GRP9"
	packNumeralFile   = "Silver.Meadow.-.%02d.1080p.WEB.x264-GRP9"
	packNumeralLocal  = "Silver.Meadow.II.-.%02d.1080p.WEB.x264-GRP9"
	packNotHeld       = "Nobody.Home.S01.1080p.WEB.x264-GRP9"
	packNotHeldFile   = "Nobody.Home.S01E%02d.1080p.WEB.x264-GRP9"
	packAliasedKey    = "vinterhagen"
	packLocalFileSize = int64(1) << 30
)

type packRig struct {
	*heldRig
	links string
}

type packService struct {
	*Service
	runs   *stubSeasonPackRunStore
	sonarr *spyARRLookupService
}

func newPackRig(t *testing.T, root string, libraries ...*heldLibrary) *packRig {
	t.Helper()
	rig := &packRig{heldRig: newHeldRig(t, libraries...), links: filepath.Join(root, "links")}
	for id := range rig.instances {
		rig.linkTo(id)
	}
	rig.settings = models.CrossSeedAutomationSettings{SeasonPackEnabled: true, SeasonPackCoverageThreshold: 0.75}
	rig.startNoBackgroundSyncs()
	return rig
}

func (r *packRig) linkTo(instanceID int) {
	r.t.Helper()
	instance := r.instances[instanceID]
	on := true
	_, err := r.store.Update(r.t.Context(), instanceID, instance.Name, instance.Host, instance.Username, "pass", nil, nil, &models.InstanceUpdateParams{
		HasLocalFilesystemAccess: &on, UseHardlinks: &on, HardlinkBaseDir: &r.links,
	})
	require.NoError(r.t, err)
}

func (r *packRig) full() packService {
	svc, sonarr := r.fullScan()
	return newPackService(svc, sonarr)
}

func (r *packRig) held(sources ...AlternateTitleSource) packService {
	r.t.Helper()
	svc, sonarr := r.heldTitles(r.t.Context(), sources...)
	r.waitReady(svc)
	return newPackService(svc, sonarr)
}

func newPackService(svc *Service, sonarr *spyARRLookupService) packService {
	runs := &stubSeasonPackRunStore{}
	svc.seasonPackRunStore = runs
	return packService{Service: svc, runs: runs, sonarr: sonarr}
}

func (p packService) check(t *testing.T, name, torrentData string, instanceIDs ...int) *SeasonPackCheckResponse {
	t.Helper()
	resp, err := p.CheckSeasonPackWebhook(t.Context(), &SeasonPackCheckRequest{TorrentName: name, TorrentData: torrentData, InstanceIDs: instanceIDs})
	require.NoError(t, err)
	return resp
}

func (p packService) takeRuns() []*models.SeasonPackRun {
	runs := p.runs.runs
	p.runs.runs = nil
	return runs
}

func packTorrents(prefix byte, pattern string, episodes ...int) []qbt.Torrent {
	torrents := make([]qbt.Torrent, len(episodes))
	for i, episode := range episodes {
		torrents[i] = qbt.Torrent{
			Hash: heldHash(prefix, episode), Name: fmt.Sprintf(pattern, episode),
			Size: packLocalFileSize, TotalSize: packLocalFileSize, Progress: 1, AddedOn: 1,
		}
	}
	return torrents
}

func packOf(t *testing.T, name, filePattern string, episodes ...int) string {
	t.Helper()
	files := make([]string, len(episodes))
	for i, episode := range episodes {
		files[i] = fmt.Sprintf(filePattern, episode) + ".mkv"
	}
	return buildSeasonPackTorrentB64(t, name, files)
}

func packAliasSource() *fakeAlternateTitles {
	return &fakeAlternateTitles{titles: map[string][]string{
		"Winter Orchard": {"Vinterhagen"},
		"Ivory Cove II":  {"Ivory Cove"},
		"Golden Reef II": {"Golden Reef"},
	}, cached: true}
}

func undecodableTorrent() string {
	return base64.StdEncoding.EncodeToString([]byte("no torrent"))
}

func requireCaughtUp(t *testing.T, svc *Service) {
	t.Helper()
	requireCaughtUpOn(t, svc, string(releases.ContentTypeTV))
}

func requireCaughtUpOn(t *testing.T, svc *Service, contentType string) {
	t.Helper()
	require.Eventually(t, func() bool { return svc.heldTitles.alternates.caughtUp(contentType) }, 30*time.Second, 5*time.Millisecond)
}
