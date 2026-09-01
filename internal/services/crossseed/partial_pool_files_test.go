// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestBuildPartialPoolFileDescriptorsJoinsV2RootsByPath(t *testing.T) {
	t.Parallel()

	rootA := strings.Repeat("a", 32)
	rootB := strings.Repeat("b", 32)
	info := &metainfo.Info{
		Name:        "Synthetic.Release",
		MetaVersion: 2,
		PieceLength: 16,
		// Hybrid v1 order deliberately disagrees with sorted v2 traversal.
		Files: []metainfo.FileInfo{
			{Path: []string{"B.mkv"}, Length: 20},
			{Path: []string{"A.mkv"}, Length: 10},
		},
		FileTree: metainfo.FileTree{Dir: map[string]metainfo.FileTree{
			"B.mkv": {File: metainfo.FileTreeFile{Length: 20, PiecesRoot: rootB}},
			"A.mkv": {File: metainfo.FileTreeFile{Length: 10, PiecesRoot: rootA}},
		}},
	}

	descriptors, err := buildPartialPoolFileDescriptors(info)
	require.NoError(t, err)
	require.Len(t, descriptors, 2)
	require.Equal(t, "Synthetic.Release/B.mkv", descriptors[0].RelativePath)
	require.Equal(t, strings.Repeat("62", 32), descriptors[0].PiecesRoot)
	require.Equal(t, "Synthetic.Release/A.mkv", descriptors[1].RelativePath)
	require.Equal(t, strings.Repeat("61", 32), descriptors[1].PiecesRoot)
}

func TestBuildPartialPoolFileDescriptorsDropsMalformedV2Evidence(t *testing.T) {
	t.Parallel()

	info := &metainfo.Info{
		Name:        "Synthetic.Release",
		MetaVersion: 2,
		PieceLength: 16,
		FileTree: metainfo.FileTree{Dir: map[string]metainfo.FileTree{
			"video.mkv": {File: metainfo.FileTreeFile{Length: 10, PiecesRoot: "short"}},
		}},
	}

	descriptors, err := buildPartialPoolFileDescriptors(info)
	require.NoError(t, err)
	require.Len(t, descriptors, 1)
	for _, descriptor := range descriptors {
		require.Empty(t, descriptor.PiecesRoot)
	}
}

func TestBuildPartialPoolFileDescriptorsDropsDuplicateAndMismatchedV2Evidence(t *testing.T) {
	t.Parallel()

	root := strings.Repeat("r", 32)
	info := &metainfo.Info{
		Name:        "Synthetic.Release",
		MetaVersion: 2,
		PieceLength: 16,
		Files: []metainfo.FileInfo{
			{Path: []string{"duplicate.mkv"}, Length: 11},
			{Path: []string{"duplicate.mkv"}, Length: 10},
			{Path: []string{"mismatch.mkv"}, Length: 20},
		},
		FileTree: metainfo.FileTree{Dir: map[string]metainfo.FileTree{
			"duplicate.mkv": {File: metainfo.FileTreeFile{Length: 10, PiecesRoot: root}},
			"mismatch.mkv":  {File: metainfo.FileTreeFile{Length: 21, PiecesRoot: root}},
		}},
	}

	descriptors, err := buildPartialPoolFileDescriptors(info)
	require.NoError(t, err)
	require.Len(t, descriptors, 3)
	for _, descriptor := range descriptors {
		require.Empty(t, descriptor.PiecesRoot)
	}
}

func TestBuildPartialPoolAdmissionFilesRequiresExactPostAddJoin(t *testing.T) {
	t.Parallel()

	descriptors := []partialPoolFileDescriptor{{Index: 0, RelativePath: "Synthetic.Release/video.mkv", SizeBytes: 100}}
	files := qbt.TorrentFiles{{Index: 0, Name: "Synthetic.Release/video.mkv", Size: 100, Priority: 1}}
	rows, missing, err := buildPartialPoolAdmissionFiles(descriptors, files, map[string]struct{}{})
	require.NoError(t, err)
	require.Equal(t, int64(100), missing)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, rows[0].Status)

	files[0].Name = "Synthetic.Release/renamed.mkv"
	_, _, err = buildPartialPoolAdmissionFiles(descriptors, files, map[string]struct{}{})
	require.Error(t, err)
}

func TestPartialPoolReplaceableTargetsTracksOnlyMissingPaths(t *testing.T) {
	rootPath := t.TempDir()
	existingPath := "Synthetic.Release/existing.mkv"
	existingLocalPath := filepath.Join(rootPath, filepath.FromSlash(existingPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(existingLocalPath), 0o755))
	require.NoError(t, os.WriteFile(existingLocalPath, []byte("existing"), 0o600))
	descriptors := []partialPoolFileDescriptor{
		{RelativePath: existingPath},
		{RelativePath: "Synthetic.Release/missing.mkv"},
		{RelativePath: "../escape.mkv"},
		{RelativePath: "/escape.mkv"},
		{RelativePath: `\escape.mkv`},
		{RelativePath: `C:\escape.mkv`},
		{RelativePath: `\\host\share\escape.mkv`},
	}

	replaceable := partialPoolReplaceableTargets(rootPath, descriptors)
	require.NotContains(t, replaceable, existingPath)
	require.Contains(t, replaceable, "Synthetic.Release/missing.mkv")
	require.NotContains(t, replaceable, "../escape.mkv")
	require.NotContains(t, replaceable, "/escape.mkv")
	require.NotContains(t, replaceable, `\escape.mkv`)
	require.NotContains(t, replaceable, `C:\escape.mkv`)
	require.NotContains(t, replaceable, `\\host\share\escape.mkv`)
}

func TestPartialPoolFilesPairPolicy(t *testing.T) {
	t.Parallel()

	source := &models.CrossSeedPartialPoolMember{Files: []*models.CrossSeedPartialPoolMemberFile{
		{RelativePath: "Source/video.mkv", SizeBytes: 100, PiecesRoot: "root"},
		{RelativePath: "Source/duplicate.nfo", SizeBytes: 5},
		{RelativePath: "Source/sub/duplicate.nfo", SizeBytes: 5},
	}}
	target := &models.CrossSeedPartialPoolMember{Files: []*models.CrossSeedPartialPoolMemberFile{
		{RelativePath: "Target/renamed.mkv", SizeBytes: 100, PiecesRoot: "root"},
		{RelativePath: "Target/video.mkv", SizeBytes: 100},
		{RelativePath: "Target/sub/duplicate.nfo", SizeBytes: 5},
	}}

	require.True(t, partialPoolFilesPair(source, target, source.Files[0], target.Files[0]), "v2 root")
	target.Files[0].RelativePath = "Target/video.mkv"
	target.Files[0].PiecesRoot = "different-root"
	require.False(t, partialPoolFilesPair(source, target, source.Files[0], target.Files[0]), "different v2 roots are definitive")
	target.Files[0].RelativePath = "Target/renamed.mkv"
	target.Files[0].PiecesRoot = ""
	require.False(t, partialPoolFilesPair(source, target, source.Files[0], target.Files[0]), "renamed v1 size alone")
	require.True(t, partialPoolFilesPair(source, target, source.Files[0], target.Files[1]), "root-stripped path")
	require.False(t, partialPoolFilesPair(source, target, source.Files[1], target.Files[2]), "ambiguous basename")

	uniqueSource := &models.CrossSeedPartialPoolMember{Files: []*models.CrossSeedPartialPoolMemberFile{{RelativePath: "Source/unique.nfo", SizeBytes: 5}}}
	uniqueTarget := &models.CrossSeedPartialPoolMember{Files: []*models.CrossSeedPartialPoolMemberFile{{RelativePath: "Other/unique.nfo", SizeBytes: 5}}}
	require.True(t, partialPoolFilesPair(uniqueSource, uniqueTarget, uniqueSource.Files[0], uniqueTarget.Files[0]), "unique basename")
	require.False(t, partialPoolFilesPair(uniqueSource, uniqueTarget, &models.CrossSeedPartialPoolMemberFile{RelativePath: "zero", SizeBytes: 0}, &models.CrossSeedPartialPoolMemberFile{RelativePath: "zero", SizeBytes: 0}), "zero length")
}

func TestValidatePartialPoolPathInsideRootRejectsExistingSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "video.mkv")
	require.NoError(t, os.WriteFile(target, []byte("synthetic payload"), 0o600))

	linkDir := filepath.Join(root, "Synthetic.Release")
	require.NoError(t, os.MkdirAll(linkDir, 0o755))
	link := filepath.Join(linkDir, "video.mkv")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported on this system: %v", err)
	}

	err := validatePartialPoolPathInsideRoot(root, link)
	require.Error(t, err)
	require.Contains(t, err.Error(), "escapes through symlink or reparse point")
}
