// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestProcessRootSearchee_SkipsItemAnotherRunFinished(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	_, store, dir := newDirScanTestService(t)

	newRoot := func(name string) *Searchee {
		return &Searchee{
			Name:  name,
			Path:  filepath.Join(dir.Path, name),
			Files: []*ScannedFile{{Path: filepath.Join(dir.Path, name, "movie.mkv"), RelPath: "movie.mkv", Size: 1000, ModTime: time.Now()}},
		}
	}
	finished := newRoot("Finished.Movie.1080p.WEB-DL")
	pending := newRoot("Pending.Movie.1080p.WEB-DL")
	scanResult := &ScanResult{Searchees: []*Searchee{finished, pending}}

	parser := NewParser(nil)
	searcher := &timedSearcher{}
	svc := &Service{store: store, parser: parser, searcher: NewSearcher(searcher, parser)}
	l := zerolog.New(io.Discard)

	// The run takes its snapshot while both items are pending.
	trackedFiles, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, scanResult, &seedingIndex{}, &l)
	require.NoError(t, err)
	enabled := map[int]struct{}{1: {}}
	work := selectEligibleRootWork(scanResult, trackedFiles, parser, 0, time.Now(), enabled, false, &l)
	require.Len(t, work.roots, 2)

	// Another run then finishes one of them.
	require.NoError(t, svc.upsertDirScanFileStatus(ctx, dir.ID, finished.Files[0], dirScanFileUpdate{
		status:             models.DirScanFileStatusMatched,
		matchedTorrentHash: "0123456789abcdef0123456789abcdef01234567",
	}))

	settings := &models.DirScanSettings{MatchMode: models.MatchModeStrict}
	for _, root := range work.roots {
		svc.processRootSearchee(ctx, dir, root.root, root.items, &seedingIndex{}, trackedFiles,
			map[tvGroupKey]struct{}{}, settings, NewMatcher(MatchModeStrict, 0), 1, 0, 0, enabled, &l)
	}

	require.Equal(t, []string{"Pending Movie"}, searcher.queries, "only the pending item searches")

	files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
	require.NoError(t, err)
	statuses := map[string]models.DirScanFileStatus{}
	for _, f := range files {
		statuses[f.FilePath] = f.Status
	}
	require.Equal(t, models.DirScanFileStatusMatched, statuses[finished.Files[0].Path], "the other run's result stays")
	require.Equal(t, models.DirScanFileStatusNoMatch, statuses[pending.Files[0].Path])
}
