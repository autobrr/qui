// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

// plannedSearch is one search that work selection plans, as the user sees it.
type plannedSearch struct {
	name    string
	files   int
	size    int64
	episode bool
}

func planSearches(t *testing.T, result *ScanResult, skipEpisodes bool) []plannedSearch {
	t.Helper()
	selection := selectEligibleRootWork(result, nil, NewParser(nil), 0, time.Now(), nil, skipEpisodes, nil)
	var planned []plannedSearch
	for _, root := range selection.roots {
		for _, item := range root.items {
			planned = append(planned, plannedSearch{
				name:    item.searchee.Name,
				files:   len(item.searchee.Files),
				size:    CalculateTotalSize(item.searchee),
				episode: item.isEpisode,
			})
		}
	}
	return planned
}

func scanFolder(t *testing.T, root string) *ScanResult {
	t.Helper()
	result, err := NewScanner(local.NewBackend()).ScanDirectory(t.Context(), root)
	require.NoError(t, err)
	return result
}

func TestArchivedRelease_ScanPlansOneSearchForTheWholeFolder(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "Show.Name.S01E01.720p.HDTV.x264-GRP")
	writeTestFile(t, filepath.Join(release, "show.name.s01e01.720p-grp.rar"), 100)
	writeTestFile(t, filepath.Join(release, "show.name.s01e01.720p-grp.r00"), 100)
	writeTestFile(t, filepath.Join(release, "show.name.s01e01.720p-grp.r01"), 40)
	writeTestFile(t, filepath.Join(release, "show.name.s01e01.720p-grp.nfo"), 3)
	writeTestFile(t, filepath.Join(release, "show.name.s01e01.720p-grp.sfv"), 2)
	// The sample is larger than one volume and reads as an episode. Only its
	// folder marks it as a sample.
	writeTestFile(t, filepath.Join(release, "Sample", "show.name.s01e01.720p-grp.mkv"), 150)
	writeTestFile(t, filepath.Join(release, "Proof", "show.name.s01e01.720p-grp-proof.jpg"), 5)

	result := scanFolder(t, root)

	require.Equal(t, []plannedSearch{{name: "Show.Name.S01E01.720p.HDTV.x264-GRP", files: 7, size: 400, episode: true}}, planSearches(t, result, false))
	// "Skip individual episodes" drops the release, since its name reads as an episode.
	require.Empty(t, planSearches(t, result, true))
}

func TestArchivedRelease_MediaFoldersStillDropSmallArchives(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "Artist-Album-2024-GRP", "01-artist-track.flac"), 50)
	writeTestFile(t, filepath.Join(root, "Artist-Album-2024-GRP", "02-artist-track.flac"), 50)
	writeTestFile(t, filepath.Join(root, "Artist-Album-2024-GRP", "artwork.zip"), 30)
	writeTestFile(t, filepath.Join(root, "Example.Movie.2024.1080p.BluRay.x264-GRP", "example.mkv"), 200)
	writeTestFile(t, filepath.Join(root, "Example.Movie.2024.1080p.BluRay.x264-GRP", "subs.rar"), 10)
	// A loose archive at the top of the scan root is not a release.
	writeTestFile(t, filepath.Join(root, "old-backup.zip"), 500)

	require.Equal(t, []plannedSearch{
		{name: "Artist-Album-2024-GRP", files: 2, size: 100},
		{name: "Example.Movie.2024.1080p.BluRay.x264-GRP", files: 1, size: 200},
	}, planSearches(t, scanFolder(t, root), false))
}

func TestArchivedRelease_SeasonFolderSplitsIntoEpisodesAndPack(t *testing.T) {
	root := t.TempDir()
	season := filepath.Join(root, "Show.Name.S02.720p.HDTV.x264-GRP")
	for _, ep := range []string{"Show.Name.S02E01.720p.HDTV.x264-GRP", "Show.Name.S02E02.720p.HDTV.x264-GRP"} {
		writeTestFile(t, filepath.Join(season, ep, "show.rar"), 100)
		writeTestFile(t, filepath.Join(season, ep, "show.r00"), 60)
		writeTestFile(t, filepath.Join(season, ep, "Sample", "show-sample.mkv"), 5)
	}
	writeTestFile(t, filepath.Join(season, "show.name.s02.nfo"), 1)
	// Subs/, Sample/, and CD1/ and CD2/ stay part of their release.
	multi := filepath.Join(root, "Example.Movie.2003.DVDRip.XviD-GRP")
	writeTestFile(t, filepath.Join(multi, "CD1", "movie.rar"), 100)
	writeTestFile(t, filepath.Join(multi, "CD1", "movie.r00"), 100)
	writeTestFile(t, filepath.Join(multi, "CD2", "movie.rar"), 100)
	writeTestFile(t, filepath.Join(multi, "CD2", "movie.r00"), 100)
	writeTestFile(t, filepath.Join(multi, "Subs", "movie.subs.rar"), 4)

	result := scanFolder(t, root)

	require.Equal(t, []plannedSearch{
		{name: "Example.Movie.2003.DVDRip.XviD-GRP", files: 5, size: 404},
		{name: "Show.Name.S02.720p.HDTV.x264-GRP", files: 7, size: 331},
		{name: "Show.Name.S02E01.720p.HDTV.x264-GRP", files: 3, size: 165, episode: true},
		{name: "Show.Name.S02E02.720p.HDTV.x264-GRP", files: 3, size: 165, episode: true},
	}, planSearches(t, result, false))

	// The episode searches keep paths below their own folder, so a volume
	// pairs with the volume of an episode torrent.
	episode := buildSearcheeWorkItems(result.Searchees[1], NewParser(nil))[1].searchee
	require.True(t, episode.IsArchived)
	require.Equal(t, filepath.Join(season, "Show.Name.S02E01.720p.HDTV.x264-GRP"), episode.Path)
	rels := make([]string, 0, len(episode.Files))
	for _, f := range episode.Files {
		rels = append(rels, f.RelPath)
	}
	require.ElementsMatch(t, []string{"show.rar", "show.r00", filepath.Join("Sample", "show-sample.mkv")}, rels)

	// "Skip individual episodes" drops the episode searches and keeps the pack.
	require.Equal(t, []plannedSearch{
		{name: "Example.Movie.2003.DVDRip.XviD-GRP", files: 5, size: 404},
		{name: "Show.Name.S02.720p.HDTV.x264-GRP", files: 7, size: 331},
	}, planSearches(t, result, true))
}

func TestArchivedRelease_AgeCutoffReadsTheVolumes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -30)
	fresh := now.Add(-time.Hour)
	release := func(name string, newestVolume time.Time) *Searchee {
		dir := "/data/" + name
		return &Searchee{Name: name, Path: dir, IsArchived: true, Files: []*ScannedFile{
			{Path: dir + "/game.rar", RelPath: "game.rar", Size: 100, ModTime: old},
			{Path: dir + "/game.r00", RelPath: "game.r00", Size: 100, ModTime: newestVolume},
			{Path: dir + "/game.nfo", RelPath: "game.nfo", Size: 1, ModTime: fresh},
			// A fresh sample does not keep an old release in scope.
			{Path: dir + "/Sample/game.mkv", RelPath: filepath.Join("Sample", "game.mkv"), Size: 5, ModTime: fresh},
		}}
	}
	result := &ScanResult{Searchees: []*Searchee{
		release("Old.Game-GRP", old),
		release("New.Game-GRP", fresh),
	}}

	selection := selectEligibleRootWork(result, nil, NewParser(nil), 7, now, nil, false, nil)

	require.Len(t, selection.roots, 1)
	require.Equal(t, "New.Game-GRP", selection.roots[0].root.Name)
}

func TestArchivedRelease_WebhookRootIsOneSearch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Example.Game-GRP")
	writeTestFile(t, filepath.Join(root, "example.game-grp.rar"), 100)
	writeTestFile(t, filepath.Join(root, "example.game-grp.u95"), 100)
	writeTestFile(t, filepath.Join(root, "example.game-grp.nfo"), 2)
	writeTestFile(t, filepath.Join(root, "Sample", "example.game-grp.mkv"), 150)

	scanner := NewScanner(local.NewBackend())
	scanner.rootMayBeRelease = true
	result, err := scanner.ScanDirectory(t.Context(), root)
	require.NoError(t, err)

	require.Len(t, result.Searchees, 1)
	got := result.Searchees[0]
	require.Equal(t, "Example.Game-GRP", got.Name)
	require.Equal(t, root, got.Path)
	require.True(t, got.IsArchived)
	rels := make([]string, 0, len(got.Files))
	for _, f := range got.Files {
		rels = append(rels, f.RelPath)
	}
	require.ElementsMatch(t, []string{"example.game-grp.rar", "example.game-grp.u95", "example.game-grp.nfo", filepath.Join("Sample", "example.game-grp.mkv")}, rels)
}

func TestArchivedRelease_WebhookRootOfSeveralReleasesKeepsThemApart(t *testing.T) {
	// Two queued webhooks for two games merge into their parent folder.
	root := filepath.Join(t.TempDir(), "games")
	for _, game := range []string{"First.Game-GRP", "Second.Game-GRP"} {
		writeTestFile(t, filepath.Join(root, game, "game.rar"), 100)
		writeTestFile(t, filepath.Join(root, game, "game.r00"), 100)
	}
	writeTestFile(t, filepath.Join(root, "readme.txt"), 1)

	scanner := NewScanner(local.NewBackend())
	scanner.rootMayBeRelease = true
	result, err := scanner.ScanDirectory(t.Context(), root)
	require.NoError(t, err)

	require.Equal(t, []plannedSearch{
		{name: "First.Game-GRP", files: 2, size: 200},
		{name: "Second.Game-GRP", files: 2, size: 200},
	}, planSearches(t, result, false))
}

func TestArchivedRelease_WebhookScanTracksEveryFileOfTheRelease(t *testing.T) {
	ctx := t.Context()
	svc, store, dir := newDirScanTestService(t)
	imported := filepath.Join(dir.Path, "Example.Game-GRP")
	writeTestFile(t, filepath.Join(imported, "example.game-grp.rar"), 100)
	writeTestFile(t, filepath.Join(imported, "example.game-grp.r00"), 100)
	writeTestFile(t, filepath.Join(imported, "example.game-grp.nfo"), 2)
	svc.backendPool = fsops.NewPool(&fakeInstanceStore{instance: &models.Instance{ID: dir.TargetInstanceID, HasLocalFilesystemAccess: true}}, local.NewBackend())

	runID, err := store.CreateRun(ctx, dir.ID, triggeredByWebhook, imported)
	require.NoError(t, err)
	svc.executeScan(ctx, dir.ID, runID)

	files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
	require.NoError(t, err)
	require.Len(t, files, 3)
}

func archivedSearchee(files ...*ScannedFile) *Searchee {
	return &Searchee{Name: "Example.Movie.2003.DVDRip.XviD-GRP", Path: "/data/Example.Movie.2003.DVDRip.XviD-GRP", Files: files, IsArchived: true}
}

func diskFile(rel string, size int64) *ScannedFile {
	return &ScannedFile{Path: "/data/Example.Movie.2003.DVDRip.XviD-GRP/" + rel, RelPath: filepath.FromSlash(rel), Size: size}
}

func TestArchivedRelease_MatchVerdicts(t *testing.T) {
	t.Parallel()

	partialOff := &models.DirScanSettings{AllowPartial: false}
	torrent := func(files ...TorrentFile) *ParsedTorrent { return &ParsedTorrent{Files: files, PieceLength: 16} }

	tests := []struct {
		name     string
		mode     MatchMode
		searchee *Searchee
		torrent  *ParsedTorrent
		perfect  bool
		matched  int
		accept   bool
	}{
		{
			name:     "strict mode with the same volumes is a full match",
			mode:     MatchModeStrict,
			searchee: archivedSearchee(diskFile("movie.rar", 100), diskFile("movie.r00", 100), diskFile("movie.nfo", 2)),
			torrent:  torrent(TorrentFile{Path: "Example.Movie.2003.DVDRip.XviD-GRP/movie.rar", Size: 100}, TorrentFile{Path: "Example.Movie.2003.DVDRip.XviD-GRP/movie.r00", Size: 100}, TorrentFile{Path: "Example.Movie.2003.DVDRip.XviD-GRP/movie.nfo", Size: 2}),
			perfect:  true, matched: 3, accept: true,
		},
		{
			name:     "volumes pair when only the case of the extension differs",
			mode:     MatchModeStrict,
			searchee: archivedSearchee(diskFile("movie.RAR", 100), diskFile("movie.R00", 100)),
			torrent:  torrent(TorrentFile{Path: "Example.Movie.2003.DVDRip.XviD-GRP/movie.rar", Size: 100}, TorrentFile{Path: "Example.Movie.2003.DVDRip.XviD-GRP/movie.r00", Size: 100}),
			perfect:  true, matched: 2, accept: true,
		},
		{
			name:     "flexible mode never pairs renamed volumes by size",
			mode:     MatchModeFlexible,
			searchee: archivedSearchee(diskFile("movie.rar", 100), diskFile("movie.r00", 100)),
			torrent:  torrent(TorrentFile{Path: "Other.Name/other.rar", Size: 100}, TorrentFile{Path: "Other.Name/other.r00", Size: 100}),
			matched:  0,
		},
		{
			name:     "a renamed top folder is still a full match",
			mode:     MatchModeFlexible,
			searchee: archivedSearchee(diskFile("CD1/movie.r00", 100), diskFile("CD2/movie.r00", 100)),
			torrent:  torrent(TorrentFile{Path: "Renamed.Folder/CD2/movie.r00", Size: 100}, TorrentFile{Path: "Renamed.Folder/CD1/movie.r00", Size: 100}),
			perfect:  true, matched: 2, accept: true,
		},
		{
			name:     "extra files on disk do not block an archived release",
			mode:     MatchModeStrict,
			searchee: archivedSearchee(diskFile("movie.rar", 100), diskFile("Thumbs.db", 1), diskFile("@eaDir/movie.rar@SynoEAStream", 1)),
			torrent:  torrent(TorrentFile{Path: "Example.Movie.2003.DVDRip.XviD-GRP/movie.rar", Size: 100}),
			perfect:  true, matched: 1, accept: true,
		},
		{
			name: "the same extra files keep a media folder partial",
			mode: MatchModeStrict,
			searchee: &Searchee{Name: "Example.Movie.2003", Path: "/data/Example.Movie.2003", Files: []*ScannedFile{
				diskFile("movie.mkv", 100), diskFile("Thumbs.db", 1),
			}},
			torrent: torrent(TorrentFile{Path: "Example.Movie.2003/movie.mkv", Size: 100}),
			matched: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			match := NewMatcher(tt.mode, 0).Match(tt.searchee, tt.torrent.Files)
			require.Equal(t, tt.perfect, match.IsPerfectMatch)
			require.Len(t, match.MatchedFiles, tt.matched)
			require.Equal(t, tt.accept, shouldAcceptDirScanMatch(match, tt.torrent, partialOff).Accept)
		})
	}
}

func TestArchivedRelease_VolumesPairByTheirPath(t *testing.T) {
	t.Parallel()

	searchee := archivedSearchee(diskFile("CD1/movie.r00", 100), diskFile("CD2/movie.r00", 100))
	files := []TorrentFile{
		{Path: "Example.Movie.2003.DVDRip.XviD-GRP/CD2/movie.r00", Size: 100},
		{Path: "Example.Movie.2003.DVDRip.XviD-GRP/CD1/movie.r00", Size: 100},
	}

	for _, mode := range []MatchMode{MatchModeStrict, MatchModeFlexible} {
		match := NewMatcher(mode, 0).Match(searchee, files)
		require.Len(t, match.MatchedFiles, 2)
		for _, pair := range match.MatchedFiles {
			require.Equal(t, "Example.Movie.2003.DVDRip.XviD-GRP/"+filepath.ToSlash(pair.SearcheeFile.RelPath), pair.TorrentFile.Path, "mode %s", mode)
		}
	}
}

func TestArchivedRelease_SearchMetadataReadsTheFolderName(t *testing.T) {
	t.Parallel()

	svc := &Service{parser: NewParser(nil)}
	tests := []struct {
		name        string
		contentType string
	}{
		{name: "Example.Movie.2003.DVDRip.XviD-GRP", contentType: "movie"},
		{name: "Example.Game-RUNE", contentType: "game"},
		// A video file must not turn a music name into a movie or TV search.
		{name: "Artist-Album-WEB-2024-GRP", contentType: "music"},
	}
	for _, tt := range tests {
		searchee := &Searchee{Name: tt.name, Path: "/data/" + tt.name, IsArchived: true, Files: []*ScannedFile{
			{Path: "/data/" + tt.name + "/x.rar", RelPath: "x.rar", Size: 100},
			// A sample that reads as music and as a TV episode.
			{Path: "/data/" + tt.name + "/Sample/Artist-Track.S01E01-sample.mkv", RelPath: filepath.Join("Sample", "Artist-Track.S01E01-sample.mkv"), Size: 5},
		}}

		meta, arrLookupName := svc.buildSearcheeMetadata(searchee)
		info := determineContentInfo(meta, searchee)

		require.Equal(t, tt.name, arrLookupName)
		require.Equal(t, tt.contentType, info.ContentType, tt.name)
	}
}

func TestArchivedRelease_WebhookRootWithDiscFoldersIsOneSearch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Example.Movie.2003.DVDRip.XviD-GRP")
	writeTestFile(t, filepath.Join(root, "movie.nfo"), 2)
	for _, cd := range []string{"CD1", "CD2"} {
		writeTestFile(t, filepath.Join(root, cd, "movie.rar"), 100)
		writeTestFile(t, filepath.Join(root, cd, "movie.r00"), 100)
	}

	scanner := NewScanner(local.NewBackend())
	scanner.rootMayBeRelease = true
	result, err := scanner.ScanDirectory(t.Context(), root)
	require.NoError(t, err)

	require.Equal(t, []plannedSearch{{name: "Example.Movie.2003.DVDRip.XviD-GRP", files: 5, size: 402}}, planSearches(t, result, false))
}

func TestArchivedRelease_AddedPackSkipsItsEpisodeSearches(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 16, 13, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -30)
	fresh := now.Add(-time.Hour)
	season := "/data/Show.Name.S02.720p.HDTV.x264-GRP"
	file := func(rel string, mod time.Time) *ScannedFile {
		return &ScannedFile{Path: season + "/" + rel, RelPath: filepath.FromSlash(rel), Size: 100, ModTime: mod}
	}
	root := &Searchee{Name: "Show.Name.S02.720p.HDTV.x264-GRP", Path: season, IsArchived: true, Files: []*ScannedFile{
		file("Show.Name.S02E01.720p.HDTV.x264-GRP/show.rar", old),
		file("Show.Name.S02E02.720p.HDTV.x264-GRP/show.rar", fresh),
		// The pack also covers an episode folder of another season.
		file("Show.Name.S03E01.720p.HDTV.x264-GRP/show.rar", fresh),
	}}

	// The pack stays in scope while its newest volume is fresh.
	selection := selectEligibleRootWork(&ScanResult{Searchees: []*Searchee{root}}, nil, NewParser(nil), 7, now, nil, false, nil)
	require.Len(t, selection.roots, 1)
	items := selection.roots[0].items
	require.Equal(t, root.Name, items[0].searchee.Name)

	injected := map[tvGroupKey]struct{}{}
	markTVGroupInjected(injected, items[0].tvGroup)
	for _, item := range items[1:] {
		require.True(t, shouldSkipWorkItemTVGroup(item, injected), item.searchee.Name)
	}
}
