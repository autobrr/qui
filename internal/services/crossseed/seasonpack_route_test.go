// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// seasonPackRig is the routed rig with the episodes of a season pack on the
// Source instance "lib": one torrent per episode file in downloads.
type seasonPackRig struct {
	*routedRig
	runs        *stubSeasonPackRunStore
	torrentData string
	hashes      []string
	episodes    []qbt.Torrent
}

func newSeasonPackRig(t *testing.T, crossSeedDown bool) *seasonPackRig {
	t.Helper()
	r := &seasonPackRig{routedRig: newRoutedRig(t, crossSeedDown), runs: &stubSeasonPackRunStore{}}
	settings := models.DefaultCrossSeedAutomationSettings()
	settings.SeasonPackEnabled = true
	settings.SeasonPackCoverageThreshold = 1
	r.svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) { return settings, nil }
	r.svc.seasonPackRunStore = r.runs
	r.settler.runs = r.runs

	contents := map[string][]byte{}
	for i, name := range seasonPackFixtureFiles {
		data := bytes.Repeat([]byte{byte(i + 1)}, 64)
		contents[name] = data
		hash := fmt.Sprintf("%040d", i+1)
		r.hashes = append(r.hashes, hash)
		r.sync.files[hash] = qbt.TorrentFiles{{Name: name, Size: int64(len(data))}}
		require.NoError(t, os.MkdirAll(r.downloads, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(r.downloads, name), data, 0o600))
		r.episodes = append(r.episodes, qbt.Torrent{
			Hash: hash, Name: strings.TrimSuffix(name, ".mkv"), Progress: 1, Size: int64(len(data)),
			ContentPath: filepath.Join(r.downloads, name), SavePath: r.downloads, Category: "tv",
		})
	}
	r.sync.cached = map[int][]internalqb.CrossInstanceTorrentView{r.source.ID: buildCrossInstanceViews(r.source, r.episodes)}
	r.torrentData = base64.StdEncoding.EncodeToString(buildMultiFileTorrent(t, seasonPackFixtureName, 64, contents))
	return r
}

func (r *seasonPackRig) apply(instanceIDs ...int) *SeasonPackApplyResponse {
	r.t.Helper()
	if len(instanceIDs) == 0 {
		instanceIDs = []int{r.source.ID}
	}
	resp, err := r.svc.ApplySeasonPackWebhook(r.ctx, &SeasonPackApplyRequest{
		TorrentName: seasonPackFixtureName, TorrentData: r.torrentData, InstanceIDs: instanceIDs,
	})
	require.NoError(r.t, err)
	return resp
}

// lastRun is the newest season pack history row.
func (r *seasonPackRig) lastRun() *models.SeasonPackRun {
	r.t.Helper()
	require.NotEmpty(r.t, r.runs.runs)
	return r.runs.runs[len(r.runs.runs)-1]
}

func TestRoutedSeasonPackLandsOnTheCrossSeedInstance(t *testing.T) {
	tests := []struct {
		preset       string
		wantSavePath string
		wantTree     string
	}{
		{preset: "flat", wantSavePath: routedMapped},
		{preset: "by-instance", wantSavePath: routedMapped + "/seeds", wantTree: "seeds"},
	}
	for _, tt := range tests {
		t.Run(tt.preset, func(t *testing.T) {
			r := newSeasonPackRig(t, false)
			r.updateSource(&models.InstanceUpdateParams{HardlinkDirPreset: new(tt.preset)})

			resp := r.apply()

			require.True(t, resp.Applied, resp.Reason+": "+resp.Message)
			assert.Equal(t, r.source.ID, resp.InstanceID)
			assert.Equal(t, r.crossSeed.ID, resp.AddedInstanceID)
			require.Empty(t, r.sync.adds[r.source.ID], "the Source instance never gets a routed add")
			adds := r.sync.adds[r.crossSeed.ID]
			require.Len(t, adds, 1)
			assert.Equal(t, tt.wantSavePath, adds[0]["savepath"])
			assert.Equal(t, "false", adds[0]["autoTMM"])
			category := adds[0]["category"]
			require.NotEmpty(t, category)
			assert.Equal(t, map[string]string{category: ""}, r.sync.categories[r.crossSeed.ID],
				"the category is created on the Cross-seed instance with no save path")
			pack := filepath.Join(r.links, tt.wantTree, seasonPackFixtureName)
			assert.True(t, r.linked(pack, seasonPackFixtureFiles[0]), "the tree is on the Source instance's disk")
			assert.Equal(t, []string{routedMapped + strings.TrimPrefix(filepath.ToSlash(pack), filepath.ToSlash(r.links))}, r.sync.listed)

			run := r.lastRun()
			assert.Equal(t, "applied", run.Status)
			assert.Equal(t, &r.source.ID, run.InstanceID)
			assert.Equal(t, &r.crossSeed.ID, run.AddedInstanceID)
		})
	}
}

// A user with no saved route sees no change: the add goes to the instance
// that holds the episodes, and history names that instance as the receiver.
func TestSeasonPackWithoutARouteAddsToTheSourceInstance(t *testing.T) {
	r := newSeasonPackRig(t, false)
	require.NoError(t, r.routes.Delete(t.Context(), r.source.ID))

	resp := r.apply()

	require.True(t, resp.Applied, resp.Reason+": "+resp.Message)
	require.Len(t, r.sync.adds[r.source.ID], 1)
	assert.Equal(t, r.links, r.sync.adds[r.source.ID][0]["savepath"])
	assert.Empty(t, r.sync.adds[r.crossSeed.ID])
	assert.Empty(t, r.sync.listed)
	assert.Equal(t, &r.source.ID, r.lastRun().AddedInstanceID)
	assert.Equal(t, r.source.ID, resp.AddedInstanceID)
}

// The route maps with the base folder of the tree that the cross-device retry
// built, not the first ranked folder.
func TestRoutedSeasonPackUsesTheBaseFolderOfTheRetry(t *testing.T) {
	r := newSeasonPackRig(t, false)
	first := filepath.Join(filepath.Dir(r.links), "first")
	r.updateSource(&models.InstanceUpdateParams{HardlinkBaseDir: new(first + "," + r.links)})
	r.svc.seasonPackLinkCreator = func(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
		if strings.HasPrefix(plan.RootDir, first) {
			return &fsops.TreeCreateResult{}, crossDeviceLinkError(plan.RootDir)
		}
		return local.NewBackend().HardlinkTree(ctx, plan)
	}

	resp := r.apply()

	require.True(t, resp.Applied, resp.Reason+": "+resp.Message)
	require.Len(t, r.sync.adds[r.crossSeed.ID], 1)
	assert.Equal(t, routedMapped, r.sync.adds[r.crossSeed.ID][0]["savepath"])
}

func TestRoutedSeasonPackStopsWhenTheCrossSeedInstanceCannotSeeTheData(t *testing.T) {
	r := newSeasonPackRig(t, false)
	r.saveRoute("/elsewhere")

	resp := r.apply()

	assert.False(t, resp.Applied)
	assert.Equal(t, crossSeedCannotSeeStatus, resp.Reason)
	assert.Equal(t, "Cross-seed instance cannot see the data at /elsewhere/"+seasonPackFixtureName, resp.Message)
	assert.Empty(t, r.sync.adds[r.crossSeed.ID])
	assert.Empty(t, r.sync.adds[r.source.ID])
	r.requireNoLinkTree()
	run := r.lastRun()
	assert.Equal(t, "failed", run.Status)
	assert.Equal(t, &r.crossSeed.ID, run.AddedInstanceID)
}

func TestRoutedSeasonPackSkipsADuplicate(t *testing.T) {
	tests := []struct {
		existingOn  string
		wantMessage string
	}{
		{existingOn: "seeds", wantMessage: "torrent already exists on instance %d"},
		{existingOn: "lib", wantMessage: "Torrent already exists in Source instance lib"},
	}
	for _, tt := range tests {
		t.Run(tt.existingOn, func(t *testing.T) {
			r := newSeasonPackRig(t, false)
			if tt.existingOn == "seeds" {
				r.sync.existing[r.crossSeed.ID] = true
			} else {
				r.sync.existing[r.source.ID] = true
			}

			resp := r.apply()

			assert.Equal(t, "already_exists", resp.Reason)
			wantMessage := tt.wantMessage
			if tt.existingOn == "seeds" {
				wantMessage = fmt.Sprintf(wantMessage, r.crossSeed.ID)
			}
			assert.Equal(t, wantMessage, resp.Message)
			assert.Empty(t, r.sync.adds[r.crossSeed.ID])
			assert.Empty(t, r.sync.adds[r.source.ID])
			r.requireNoLinkTree()
		})
	}
}

func TestRoutedSeasonPackAppliesOnlyTheCrossSeedInstanceBlocklist(t *testing.T) {
	for _, blockedOn := range []string{"seeds", "lib"} {
		t.Run(blockedOn, func(t *testing.T) {
			r := newSeasonPackRig(t, false)
			instanceID := r.crossSeed.ID
			if blockedOn == "lib" {
				instanceID = r.source.ID
			}
			r.blockPack(instanceID)

			resp := r.apply()

			if blockedOn == "seeds" {
				assert.Equal(t, "blocked", resp.Reason)
				assert.Equal(t, fmt.Sprintf("torrent is on the cross-seed blocklist for instance %d", r.crossSeed.ID), resp.Message)
				assert.Empty(t, r.sync.adds[r.crossSeed.ID])
				return
			}
			assert.True(t, resp.Applied, resp.Reason+": "+resp.Message)
			assert.Len(t, r.sync.adds[r.crossSeed.ID], 1)
		})
	}
}

// An unavailable Cross-seed instance of any eligible instance stops the whole
// apply. Another instance never takes the pack in its place.
func TestRoutedSeasonPackStopsWhenTheCrossSeedInstanceIsUnavailable(t *testing.T) {
	tests := []struct {
		name string
		down bool
		// other adds an instance without a route that holds the same episodes
		// and comes first, so it would win.
		other   bool
		wantMsg string
	}{
		{name: "offline", down: true, wantMsg: "Cross-seed instance unavailable: seeds: "},
		{name: "disabled in qui", wantMsg: "Cross-seed instance unavailable: seeds: qBittorrent instance is disabled"},
		{name: "an unrouted instance would win", down: true, other: true, wantMsg: "Cross-seed instance unavailable: seeds: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSeasonPackRig(t, tt.down)
			if !tt.down {
				_, err := r.instances.SetActiveState(t.Context(), r.crossSeed.ID, false)
				require.NoError(t, err)
			}
			instanceIDs := []int{r.source.ID}
			if tt.other {
				other := r.addEpisodeInstance("other")
				instanceIDs = []int{other.ID, r.source.ID}
			}

			resp := r.apply(instanceIDs...)

			assert.False(t, resp.Applied)
			assert.Equal(t, crossSeedUnavailableStatus, resp.Reason)
			assert.Contains(t, resp.Message, tt.wantMsg)
			for id, adds := range r.sync.adds {
				assert.Empty(t, adds, "instance %d got an add", id)
			}
			r.requireNoLinkTree()
		})
	}
}

// A link error keeps its status and never makes a regular add.
func TestRoutedSeasonPackLinkErrorFailsWithItsStatus(t *testing.T) {
	r := newSeasonPackRig(t, false)
	r.svc.seasonPackLinkCreator = func(context.Context, *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
		return &fsops.TreeCreateResult{}, errors.New("link refused")
	}

	resp := r.apply()

	assert.False(t, resp.Applied)
	assert.Equal(t, "link_failed", resp.Reason)
	assert.Contains(t, resp.Message, "link refused")
	assert.Empty(t, r.sync.adds[r.crossSeed.ID])
	assert.Empty(t, r.sync.adds[r.source.ID])
	assert.Equal(t, &r.crossSeed.ID, r.lastRun().AddedInstanceID)
}

// The Cross-seed instance has no route, so a match on it stays on it. Its
// add still checks the hashes on its Source instance.
func TestRoutedSeasonPackMatchOnTheCrossSeedInstanceStaysThere(t *testing.T) {
	for _, hashOnSource := range []bool{false, true} {
		t.Run(fmt.Sprintf("hash on the Source instance %v", hashOnSource), func(t *testing.T) {
			r := newSeasonPackRig(t, false)
			_, err := r.instances.Update(t.Context(), r.crossSeed.ID, r.crossSeed.Name, r.crossSeed.Host, r.crossSeed.Username, "", nil, nil,
				&models.InstanceUpdateParams{HasLocalFilesystemAccess: new(true), UseHardlinks: new(true), HardlinkBaseDir: new(r.links)})
			require.NoError(t, err)
			r.sync.cached[r.crossSeed.ID] = buildCrossInstanceViews(r.crossSeed, r.episodes)
			r.sync.existing[r.source.ID] = hashOnSource

			resp := r.apply(r.crossSeed.ID)

			assert.Empty(t, r.sync.adds[r.source.ID])
			assert.Equal(t, &r.crossSeed.ID, r.lastRun().AddedInstanceID)
			if hashOnSource {
				assert.Equal(t, "already_exists", resp.Reason)
				assert.Equal(t, "Torrent already exists in Source instance lib", resp.Message)
				assert.Empty(t, r.sync.adds[r.crossSeed.ID])
				return
			}
			require.True(t, resp.Applied, resp.Reason+": "+resp.Message)
			require.Len(t, r.sync.adds[r.crossSeed.ID], 1)
			assert.Equal(t, r.links, r.sync.adds[r.crossSeed.ID][0]["savepath"])
			assert.Empty(t, r.sync.listed, "an add without a route lists nothing")
		})
	}
}

// Manual assembly sends the add to the Cross-seed instance, skips its
// blocklist, and shows the mapped destination. The Settle row of the
// linked-file check names the Cross-seed instance too.
func TestRoutedManualAssembly(t *testing.T) {
	r := newSeasonPackRig(t, false)
	r.updateSource(&models.InstanceUpdateParams{HardlinkDirPreset: new("by-instance")})
	r.blockPack(r.crossSeed.ID)
	req := &ManualAssembleRequest{InstanceID: r.source.ID, TorrentData: r.torrentData, TargetHashes: r.hashes}

	preview, err := r.svc.CheckManualAssemble(r.ctx, req)
	require.NoError(t, err)
	require.True(t, preview.Ready, preview.Reason+": "+preview.Message)
	assert.Equal(t, routedMapped+"/seeds", preview.Destination)

	resp, err := r.svc.ApplyManualAssemble(r.ctx, req)
	require.NoError(t, err)
	require.True(t, resp.Applied, resp.Reason+": "+resp.Message)
	assert.Equal(t, routedMapped+"/seeds", resp.Destination)
	assert.Empty(t, r.sync.adds[r.source.ID])
	require.Len(t, r.sync.adds[r.crossSeed.ID], 1)
	assert.Equal(t, routedMapped+"/seeds", r.sync.adds[r.crossSeed.ID][0]["savepath"])
	assert.True(t, r.linked(filepath.Join(r.links, "seeds", seasonPackFixtureName), seasonPackFixtureFiles[0]))
	assert.Equal(t, &r.crossSeed.ID, r.lastRun().AddedInstanceID)
	queued := <-r.settler.queue
	assert.Equal(t, r.crossSeed.ID, queued.instanceID, "Settle rechecks on the Cross-seed instance")
	require.NotNil(t, queued.blockedRun)
	assert.Equal(t, &r.crossSeed.ID, queued.blockedRun.AddedInstanceID)
}

func TestRoutedManualAssemblyStopsWhenTheCrossSeedInstanceIsUnavailable(t *testing.T) {
	r := newSeasonPackRig(t, true)
	req := &ManualAssembleRequest{InstanceID: r.source.ID, TorrentData: r.torrentData, TargetHashes: r.hashes}

	resp, err := r.svc.ApplyManualAssemble(r.ctx, req)

	require.NoError(t, err)
	assert.False(t, resp.Applied)
	assert.Equal(t, crossSeedUnavailableStatus, resp.Reason)
	assert.Contains(t, resp.Message, "Cross-seed instance unavailable: seeds: ")
	assert.Empty(t, r.sync.adds[r.source.ID])
	r.requireNoLinkTree()
}

// blockPack puts the hashes of the pack on the blocklist of instanceID.
func (r *seasonPackRig) blockPack(instanceID int) {
	r.t.Helper()
	data, err := base64.StdEncoding.DecodeString(r.torrentData)
	require.NoError(r.t, err)
	meta, err := ParseTorrentMetadataWithInfo(data)
	require.NoError(r.t, err)
	_, err = r.svc.blocklistStore.Upsert(r.t.Context(), &models.CrossSeedBlocklistEntry{InstanceID: instanceID, InfoHash: meta.HashV1})
	require.NoError(r.t, err)
}

// addEpisodeInstance adds an instance without a route, in hardlink mode, that
// holds the same episodes as lib.
func (r *seasonPackRig) addEpisodeInstance(name string) *models.Instance {
	r.t.Helper()
	inst, err := r.instances.Create(r.t.Context(), name, qbtstub.New(r.t, r.t.TempDir()).URL, "user", "pass", nil, nil, false, new(true))
	require.NoError(r.t, err)
	inst, err = r.instances.Update(r.t.Context(), inst.ID, inst.Name, inst.Host, inst.Username, "", nil, nil,
		&models.InstanceUpdateParams{UseHardlinks: new(true), HardlinkBaseDir: new(filepath.Join(filepath.Dir(r.links), name))})
	require.NoError(r.t, err)
	r.sync.cached[inst.ID] = buildCrossInstanceViews(inst, r.episodes)
	return inst
}
