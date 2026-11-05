// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
)

// recordingBackend answers with a canned walk, in the slash dialect unless d
// names another, and records every path it is asked to stat, alone or in a
// batch, so a test can see the exact bytes the caller built.
type recordingBackend struct {
	fsops.Backend
	d       fsops.PathDialect
	walk    []fsops.WalkEntry
	stated  []string
	batches [][]string
}

func newRecordingBackend(walk ...fsops.WalkEntry) *recordingBackend {
	return &recordingBackend{Backend: localbackend.NewBackend(), walk: walk}
}

func (b *recordingBackend) Paths() fsops.PathDialect {
	if b.d != nil {
		return b.d
	}
	return fsops.SlashPaths
}

func (b *recordingBackend) WalkDir(context.Context, string, fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	ch := make(chan fsops.WalkEntry, len(b.walk))
	for _, e := range b.walk {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func (b *recordingBackend) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	b.stated = append(b.stated, p)
	return &fsops.LstatInfo{Path: p, Size: 4, Nlinks: 1}, nil
}

func (b *recordingBackend) StatBatch(_ context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	b.batches = append(b.batches, paths)
	infos := make([]*fsops.LstatInfo, len(paths))
	for i, p := range paths {
		infos[i] = &fsops.LstatInfo{Path: p, Size: 4, Nlinks: 1}
	}
	return infos, make([]error, len(paths))
}

func (b *recordingBackend) LstatBatch(ctx context.Context, paths []string) ([]*fsops.LstatInfo, []error) {
	return b.StatBatch(ctx, paths)
}

func slashEntry(p, rel string, mode fs.FileMode) fsops.WalkEntry {
	return fsops.WalkEntry{
		RelPath:   rel,
		Path:      p,
		Mode:      mode,
		IsDir:     mode.IsDir(),
		IsSymlink: mode&fs.ModeSymlink != 0,
		Size:      1,
	}
}

// On any host, a scan of a remote root groups the walk and builds paths in
// the backend's dialect, and reads every root-level link in one batch.
func TestScanDirectoryUsesBackendDialect(t *testing.T) {
	t.Parallel()

	b := newRecordingBackend(
		slashEntry("/data/torrents", ".", fs.ModeDir),
		slashEntry("/data/torrents/Linked.2024.1080p.WEB.x264-GRP.mkv", "Linked.2024.1080p.WEB.x264-GRP.mkv", fs.ModeSymlink),
		slashEntry("/data/torrents/Other.2023.1080p.WEB.x264-GRP.mkv", "Other.2023.1080p.WEB.x264-GRP.mkv", fs.ModeSymlink),
		slashEntry("/data/torrents/Show.S01", "Show.S01", fs.ModeDir),
		slashEntry("/data/torrents/Show.S01/e01.mkv", "Show.S01/e01.mkv", 0),
	)
	result, err := NewScanner(b).ScanDirectory(context.Background(), "/data/torrents/")
	require.NoError(t, err)

	require.Empty(t, b.stated)
	require.Equal(t, [][]string{{
		"/data/torrents/Linked.2024.1080p.WEB.x264-GRP.mkv",
		"/data/torrents/Other.2023.1080p.WEB.x264-GRP.mkv",
	}}, b.batches)
	require.Len(t, result.Searchees, 3)
	require.Equal(t, "/data/torrents/Linked.2024.1080p.WEB.x264-GRP.mkv", result.Searchees[0].Path)
	require.EqualValues(t, 4, result.Searchees[0].Files[0].Size, "a link carries its target's metadata")
	require.Equal(t, "/data/torrents/Show.S01", result.Searchees[2].Path)
	require.Equal(t, "e01.mkv", result.Searchees[2].Files[0].RelPath)
}

func TestFileIDIndexUsesBackendDialect(t *testing.T) {
	t.Parallel()

	for _, tt := range dialects() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := newRecordingBackend()
			b.d = tt.d
			index := &seedingIndex{byKey: map[fsops.FileKey]string{}}
			files := qbt.TorrentFiles{{Name: "Show.S01/episode.mkv"}, {Name: "Show.S01/sample/s.mkv"}}
			savePath := dialectPath(tt.d, "/data/torrents")
			statErrors, err := addTorrentsToFileIDIndex(context.Background(), index, map[string]qbt.TorrentFiles{"hash": files}, map[string]string{"hash": savePath}, b)

			require.NoError(t, err)
			require.Zero(t, statErrors)
			require.Equal(t, [][]string{
				{savePath},
				{dialectPath(tt.d, "/data/torrents/Show.S01/episode.mkv"), dialectPath(tt.d, "/data/torrents/Show.S01/sample/s.mkv")},
			}, b.batches)
			// The fake reports no identity, so nothing is indexed. The paths are the point.
			require.Empty(t, index.byKey)
		})
	}
}

// backslashPaths is Windows grammar on any GOOS, so a host filepath left on a backend path fails on Linux too.
type backslashPaths struct{}

func (backslashPaths) FromSlash(p string) string { return strings.ReplaceAll(p, "/", `\`) }
func (backslashPaths) ToSlash(p string) string   { return strings.ReplaceAll(p, `\`, "/") }
func (backslashPaths) Separator() string         { return `\` }
func (backslashPaths) IsAbs(p string) bool       { return len(p) > 2 && p[1] == ':' }

// Join leaves a slash in name alone, so a caller that skipped FromSlash shows up.
func (backslashPaths) Join(base, name string) string {
	return strings.TrimRight(base, `\`) + `\` + name
}

func (d backslashPaths) Dir(p string) string   { return d.FromSlash(path.Dir(d.ToSlash(p))) }
func (d backslashPaths) Base(p string) string  { return d.FromSlash(path.Base(d.ToSlash(p))) }
func (d backslashPaths) Clean(p string) string { return d.FromSlash(path.Clean(d.ToSlash(p))) }

func (backslashPaths) Rel(string, string) (string, error) {
	return "", errors.ErrUnsupported
}

func dialects() []struct {
	name string
	d    fsops.PathDialect
} {
	return []struct {
		name string
		d    fsops.PathDialect
	}{{"slash", fsops.SlashPaths}, {"backslash", backslashPaths{}}}
}

// dialectPath is a slash-written path in the grammar of d.
func dialectPath(d fsops.PathDialect, slashPath string) string {
	if _, ok := d.(backslashPaths); ok {
		return `D:` + d.FromSlash(slashPath)
	}
	return slashPath
}

func TestBuildEpisodeSearcheeUsesBackendDialect(t *testing.T) {
	t.Parallel()

	for _, tt := range dialects() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file := &ScannedFile{Path: dialectPath(tt.d, "/data/Show/Synthetic.Show.S01E01.1080p.WEB.x264-GRP.mkv")}

			got := buildEpisodeSearchee(tt.d, file)

			require.Equal(t, "Synthetic.Show.S01E01.1080p.WEB.x264-GRP", got.Name)
			require.Equal(t, dialectPath(tt.d, "/data/Show"), got.Path)
			require.Equal(t, "Synthetic.Show.S01E01.1080p.WEB.x264-GRP.mkv", got.Files[0].RelPath)
		})
	}
}

func TestIsInGroupDirUsesBackendDialect(t *testing.T) {
	t.Parallel()

	for _, tt := range dialects() {
		for _, row := range []struct {
			name, dir, file string
			want            bool
		}{
			{name: "file in the folder", dir: "/data/Show", file: "/data/Show/e01.mkv", want: true},
			{name: "the folder itself", dir: "/data/Show", file: "/data/Show", want: true},
			{name: "group folder has a trailing separator", dir: "/data/Show/", file: "/data/Show/e01.mkv", want: true},
			{name: "file climbs out of the folder", dir: "/data/Show", file: "/data/Show/../Other/e01.mkv"},
			{name: "sibling with the same prefix", dir: "/data/Show", file: "/data/Showroom/e01.mkv"},
		} {
			t.Run(tt.name+"/"+row.name, func(t *testing.T) {
				t.Parallel()
				group := &tvGroup{parentDirs: []string{dialectPath(tt.d, row.dir)}}

				require.Equal(t, row.want, group.isInGroupDir(tt.d, dialectPath(tt.d, row.file)))
			})
		}
	}
}

// One folder of two episodes yields a season search over both and an episode search naming the folder.
func TestBuildSearcheeWorkItemsUsesBackendDialect(t *testing.T) {
	t.Parallel()

	for _, tt := range dialects() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := &Searchee{Name: "Show", Path: dialectPath(tt.d, "/data/Show"), Files: []*ScannedFile{
				{Path: dialectPath(tt.d, "/data/Show/Synthetic.Show.S01E01.1080p.WEB.x264-GRP.mkv")},
				{Path: dialectPath(tt.d, "/data/Show/Synthetic.Show.S01E02.1080p.WEB.x264-GRP.mkv")},
				{Path: dialectPath(tt.d, "/data/Other/notes.nfo")},
			}}

			items := buildSearcheeWorkItems(tt.d, root, NewParser(nil))

			names := make([]string, 0, len(items))
			var season *Searchee
			for _, item := range items {
				names = append(names, item.searchee.Name)
				if !item.isEpisode {
					season = item.searchee
					continue
				}
				require.Equal(t, dialectPath(tt.d, "/data/Show"), item.searchee.Path)
			}
			require.ElementsMatch(t, []string{
				"Synthetic Show S01",
				"Synthetic.Show.S01E01.1080p.WEB.x264-GRP",
				"Synthetic.Show.S01E02.1080p.WEB.x264-GRP",
			}, names)
			require.NotNil(t, season)
			require.Len(t, season.Files, 2, "the season search leaves out the file in the other folder")
		})
	}
}

func TestSelectEligibleRootWorkUsesBackendDialect(t *testing.T) {
	t.Parallel()

	for _, tt := range dialects() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := &Searchee{Name: "Show", Path: dialectPath(tt.d, "/data/Show"), Files: []*ScannedFile{
				{Path: dialectPath(tt.d, "/data/Show/Synthetic.Show.S01E01.1080p.WEB.x264-GRP.mkv")},
				{Path: dialectPath(tt.d, "/data/Show/Synthetic.Show.S01E02.1080p.WEB.x264-GRP.mkv")},
			}}

			selection := selectEligibleRootWork(tt.d, &ScanResult{Searchees: []*Searchee{root}}, nil, NewParser(nil), 0, time.Now(), nil, false, nil)

			require.Len(t, selection.roots, 1)
			require.Len(t, selection.roots[0].items, 3, "a season search and one search per episode")
			for _, item := range selection.roots[0].items {
				if item.isEpisode {
					require.Equal(t, dialectPath(tt.d, "/data/Show"), item.searchee.Path)
				}
			}
		})
	}
}

func TestBuildSearcheeMetadataUsesBackendDialect(t *testing.T) {
	t.Parallel()

	for _, tt := range dialects() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := &Service{parser: NewParser(nil)}
			searchee := &Searchee{Name: "Disc 1", Files: []*ScannedFile{{
				Path: dialectPath(tt.d, "/data/Disc 1/Synthetic.Film.2024.1080p.WEB.x264-GRP.mkv"),
				Size: 1,
			}}}

			_, lookupName := svc.buildSearcheeMetadata(tt.d, searchee)

			require.Equal(t, "Synthetic.Film.2024.1080p.WEB.x264-GRP", lookupName)
		})
	}
}

// dialectDirBackend reads every path as a directory, in dialect d.
type dialectDirBackend struct {
	fsops.Backend
	d fsops.PathDialect
}

func (b dialectDirBackend) Paths() fsops.PathDialect { return b.d }

func (b dialectDirBackend) Stat(_ context.Context, p string) (*fsops.LstatInfo, error) {
	return &fsops.LstatInfo{Path: p, IsDir: true, Mode: fs.ModeDir}, nil
}

// regression: #2919
func TestCalculateSavePathUsesBackendDialect(t *testing.T) {
	t.Parallel()

	rooted := &ParsedTorrent{Files: []TorrentFile{{Path: "Root/a.mkv", Size: 1}}}
	rootless := &ParsedTorrent{Files: []TorrentFile{{Path: "a.mkv", Size: 1}}}
	for _, tt := range dialects() {
		for _, row := range []struct {
			name   string
			parsed *ParsedTorrent
			prefix string
			want   func(d fsops.PathDialect) string
		}{
			{
				name:   "rooted torrent lands in the parent",
				parsed: rooted,
				want:   func(d fsops.PathDialect) string { return dialectPath(d, "/data") },
			},
			{
				name:   "rootless torrent lands in the folder",
				parsed: rootless,
				want:   func(d fsops.PathDialect) string { return dialectPath(d, "/data/Show") },
			},
			{
				name:   "path mapping replaces the parent",
				parsed: rootless,
				prefix: "/downloads",
				want:   func(d fsops.PathDialect) string { return "/downloads" + d.Separator() + "Show" },
			},
		} {
			t.Run(tt.name+"/"+row.name, func(t *testing.T) {
				t.Parallel()
				injector := &Injector{}
				req := &InjectRequest{
					ParsedTorrent:  row.parsed,
					Searchee:       &Searchee{Path: dialectPath(tt.d, "/data/Show")},
					QbitPathPrefix: row.prefix,
				}

				got := injector.calculateSavePath(context.Background(), dialectDirBackend{d: tt.d}, req)

				require.Equal(t, row.want(tt.d), got)
			})
		}
	}
}

func TestBuildAlignmentPlanUsesBackendDialect(t *testing.T) {
	t.Parallel()

	match := func(rel, torrentPath string) *MatchResult {
		return &MatchResult{MatchedFiles: []MatchedFilePair{{
			SearcheeFile: &ScannedFile{RelPath: rel, Size: 1},
			TorrentFile:  TorrentFile{Path: torrentPath, Size: 1},
		}}}
	}
	t.Run("backslash folder and relative path", func(t *testing.T) {
		t.Parallel()
		req := &InjectRequest{
			ParsedTorrent: &ParsedTorrent{Files: []TorrentFile{{Path: "Root/sub/a.mkv", Size: 1}}},
			Searchee:      &Searchee{Path: `D:\data\Show\`},
			MatchResult:   match(`sub\b.mkv`, "Root/sub/a.mkv"),
		}

		plan := buildAlignmentPlan(backslashPaths{}, req, true)

		require.Equal(t, "Show", plan.targetRoot)
		require.Equal(t, "Root/sub/b.mkv", plan.fileRenames[0].NewPath)
	})
	t.Run("backslash root has no folder to rename to", func(t *testing.T) {
		t.Parallel()
		req := &InjectRequest{
			ParsedTorrent: &ParsedTorrent{Files: []TorrentFile{{Path: "Root/a.mkv", Size: 1}}},
			Searchee:      &Searchee{Path: `\`},
			MatchResult:   match("a.mkv", "Root/a.mkv"),
		}

		require.False(t, buildAlignmentPlan(backslashPaths{}, req, true).needed())
	})
	t.Run("backslash in a slash name stays part of the name", func(t *testing.T) {
		t.Parallel()
		req := &InjectRequest{
			ParsedTorrent: &ParsedTorrent{Files: []TorrentFile{{Path: "Root/a.mkv", Size: 1}}},
			Searchee:      &Searchee{Path: "/data/Show"},
			MatchResult:   match(`b\c.mkv`, "Root/a.mkv"),
		}

		plan := buildAlignmentPlan(fsops.SlashPaths, req, true)

		require.Equal(t, "Show", plan.targetRoot)
		require.Equal(t, `Root/b\c.mkv`, plan.fileRenames[0].NewPath)
	})
}
