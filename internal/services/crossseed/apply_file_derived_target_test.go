// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
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

func TestDeriveTVReleaseFromFiles(t *testing.T) {
	service := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	parsed := service.releaseCache.Parse(bracketAnimePackName)
	require.False(t, isTVRelease(parsed), "bracket-anime pack name should parse as non-TV for this regression to mean anything")

	derived := service.deriveTVReleaseFromFiles(bracketAnimePackName, parsed, bracketAnimePackTorrentFiles())
	require.NotNil(t, derived)
	require.Equal(t, rls.Series, derived.Type)
	require.Equal(t, 1, derived.Series)
	require.Zero(t, derived.Episode)

	require.Nil(t, service.deriveTVReleaseFromFiles(bracketAnimePackName, parsed, nil))
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
		SearchDecisionClass:          searchCandidateClassStrict,
		SearchSourceInstanceID:       instanceID,
		SearchSourceHash:             sourceHash,
	})
	require.NoError(t, err)
	require.True(t, resp.Success, "apply rejected the pack: %+v", resp.Results)
}
