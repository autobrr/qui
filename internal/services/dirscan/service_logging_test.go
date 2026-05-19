// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestLogDirScanAcceptedMatch_PartialIncludesUnmatchedSamples(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	logDirScanAcceptedMatch(
		&logger,
		&Searchee{Name: "Avatar - De feu et de cendres (2025)"},
		&ParsedTorrent{Name: "Avatar.Fire.and.Ash.2025.2160p.WEB-DL.mkv", InfoHash: "deadbeef"},
		&MatchResult{
			IsPartialMatch: true,
			MatchRatio:     0.5,
			MatchedFiles: []MatchedFilePair{
				{TorrentFile: TorrentFile{Path: "Avatar.Fire.and.Ash.2025.2160p.WEB-DL.mkv", Size: 100}},
			},
			UnmatchedSearcheeFiles: []*ScannedFile{
				{RelPath: "Avatar - De feu et de cendres (2025).nfo", Size: 12},
			},
			UnmatchedTorrentFiles: []TorrentFile{
				{Path: "Avatar.Fire.and.Ash.2025.2160p.WEB-DL/Proof/sample.txt", Size: 8},
			},
		},
		dirScanMatchDecision{PieceMatchPercent: 99.98},
	)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	require.Equal(t, "dirscan: found match", entry["message"])
	require.Equal(t, true, entry["partial"])
	require.EqualValues(t, 1, entry["matchedFiles"])
	require.EqualValues(t, 1, entry["unmatchedSearcheeFiles"])
	require.EqualValues(t, 1, entry["unmatchedTorrentFiles"])

	searcheeSamples, ok := entry["unmatchedSearcheeSample"].([]any)
	require.True(t, ok)
	require.Len(t, searcheeSamples, 1)

	torrentSamples, ok := entry["unmatchedTorrentSample"].([]any)
	require.True(t, ok)
	require.Len(t, torrentSamples, 1)
}
