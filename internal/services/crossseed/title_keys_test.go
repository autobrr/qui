// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

type announcementTitleKeyPair struct {
	row    corpusRow
	policy announcementMatchPolicy
}

const (
	titleKeyEpisodeSize = int64(1_000_000)
	titleKeyAliasSize   = int64(1_500_000_000)
	titleKeyCRCSize     = int64(1_468_228_504)
	titleKeyRoundedSize = int64(1_471_026_298)
	titleKeyPackSize    = int64(28_408_496_978)

	titleKeyAzure          = "Azure.Compass.S01E05.1080p.WEB-DL.H.264-KIRI"
	titleKeyFrieren        = "[KiraSubs] Frieren S2 - 10 (1080p) [ABCD1234].mkv"
	titleKeyFrierenEnglish = "Frieren: Beyond Journey's End S02E10 1080p CR WEB-DL AAC 2.0 H.264-KiraSubs"
)

var (
	basePolicy        = announcementMatchPolicy{}
	unknownSizePolicy = announcementMatchPolicy{allowUnknownSize: true}
	episodesPolicy    = announcementMatchPolicy{findIndividualEpisodes: true}
	titleRescuePolicy = announcementMatchPolicy{rescueTitleMismatches: true}
	episodeMapPolicy  = announcementMatchPolicy{findIndividualEpisodes: true, skipRecheck: true, episodeMap: episodeMapS04E15}
)

func announcementPair(name, source, candidate string, sourceSize, candidateSize int64, policy announcementMatchPolicy) announcementTitleKeyPair {
	return announcementTitleKeyPair{
		row:    corpusRow{name: name, source: source, candidate: candidate, sourceSize: sourceSize, candidateSize: candidateSize},
		policy: policy,
	}
}

func (pair announcementTitleKeyPair) withTitles(titles ...string) announcementTitleKeyPair {
	pair.row.titles = titles
	return pair
}

func (pair announcementTitleKeyPair) withSourceFiles(files qbt.TorrentFiles) announcementTitleKeyPair {
	pair.row.sourceFiles = files
	return pair
}

var frierenAliases = []string{"Frieren: Beyond Journey's End", "Sousou no Frieren", "Frieren"}

var announcementTitleKeyPairs = []announcementTitleKeyPair{
	announcementPair("strict", titleKeyAzure, titleKeyAzure, titleKeyEpisodeSize, titleKeyEpisodeSize, basePolicy),
	announcementPair("codec", titleKeyAzure, "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI", titleKeyEpisodeSize, titleKeyEpisodeSize, basePolicy),
	announcementPair("episode", titleKeyAzure, "Azure.Compass.S01E06.1080p.WEB-DL.H.264-KIRI", titleKeyEpisodeSize, titleKeyEpisodeSize, basePolicy),
	announcementPair("split group", titleKeyAzure, "[KIRI] Azure Compass S01E05 [Web][MKV][h264][1080p][Softsubs (KIRI)]", titleKeyEpisodeSize, titleKeyEpisodeSize, basePolicy),
	announcementPair("title rescue", titleKeyAzure, "Different.Voyage.S01E05.1080p.WEB-DL.H.264-KIRI", titleKeyEpisodeSize, titleKeyEpisodeSize, titleRescuePolicy),
	announcementPair("nonexact size", titleKeyAzure, titleKeyAzure, titleKeyEpisodeSize, titleKeyEpisodeSize-1, basePolicy),
	announcementPair("file-derived TV structure", "[smol] Sakura Trick (BD 1080p HEVC Opus)", "Sakura Trick S01 JAPANESE 1080p BluRay Opus 2.0 x265-smol", titleKeyPackSize, titleKeyPackSize, basePolicy).
		withSourceFiles(qbt.TorrentFiles{
			{Name: "[smol] Sakura Trick (BD 1080p HEVC Opus)/[smol] Sakura Trick - S01E01 (BD 1080p HEVC Opus) [DF06DEC0].mkv", Size: 2_367_374_748},
			{Name: "[smol] Sakura Trick (BD 1080p HEVC Opus)/[smol] Sakura Trick - S01E02 (BD 1080p HEVC Opus) [DF06DEC1].mkv", Size: 2_367_374_748},
		}),
	announcementPair("unknown size strict", titleKeyAzure, titleKeyAzure, titleKeyEpisodeSize, 0, unknownSizePolicy),
	announcementPair("unknown size codec", titleKeyAzure, "Azure.Compass.S01E05.1080p.WEB-DL.H.265-KIRI", titleKeyEpisodeSize, 0, unknownSizePolicy),
	announcementPair("unknown size missing announcement group", "Sample.Show.S08E11.Episode.Title.1080p.DSNP.WEB-DL.DDP5.1.H.264-NTb", "Sample Show S08E11 1080p WEB-DL DD+5.1 H.264", titleKeyEpisodeSize, 0, unknownSizePolicy),
	announcementPair("unknown size missing announcement group without resolution", "Sample.Show.S08E11.WEB-DL.H.264-NTb", "Sample Show S08E11 WEB-DL H.264", titleKeyEpisodeSize, 0, unknownSizePolicy),
	announcementPair("unknown size missing announcement group with web relabel", "Sample.Show.S08E11.WEBRip.H.264-NTb", "Sample Show S08E11 WEB-DL H.264", titleKeyEpisodeSize, 0, unknownSizePolicy),
	announcementPair("unknown size missing announcement group with codec and resolution", "Sample.Show.S08E11.1080p.WEB-DL.H.264-NTb", "Sample Show S08E11 1080p WEB-DL H.265", titleKeyEpisodeSize, 0, unknownSizePolicy),
	announcementPair("unknown size split group", "Azure Compass S01 [Web][MKV][h264][1080p][Softsubs (KIRI)][Batch][KIRI]", "Azure.Compass.S01.1080p.WEB-DL.H.264-KIRI", titleKeyEpisodeSize, 0, unknownSizePolicy),
	announcementPair("english alias title exact size", titleKeyFrieren, titleKeyFrierenEnglish, titleKeyAliasSize, titleKeyAliasSize, basePolicy).withTitles(frierenAliases...),
	announcementPair("romaji alias title exact size", titleKeyFrieren, "Sousou no Frieren S02E10 1080p WEB-DL AAC2.0 H.264-KiraSubs", titleKeyAliasSize, titleKeyAliasSize, basePolicy).withTitles(frierenAliases...),
	announcementPair("bracket alias title unknown size", titleKeyFrieren, "[KiraSubs] Frieren: Beyond Journey's End Season 2 - 10 [2026][Web][MKV][h264][1080p][AAC 2.0][Softsubs (KiraSubs)]", titleKeyAliasSize, 0, unknownSizePolicy).withTitles(frierenAliases...),
	announcementPair("CRC tag on the source only", titleKeyFrieren, titleKeyFrierenEnglish, titleKeyCRCSize, titleKeyRoundedSize, basePolicy).withTitles(frierenAliases...),
	announcementPair("CRC tag on the announce only", "[KiraSubs] Frieren S2 - 10 (1080p).mkv", titleKeyFrieren, titleKeyCRCSize, titleKeyRoundedSize, basePolicy).withTitles(frierenAliases...),
	announcementPair("colon title absolute episode", "[KiraSubs] Re Start Isekai Life - 81 (1080p) [ABCD1234].mkv", "Re:Start Isekai Life S04E15 1080p WEB-DL AAC2.0 H.264-KiraSubs", corpusEpisodeSize, corpusEpisodeSize, episodesPolicy),
	announcementPair("movie with an ellipsis", "The.Squad.of.Madame.K.1953.1080p.BluRay.x264-GRP", "The Squad of Madame K... 1953 1080p BluRay x264-GRP", corpusEpisodeSize, corpusEpisodeSize, episodesPolicy),
	announcementPair("seasoned announce against absolute local", episodeMapAbsoluteName, episodeMapSeasonedName, episodeMapSize, episodeMapSize+500, episodeMapPolicy),
	announcementPair("absolute announce against seasoned local", episodeMapSeasonedName, episodeMapAbsoluteName, episodeMapSize, episodeMapSize+500, episodeMapPolicy),
	announcementPair("tracker disagrees with the map at a byte-equal size", episodeMapAbsoluteName, episodeMapOffByOneName, episodeMapSize, episodeMapSize, episodeMapPolicy),
	announcementPair("same-scheme pair ignores the map", episodeMapSeasonedName, episodeMapSeasonedName, episodeMapSize, episodeMapSize+500, episodeMapPolicy),
	announcementPair("held Title III against an announced Title S03E01", "Kaiju.Squad.100.III.1080p.WEB.H264-GRP", "Kaiju.Squad.100.S03E01.1080p.WEB.H264-GRP", corpusPackSize, 0, announcementMatchPolicy{findIndividualEpisodes: true, allowUnknownSize: true}).
		withSourceFiles(corpusPackFiles("Kaiju.Squad.100.III.1080p.WEB.H264-GRP", "Kaiju.Squad.100.III.S03E%02d.1080p.WEB.H264-GRP")),
	announcementPair("held Title S03E01 against an announced Title III", "Kaiju.Squad.100.S03E01.1080p.WEB.H264-GRP", "Kaiju.Squad.100.III.1080p.WEB.H264-GRP", corpusEpisodeSize, 0, announcementMatchPolicy{findIndividualEpisodes: true, allowUnknownSize: true}),
}

func corpusRowAccepted(row corpusRow) bool {
	if row.want == corpusMatch {
		return true
	}
	for _, difference := range row.differences {
		if difference.verdict == corpusMatch {
			return true
		}
	}
	return false
}

func TestTitleKeysCoverAcceptedPairs(t *testing.T) {
	for _, row := range corpusRows {
		if !corpusRowAccepted(row) {
			continue
		}
		t.Run("corpus/"+row.name, func(t *testing.T) {
			requireSharedTitleKeyOrExactSize(t, row)
		})
	}
	for _, pair := range announcementTitleKeyPairs {
		t.Run("announcement/"+pair.row.name, func(t *testing.T) {
			svc := pair.row.corpusService(false)
			held := pair.row.sourceTorrent()
			policy := pair.policy
			policy.candidateTitles = pair.row.titles
			got := svc.classifyWebhookAnnouncementSource(t.Context(), corpusInstanceID, &held, svc.matcher().readName(pair.row.candidate), pair.row.candidateSize, policy)
			require.True(t, got.decision.Accepted, got.decision.RejectReason)
			requireSharedTitleKeyOrExactSize(t, pair.row)
		})
	}
}

func requireSharedTitleKeyOrExactSize(t *testing.T, row corpusRow) {
	t.Helper()
	if row.sourceSize > 0 && row.sourceSize == row.candidateSize {
		return
	}
	svc := row.corpusService(false)
	m := svc.matcher()
	held := row.sourceTorrent()
	reads := svc.readLocalTorrents(t.Context(), corpusInstanceID, slices.Values([]*qbt.Torrent{&held}))
	heldKeys := m.titleKeys(reads.read(m, corpusInstanceID, &held))
	announceKeys := m.titleKeys(m.readName(row.candidate))
	heldWithTitles, announceWithTitles := withAlternateTitles(heldKeys, row.titles), withAlternateTitles(announceKeys, row.titles)
	const lostMatch = "Held titles would answer no match for a pair that the matcher accepts, and the cross-seed is lost: held %q keys %q, announce %q keys %q"
	require.True(t, normalizedTitleSetsOverlap(heldWithTitles, announceKeys), "alternate titles on the held release: "+lostMatch, row.source, heldWithTitles, row.candidate, announceKeys)
	require.True(t, normalizedTitleSetsOverlap(heldKeys, announceWithTitles), "alternate titles on the announce: "+lostMatch, row.source, heldKeys, row.candidate, announceWithTitles)
}

func withAlternateTitles(keys, titles []string) []string {
	keys = slices.Clone(keys)
	for _, title := range titles {
		keys = addNormalizedTitle(keys, title)
	}
	return keys
}
