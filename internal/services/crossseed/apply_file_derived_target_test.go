// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

// Bracket-anime pack names parse as non-TV; the season structure lives only in
// the episode file names. Trackers retitle listings but keep the original
// info.name, so apply always compares the bracket name against an identical
// local name: the release prefilter passes trivially, then match typing fails
// because the target parses as a movie while the candidate files parse as
// episodes. Field report: 19 packs found by search, all rejected on add with
// "No matching torrents found with required files".
var bracketAnimePackFiles = []struct {
	name string
	size int64
}{
	// Episodes must dominate the extras: content-type detection keys off the
	// largest file, mirroring real packs (multi-GB episodes vs small NC clips).
	{"Extras/[smol] Sakura Trick - NCED 1 - Kiss (and) Love [BD 1080p HEVC Opus] [8CD26C4D].mkv", 300},
	{"Extras/[smol] Sakura Trick - NCED 2 - Sakura Sweet Kiss [BD 1080p HEVC Opus] [24D89805].mkv", 280},
	{"Extras/[smol] Sakura Trick - NCOP - Won Chu KissMe! [BD 1080p HEVC Opus] [4FB63779].mkv", 260},
	{"[smol] Sakura Trick - S01E01 (BD 1080p HEVC Opus) [66D67F99].mkv", 5001},
	{"[smol] Sakura Trick - S01E02 (BD 1080p HEVC Opus) [D04DC641].mkv", 5002},
	{"[smol] Sakura Trick - S01E03 (BD 1080p HEVC Opus) [D8103511].mkv", 5003},
	{"[smol] Sakura Trick - S01E04 (BD 1080p HEVC Opus) [E9F9AD61].mkv", 5004},
	{"[smol] Sakura Trick - S01E05 (BD 1080p HEVC Opus) [628D01BC].mkv", 5005},
	{"[smol] Sakura Trick - S01E06 (BD 1080p HEVC Opus) [B22641EB].mkv", 5006},
	{"[smol] Sakura Trick - S01E07 (BD 1080p HEVC Opus) [22362963].mkv", 5007},
	{"[smol] Sakura Trick - S01E08 (BD 1080p HEVC Opus) [DF06DEC0].mkv", 5008},
	{"[smol] Sakura Trick - S01E09 (BD 1080p HEVC Opus) [E428CB49].mkv", 5009},
	{"[smol] Sakura Trick - S01E10 (BD 1080p HEVC Opus) [70BC4B46].mkv", 5010},
	{"[smol] Sakura Trick - S01E11 (BD 1080p HEVC Opus) [67A75E7E].mkv", 5011},
	{"[smol] Sakura Trick - S01E12 (BD 1080p HEVC Opus) [82720957].mkv", 5012},
}

const bracketAnimePackName = "[smol] Sakura Trick (BD 1080p HEVC Opus)"

func bracketAnimePackTorrentFiles() qbt.TorrentFiles {
	files := make(qbt.TorrentFiles, 0, len(bracketAnimePackFiles))
	for _, f := range bracketAnimePackFiles {
		files = append(files, qbt.TorrentFile{
			Name: bracketAnimePackName + "/" + f.name,
			Size: f.size,
		})
	}
	return files
}

// createSizedTestTorrent builds torrent bytes whose files carry the exact sizes
// above, so the metainfo file list and the fake qbit candidate agree byte for
// byte the way a real cross-seed pairing does.
func createSizedTestTorrent(t *testing.T, name string) []byte {
	t.Helper()

	info := metainfo.Info{Name: name, PieceLength: 16384}
	for _, f := range bracketAnimePackFiles {
		info.Files = append(info.Files, metainfo.FileInfo{
			Path:   strings.Split(f.name, "/"),
			Length: f.size,
		})
	}
	sortTorrentFiles(info.Files)

	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}

	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes()
}

func createNamedFileTestTorrent(t *testing.T, rootName, fileName string, size int64) []byte {
	t.Helper()

	info := metainfo.Info{
		Name:        rootName,
		PieceLength: 16 * 1024,
		Files: []metainfo.FileInfo{{
			Path:   strings.Split(fileName, "/"),
			Length: size,
		}},
	}
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)

	var buf bytes.Buffer
	meta := metainfo.MetaInfo{InfoBytes: infoBytes}
	require.NoError(t, meta.Write(&buf))
	return buf.Bytes()
}

func TestApplyTargetReleaseViewFromFiles(t *testing.T) {
	service := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	parsed := service.releaseCache.Parse(bracketAnimePackName)
	require.False(t, isTVRelease(parsed), "bracket-anime pack name should parse as non-TV for this regression to mean anything")

	view := service.applyTargetReleaseViewFromFiles(bracketAnimePackName, parsed, bracketAnimePackTorrentFiles(), true)
	require.NotNil(t, view.release)
	require.NotNil(t, view.tagOrigin)
	require.Equal(t, rls.Series, view.release.Type)
	require.Equal(t, 1, view.release.Series)
	require.Zero(t, view.release.Episode)

	view = service.applyTargetReleaseViewFromFiles(bracketAnimePackName, parsed, nil, true)
	require.Same(t, parsed, view.release)
	require.Nil(t, view.tagOrigin)
}

func TestReleaseViewRetainsNonTVSelectedFileProvenance(t *testing.T) {
	t.Parallel()

	const (
		splitName = "[KIRI] Azure Compass (2024) [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Batch]"
		fileName  = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-FoV.mkv"
	)
	service := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	parsed := service.releaseCache.Parse(splitName)
	require.False(t, isTVRelease(parsed))

	view := service.applyTargetReleaseViewFromFiles(splitName, parsed, qbt.TorrentFiles{{Name: fileName, Size: 1_000_000}}, true)
	require.Same(t, parsed, view.release, "movie files must not invent TV structure")
	require.NotNil(t, view.tagOrigin, "non-TV selected-file tags remain identity evidence")
	require.Equal(t, "FoV", view.tagOrigin.Group)
	require.True(t, releaseHasGroupTag(view.tagOrigin, "FoV"))
}

func TestApplyTargetReleaseViewPreservesExplicitRawGroupWhenFilesSupplyTVStructure(t *testing.T) {
	t.Parallel()

	const (
		rawName  = "Azure.Compass.2025.1080p.WEB-DL.AAC2.0.H.264-FoV"
		fileName = "Azure.Compass.S01E03.1080p.WEB-DL.AAC2.0.H.264-KIRI.mkv"
	)
	service := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
	parsed := service.releaseCache.Parse(rawName)
	require.False(t, isTVRelease(parsed))
	require.True(t, releaseHasExplicitGroupTag(parsed))

	view := service.applyTargetReleaseViewFromFiles(rawName, parsed, qbt.TorrentFiles{{Name: fileName, Size: 1_000_000}}, true)
	require.Equal(t, rls.Episode, view.release.Type)
	require.Equal(t, 1, view.release.Series)
	require.Equal(t, 3, view.release.Episode)
	require.Equal(t, "FoV", view.release.Group, "the explicit info.name group remains authoritative")
	require.NotNil(t, view.tagOrigin)
	require.Equal(t, "KIRI", view.tagOrigin.Group, "the selected-file group remains available as veto evidence")
}

// applyFakeSyncManager extends fakeSyncManager just far enough for the add
// stage: matching is the regression under test, the qbit add only has to not
// error.
type applyFakeSyncManager struct {
	*fakeSyncManager
}

func (f *applyFakeSyncManager) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
}

func (f *applyFakeSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) (*qbt.TorrentAddResponse, error) {
	return nil, nil
}

func TestCrossSeedDerivesTargetStructureFromMetainfoFiles(t *testing.T) {
	const (
		instanceID = 1
		sourceHash = "c6d7f67e4726bc80b43cad4a471f36a8de32d456"
	)

	torrentBytes := createSizedTestTorrent(t, bracketAnimePackName)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{
		Hash:     sourceHash,
		Name:     bracketAnimePackName,
		SavePath: "/downloads",
		Progress: 1,
	}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: &applyFakeSyncManager{newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			sourceHash: bracketAnimePackTorrentFiles(),
		})},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	resp, err := service.CrossSeed(context.Background(), &CrossSeedRequest{
		TorrentData:                  base64.StdEncoding.EncodeToString(torrentBytes),
		TargetInstanceIDs:            []int{instanceID},
		SkipPieceBoundarySafetyCheck: true,
		SearchDecision: searchDecisionProvenance{
			Class:            searchCandidateClassStrict,
			SourceInstanceID: instanceID,
			SourceHash:       sourceHash,
		},
	})
	require.NoError(t, err)
	require.True(t, resp.Success, "apply rejected the pack: %+v", resp.Results)
}

func TestCrossSeedDoesNotReplaceExplicitTVGroupFromFiles(t *testing.T) {
	const (
		instanceID   = 1
		existingHash = "existing-kiri"
		incomingName = "Azure.Compass.S01.1080p.WEB-DL.AAC2.0.H.264-FoV"
		existingName = "Azure.Compass.S01.1080p.WEB-DL.AAC2.0.H.264-KIRI"
	)
	fileNames := []string{
		"Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-KIRI.mkv",
		"Azure.Compass.S01E02.1080p.WEB-DL.AAC2.0.H.264-KIRI.mkv",
	}
	torrentBytes := createTestTorrent(t, incomingName, fileNames, 256*1024)
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: existingHash, Name: existingName, Progress: 1}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: &applyFakeSyncManager{newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			existingHash: meta.Files,
		})},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	for _, tt := range []struct {
		name          string
		decisionClass searchCandidateClass
	}{
		{name: "manual apply"},
		{name: "cached strict search decision", decisionClass: searchCandidateClassStrict},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := service.CrossSeed(context.Background(), &CrossSeedRequest{
				TorrentData:       base64.StdEncoding.EncodeToString(torrentBytes),
				TargetInstanceIDs: []int{instanceID},
				SearchDecision: searchDecisionProvenance{
					Class:            tt.decisionClass,
					SourceInstanceID: instanceID,
					SourceHash:       existingHash,
				},
			})
			require.NoError(t, err)
			require.False(t, resp.Success)
			require.Len(t, resp.Results, 1)
			require.Equal(t, "no_match", resp.Results[0].Status,
				"an explicit info.name group must not be replaced by a selected-file group")
		})
	}
}

func TestCrossSeedGroupFallbackRetainsNonTVSelectedFileProvenance(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		existingHash = "bound-movie"
		size         = int64(1_000_000)
		splitName    = "[KIRI] Azure Compass (2024) [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KIRI)][Batch]"
		taggedName   = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-KIRI"
		incomingFile = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-FoV.mkv"
		existingFile = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-KIRI.mkv"
	)
	torrentBytes := createNamedFileTestTorrent(t, splitName, incomingFile, size)
	instance := &models.Instance{ID: instanceID, Name: "main"}
	existing := qbt.Torrent{Hash: existingHash, Name: taggedName, Size: size, Progress: 1}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: instance}},
		syncManager: &applyFakeSyncManager{newFakeSyncManager(instance, []qbt.Torrent{existing}, map[string]qbt.TorrentFiles{
			existingHash: {{Name: existingFile, Size: size}},
		})},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	resp, err := service.CrossSeed(context.Background(), &CrossSeedRequest{
		TorrentData:       base64.StdEncoding.EncodeToString(torrentBytes),
		TargetInstanceIDs: []int{instanceID},
		SearchDecision: searchDecisionProvenance{
			Class:                 searchCandidateClassExactSizeFallback,
			SourceInstanceID:      instanceID,
			SourceHash:            existingHash,
			StrictMismatchReason:  groupMismatchReason,
			RelaxedDifferences:    []string{"group"},
			GroupFallbackIdentity: "kiri",
		},
	})
	require.NoError(t, err)
	require.False(t, resp.Success)
	require.Len(t, resp.Results, 1)
	require.Equal(t, "no_match", resp.Results[0].Status,
		"the selected-file FoV tag must veto a KIRI group fallback even for non-TV content")
}

func TestProcessCrossSeedCandidate_FileDerivedMatchedPackUsesContentPath(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		matchedHash  = "raw-episode-pack"
		incomingHash = "incoming-episode"
		rawEpisode   = "Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI"
		episodeName  = "Azure.Compass.S01E02.1080p.WEB-DL.H.264-KIRI"
		packRoot     = "Azure.Compass.S01"
	)
	downloadsDir := filepath.Join(t.TempDir(), "downloads", "tv")
	packPath := filepath.Join(downloadsDir, packRoot)
	packFiles := qbt.TorrentFiles{
		{Name: packRoot + "/Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI.mkv", Size: 999},
		{Name: packRoot + "/Azure.Compass.S01E02.1080p.WEB-DL.H.264-KIRI.mkv", Size: 999},
	}
	episodeFiles := qbt.TorrentFiles{
		{Name: "Azure.Compass.S01E02.1080p.WEB-DL.H.264-KIRI.mkv", Size: 999},
	}
	matched := qbt.Torrent{
		Hash: matchedHash, Name: rawEpisode, Size: 1998, TotalSize: 1998, Progress: 1,
		ContentPath: packPath,
	}
	sync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash:  packFiles,
			incomingHash: episodeFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {SavePath: downloadsDir},
		},
	}
	service := &Service{
		instanceStore: &rootlessSavePathInstanceStore{instances: map[int]*models.Instance{
			instanceID: {ID: instanceID, Name: "main"},
		}},
		syncManager: sync, releaseCache: NewReleaseCache(), stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}
	startPaused := true
	result := service.processCrossSeedCandidate(
		context.Background(),
		CrossSeedCandidate{InstanceID: instanceID, InstanceName: "main", Torrents: []qbt.Torrent{matched}},
		[]byte("torrent"), incomingHash, "", episodeName,
		&CrossSeedRequest{
			StartPaused: &startPaused,
			SearchDecision: searchDecisionProvenance{
				Class: searchCandidateClassStrict, SourceInstanceID: instanceID, SourceHash: matchedHash,
			},
		},
		service.releaseCache.Parse(episodeName), episodeFiles, nil,
	)
	require.True(t, result.Success, result.Message)
	require.Equal(t, "added", result.Status)
	require.Equal(t, packPath, sync.addedOptions["savepath"],
		"a single episode matched into a file-derived season pack must use the pack ContentPath")
}

type fileDerivedPackAlignmentSyncManager struct {
	*rootlessSavePathSyncManager
	folderRenames []fileRenameInstruction
}

func (m *fileDerivedPackAlignmentSyncManager) RenameTorrentFolder(_ context.Context, _ int, hash, oldPath, newPath string) error {
	m.folderRenames = append(m.folderRenames, fileRenameInstruction{oldPath: oldPath, newPath: newPath})
	files := m.files[normalizeHash(hash)]
	for i := range files {
		if files[i].Name == oldPath {
			files[i].Name = newPath
			continue
		}
		if suffix, ok := strings.CutPrefix(files[i].Name, oldPath+"/"); ok {
			files[i].Name = newPath + "/" + suffix
		}
	}
	m.files[normalizeHash(hash)] = files
	return nil
}

func TestProcessCrossSeedCandidate_FileDerivedIncomingPackStillAlignsPackRoots(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		matchedHash  = "matched-pack"
		incomingHash = "incoming-pack"
		rawEpisode   = "Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI"
		matchedName  = "Azure.Compass.S01.1080p.WEB-DL.H.264-KIRI"
		incomingRoot = "Incoming.Azure.Compass.S01"
		matchedRoot  = "Matched.Azure.Compass.S01"
	)
	downloadsDir := filepath.Join(t.TempDir(), "downloads", "tv")
	incomingFiles := qbt.TorrentFiles{
		{Name: incomingRoot + "/Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI.mkv", Size: 1001},
		{Name: incomingRoot + "/Azure.Compass.S01E02.1080p.WEB-DL.H.264-KIRI.mkv", Size: 999},
	}
	matchedFiles := qbt.TorrentFiles{
		{Name: matchedRoot + "/Azure.Compass.S01E01.1080p.WEB-DL.H.264-KIRI.mkv", Size: 1001},
		{Name: matchedRoot + "/Azure.Compass.S01E02.1080p.WEB-DL.H.264-KIRI.mkv", Size: 999},
	}
	matched := qbt.Torrent{
		Hash: matchedHash, Name: matchedName, Size: 2000, TotalSize: 2000, Progress: 1,
		ContentPath: filepath.Join(downloadsDir, matchedRoot),
	}
	baseSync := &rootlessSavePathSyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash:  matchedFiles,
			incomingHash: incomingFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {SavePath: downloadsDir},
		},
	}
	sync := &fileDerivedPackAlignmentSyncManager{rootlessSavePathSyncManager: baseSync}
	service := &Service{
		instanceStore: &rootlessSavePathInstanceStore{instances: map[int]*models.Instance{
			instanceID: {ID: instanceID, Name: "main"},
		}},
		syncManager: sync, releaseCache: NewReleaseCache(), stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}
	rawRelease := service.releaseCache.Parse(rawEpisode)
	incomingView := service.applyTargetReleaseViewFromFiles(rawEpisode, rawRelease, incomingFiles, true)
	require.True(t, isTVSeasonPack(incomingView.release), "fixture must derive the incoming pack from its files")

	startPaused := true
	result := service.processCrossSeedCandidate(
		context.Background(),
		CrossSeedCandidate{InstanceID: instanceID, InstanceName: "main", Torrents: []qbt.Torrent{matched}},
		[]byte("torrent"), incomingHash, "", rawEpisode,
		&CrossSeedRequest{
			StartPaused: &startPaused,
			SearchDecision: searchDecisionProvenance{
				Class: searchCandidateClassStrict, SourceInstanceID: instanceID, SourceHash: matchedHash,
			},
		},
		incomingView.release, incomingFiles, nil,
	)
	require.True(t, result.Success, result.Message)
	require.Equal(t, "added", result.Status)
	require.Equal(t, []fileRenameInstruction{{oldPath: incomingRoot, newPath: matchedRoot}}, sync.folderRenames,
		"a file-derived incoming pack must not be mistaken for an episode and skip required root alignment")
}
