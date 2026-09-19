// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/autobrr/qui/pkg/releases"
)

// pkg/releases.DetermineContentType (automations CONTENT_TYPE) and this package's
// name-only DetermineContentType (cross-seed) classify the same parsed names, except
// Education: cross-seed searches it as educationContentType, while automations keep
// "book" so saved Content Type rules match as before.
func TestContentTypeParityWithReleases(t *testing.T) {
	t.Parallel()

	names := []struct{ group, name string }{
		{"adult", "Some.Studio.XXX.2024.Scene.Name.1080p.WEB-DL"},
		{"adult", "ABCD-123 Actress Name Scene Title 1080p"},
		{"adult", "[2024.05.01] Studio Scene Title 1080p"},
		{"adult", "240501_123 Scene Title"},
		{"adult", "xXx.2002.1080p.BluRay.x264-GRPA"},
		{"adult", "xXx.Return.of.Xander.Cage.2017.1080p.WEB-DL-GRPA"},
		{"riaj", "Artist Name - Album Title (VICL-12345) [FLAC]"},
		{"riaj", "TOCT-1234 Artist Name Album Title"},
		{"riaj", "Artist Name - Single Title (VIBX-1234)"},
		{"riaj", "Artist Name - Live Title (VIXL-123)"},
		{"music", "Artist_Name-Album_Title-WEB-2023-GRPM"},
		{"music", "Artist Name - Album Title (2020) [FLAC 24-96]"},
		{"music", "VA-Compilation_Title-2CD-2019-GRPM"},
		{"music", "Artist_Name-Album_Title-(CAT123)-VINYL-2021-GRPM"},
		{"music-video", "Artist_Name-Live_At_Venue-BDMV-2021-GRPM"},
		{"music-video", "Artist Name - Concert Title 2019 1080p BluRay x264-GRPV"},
		{"music-video", "Artist_Name-Song_Title-DVDRip-x264-2018-GRPM"},
		{"music-video", "Artist_Name-Song_Title-1080p-x264-2022-GRPM"},
		{"disc", "Movie.Title.2019.COMPLETE.UHD.BLURAY-GRPD"},
		{"disc", "Show.Name.S01.COMPLETE.BLURAY-GRPD"},
		{"disc", "Movie Title 2010 1080p BluRay REMUX AVC DTS-HD MA 5.1-GRPD"},
		{"disc", "MOVIE_TITLE_DVD9"},
		{"disc", "Movie.Title.1999.NTSC.DVDR-GRPD"},
		{"comic", "Comic Title 001 (2021) (Digital) (Scanner-Group)"},
		{"comic", "Comic.Title.v01.2020.Comic.eBook-GRPC"},
		{"audiobook", "Author Name - Book Title (Unabridged) [M4B]"},
		{"audiobook", "Author.Name-Book.Title.2019.AUDIOBOOK.MP3-GRPA"},
		{"book", "Author Name - Book Title (2020) [EPUB]"},
		{"book", "Magazine.Name.June.2023.PDF"},
		{"book", "Author.Name.Book.Title.2021.RETAIL.EPUB.eBook-GRPB"},
		{"magazine", "Magazine.Name.2023.06.MAGAZiNE.eBook-GRPB"},
		{"magazine", "Magazine.Name.No.123.2023.HYBRID.MAGAZINE.eBook-GRPB"},
		{"education", "Tutorial.Name.2022.TUTORiAL-GRPE"},
		{"education", "Course.Name.2021.Udemy.Course"},
		{"education", "Company.Course.Name.2022.BOOKWARE-GRPE"},
		{"education", "Tutorial.Name.TUTORiAL-GRPE"},
		{"adult", "Studio.XXX.Tutorial.Name.2022.TUTORiAL-GRPE"},
		{"game", "Game.Title-CODEX"},
		{"game", "Game Title v1.2.3 [GOG]"},
		{"game", "Game.Title.PS4-DUPLEX"},
		{"game", "Game.Title.NSW-VENOM"},
		{"app", "Some.App.v2.1.0.x64-GRPX"},
		{"app", "Some App 2024 v25.0 macOS"},
		{"pack", "Show.Name.S01-S05.1080p.WEB-DL-GRPT"},
		{"pack", "Show.Name.S02.1080p.WEB.h264-GRPT"},
		{"pack", "Movie.Collection.2001-2010.1080p.BluRay.x264-GRPT"},
		{"tv", "Show.Name.S01E02.720p.HDTV.x264-GRPT"},
		{"tv", "Show Name 2023-05-01 720p WEB h264-GRPT"},
		{"movie", "Movie.Title.2021.1080p.WEB-DL.DDP5.1.H.264-GRPF"},
		{"unknown", "random_file_name"},
		{"unknown", ""},
	}

	parser := releases.NewDefaultParser()
	for _, tc := range names {
		fromReleases := releases.DetermineContentType(parser.Parse(tc.name)).ContentType
		fromCrossseed := DetermineContentType(parser.Parse(tc.name)).ContentType
		want := fromReleases
		if tc.group == "education" {
			want = educationContentType
		}
		if fromCrossseed != string(want) {
			t.Errorf("%s %q: releases=%s crossseed=%s, want crossseed=%s", tc.group, tc.name, fromReleases, fromCrossseed, want)
		}
	}
}
