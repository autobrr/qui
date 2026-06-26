// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"testing"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

func TestShouldAcceptDirScanMatch_PiecePercentThreshold(t *testing.T) {
	parsed := &ParsedTorrent{
		Name:        "Example",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "Example/a.bin", Size: 8, Offset: 0},
			{Path: "Example/b.bin", Size: 8, Offset: 8},
		},
		TotalSize: 16,
	}

	match := &MatchResult{
		MatchedFiles: []MatchedFilePair{
			{TorrentFile: TorrentFile{Path: "Example/a.bin", Size: 8}},
			{TorrentFile: TorrentFile{Path: "Example/b.bin", Size: 4}},
		},
		UnmatchedTorrentFiles: []TorrentFile{{Path: "Example/c.bin", Size: 4}},
		IsPerfectMatch:        false,
		IsPartialMatch:        true,
		IsMatch:               true,
	}

	settings := &models.DirScanSettings{
		AllowPartial:                 true,
		MinPieceRatio:                70, // percent
		SkipPieceBoundarySafetyCheck: true,
	}

	decision := shouldAcceptDirScanMatch(match, parsed, settings)
	if !decision.Accept {
		t.Fatalf("expected match to be accepted")
	}

	settings.MinPieceRatio = 80
	decision = shouldAcceptDirScanMatch(match, parsed, settings)
	if decision.Accept {
		t.Fatalf("expected match to be rejected")
	}
}

func TestShouldAcceptDirScanMatch_NoFilesMatched(t *testing.T) {
	parsed := &ParsedTorrent{
		Name:        "Example",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "Example/a.bin", Size: 8, Offset: 0},
		},
		TotalSize: 8,
	}

	match := &MatchResult{
		MatchedFiles:           nil,
		UnmatchedTorrentFiles:  []TorrentFile{{Path: "Example/a.bin", Size: 8}},
		UnmatchedSearcheeFiles: []*ScannedFile{{RelPath: "a.bin", Size: 8}},
		IsPerfectMatch:         false,
		IsPartialMatch:         false,
		IsMatch:                false,
	}

	settings := &models.DirScanSettings{
		AllowPartial: false,
	}

	decision := shouldAcceptDirScanMatch(match, parsed, settings)
	if decision.Accept {
		t.Fatalf("expected match to be rejected")
	}
	if decision.Reason != "no files matched" {
		t.Fatalf("expected reason %q, got %q", "no files matched", decision.Reason)
	}
}

func TestRefineDirScanMatchDecision_RejectsFlexibleSingleFilePerfectWithoutCorroboration(t *testing.T) {
	searchee := &Searchee{
		Name: "John Wick Chapter 4 (2023) {imdb-tt10366206}",
		Files: []*ScannedFile{{
			Path:    "/media/movies/John Wick Chapter 4 (2023) {imdb-tt10366206}/John Wick Chapter 4 (2023).mkv",
			RelPath: "John Wick Chapter 4 (2023).mkv",
			Size:    1000,
		}},
	}
	parsed := &ParsedTorrent{
		Name:        "Totally.Different.Movie.2023.1080p.BluRay.x264-GROUP",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "Totally.Different.Movie.2023.1080p.BluRay.x264-GROUP.mkv", Size: 1000, Offset: 0},
		},
		TotalSize: 1000,
	}
	match := &MatchResult{
		MatchedFiles: []MatchedFilePair{{
			SearcheeFile: searchee.Files[0],
			TorrentFile:  parsed.Files[0],
		}},
		IsPerfectMatch: true,
		IsMatch:        true,
	}
	settings := &models.DirScanSettings{
		MatchMode:    models.MatchModeFlexible,
		AllowPartial: false,
	}
	searcheeMeta := NewParser(nil).Parse(searchee.Name)
	baseDecision := shouldAcceptDirScanMatch(match, parsed, settings)
	if !baseDecision.Accept {
		t.Fatalf("expected base decision to accept perfect match")
	}

	decision := refineDirScanMatchDecision(searchee, searcheeMeta, parsed, &jackett.SearchResult{
		Title: "Totally Different Movie 2023 1080p BluRay x264-GROUP",
	}, settings, match, baseDecision)

	if decision.Accept {
		t.Fatalf("expected flexible size-only mismatch to be rejected")
	}
	if decision.Reason != "flexible size-only match lacks title or ID corroboration" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
}

func TestRefineDirScanMatchDecision_AcceptsFlexibleSingleFilePerfectWithIMDbCorroboration(t *testing.T) {
	searchee := &Searchee{
		Name: "John Wick Chapter 4 (2023) {imdb-tt10366206}",
		Files: []*ScannedFile{{
			Path:    "/media/movies/John Wick Chapter 4 (2023) {imdb-tt10366206}/John Wick Chapter 4 (2023).mkv",
			RelPath: "John Wick Chapter 4 (2023).mkv",
			Size:    1000,
		}},
	}
	parsed := &ParsedTorrent{
		Name:        "John.Wick.Chapter.4.2023.1080p.WEB-DL.DDP5.1.Atmos.H.264-WDYM",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "John.Wick.Chapter.4.2023.1080p.WEB-DL.DDP5.1.Atmos.H.264-WDYM.mkv", Size: 1000, Offset: 0},
		},
		TotalSize: 1000,
	}
	match := &MatchResult{
		MatchedFiles: []MatchedFilePair{{
			SearcheeFile: searchee.Files[0],
			TorrentFile:  parsed.Files[0],
		}},
		IsPerfectMatch: true,
		IsMatch:        true,
	}
	settings := &models.DirScanSettings{
		MatchMode:    models.MatchModeFlexible,
		AllowPartial: false,
	}
	searcheeMeta := NewParser(nil).Parse(searchee.Name)
	baseDecision := shouldAcceptDirScanMatch(match, parsed, settings)
	if !baseDecision.Accept {
		t.Fatalf("expected base decision to accept perfect match")
	}

	decision := refineDirScanMatchDecision(searchee, searcheeMeta, parsed, &jackett.SearchResult{
		Title:  "John Wick Chapter 4 2023 1080p WEB-DL DDP5 1 Atmos H 264-WDYM",
		IMDbID: "tt10366206",
	}, settings, match, baseDecision)

	if !decision.Accept {
		t.Fatalf("expected corroborated flexible match to be accepted, got reason %q", decision.Reason)
	}
}

func TestRefineDirScanMatchDecision_AcceptsFlexibleSingleFilePerfectWithSearchIMDbContext(t *testing.T) {
	searchee := &Searchee{
		Name: "Le Comte de Monte-Cristo (2024)",
		Files: []*ScannedFile{{
			Path:    "/media/movies/Le Comte de Monte-Cristo (2024)/Le Comte de Monte-Cristo (2024).mkv",
			RelPath: "Le Comte de Monte-Cristo (2024).mkv",
			Size:    1000,
		}},
	}
	parsed := &ParsedTorrent{
		Name:        "The.Count.of.Monte.Cristo.2024.1080p.WEB-DL.x264-GROUP",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "The.Count.of.Monte.Cristo.2024.1080p.WEB-DL.x264-GROUP.mkv", Size: 1000, Offset: 0},
		},
		TotalSize: 1000,
	}
	match := &MatchResult{
		MatchedFiles: []MatchedFilePair{{
			SearcheeFile: searchee.Files[0],
			TorrentFile:  parsed.Files[0],
		}},
		IsPerfectMatch: true,
		IsMatch:        true,
	}
	settings := &models.DirScanSettings{
		MatchMode:    models.MatchModeFlexible,
		AllowPartial: false,
	}
	searcheeMeta := NewParser(nil).Parse(searchee.Name)
	searcheeMeta.SetExternalIDs("tt26446278", 0, 0)
	baseDecision := shouldAcceptDirScanMatch(match, parsed, settings)
	decision := refineDirScanMatchDecision(searchee, searcheeMeta, parsed, &jackett.SearchResult{
		Title:        "The Count of Monte Cristo 2024 1080p WEB-DL x264-GROUP",
		SearchIMDbID: "26446278",
	}, settings, match, baseDecision)

	if !decision.Accept {
		t.Fatalf("expected search-context IMDb corroboration to be accepted, got reason %q", decision.Reason)
	}
}

func TestRefineDirScanMatchDecision_AcceptsFlexibleSingleFilePerfectWithTVDbCorroboration(t *testing.T) {
	searchee := &Searchee{
		Name: "Le Bureau des Légendes - S01E01 [tvdb-295770]",
		Files: []*ScannedFile{{
			Path:    "/media/tv/Le Bureau des Légendes/S01E01.mkv",
			RelPath: "Le Bureau des Légendes - S01E01.mkv",
			Size:    1000,
		}},
	}
	parsed := &ParsedTorrent{
		Name:        "The.Bureau.S01E01.1080p.WEB-DL.x264-GROUP",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "The.Bureau.S01E01.1080p.WEB-DL.x264-GROUP.mkv", Size: 1000, Offset: 0},
		},
		TotalSize: 1000,
	}
	match := &MatchResult{
		MatchedFiles: []MatchedFilePair{{
			SearcheeFile: searchee.Files[0],
			TorrentFile:  parsed.Files[0],
		}},
		IsPerfectMatch: true,
		IsMatch:        true,
	}
	settings := &models.DirScanSettings{
		MatchMode:    models.MatchModeFlexible,
		AllowPartial: false,
	}
	searcheeMeta := NewParser(nil).Parse(searchee.Name)
	baseDecision := shouldAcceptDirScanMatch(match, parsed, settings)
	decision := refineDirScanMatchDecision(searchee, searcheeMeta, parsed, &jackett.SearchResult{
		Title:        "The Bureau S01E01 1080p WEB-DL x264-GROUP",
		SearchTVDbID: "295770",
	}, settings, match, baseDecision)

	if !decision.Accept {
		t.Fatalf("expected TVDb corroboration to be accepted, got reason %q", decision.Reason)
	}
}

func TestRefineDirScanMatchDecision_AcceptsFlexibleSingleFilePerfectWithTMDbCorroboration(t *testing.T) {
	searchee := &Searchee{
		Name: "Avatar - De feu et de cendres (2025) {tmdb-83533}",
		Files: []*ScannedFile{{
			Path:    "/media/movies/Avatar - De feu et de cendres (2025)/Avatar - De feu et de cendres (2025).mkv",
			RelPath: "Avatar - De feu et de cendres (2025).mkv",
			Size:    1000,
		}},
	}
	parsed := &ParsedTorrent{
		Name:        "Avatar.Fire.and.Ash.2025.2160p.WEB-DL.x265-GROUP",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "Avatar.Fire.and.Ash.2025.2160p.WEB-DL.x265-GROUP.mkv", Size: 1000, Offset: 0},
		},
		TotalSize: 1000,
	}
	match := &MatchResult{
		MatchedFiles: []MatchedFilePair{{
			SearcheeFile: searchee.Files[0],
			TorrentFile:  parsed.Files[0],
		}},
		IsPerfectMatch: true,
		IsMatch:        true,
	}
	settings := &models.DirScanSettings{
		MatchMode:    models.MatchModeFlexible,
		AllowPartial: false,
	}
	searcheeMeta := NewParser(nil).Parse(searchee.Name)
	baseDecision := shouldAcceptDirScanMatch(match, parsed, settings)
	decision := refineDirScanMatchDecision(searchee, searcheeMeta, parsed, &jackett.SearchResult{
		Title:        "Avatar Fire and Ash 2025 2160p WEB-DL x265-GROUP",
		SearchTMDbID: 83533,
	}, settings, match, baseDecision)

	if !decision.Accept {
		t.Fatalf("expected TMDb corroboration to be accepted, got reason %q", decision.Reason)
	}
}

func TestRefineDirScanMatchDecision_AcceptsFlexibleSingleFilePerfectWithResultTMDbCorroboration(t *testing.T) {
	searchee := &Searchee{
		Name: "Avatar - De feu et de cendres (2025) {tmdb-83533}",
		Files: []*ScannedFile{{
			Path:    "/media/movies/Avatar - De feu et de cendres (2025)/Avatar - De feu et de cendres (2025).mkv",
			RelPath: "Avatar - De feu et de cendres (2025).mkv",
			Size:    1000,
		}},
	}
	parsed := &ParsedTorrent{
		Name:        "Avatar.Fire.and.Ash.2025.2160p.WEB-DL.x265-GROUP",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "Avatar.Fire.and.Ash.2025.2160p.WEB-DL.x265-GROUP.mkv", Size: 1000, Offset: 0},
		},
		TotalSize: 1000,
	}
	match := &MatchResult{
		MatchedFiles: []MatchedFilePair{{
			SearcheeFile: searchee.Files[0],
			TorrentFile:  parsed.Files[0],
		}},
		IsPerfectMatch: true,
		IsMatch:        true,
	}
	settings := &models.DirScanSettings{
		MatchMode:    models.MatchModeFlexible,
		AllowPartial: false,
	}
	searcheeMeta := NewParser(nil).Parse(searchee.Name)
	baseDecision := shouldAcceptDirScanMatch(match, parsed, settings)
	decision := refineDirScanMatchDecision(searchee, searcheeMeta, parsed, &jackett.SearchResult{
		Title:  "Avatar Fire and Ash 2025 2160p WEB-DL x265-GROUP",
		TMDbID: "83533",
	}, settings, match, baseDecision)

	if !decision.Accept {
		t.Fatalf("expected result TMDb corroboration to be accepted, got reason %q", decision.Reason)
	}
}

func TestRefineDirScanMatchDecision_RejectsFlexibleSingleFilePerfectForWrongEpisode(t *testing.T) {
	searchee := &Searchee{
		Name: "The Office S01E01",
		Files: []*ScannedFile{{
			Path:    "/media/tv/The Office/Season 01/The Office - S01E01.mkv",
			RelPath: "The Office - S01E01.mkv",
			Size:    1000,
		}},
	}
	parsed := &ParsedTorrent{
		Name:        "The.Office.S01E02.1080p.WEB-DL.x264-GROUP",
		InfoHash:    "deadbeef",
		PieceLength: 4,
		Files: []TorrentFile{
			{Path: "The.Office.S01E02.1080p.WEB-DL.x264-GROUP.mkv", Size: 1000, Offset: 0},
		},
		TotalSize: 1000,
	}
	match := &MatchResult{
		MatchedFiles: []MatchedFilePair{{
			SearcheeFile: searchee.Files[0],
			TorrentFile:  parsed.Files[0],
		}},
		IsPerfectMatch: true,
		IsMatch:        true,
	}
	settings := &models.DirScanSettings{
		MatchMode:    models.MatchModeFlexible,
		AllowPartial: false,
	}
	searcheeMeta := NewParser(nil).Parse(searchee.Name)
	baseDecision := shouldAcceptDirScanMatch(match, parsed, settings)
	if !baseDecision.Accept {
		t.Fatalf("expected base decision to accept perfect match")
	}

	decision := refineDirScanMatchDecision(searchee, searcheeMeta, parsed, &jackett.SearchResult{
		Title: "The Office S01E02 1080p WEB-DL x264-GROUP",
	}, settings, match, baseDecision)

	if decision.Accept {
		t.Fatalf("expected wrong-episode title corroboration to be rejected")
	}
	if decision.Reason != "flexible size-only match lacks title or ID corroboration" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
}

func TestDescribeDirScanDecisionHints_FlexibleCorroborationFailure(t *testing.T) {
	hint, hints := describeDirScanDecisionHints(dirScanMatchDecision{
		Reason: "flexible size-only match lacks title or ID corroboration",
	})

	if hint != "size-only match rejected" {
		t.Fatalf("expected hint %q, got %q", "size-only match rejected", hint)
	}
	if len(hints) == 0 {
		t.Fatal("expected at least one hint")
	}
}
