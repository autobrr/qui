// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

// Standard Torznab categories as defined in the torznab specification
// https://torznab.github.io/spec-1.3-draft/torznab/Specification-v1.3.html

const (
	// CategoryAll represents all categories
	CategoryAll = "0"

	// TV Categories
	CategoryTV        = "5000"
	CategoryTVSD      = "5030"
	CategoryTVHD      = "5040"
	CategoryTVUHD     = "5045"
	CategoryTVForeign = "5020"
	CategoryTVSport   = "5060"
	CategoryTVAnime   = "5070"
	CategoryTVDocumen = "5080"
	CategoryTVOther   = "5090"
	CategoryTVWeb     = "5010"

	// Movie Categories
	CategoryMovies        = "2000"
	CategoryMoviesForeign = "2010"
	CategoryMoviesOther   = "2020"
	CategoryMoviesSD      = "2030"
	CategoryMoviesHD      = "2040"
	CategoryMoviesUHD     = "2045"
	CategoryMoviesBluRay  = "2050"
	CategoryMovies3D      = "2060"
	CategoryMoviesWeb     = "2070"

	// Music Categories
	CategoryAudio          = "3000"
	CategoryAudioMP3       = "3010"
	CategoryAudioVideo     = "3020"
	CategoryAudioAudiobook = "3030"
	CategoryAudioLossless  = "3040"
	CategoryAudioOther     = "3050"
	CategoryAudioForeign   = "3060"

	// PC Categories
	CategoryPC             = "4000"
	CategoryPC0day         = "4010"
	CategoryPCISO          = "4020"
	CategoryPCMac          = "4030"
	CategoryPCPhoneOther   = "4040"
	CategoryPCGames        = "4050"
	CategoryPCPhoneIOS     = "4060"
	CategoryPCPhoneAndroid = "4070"

	// XXX Categories
	CategoryXXX         = "6000"
	CategoryXXXDVD      = "6010"
	CategoryXXXWMV      = "6020"
	CategoryXXXXviD     = "6030"
	CategoryXXXx264     = "6040"
	CategoryXXXUHD      = "6045"
	CategoryXXXPack     = "6050"
	CategoryXXXImageSet = "6060"
	CategoryXXXOther    = "6070"
	CategoryXXXSD       = "6080"
	CategoryXXXWeb      = "6090"

	// Other Categories
	CategoryOther       = "7000"
	CategoryOtherMisc   = "7010"
	CategoryOtherHashed = "7020"

	// Books Categories
	CategoryBooks          = "8000"
	CategoryBooksMags      = "8010"
	CategoryBooksEBook     = "8020"
	CategoryBooksComics    = "8030"
	CategoryBooksTechnical = "8040"
	CategoryBooksForeign   = "8050"
	CategoryBooksOther     = "8060"
)

// SearchType represents the type of search to perform
type SearchType string

const (
	// SearchTypeSearch is a free text search
	SearchTypeSearch SearchType = "search"

	// SearchTypeTV is a TV-specific search
	SearchTypeTV SearchType = "tvsearch"

	// SearchTypeMovie is a movie-specific search
	SearchTypeMovie SearchType = "movie"

	// SearchTypeMusic is a music-specific search
	SearchTypeMusic SearchType = "music"

	// SearchTypeBook is a book-specific search
	SearchTypeBook SearchType = "book"

	// SearchTypeCaps retrieves indexer capabilities
	SearchTypeCaps SearchType = "caps"
)

// Common torznab attribute names
const (
	AttrSize                 = "size"
	AttrCategory             = "category"
	AttrTag                  = "tag"
	AttrGUID                 = "guid"
	AttrFiles                = "files"
	AttrPoster               = "poster"
	AttrGroup                = "group"
	AttrTeam                 = "team"
	AttrGrabs                = "grabs"
	AttrSeeders              = "seeders"
	AttrPeers                = "peers"
	AttrLeechers             = "leechers"
	AttrInfoHash             = "infohash"
	AttrMagnetURL            = "magneturl"
	AttrDownloadVolumeFactor = "downloadvolumefactor"
	AttrUploadVolumeFactor   = "uploadvolumefactor"
	AttrMinimumRatio         = "minimumratio"
	AttrMinimumSeedTime      = "minimumseedtime"

	// TV attributes
	AttrSeason    = "season"
	AttrEpisode   = "episode"
	AttrTVDBID    = "tvdbid"
	AttrTVMazeID  = "tvmazeid"
	AttrTVRageID  = "rageid"
	AttrTVTitle   = "tvtitle"
	AttrTVAirDate = "tvairdate"

	// Movie attributes
	AttrIMDB      = "imdb"
	AttrIMDBID    = "imdbid"
	AttrTMDBID    = "tmdbid"
	AttrIMDBScore = "imdbscore"
	AttrIMDBTitle = "imdbtitle"
	AttrIMDBYear  = "imdbyear"

	// General media attributes
	AttrGenre      = "genre"
	AttrYear       = "year"
	AttrLanguage   = "language"
	AttrSubtitles  = "subs"
	AttrVideo      = "video"
	AttrAudio      = "audio"
	AttrResolution = "resolution"
	AttrFramerate  = "framerate"

	// Music attributes
	AttrArtist    = "artist"
	AttrAlbum     = "album"
	AttrLabel     = "label"
	AttrTrack     = "track"
	AttrPublisher = "publisher"

	// Book attributes
	AttrBookTitle   = "booktitle"
	AttrAuthor      = "author"
	AttrPublishDate = "publishdate"
	AttrPages       = "pages"
)
