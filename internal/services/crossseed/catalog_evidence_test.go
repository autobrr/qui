// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/linktree"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

func TestCatalogPlacementKeepsValidatedEdges(t *testing.T) {
	root := t.TempDir()
	left := qbt.TorrentFiles{{Index: 0, Name: "Incoming/new.flac", Size: 2}, {Index: 1, Name: "Incoming/exact.flac", Size: 2}}
	right := qbt.TorrentFiles{{Name: "Existing/exact.flac", Size: 2}, {Name: "Existing/old.flac", Size: 2}}
	pairs, err := completeCatalogPairing(left, right, "Incoming", "Existing")
	require.NoError(t, err)
	existing := make([]hardlinktree.ExistingFile, len(right))
	for i, file := range right {
		name := filepath.Join(root, filepath.FromSlash(file.Name))
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o755))
		require.NoError(t, os.WriteFile(name, []byte{byte('A' + i), byte('A' + i)}, 0o600))
		existing[i] = hardlinktree.ExistingFile{AbsPath: name, RelPath: file.Name, Size: 2}
	}
	links, err := catalogLinks(fsops.HostPaths, pairs, existing)
	require.NoError(t, err)
	plan, err := linktree.Plan(t.Context(), linktree.Request{
		Backend: local.NewBackend(),
		Config:  linktree.Config{BaseDirs: []string{filepath.Join(root, "links")}, Kind: linktree.Hardlink},
		Torrent: linktree.Torrent{InfoHash: "hash", Name: "Incoming", Files: []string{left[0].Name, left[1].Name}},
		Links:   links,
	})
	require.NoError(t, err)
	tree, err := plan.Create(t.Context())
	require.NoError(t, err)
	for _, tt := range []struct{ name, want string }{{"new.flac", "BB"}, {"exact.flac", "AA"}} {
		data, err := os.ReadFile(filepath.Join(tree.SavePath, "Incoming", tt.name))
		require.NoError(t, err)
		require.Equal(t, tt.want, string(data))
	}
	alignment, err := catalogAlignment(&crossSeedAddPlan{sourceFiles: left, files: right, filePairs: pairs}, left)
	require.NoError(t, err)
	require.ElementsMatch(t, []FileRename{{OldPath: "Incoming/new.flac", NewPath: "Existing/old.flac"}, {OldPath: "Incoming/exact.flac", NewPath: "Existing/exact.flac"}}, alignment.Files)
}

func TestCatalogRootOnlyAlignment(t *testing.T) {
	left := qbt.TorrentFiles{{Index: 0, Name: "Incoming/CD1/01.flac", Size: 100}, {Index: 1, Name: "Incoming/CD2/01.flac", Size: 100}}
	right := qbt.TorrentFiles{{Index: 0, Name: "Existing/CD1/01.flac", Size: 100}, {Index: 1, Name: "Existing/CD2/01.flac", Size: 100}}
	pairs, err := completeCatalogPairing(left, right, "Incoming", "Existing")
	require.NoError(t, err)
	plan := &crossSeedAddPlan{sourceFiles: left, files: right, filePairs: pairs}
	alignment, err := catalogAlignment(plan, left)
	require.NoError(t, err)
	require.Equal(t, AlignmentPlan{FolderFrom: "Incoming", FolderTo: "Existing"}, alignment)
	changed := slices.Clone(left)
	changed[1].Name = "Incoming/changed.flac"
	_, err = catalogAlignment(plan, changed)
	require.Error(t, err, "folder optimization must retain added-file identity validation")
}

func TestCatalogMixedRootAlignment(t *testing.T) {
	for _, tt := range []struct {
		name  string
		right qbt.TorrentFiles
	}{
		{"swapped names", qbt.TorrentFiles{{Name: "Existing/A.flac", Size: 200}, {Name: "Existing/B.flac", Size: 100}}},
		{"one-way dependency", qbt.TorrentFiles{{Name: "Existing/B.flac", Size: 100}, {Name: "Existing/C.flac", Size: 200}}},
		{"file and directory", qbt.TorrentFiles{{Name: "Existing/B.flac/track.flac", Size: 100}, {Name: "Existing/C.flac", Size: 200}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			left := qbt.TorrentFiles{{Index: 0, Name: "Incoming/A.flac", Size: 100}, {Index: 1, Name: "Incoming/B.flac", Size: 200}}
			pairs, err := completeCatalogPairing(left, tt.right, "Incoming", "Existing")
			require.NoError(t, err)
			alignment, err := catalogAlignment(&crossSeedAddPlan{sourceFiles: left, files: tt.right, filePairs: pairs}, left)
			require.NoError(t, err)
			fake := &alignerFake{hash: alignerHash, files: slices.Clone(left)}
			aligner := NewAligner(fake)
			aligner.verifyWait, aligner.retryDelay = 0, 0
			require.True(t, aligner.Align(t.Context(), 1, alignerHash, alignment))
			for _, pair := range pairs {
				require.Equal(t, pair.candidatePath, fake.files[pair.sourceIndex].Name)
			}
			require.Empty(t, alignment.FolderFrom, "mixed plans must not stage file renames under occupied incoming paths")
		})
	}
}

func TestCatalogPairing(t *testing.T) {
	for _, tt := range []struct {
		name        string
		left, right qbt.TorrentFiles
		want        []catalogPairTier
	}{
		{"root difference", qbt.TorrentFiles{{Name: "Left/01.flac", Size: 100}, {Name: "Left/booklet.pdf", Size: 10}}, qbt.TorrentFiles{{Name: "Right/01.flac", Size: 100}, {Name: "Right/booklet.pdf", Size: 10}}, []catalogPairTier{catalogPairExact, catalogPairExact}},
		{"case", qbt.TorrentFiles{{Name: "Left/TRACK.FLAC", Size: 100}}, qbt.TorrentFiles{{Name: "Right/track.flac", Size: 100}}, []catalogPairTier{catalogPairBasename}},
		{"normalized", qbt.TorrentFiles{{Name: "Left/Café - Track.flac", Size: 100}}, qbt.TorrentFiles{{Name: "Right/Café.Track.flac", Size: 100}}, []catalogPairTier{catalogPairNormalized}},
		{"unique renamed track", qbt.TorrentFiles{{Name: "Left/new.flac", Size: 100}}, qbt.TorrentFiles{{Name: "Right/old.flac", Size: 100}}, []catalogPairTier{catalogPairSize}},
		{"stronger match first", qbt.TorrentFiles{{Name: "Left/new.flac", Size: 100}, {Name: "Left/exact.flac", Size: 100}}, qbt.TorrentFiles{{Name: "Right/exact.flac", Size: 100}, {Name: "Right/old.flac", Size: 100}}, []catalogPairTier{catalogPairExact, catalogPairSize}},
		{"ambiguous", qbt.TorrentFiles{{Name: "Left/a.flac", Size: 100}, {Name: "Left/b.flac", Size: 100}}, qbt.TorrentFiles{{Name: "Right/c.flac", Size: 100}, {Name: "Right/d.flac", Size: 100}}, nil},
		{"mutual collision", qbt.TorrentFiles{{Name: "Left/CD1/track.flac", Size: 100}, {Name: "Left/CD2/track.flac", Size: 100}}, qbt.TorrentFiles{{Name: "Right/CD3/TRACK.FLAC", Size: 100}, {Name: "Right/other.flac", Size: 100}}, nil},
		{"same total different lengths", qbt.TorrentFiles{{Name: "Left/a.flac", Size: 100}, {Name: "Left/b.flac", Size: 200}}, qbt.TorrentFiles{{Name: "Right/a.flac", Size: 110}, {Name: "Right/b.flac", Size: 190}}, nil},
		{"missing booklet", qbt.TorrentFiles{{Name: "Left/a.flac", Size: 100}, {Name: "Left/booklet.pdf", Size: 10}}, qbt.TorrentFiles{{Name: "Right/a.flac", Size: 110}}, nil},
		{"renamed sidecars of equal size", qbt.TorrentFiles{{Name: "Left/01.flac", Size: 100}, {Name: "Left/rip.cue", Size: 10}, {Name: "Left/rip.log", Size: 10}}, qbt.TorrentFiles{{Name: "Right/01.flac", Size: 100}, {Name: "Right/album.cue", Size: 10}, {Name: "Right/eac.log", Size: 10}}, []catalogPairTier{catalogPairExact, catalogPairSize, catalogPairSize}},
		{"incompatible extension", qbt.TorrentFiles{{Name: "Left/a.flac", Size: 100}}, qbt.TorrentFiles{{Name: "Right/b.mp3", Size: 100}}, nil},
		{"ancestor path collision", qbt.TorrentFiles{{Name: "Left/a.flac", Size: 100}, {Name: "Left/a.flac-extra.flac", Size: 100}, {Name: "Left/a.flac/track.flac", Size: 100}}, qbt.TorrentFiles{{Name: "Right/a.flac", Size: 100}, {Name: "Right/a.flac-extra.flac", Size: 100}, {Name: "Right/a.flac/track.flac", Size: 100}}, nil},
		{"duplicate path", qbt.TorrentFiles{{Name: "Left/a.flac", Size: 100}, {Name: "Left/a.flac", Size: 100}}, qbt.TorrentFiles{{Name: "Right/a.flac", Size: 100}, {Name: "Right/b.flac", Size: 100}}, nil},
		{"traversal", qbt.TorrentFiles{{Name: "../a.flac", Size: 100}}, qbt.TorrentFiles{{Name: "Right/a.flac", Size: 100}}, nil},
		{"windows traversal", qbt.TorrentFiles{{Name: `C:\a.flac`, Size: 100}}, qbt.TorrentFiles{{Name: "Right/a.flac", Size: 100}}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pairs, err := completeCatalogPairing(tt.left, tt.right, "Left", "Right")
			if tt.want == nil {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, pairs, len(tt.want))
			for i, pair := range pairs {
				require.Equal(t, tt.want[i], pair.tier)
				require.Equal(t, tt.left[pair.sourceIndex].Size, tt.right[pair.candidateIndex].Size)
			}
			reversedLeft, reversedRight := slices.Clone(tt.left), slices.Clone(tt.right)
			slices.Reverse(reversedLeft)
			slices.Reverse(reversedRight)
			reversed, err := completeCatalogPairing(reversedLeft, reversedRight, "Left", "Right")
			require.NoError(t, err)
			edges := func(pairs []catalogFilePair) map[string]string {
				out := make(map[string]string)
				for _, pair := range pairs {
					out[pair.sourcePath] = pair.candidatePath
				}
				return out
			}
			require.Equal(t, edges(pairs), edges(reversed))
		})
	}
}

func TestCatalogPairingKeepsFullPathPriority(t *testing.T) {
	files := qbt.TorrentFiles{
		{Name: "Album/CD1/01.flac", Size: 100},
		{Name: "Album/CD1/cover.jpg", Size: 10},
		{Name: "Album/CD2/01.flac", Size: 200},
		{Name: "Album/CD2/cover.jpg", Size: 10},
	}
	pairs, err := completeCatalogPairing(files, slices.Clone(files), detectCommonRoot(files), detectCommonRoot(files))
	require.NoError(t, err)
	require.Len(t, pairs, len(files))
	for _, pair := range pairs {
		require.Equal(t, pair.sourcePath, pair.candidatePath)
		require.Equal(t, catalogPairPath, pair.tier)
	}
}

func TestCatalogAdmissionAndSnapshots(t *testing.T) {
	s := &Service{releaseCache: NewReleaseCache()}
	files := qbt.TorrentFiles{{Name: "Album/01.flac", Size: 100, Progress: 1}, {Name: "Album/booklet.pdf", Size: 10, Progress: 1}}
	source := &qbt.Torrent{Hash: "source", Name: "Mira Calder - Beneath Quiet Skies - 2001 [WEB 2011 FLAC 24-96] - HDtracks", Size: 50, TotalSize: 110}
	match := &gazellemusic.Match{Kind: gazellemusic.MatchSize, Host: "orpheus.network", TorrentID: 7, Size: 110, Title: "Other Artist - Different Album (2002) [FLAC]-OTHER"}
	p, err := s.gazelleDecision(1, source, files, match)
	require.NoError(t, err)
	require.Equal(t, searchCandidateClassFileEvidence, p.Class)
	require.False(t, searchDecisionRequiresVerification(p))
	require.Equal(t, p, p.bindSource(2, "another"))
	require.Equal(t, p, p.clone())
	require.NoError(t, validateCatalogTarget(p, adderV1, files))
	require.NoError(t, validateCatalogTarget(p, adderV2, files), "the torrent ID, not a hash, names a size match")
	require.Error(t, validateCatalogTarget(p, adderV1, files[:1]), "a target of another full size fails")
	require.NoError(t, validateCatalogSource(p, source, files))
	changed := append(qbt.TorrentFiles(nil), files...)
	changed[0].Name = "Album/renamed.flac"
	require.Error(t, validateCatalogSource(p, source, changed))
	read, err := s.readCatalogRelease("Mira Calder - Beneath Quiet Skies (2002) HDTracks 2011 [FLAC 24-96]", files, rls.Music)
	require.NoError(t, err)
	require.Equal(t, rls.Music, read.release.Type)
	require.Zero(t, read.release.Episode)
	require.Empty(t, read.release.SeriesEpisodes())
	require.NotNil(t, read.tagOrigin)
	require.Equal(t, rls.Music, catalogFileFamily(files))
	require.Equal(t, rls.Unknown, catalogFileFamily(append(changed, qbt.TorrentFile{Name: "Sample/hidden.mkv", Size: 1})))
	require.Equal(t, rls.Unknown, catalogFileFamily(qbt.TorrentFiles{{Name: "cover.jpg", Size: 100}}))
	ordinary := s.matcher().matchVerdict(s.matcher().readName(source.Name), s.matcher().readName(match.Title), matchEvidence{SourceSize: 110, CandidateSize: 110}, matchPolicy{})
	require.False(t, ordinary.Matched)
	sourceRead := namedRelease{release: &rls.Release{Type: rls.Music, Title: "Album", Artist: "One", Year: 2001, Group: "FIRST", Sum: "11111111"}, rawName: "Album-FIRST"}
	targetRead := namedRelease{release: &rls.Release{Type: rls.Music, Title: "Album", Artist: "Two", Year: 2002, Group: "SECOND", Sum: "22222222"}, rawName: "Album-SECOND"}
	evidence := matchEvidence{SourceSize: 110, CandidateSize: 110, SourceFiles: files, Catalog: p.Catalog}
	permitted := s.matcher().matchVerdict(sourceRead, targetRead, evidence, matchPolicy{CatalogFiles: true})
	require.True(t, permitted.Matched)
	for _, difference := range []matchDifference{differenceArtist, differenceYear, differenceGroup, differenceChecksum} {
		require.True(t, permitted.Relaxed.has(difference), "difference %v lost", difference)
	}
	require.False(t, s.matcher().matchVerdict(sourceRead, targetRead, evidence, matchPolicy{}).Matched)
	match.Kind, match.TargetHash, match.QueriedHash = gazellemusic.MatchHash, adderV1, adderV1
	hashed, err := s.gazelleDecision(1, source, files, match)
	require.NoError(t, err)
	require.NoError(t, validateCatalogTarget(hashed, adderV1, files))
	require.Error(t, validateCatalogTarget(hashed, adderV2, files))
	sized := p
	sized.Catalog.TargetHash = adderV1
	require.False(t, sized.Catalog.valid(), "a size match carries no target hash")
	match.Size = 50
	_, err = s.gazelleDecision(1, source, files, match)
	require.Error(t, err, "wanted bytes cannot authorize a full torrent")
}
