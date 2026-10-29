// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/jackett"
)

// The match corpus runs every release-name pair through every match path (see
// GLOSSARY.md). A path that gives a verdict other than the row expects fails the
// test, unless the row lists that difference with a reason.
//
// Dir Scan is not in the corpus, because the dirscan package imports this one.
// Its title check runs the title rule of the Match verdict, and
// TestRefineDirScanMatchDecision_TitlesMatch pins its pairs.
//
// Every name is built: the tokens that the matcher reads are kept, the titles
// and the groups are invented.
//
// Edits that relax the corpus: see .github/workflows/match-corpus.yml.

type corpusVerdict string

const (
	corpusMatch   corpusVerdict = "match"
	corpusNoMatch corpusVerdict = "no match"
	// corpusSkip means that the path never reads a pair of this shape.
	corpusSkip corpusVerdict = "skip"
)

type matchPath string

const (
	pathSearch       matchPath = "search classifier"
	pathRetry        matchPath = "retry step"
	pathApply        matchPath = "apply"
	pathSeasonPack   matchPath = "season pack check"
	pathCrossSeedTab matchPath = "Cross-Seed tab"
	pathPrefilter    matchPath = "content prefilter"
	pathCrossMatch   matchPath = "cross-match sets"
	pathDedup        matchPath = "dedup"
	pathWebhook      matchPath = "webhook check"
)

// corpusDifference is a verdict that one path gives for a row instead of the
// row's verdict. An allowed difference is deliberate and has no issue. A known
// difference is a bug, and issue names the ticket that removes it.
type corpusDifference struct {
	verdict corpusVerdict
	reason  string
	issue   int
}

// corpusTickets are the open issues that known differences name. Add a ticket
// with its first known difference, and remove it with its last one.
var corpusTickets = []int{}

func allowedDifference(verdict corpusVerdict, reason string) corpusDifference {
	return corpusDifference{verdict: verdict, reason: reason}
}

// corpusRow is one release-name pair. The source is the torrent that the user
// seeds. The candidate is the new torrent: a search result, an announce, or
// the torrent that apply adds. Paths that compare two local torrents seed both.
type corpusRow struct {
	name          string
	source        string
	candidate     string
	sourceSize    int64
	candidateSize int64
	// sourceFiles and candidateFiles default to one file named after the release.
	sourceFiles    qbt.TorrentFiles
	candidateFiles qbt.TorrentFiles
	// titles are the Sonarr alternate titles of the show. Only the paths that look
	// the show up hold them.
	titles     []string
	episodeMap *models.EpisodeMap
	rescue     bool
	// searchSource seeds a second local torrent for apply: the one that search
	// compared, a local copy of the candidate. Apply keeps the stored verdict
	// for it, and the source gets a new Match verdict.
	searchSource string
	want         corpusVerdict
	differences  map[matchPath]corpusDifference
}

const (
	corpusEpisodeSize = int64(1_449_551_462)
	// corpusRoundedSize is an announce size that IRC rounded.
	corpusRoundedSize = corpusEpisodeSize + 500
	corpusPackSize    = 10 * corpusEpisodeSize
	corpusNFOSize     = int64(4_096)
)

func corpusPackFiles(dir, episodeName string) qbt.TorrentFiles {
	files := make(qbt.TorrentFiles, 0, 10)
	for episode := 1; episode <= 10; episode++ {
		files = append(files, qbt.TorrentFile{
			Name: dir + "/" + fmt.Sprintf(episodeName, episode) + ".mkv",
			Size: corpusEpisodeSize,
		})
	}
	return files
}

// corpusPackWithBonus is a pack of ten episodes and a bonus video that is
// larger than each episode.
func corpusPackWithBonus(dir string) qbt.TorrentFiles {
	files := corpusPackFiles(dir, "Kaiju Squad (2020) - S02E%02d - Episode (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)")
	return append(files, qbt.TorrentFile{Name: dir + "/Extras/Behind the Scenes (1080p).mkv", Size: 2 * corpusEpisodeSize})
}

// corpusPackWithSpecial is a pack of ten episodes and a season 0 special that
// is larger than each episode.
func corpusPackWithSpecial(dir string) qbt.TorrentFiles {
	files := corpusPackFiles(dir, "Kaiju Squad (2020) - S02E%02d - Episode (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)")
	return append(files, qbt.TorrentFile{Name: dir + "/Kaiju Squad (2020) - S00E01 - Special (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP).mkv", Size: 3 * corpusEpisodeSize})
}

// corpusHybridExtrasSize is 2% of a pack: inside the size tolerance, never an
// exact size.
const corpusHybridExtrasSize = corpusPackSize / 50

// corpusPackWithExtras is a pack of ten episodes plus an Extras folder.
func corpusPackWithExtras(dir string) qbt.TorrentFiles {
	return append(corpusPackFiles(dir, "Kaiju.Squad.S01E%02d.1080p.BluRay.x264-GRP"),
		qbt.TorrentFile{Name: dir + "/Extras/Kaiju.Squad.NCOP.1080p.BluRay.x264-GRP.mkv", Size: corpusHybridExtrasSize})
}

// corpusResized gives each file of files the same size.
func corpusResized(files qbt.TorrentFiles, size int64) qbt.TorrentFiles {
	for i := range files {
		files[i].Size = size
	}
	return files
}

// hybridSearchOnlyDifferences lists the differences of a row whose only name difference is
// a HYBRID tag on one side, inside the size tolerance.
func hybridSearchOnlyDifferences() map[matchPath]corpusDifference {
	differences := make(map[matchPath]corpusDifference)
	for _, path := range []matchPath{pathCrossSeedTab, pathPrefilter, pathCrossMatch, pathDedup, pathWebhook} {
		differences[path] = allowedDifference(corpusNoMatch, reasonOnlySearchComparesFiles)
	}
	return differences
}

// corpusEpisodeWithNFO is an episode with an NFO file, so its total size is not
// equal to the bare episode. The Batch matcher then cannot rescue the title, and
// the name rules alone decide.
func corpusEpisodeWithNFO(name string) qbt.TorrentFiles {
	return qbt.TorrentFiles{
		{Name: name + "/" + name + ".mkv", Size: corpusEpisodeSize},
		{Name: name + "/" + name + ".nfo", Size: corpusNFOSize},
	}
}

// packFromEpisodes lists the differences of a row whose candidate is a season
// pack and whose source is one episode of it. The row verdict is the answer of
// the season pack check and the apply hint: qui can build the pack from the
// episode. The paths that compare two whole torrents give "no match". extra
// adds the differences of the row itself.
func packFromEpisodes(extra map[matchPath]corpusDifference) map[matchPath]corpusDifference {
	differences := make(map[matchPath]corpusDifference)
	for _, path := range []matchPath{pathSearch, pathRetry, pathCrossSeedTab, pathPrefilter, pathCrossMatch, pathDedup, pathWebhook} {
		differences[path] = allowedDifference(corpusNoMatch, "A season pack and one episode are different torrents. Only the season pack check and the apply hint pair them, because qui can build the pack from the local episodes.")
	}
	maps.Copy(differences, extra)
	return differences
}

const (
	reasonNoAlternateTitles       = "Missing input: Sonarr alternate titles. This path compares two local torrents, and qui looks up no Sonarr data for local names (ADR 0002)."
	reasonNoEpisodeMap            = "Missing input: the episode map. This path compares two local torrents, and qui looks up no Sonarr data for local names (ADR 0002)."
	reasonNoTitleRescue           = "Title rescue needs the recheck after the add. This path adds nothing, so it does not rescue a title, and the worst result is one more search."
	reasonDedupNoRelabel          = "Missing input: sizes. Dedup compares without sizes, and the web source relabel needs both sizes."
	reasonDedupNoExactSize        = "Missing input: sizes. Dedup compares without sizes, and the exact size relaxation needs both sizes."
	reasonRetitledSameSizes       = "The Batch matcher pairs a retitled upload whose file sizes pair one to one with the source. The corpus gives every episode the same size, so two shows pair here. Real files of two shows do not have the same byte size."
	reasonDedupNoSizes            = "Dedup compares without sizes, and a yearless \"Title III\" reads like the \"Title S03\" pack, so dedup keeps one of the two and does not search the other. A library seldom holds both."
	reasonNumeralSameSize         = "This path has no file list for the candidate, and a movie of the same size reads like the pack. Apply compares the files and has the final say."
	reasonOnlySearchComparesFiles = "Only search and apply compare files before deciding."
)

var corpusRows = []corpusRow{
	{
		name:          "scene episode",
		source:        "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:     "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		searchSource:  "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		want:          corpusMatch,
	},
	{
		name:          "P2P episode in spaced and dotted style",
		source:        "Kaiju Squad S02E05 1080p AMZN WEB-DL DDP5.1 H.264-GRP",
		candidate:     "Kaiju.Squad.S02E05.1080p.AMZN.WEB-DL.DDP5.1.H.264-GRP",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		// The episode file has no season, so it gives apply no file key, and apply compares the absolute episodes.
		name:          "anime absolute episode with and without a leading zero",
		source:        "[KaijuSubs] Kaiju Squad - 5 (1080p)",
		candidate:     "[KaijuSubs] Kaiju Squad - 05 (1080p)",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:          "anime absolute against seasoned with an episode map",
		source:        "[KaijuSubs] Kaiju Squad - 81 (1080p).mkv",
		candidate:     "Kaiju Squad S04E15 1080p WEB-DL AAC2.0 H.264-KaijuSubs",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusRoundedSize,
		episodeMap:    &models.EpisodeMap{Season: 4, Episode: 15, Absolute: 81},
		want:          corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathCrossSeedTab: allowedDifference(corpusNoMatch, reasonNoEpisodeMap),
			pathPrefilter:    allowedDifference(corpusNoMatch, reasonNoEpisodeMap),
			pathCrossMatch:   allowedDifference(corpusNoMatch, reasonNoEpisodeMap),
			pathDedup:        allowedDifference(corpusNoMatch, reasonNoEpisodeMap),
		},
	},
	{
		name:          "anime absolute against seasoned without an episode map",
		source:        "[KaijuSubs] Kaiju Squad - 81 (1080p).mkv",
		candidate:     "Kaiju Squad S04E15 1080p WEB-DL AAC2.0 H.264-KaijuSubs",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusRoundedSize,
		want:          corpusNoMatch,
	},
	{
		// rls reads this name as music. The range must still read as a pack.
		name:          "episode range with a music-style name against its first episode",
		source:        "Talk.Show-Part.E05.E06-WEB-2021-GRP",
		candidate:     "Talk.Show-Part.E05-WEB-2021-GRP",
		sourceSize:    2 * corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusNoMatch,
	},
	{
		name:           "season numeral pack against season pack",
		source:         "Kaiju.Squad.III.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Squad.S03.1080p.WEB.H264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize + 500,
		sourceFiles:    corpusPackFiles("Kaiju.Squad.III.1080p.WEB.H264-GRP", "Kaiju.Squad.III.S03E%02d.1080p.WEB.H264-GRP"),
		candidateFiles: corpusPackFiles("Kaiju.Squad.S03.1080p.WEB.H264-GRP", "Kaiju.Squad.S03E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
	},
	{
		name:           "season numeral announce against season pack",
		source:         "Kaiju.Squad.S03.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Squad.III.1080p.WEB.H264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize + 500,
		sourceFiles:    corpusPackFiles("Kaiju.Squad.S03.1080p.WEB.H264-GRP", "Kaiju.Squad.S03E%02d.1080p.WEB.H264-GRP"),
		candidateFiles: corpusPackFiles("Kaiju.Squad.III.1080p.WEB.H264-GRP", "Kaiju.Squad.III.S03E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
	},
	{
		name:           "season numeral announce against another season",
		source:         "Kaiju.Squad.S03.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Squad.II.1080p.WEB.H264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize + 500,
		sourceFiles:    corpusPackFiles("Kaiju.Squad.S03.1080p.WEB.H264-GRP", "Kaiju.Squad.S03E%02d.1080p.WEB.H264-GRP"),
		candidateFiles: corpusPackFiles("Kaiju.Squad.II.1080p.WEB.H264-GRP", "Kaiju.Squad.II.S02E%02d.1080p.WEB.H264-GRP"),
		want:           corpusNoMatch,
	},
	{
		name:          "movie sequel against season pack",
		source:        "Kaiju.Squad.S03.1080p.WEB.H264-GRP",
		candidate:     "Kaiju.Squad.III.2019.1080p.WEB.H264-GRP",
		sourceSize:    corpusPackSize,
		candidateSize: corpusPackSize + 500,
		sourceFiles:   corpusPackFiles("Kaiju.Squad.S03.1080p.WEB.H264-GRP", "Kaiju.Squad.S03E%02d.1080p.WEB.H264-GRP"),
		want:          corpusNoMatch,
	},
	{
		name:          "yearless movie sequel against season pack",
		source:        "Kaiju.Squad.S03.1080p.BluRay.x264-GRP",
		candidate:     "Kaiju.Squad.III.1080p.BluRay.x264-GRP",
		sourceSize:    corpusPackSize,
		candidateSize: 3 * corpusEpisodeSize,
		sourceFiles:   corpusPackFiles("Kaiju.Squad.S03.1080p.BluRay.x264-GRP", "Kaiju.Squad.S03E%02d.1080p.BluRay.x264-GRP"),
		want:          corpusNoMatch,
		differences: map[matchPath]corpusDifference{
			pathDedup: allowedDifference(corpusMatch, reasonDedupNoSizes),
		},
	},
	{
		name:          "yearless movie sequel of the season pack size",
		source:        "Kaiju.Squad.S03.1080p.BluRay.x264-GRP",
		candidate:     "Kaiju.Squad.III.1080p.BluRay.x264-GRP",
		sourceSize:    corpusPackSize,
		candidateSize: corpusPackSize + 500,
		sourceFiles:   corpusPackFiles("Kaiju.Squad.S03.1080p.BluRay.x264-GRP", "Kaiju.Squad.S03E%02d.1080p.BluRay.x264-GRP"),
		want:          corpusNoMatch,
		differences: map[matchPath]corpusDifference{
			pathDedup:   allowedDifference(corpusMatch, reasonDedupNoSizes),
			pathSearch:  allowedDifference(corpusMatch, reasonNumeralSameSize),
			pathRetry:   allowedDifference(corpusMatch, reasonNumeralSameSize),
			pathWebhook: allowedDifference(corpusMatch, reasonNumeralSameSize),
		},
	},
	{
		name:          "season numeral with an absolute episode against the seasoned episode",
		source:        "[KaijuSubs] Kaiju Squad 100 II - 03 (1080p).mkv",
		candidate:     "Kaiju Squad 100 S02E03 1080p WEB-DL AAC2.0 H.264-KaijuSubs",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusRoundedSize,
		want:          corpusNoMatch,
	},
	{
		name:           "season numeral I with a season 1 marker",
		source:         "Kaiju.Squad.I.S01.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Squad.S01.1080p.WEB.H264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize + 500,
		sourceFiles:    corpusPackFiles("Kaiju.Squad.I.S01.1080p.WEB.H264-GRP", "Kaiju.Squad.I.S01E%02d.1080p.WEB.H264-GRP"),
		candidateFiles: corpusPackFiles("Kaiju.Squad.S01.1080p.WEB.H264-GRP", "Kaiju.Squad.S01E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
	},
	{
		name:           "season numeral XI with a season 11 marker",
		source:         "Kaiju.Squad.XI.S11.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Squad.S11.1080p.WEB.H264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize + 500,
		sourceFiles:    corpusPackFiles("Kaiju.Squad.XI.S11.1080p.WEB.H264-GRP", "Kaiju.Squad.XI.S11E%02d.1080p.WEB.H264-GRP"),
		candidateFiles: corpusPackFiles("Kaiju.Squad.S11.1080p.WEB.H264-GRP", "Kaiju.Squad.S11E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
	},
	{
		// The listing names the show "Kaiju Squad 100 II", so the numeral is part of the title and not season 2.
		name:           "seasonless season numeral pack against a season 1 listing of the same title",
		source:         "[GRP] Kaiju Squad 100 II [BD 1080p HEVC FLAC]",
		candidate:      "Kaiju Squad 100 II S01 1080p BluRay FLAC 2.0 x265-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize + 500,
		sourceFiles:    corpusPackFiles("[GRP] Kaiju Squad 100 II [BD 1080p HEVC FLAC]", "[GRP] Kaiju Squad 100 II - %02d [BD 1080p HEVC FLAC]"),
		candidateFiles: corpusPackFiles("[GRP] Kaiju Squad 100 II [BD 1080p HEVC FLAC]", "[GRP] Kaiju Squad 100 II - %02d [BD 1080p HEVC FLAC]"),
		want:           corpusMatch,
	},
	{
		name:           "AKA title",
		source:         "Kaiju.Kyoutai.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju Squad AKA Kaiju Kyoutai S02E05 1080p WEB H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusEpisodeSize + corpusNFOSize,
		candidateFiles: corpusEpisodeWithNFO("Kaiju.Squad.S02E05.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
	},
	{
		// An announce keeps the periods of a title; a dotted name cannot.
		name:          "period in title",
		source:        "The.Squad.of.Madame.K.AKA.Madame.K.1953.1080p.BluRay.FLAC.1.0.x264-GRP",
		candidate:     "The Squad of Madame K... AKA Madame K... 1953 1080p BluRay FLAC 1.0 x264-GRP",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		// An AKA after the episode number starts an episode title, not a show title.
		name:           "AKA episode title of another show",
		source:         "Beta.Show.S01E01.1080p.WEB.H264-GRP",
		candidate:      "Alpha Show S01E01 AKA Beta Show 1080p WEB H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusEpisodeSize + corpusNFOSize,
		candidateFiles: corpusEpisodeWithNFO("Alpha.Show.S01E01.1080p.WEB.H264-GRP"),
		want:           corpusNoMatch,
	},
	{
		name:           "slash title",
		source:         "Kaiju.Squad.Zero.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju/Squad Zero S02E05 1080p WEB H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusEpisodeSize + corpusNFOSize,
		candidateFiles: corpusEpisodeWithNFO("Kaiju.Squad.Zero.S02E05.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
	},
	{
		name:          "title that differs only by punctuation",
		source:        "Bob's Burgers S02E05 1080p WEB H264-GRP",
		candidate:     "Bobs.Burgers.S02E05.1080p.WEB.H264-GRP",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:          "yearless movie against the same movie with a year",
		source:        "Harbor.Lights.1080p.BluRay.x264-GRP",
		candidate:     "Harbor.Lights.2019.1080p.BluRay.x264-GRP",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:           "Sonarr alternate title",
		source:         "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Kyoutai.S02E05.1080p.WEB.H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusEpisodeSize + corpusNFOSize,
		candidateFiles: corpusEpisodeWithNFO("Kaiju.Kyoutai.S02E05.1080p.WEB.H264-GRP"),
		titles:         []string{"Kaiju Squad", "Kaiju Kyoutai"},
		want:           corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathCrossSeedTab: allowedDifference(corpusNoMatch, reasonNoAlternateTitles),
			pathPrefilter:    allowedDifference(corpusNoMatch, reasonNoAlternateTitles),
			pathCrossMatch:   allowedDifference(corpusNoMatch, reasonNoAlternateTitles),
			pathDedup:        allowedDifference(corpusNoMatch, reasonNoAlternateTitles),
		},
	},
	{
		name:          "bracket site against group tag",
		source:        "Kaiju.Squad.S02E05.1080p.WEB-DL.AAC2.0.H.264-KAIJU",
		candidate:     "[KAIJU] Kaiju Squad S02E05 [Web][MKV][h264][1080p][AAC 2.0][Softsubs (KAIJU)]",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:          "bracket fansub with a hyphen against its dash suffix",
		source:        "[Moe-Fansub] Kaiju Squad S02E05 [BD HEVC 1080p FLAC]",
		candidate:     "Kaiju Squad S02E05 1080p BluRay FLAC 2.0 x265-Moe-Fansub",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:          "dash suffix with a hyphen against its bracket fansub",
		source:        "Kaiju Squad S02E05 1080p BluRay FLAC 2.0 x265-Moe-Fansub",
		candidate:     "[Moe-Fansub] Kaiju Squad S02E05 [BD HEVC 1080p FLAC]",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:          "dash suffix with a hyphen after a non-text tag against its bracket fansub",
		source:        "Kaiju Squad S02E05 1080p BluRay FLAC 2.0 x264-Hi10P-Moe-Fansub",
		candidate:     "[Moe-Fansub] Kaiju Squad S02E05 [BD AVC 1080p FLAC]",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:          "last word of a hyphenated bracket fansub as a group",
		source:        "Kaiju Squad S02E05 1080p BluRay FLAC 2.0 x265-Fansub",
		candidate:     "[Moe-Fansub] Kaiju Squad S02E05 [BD HEVC 1080p FLAC]",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusNoMatch,
	},
	{
		name:          "longer dash suffix than the bracket fansub",
		source:        "Kaiju Squad S02E05 1080p BluRay FLAC 2.0 x265-Big-Moe-Fansub",
		candidate:     "[Moe-Fansub] Kaiju Squad S02E05 [BD HEVC 1080p FLAC]",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusNoMatch,
	},
	{
		name:          "dash suffix with a hyphen whose file has no group against its bracket fansub",
		source:        "Kaiju.Squad.S02E05.1080p.BluRay.FLAC.2.0.x265-Moe-Fansub",
		candidate:     "[Moe-Fansub] Kaiju Squad S02E05 [BD HEVC 1080p FLAC]",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		sourceFiles: qbt.TorrentFiles{{
			Name: "Kaiju.Squad.S02E05.1080p.BluRay.FLAC.2.0.x265-Moe-Fansub/Kaiju Squad S02E05 1080p BluRay x265.mkv",
			Size: corpusEpisodeSize,
		}},
		want: corpusMatch,
	},
	{
		name:           "bracket fansub pack with a hyphen against its dash suffix pack",
		source:         "[Moe-Fansub] Kaiju Squad S02 [BD HEVC 1080p FLAC]",
		candidate:      "Kaiju Squad S02 1080p BluRay FLAC 2.0 x265-Moe-Fansub",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize,
		sourceFiles:    corpusPackFiles("[Moe-Fansub] Kaiju Squad S02 [BD HEVC 1080p FLAC]", "[Moe-Fansub] Kaiju Squad S02E%02d [BD HEVC 1080p FLAC]"),
		candidateFiles: corpusPackFiles("Kaiju Squad S02 1080p BluRay FLAC 2.0 x265-Moe-Fansub", "Kaiju Squad S02E%02d 1080p BluRay FLAC 2.0 x265-Moe-Fansub"),
		want:           corpusMatch,
	},
	{
		name:          "bracket fansub without a hyphen against its dash suffix",
		source:        "[MoeFansub] Kaiju Squad S02E05 [BD HEVC 1080p FLAC]",
		candidate:     "Kaiju Squad S02E05 1080p BluRay FLAC 2.0 x265-MoeFansub",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:          "two dash suffixes with a hyphen",
		source:        "Kaiju.Squad.S02E05.1080p.BluRay.FLAC.2.0.x265-Moe-Fansub",
		candidate:     "Kaiju Squad S02E05 1080p BluRay FLAC 2.0 x265-Moe-Fansub",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:          "space-joined group and collective against the bracket collective",
		source:        "Kaiju Squad (2020) (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo) [Mura]",
		candidate:     "Kaiju Squad (2020) (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo Mura)",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:          "bracket collective against the space-joined group and collective",
		source:        "Kaiju Squad (2020) (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo Mura)",
		candidate:     "Kaiju Squad (2020) (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo) [Mura]",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:           "space-joined season pack against a bracket collective episode",
		source:         "Kaiju Squad S02E05 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo) [Mura]",
		candidate:      "Kaiju Squad S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo Mura)",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("Kaiju Squad S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo Mura)", "Kaiju Squad S02E%02d (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo) [Mura]"),
		want:           corpusMatch,
		differences:    packFromEpisodes(nil),
	},
	{
		name:          "space-joined group and collective against another bracket collective",
		source:        "Kaiju Squad (2020) (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo) [Other]",
		candidate:     "Kaiju Squad (2020) (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo Mura)",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusNoMatch,
	},
	{
		name:          "space-joined group and collective against the group alone",
		source:        "Kaiju Squad (2020) (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo)",
		candidate:     "Kaiju Squad (2020) (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo Mura)",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusNoMatch,
	},
	{
		name:          "reversed space-joined group and collective against the bracket collective",
		source:        "Kaiju Squad (2020) (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Kyo) [Mura]",
		candidate:     "Kaiju Squad (2020) (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - Mura Kyo)",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusNoMatch,
	},
	{
		name:           "bracket batch with a Ma10 tag against a season pack with a year",
		source:         "[KAIJU] Kaiju Squad (BD 720p Ma10 FLAC) [Dual-Audio]",
		candidate:      "Kaiju Squad 2003 AKA Kaiju Kyoutai S01 720p BluRay Dual-Audio FLAC 2.0 x265-KAIJU",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize,
		sourceFiles:    corpusPackFiles("[KAIJU] Kaiju Squad (BD 720p Ma10 FLAC) [Dual-Audio]", "[KAIJU] Kaiju Squad - %02d (BD 720p Ma10 FLAC)"),
		candidateFiles: corpusPackFiles("[KAIJU] Kaiju Squad (BD 720p Ma10 FLAC) [Dual-Audio]", "[KAIJU] Kaiju Squad - %02d (BD 720p Ma10 FLAC)"),
		want:           corpusMatch,
	},
	{
		name:          "two-sided CRC that differs",
		source:        "[KaijuSubs] Kaiju Squad - 81 (1080p) [ABCD1234].mkv",
		candidate:     "[KaijuSubs] Kaiju Squad - 81 (1080p) [EF567890].mkv",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusRoundedSize,
		want:          corpusNoMatch,
	},
	{
		name:          "two-sided CRC that agrees",
		source:        "[KaijuSubs] Kaiju Squad - 81 (1080p) [ABCD1234].mkv",
		candidate:     "[KaijuSubs] Kaiju Squad - 81 (1080p) [ABCD1234].mkv",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:          "one-sided CRC on the candidate",
		source:        "[KaijuSubs] Kaiju Squad - 81 (1080p).mkv",
		candidate:     "[KaijuSubs] Kaiju Squad - 81 (1080p) [ABCD1234].mkv",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		// Only the announce rounds the size. The torrent holds the file of the source.
		name:           "one-sided CRC on the source at a rounded size",
		source:         "[KaijuSubs] Kaiju Squad - 81 (1080p) [ABCD1234].mkv",
		candidate:      "[KaijuSubs] Kaiju Squad - 81 (1080p).mkv",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusRoundedSize,
		candidateFiles: qbt.TorrentFiles{{Name: "[KaijuSubs] Kaiju Squad - 81 (1080p).mkv", Size: corpusEpisodeSize}},
		want:           corpusMatch,
	},
	{
		name:          "WEB against WEB-DL",
		source:        "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:     "Kaiju.Squad.S02E05.1080p.WEB-DL.H264-GRP",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		// The NFO keeps the total sizes apart, so the exact-size fallback cannot
		// rescue the pair, and the HDR rule alone decides.
		name:          "DoVi HDR10 against a generic DV HDR",
		source:        "Kaiju.Squad.S02E05.2160p.WEB-DL.DDP5.1.DoVi.HDR10.H.265-GRP",
		candidate:     "Kaiju Squad S02E05 2160p WEB-DL DDP5.1 DV HDR H.265-GRP",
		sourceSize:    corpusEpisodeSize + corpusNFOSize,
		sourceFiles:   corpusEpisodeWithNFO("Kaiju.Squad.S02E05.2160p.WEB-DL.DDP5.1.DoVi.HDR10.H.265-GRP"),
		candidateSize: corpusEpisodeSize,
		want:          corpusMatch,
	},
	{
		name:           "web source relabel",
		source:         "Kaiju.Squad.S02.1080p.AMZN.WEBRip.DD2.0.x264-GRP",
		candidate:      "Kaiju.Squad.S02.1080p.AMZN.WEB-DL.DD+2.0.x264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize,
		sourceFiles:    corpusPackFiles("Kaiju.Squad.S02.1080p.AMZN.WEBRip.DD2.0.x264-GRP", "Kaiju.Squad.S02E%02d.1080p.AMZN.WEBRip.DD2.0.x264-GRP"),
		candidateFiles: corpusPackFiles("Kaiju.Squad.S02.1080p.AMZN.WEB-DL.DD+2.0.x264-GRP", "Kaiju.Squad.S02E%02d.1080p.AMZN.WEB-DL.DD+2.0.x264-GRP"),
		want:           corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathDedup: allowedDifference(corpusNoMatch, reasonDedupNoRelabel),
		},
	},
	{
		name:           "bracket batch BDRip against a BluRay season pack with a year",
		source:         "[KAIJU] Kaiju Squad (BDRip 1080p x264) [dual-audio]",
		candidate:      "Kaiju Squad S01 2003 1080p BluRay x264 FLAC 2.0 Dual Audio-KAIJU",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize,
		sourceFiles:    corpusPackFiles("[KAIJU] Kaiju Squad (BDRip 1080p x264) [dual-audio]", "[KAIJU] Kaiju Squad - %02d (BDRip 1080p x264)"),
		candidateFiles: corpusPackFiles("[KAIJU] Kaiju Squad (BDRip 1080p x264) [dual-audio]", "[KAIJU] Kaiju Squad - %02d (BDRip 1080p x264)"),
		want:           corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathDedup: allowedDifference(corpusNoMatch, reasonDedupNoExactSize),
		},
	},
	{
		name:           "bracket batch with a Hi10 tag against a Hi10P season pack with a year",
		source:         "[KAIJU] Kaiju Squad (BD 1080p Hi10 FLAC) [Dual-Audio]",
		candidate:      "Kaiju Squad 2014 S01 1080p BluRay Dual-Audio FLAC 2.0 Hi10P x264-KAIJU",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize,
		sourceFiles:    corpusPackFiles("[KAIJU] Kaiju Squad (BD 1080p Hi10 FLAC) [Dual-Audio]", "[KAIJU] Kaiju Squad - %02d (BD 1080p Hi10 FLAC)"),
		candidateFiles: corpusPackFiles("[KAIJU] Kaiju Squad (BD 1080p Hi10 FLAC) [Dual-Audio]", "[KAIJU] Kaiju Squad - %02d (BD 1080p Hi10 FLAC)"),
		want:           corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathDedup: allowedDifference(corpusNoMatch, reasonDedupNoExactSize),
		},
	},
	{
		name:          "title rescue at an exact size",
		source:        "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:     "Monster.Unit.S02E05.1080p.WEB.H264",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		rescue:        true,
		want:          corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathPrefilter: allowedDifference(corpusNoMatch, reasonNoTitleRescue),
			pathDedup:     allowedDifference(corpusNoMatch, reasonNoTitleRescue),
		},
	},
	{
		name:          "unrelated show of the same size",
		source:        "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:     "Harbor.Lights.S02E05.1080p.WEB.H264-GRP",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		// The new Match verdict has no sizes, so exact size does not admit the source.
		searchSource: "Harbor.Lights.S02E05.1080p.WEB.H264-GRP",
		want:         corpusNoMatch,
		differences: map[matchPath]corpusDifference{
			pathCrossSeedTab: allowedDifference(corpusMatch, reasonRetitledSameSizes),
			pathCrossMatch:   allowedDifference(corpusMatch, reasonRetitledSameSizes),
		},
	},
	{
		name:          "local season pack against an episode",
		source:        "Kaiju.Squad.S02.1080p.WEB.H264-GRP",
		candidate:     "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		sourceSize:    corpusPackSize,
		candidateSize: corpusEpisodeSize,
		sourceFiles:   corpusPackFiles("Kaiju.Squad.S02.1080p.WEB.H264-GRP", "Kaiju.Squad.S02E%02d.1080p.WEB.H264-GRP"),
		want:          corpusNoMatch,
	},
	{
		name:           "announced season pack against a local episode",
		source:         "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Squad.S02.1080p.WEB.H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("Kaiju.Squad.S02.1080p.WEB.H264-GRP", "Kaiju.Squad.S02E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
		differences:    packFromEpisodes(nil),
	},
	{
		name:           "announced WEBRip season pack against a local WEB-DL episode",
		source:         "Kaiju.Squad.S02E05.1080p.AMZN.WEB-DL.DDP5.1.H.264-GRP",
		candidate:      "Kaiju.Squad.S02.1080p.AMZN.WEBRip.DDP5.1.H.264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("Kaiju.Squad.S02.1080p.AMZN.WEBRip.DDP5.1.H.264-GRP", "Kaiju.Squad.S02E%02d.1080p.AMZN.WEBRip.DDP5.1.H.264-GRP"),
		want:           corpusMatch,
		differences:    packFromEpisodes(nil),
	},
	{
		name:           "announced season pack with a Sonarr alternate title",
		source:         "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Kyoutai.S02.1080p.WEB.H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("Kaiju.Kyoutai.S02.1080p.WEB.H264-GRP", "Kaiju.Kyoutai.S02E%02d.1080p.WEB.H264-GRP"),
		titles:         []string{"Kaiju Squad", "Kaiju Kyoutai"},
		want:           corpusMatch,
		differences: packFromEpisodes(map[matchPath]corpusDifference{
			pathApply: allowedDifference(corpusNoMatch, "Missing input: Sonarr alternate titles. Search rejects a pack against one episode, so apply holds no stored verdict and no alternate titles, and the apply hint cannot pair the two titles."),
		}),
	},
	{
		name:           "announced season pack with an AKA title",
		source:         "Kaiju.Kyoutai.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju Squad AKA Kaiju Kyoutai S02 1080p WEB H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("Kaiju Squad AKA Kaiju Kyoutai S02 1080p WEB H264-GRP", "Kaiju.Squad.S02E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
		differences:    packFromEpisodes(nil),
	},
	{
		name:           "announced season pack with a slash title",
		source:         "Kaiju.Squad.Zero.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju/Squad Zero S02 1080p WEB H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("Kaiju Squad Zero S02 1080p WEB H264-GRP", "Kaiju.Squad.Zero.S02E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
		differences:    packFromEpisodes(nil),
	},
	{
		name:           "announced seasonless season numeral pack",
		source:         "Kaiju Squad 100 S02E05 1080p BluRay FLAC 2.0 x265-GRP",
		candidate:      "[GRP] Kaiju Squad 100 II [BD 1080p HEVC FLAC]",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("[GRP] Kaiju Squad 100 II [BD 1080p HEVC FLAC]", "[GRP] Kaiju Squad 100 II - %02d [BD 1080p HEVC FLAC]"),
		want:           corpusMatch,
		differences:    packFromEpisodes(nil),
	},
	{
		name:           "announced season pack with a season numeral",
		source:         "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Squad.II.S02.1080p.WEB.H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("Kaiju.Squad.II.S02.1080p.WEB.H264-GRP", "Kaiju.Squad.II.S02E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
		differences:    packFromEpisodes(nil),
	},
	{
		name:           "identical season pack of a show whose name starts with an ignore word",
		source:         "Trailer.Park.Boys.S02.1080p.WEB.H264-GRP",
		candidate:      "Trailer.Park.Boys.S02.1080p.WEB.H264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize,
		sourceFiles:    corpusPackFiles("Trailer.Park.Boys.S02.1080p.WEB.H264-GRP", "Trailer.Park.Boys.S02E%02d.1080p.WEB.H264-GRP"),
		candidateFiles: corpusPackFiles("Trailer.Park.Boys.S02.1080p.WEB.H264-GRP", "Trailer.Park.Boys.S02E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
	},
	{
		name:           "identical season pack of a show named after an ignore word",
		source:         "Extras.S02.1080p.WEB.H264-GRP",
		candidate:      "Extras.S02.1080p.WEB.H264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize,
		sourceFiles:    corpusPackFiles("Extras.S02.1080p.WEB.H264-GRP", "Extras.S02E%02d.1080p.WEB.H264-GRP"),
		candidateFiles: corpusPackFiles("Extras.S02.1080p.WEB.H264-GRP", "Extras.S02E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
	},
	{
		// The season folder name has no title, so the Read release takes it from the files.
		name:           "season folder pack against its season pack listing",
		source:         "Season 2",
		candidate:      "Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize,
		sourceFiles:    corpusPackFiles("Season 2", "Kaiju Squad (2020) - S02E%02d - Episode (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)"),
		candidateFiles: corpusPackFiles("Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)", "Kaiju Squad (2020) - S02E%02d - Episode (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)"),
		want:           corpusMatch,
	},
	{
		// The CRC tag of an episode file does not name the pack, so the pack listing
		// still matches when its size is rounded.
		name:           "season folder pack of CRC-tagged episodes against its season pack listing",
		source:         "Season 2",
		candidate:      "[smol] Kaiju Squad S02 (BD 1080p HEVC Opus)",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize + 500,
		sourceFiles:    corpusPackFiles("Season 2", "[smol] Kaiju Squad - S02E%02d (BD 1080p HEVC Opus) [DF06DEC0]"),
		candidateFiles: corpusPackFiles("[smol] Kaiju Squad S02 (BD 1080p HEVC Opus)", "[smol] Kaiju Squad - S02E%02d (BD 1080p HEVC Opus) [DF06DEC0]"),
		want:           corpusMatch,
	},
	{
		// The bonus video is the largest file, and it names no season, so the Read
		// release takes its identity from the largest episode.
		name:           "season folder pack with a large bonus video against its season pack listing",
		source:         "Season 2",
		candidate:      "Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)",
		sourceSize:     corpusPackSize + 2*corpusEpisodeSize,
		candidateSize:  corpusPackSize + 2*corpusEpisodeSize,
		sourceFiles:    corpusPackWithBonus("Season 2"),
		candidateFiles: corpusPackWithBonus("Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)"),
		want:           corpusMatch,
	},
	{
		// The special is the largest file, and it names season 0, so the Read
		// release takes its identity from the largest episode of season 2.
		name:           "season folder pack with a large special against its season pack listing",
		source:         "Season 2",
		candidate:      "Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)",
		sourceSize:     corpusPackSize + 3*corpusEpisodeSize,
		candidateSize:  corpusPackSize + 3*corpusEpisodeSize,
		sourceFiles:    corpusPackWithSpecial("Season 2"),
		candidateFiles: corpusPackWithSpecial("Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)"),
		want:           corpusMatch,
	},
	{
		name:           "season folder pack of another show",
		source:         "Season 2",
		candidate:      "Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize + 500,
		sourceFiles:    corpusPackFiles("Season 2", "Harbor Lights (2020) - S02E%02d - Episode (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)"),
		candidateFiles: corpusPackFiles("Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)", "Kaiju Squad (2020) - S02E%02d - Episode (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)"),
		want:           corpusNoMatch,
	},
	{
		// Files of another season lend the season folder name nothing.
		name:           "season folder pack whose files hold another season",
		source:         "Season 2",
		candidate:      "Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize + 500,
		sourceFiles:    corpusPackFiles("Season 2", "Kaiju Squad (2020) - S03E%02d - Episode (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)"),
		candidateFiles: corpusPackFiles("Kaiju Squad (2020) S02 (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)", "Kaiju Squad (2020) - S02E%02d - Episode (1080p AMZN WEB-DL x265 10bit EAC3 5.1 - GRP)"),
		want:           corpusNoMatch,
	},
	{
		name:           "one-sided HYBRID within the size tolerance, extra folder on the source",
		source:         "Kaiju.Squad.S01.1080p.BluRay.x264-GRP",
		candidate:      "Kaiju.Squad.S01.1080p.BluRay.HYBRID.x264-GRP",
		sourceSize:     corpusPackSize + corpusHybridExtrasSize,
		candidateSize:  corpusPackSize,
		sourceFiles:    corpusPackWithExtras("Kaiju.Squad.S01.1080p.BluRay.x264-GRP"),
		candidateFiles: corpusPackFiles("Kaiju.Squad.S01.1080p.BluRay.HYBRID.x264-GRP", "Kaiju.Squad.S01E%02d.1080p.BluRay.HYBRID.x264-GRP"),
		want:           corpusMatch,
		differences:    hybridSearchOnlyDifferences(),
	},
	{
		name:           "one-sided HYBRID within the size tolerance, different file sizes",
		source:         "Kaiju.Squad.S01.1080p.BluRay.x264-GRP",
		candidate:      "Kaiju.Squad.S01.1080p.BluRay.HYBRID.x264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize - corpusHybridExtrasSize,
		sourceFiles:    corpusPackFiles("Kaiju.Squad.S01.1080p.BluRay.x264-GRP", "Kaiju.Squad.S01E%02d.1080p.BluRay.x264-GRP"),
		candidateFiles: corpusResized(corpusPackFiles("Kaiju.Squad.S01.1080p.BluRay.HYBRID.x264-GRP", "Kaiju.Squad.S01E%02d.1080p.BluRay.HYBRID.x264-GRP"), corpusEpisodeSize-corpusHybridExtrasSize/10),
		// The release names decide here. The file comparison after the download
		// rejects the pair, because the episode sizes differ.
		want:        corpusMatch,
		differences: hybridSearchOnlyDifferences(),
	},
	{
		name:          "one-sided HYBRID outside the size tolerance",
		source:        "Kaiju.Squad.S01.1080p.BluRay.x264-GRP",
		candidate:     "Kaiju.Squad.S01.1080p.BluRay.HYBRID.x264-GRP",
		sourceSize:    corpusPackSize,
		candidateSize: corpusPackSize * 9 / 10,
		want:          corpusNoMatch,
	},
	{
		name:          "one-sided HYBRID with an exact size",
		source:        "Kaiju.Squad.S01.1080p.BluRay.x264-GRP",
		candidate:     "Kaiju.Squad.S01.1080p.BluRay.HYBRID.x264-GRP",
		sourceSize:    corpusPackSize,
		candidateSize: corpusPackSize,
		want:          corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathDedup: allowedDifference(corpusNoMatch, reasonDedupNoExactSize),
		},
	},
	{
		name:          "one-sided HYBRID with an exact size and no group",
		source:        "Kaiju.Squad.S01.1080p.BluRay.x264",
		candidate:     "Kaiju.Squad.S01.1080p.BluRay.HYBRID.x264",
		sourceSize:    corpusPackSize,
		candidateSize: corpusPackSize,
		want:          corpusMatch,
		differences:   hybridSearchOnlyDifferences(),
	},
	{
		name:          "HYBRID against IMAX within the size tolerance",
		source:        "Kaiju.Squad.S01.1080p.BluRay.IMAX.x264-GRP",
		candidate:     "Kaiju.Squad.S01.1080p.BluRay.HYBRID.x264-GRP",
		sourceSize:    corpusPackSize,
		candidateSize: corpusPackSize - corpusHybridExtrasSize,
		want:          corpusNoMatch,
	},
	{
		// Strict matching fails on HYBRID first here, so search runs the HYBRID
		// tolerance, and IMAX must still reject the pair.
		name:          "HYBRID against IMAX within the size tolerance, HYBRID on the source",
		source:        "Kaiju.Squad.S01.1080p.BluRay.HYBRID.x264-GRP",
		candidate:     "Kaiju.Squad.S01.1080p.BluRay.IMAX.x264-GRP",
		sourceSize:    corpusPackSize,
		candidateSize: corpusPackSize - corpusHybridExtrasSize,
		want:          corpusNoMatch,
	},
	{
		name:          "one-sided IMAX within the size tolerance",
		source:        "Kaiju.Squad.S01.1080p.BluRay.x264-GRP",
		candidate:     "Kaiju.Squad.S01.1080p.BluRay.IMAX.x264-GRP",
		sourceSize:    corpusPackSize,
		candidateSize: corpusPackSize - corpusHybridExtrasSize,
		want:          corpusNoMatch,
	},
	{
		name:          "one-sided HYBRID and a codec difference within the size tolerance",
		source:        "Kaiju.Squad.S01.1080p.BluRay.x264-GRP",
		candidate:     "Kaiju.Squad.S01.1080p.BluRay.HYBRID.x265-GRP",
		sourceSize:    corpusPackSize,
		candidateSize: corpusPackSize - corpusHybridExtrasSize,
		want:          corpusNoMatch,
	},
}

var corpusPaths = []struct {
	path    matchPath
	verdict func(t *testing.T, row corpusRow) corpusVerdict
}{
	{pathSearch, searchCorpusVerdict},
	{pathRetry, retryCorpusVerdict},
	{pathApply, applyCorpusVerdict},
	{pathSeasonPack, seasonPackCorpusVerdict},
	{pathCrossSeedTab, crossSeedTabCorpusVerdict},
	{pathPrefilter, prefilterCorpusVerdict},
	{pathCrossMatch, crossMatchCorpusVerdict},
	{pathDedup, dedupCorpusVerdict},
	{pathWebhook, webhookCorpusVerdict},
}

func TestMatchCorpus(t *testing.T) {
	unusedTickets := slices.Clone(corpusTickets)
	// A path that skips every row has dropped out of the corpus.
	unreadPaths := make(map[matchPath]bool, len(corpusPaths))
	for _, p := range corpusPaths {
		unreadPaths[p.path] = true
	}
	for _, row := range corpusRows {
		t.Run(row.name, func(t *testing.T) {
			for path, difference := range row.differences {
				require.NotEqual(t, row.want, difference.verdict, "%s: a difference must differ from the row verdict", path)
				require.NotEmpty(t, difference.reason, "%s: a difference needs a reason", path)
				if difference.issue != 0 {
					require.Contains(t, corpusTickets, difference.issue, "%s: a known difference must name a ticket in corpusTickets", path)
					unusedTickets = slices.DeleteFunc(unusedTickets, func(issue int) bool { return issue == difference.issue })
				}
			}

			for _, p := range corpusPaths {
				difference, listed := row.differences[p.path]
				want := row.want
				if listed {
					want = difference.verdict
				}
				got := p.verdict(t, row)
				if got == corpusSkip {
					require.False(t, listed, "%s: the path skips this row, so it cannot list a difference", p.path)
					continue
				}
				delete(unreadPaths, p.path)
				assert.Equal(t, want, got, "%s", p.path)
			}
		})
	}
	assert.Empty(t, unusedTickets, "remove the tickets that no known difference names")
	assert.Empty(t, unreadPaths, "these paths skip every row")
}

const (
	corpusInstanceID       = 1
	corpusSourceHash       = "c0a5000000000000000000000000000000000001"
	corpusCandidateHash    = "c0a5000000000000000000000000000000000002"
	corpusSearchSourceHash = "c0a5000000000000000000000000000000000003"
)

var corpusInstance = &models.Instance{ID: corpusInstanceID, Name: "main"}

func corpusFiles(name string, size int64, files qbt.TorrentFiles) qbt.TorrentFiles {
	if files != nil {
		return files
	}
	if !strings.HasSuffix(name, ".mkv") {
		name += ".mkv"
	}
	return qbt.TorrentFiles{{Name: name, Size: size}}
}

func corpusTorrent(hash, dir, name string, size int64) qbt.Torrent {
	return qbt.Torrent{
		Hash:        hash,
		Name:        name,
		Size:        size,
		TotalSize:   size,
		Progress:    1,
		State:       qbt.TorrentStateUploading,
		SavePath:    "/downloads/" + dir,
		ContentPath: "/downloads/" + dir + "/" + name,
	}
}

func (row corpusRow) sourceTorrent() qbt.Torrent {
	return corpusTorrent(corpusSourceHash, "source", row.source, row.sourceSize)
}

func (row corpusRow) candidateTorrent() qbt.Torrent {
	return corpusTorrent(corpusCandidateHash, "candidate", row.candidate, row.candidateSize)
}

// corpusService serves the source torrent, and the candidate torrent when the
// path compares two local torrents, from a fake sync manager.
func (row corpusRow) corpusService(withCandidate bool) *Service {
	torrents := []qbt.Torrent{row.sourceTorrent()}
	files := map[string]qbt.TorrentFiles{
		corpusSourceHash: corpusFiles(row.source, row.sourceSize, row.sourceFiles),
	}
	if withCandidate {
		torrents = append(torrents, row.candidateTorrent())
		files[corpusCandidateHash] = corpusFiles(row.candidate, row.candidateSize, row.candidateFiles)
	}
	return manualMatchTestService(corpusInstance, torrents, files)
}

func corpusVerdictOf(matched bool) corpusVerdict {
	if matched {
		return corpusMatch
	}
	return corpusNoMatch
}

// searchInput builds the source view from the source files, as search does.
func (row corpusRow) searchInput(t *testing.T, svc *Service) searchCandidateInput {
	t.Helper()
	source := row.sourceTorrent()
	sourceRead, _ := svc.readSearchSource(t.Context(), &source, corpusFiles(row.source, row.sourceSize, row.sourceFiles))
	return searchCandidateInput{
		Source:                sourceRead,
		Candidate:             namedRelease{release: svc.releaseCache.Parse(row.candidate), rawName: row.candidate},
		SourceTitles:          row.titles,
		EpisodeMap:            row.episodeMap,
		SourceSize:            row.sourceSize,
		CandidateSize:         row.candidateSize,
		TolerancePercent:      defaultSizeMismatchTolerancePercent,
		RescueTitleMismatches: row.rescue,
	}
}

// searchCorpusVerdict covers search, completion, interactive, RSS and autobrr
// apply: all of them classify through classifySearchCandidate. Search looks the
// source up, so the source side holds the alternate titles.
func searchCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(false)
	return corpusVerdictOf(svc.matcher().classifySearchCandidate(row.searchInput(t, svc)).Accepted)
}

func retryCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(false)
	usable := svc.searchUsablePredicate(row.searchInput(t, svc))
	return corpusVerdictOf(usable(jackett.SearchResult{Title: row.candidate, Size: row.candidateSize}))
}

// applyCorpusVerdict keeps the search verdict when search admitted the pair.
// For a season pack announce, apply also reports the season pack hint: the
// library holds an episode of the same show, so qui tries to build the pack.
func applyCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(false)
	input := row.searchInput(t, svc)
	boundHash := corpusSourceHash
	if row.searchSource != "" {
		compared := corpusTorrent(corpusSearchSourceHash, "compared", row.searchSource, row.candidateSize)
		comparedFiles := corpusFiles(row.searchSource, row.candidateSize, row.candidateFiles)
		svc = manualMatchTestService(corpusInstance, []qbt.Torrent{row.sourceTorrent(), compared}, map[string]qbt.TorrentFiles{
			corpusSourceHash:       corpusFiles(row.source, row.sourceSize, row.sourceFiles),
			corpusSearchSourceHash: comparedFiles,
		})
		input.Source, _ = svc.readSearchSource(t.Context(), &compared, comparedFiles)
		input.SourceSize = row.candidateSize
		boundHash = corpusSearchSourceHash
	}
	req := &FindCandidatesRequest{TorrentName: row.candidate, TargetInstanceIDs: []int{corpusInstanceID}}
	decision := svc.matcher().classifySearchCandidate(input)
	require.True(t, decision.Accepted || row.searchSource == "", "search must admit the search source: %s", decision.RejectReason)
	if decision.Accepted {
		req.SearchDecision = decision.provenance().bindSource(corpusInstanceID, boundHash)
	}
	// Read the target from the downloaded files, as processCrossSeedCandidate does.
	req.TargetRelease = svc.readApplyTarget(row.candidate,
		corpusFiles(row.candidate, row.candidateSize, row.candidateFiles), req.SearchDecision.admitted())
	resp, err := svc.FindCandidates(t.Context(), req)
	require.NoError(t, err)
	if resp.seasonPackEpisodeCandidates {
		return corpusMatch
	}
	for _, candidate := range resp.Candidates {
		if slices.ContainsFunc(candidate.Torrents, func(torrent qbt.Torrent) bool { return torrent.Hash == corpusSourceHash }) {
			return corpusMatch
		}
	}
	return corpusNoMatch
}

// seasonPackCorpusVerdict runs the season pack check of an announced pack
// against the local episode. A row with candidate files sends them as the
// torrent data, and the check then compares the local episode file with its
// pack file, as apply does. The other rows run the light check. The check reads only
// local episodes, so it skips a row whose source is not an episode, and a row
// whose candidate is an episode.
func seasonPackCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(false)
	pack := svc.matcher().parseSeasonPackName(row.candidate)
	if !isTVEpisode(svc.releaseCache.Parse(row.source)) || isTVEpisode(pack) {
		return corpusSkip
	}
	if !isTVSeasonPack(pack) {
		// CheckSeasonPackWebhook answers not_season_pack.
		return corpusNoMatch
	}
	var packEpisodes map[episodeIdentity]packEpisodeOrigin
	if row.candidateFiles != nil {
		packEpisodes = svc.matcher().extractPackEpisodes(row.candidateFiles, pack)
		require.NotEmpty(t, packEpisodes, "the candidate files hold no episode")
	}
	settings := models.DefaultCrossSeedAutomationSettings()
	settings.SeasonPackEnabled = true
	views := buildCrossInstanceViews(corpusInstance, []qbt.Torrent{row.sourceTorrent()})
	if row.candidateFiles != nil {
		matched := svc.matchEpisodeCandidatesDetailed(views, row.candidate, pack, packEpisodes, settings, row.titles)
		if len(matched) == 0 {
			return corpusNoMatch
		}
		selected, _, err := svc.resolveSeasonPackLocalFilesForCandidates(t.Context(), fsops.HostPaths, corpusInstanceID,
			matched, row.candidateFiles, pack, row.candidate, settings, row.titles, nil)
		require.NoError(t, err)
		return corpusVerdictOf(len(selected) > 0)
	}
	return corpusVerdictOf(len(svc.matchEpisodeCandidatesDetailed(views, row.candidate, pack, packEpisodes, settings, row.titles)) > 0)
}

func crossSeedTabCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(true)
	resp, err := svc.FindCrossSeeds(t.Context(), corpusInstanceID, corpusSourceHash)
	require.NoError(t, err)
	return corpusVerdictOf(slices.ContainsFunc(resp.Matches, func(match LocalMatch) bool { return match.Hash == corpusCandidateHash }))
}

// prefilterCorpusVerdict asks whether the content prefilter counts the
// candidate, seeded locally, as content that the source search already has.
func prefilterCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(true)
	source := row.sourceTorrent()
	views := buildCrossInstanceViews(corpusInstance, []qbt.Torrent{source, row.candidateTorrent()})
	matched, _, _, err := svc.findLayoutAwareContentPrefilterMatches(t.Context(), corpusInstanceID, corpusSourceHash,
		&source, svc.releaseCache.Parse(row.source), views)
	require.NoError(t, err)
	return corpusVerdictOf(slices.ContainsFunc(matched, func(match contentPrefilterMatchedTorrent) bool {
		return match.view.Hash == corpusCandidateHash
	}))
}

func crossMatchCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(true)
	result := svc.BuildCrossMatchSets(t.Context(), corpusInstanceID, CrossMatchNeeds{SameExists: true})
	_, matched := result.SameInstanceExists[corpusSourceHash]
	return corpusVerdictOf(matched)
}

func dedupCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(true)
	deduped, _ := svc.deduplicateSourceTorrents(t.Context(), corpusInstanceID, []qbt.Torrent{row.sourceTorrent(), row.candidateTorrent()})
	return corpusVerdictOf(len(deduped) == 1)
}

// webhookCorpusVerdict sends the candidate as the autobrr announce. The webhook
// check looks the announce up, so Sonarr serves the row's titles and map.
func webhookCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(false)
	svc.automationSettingsLoader = func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		settings := models.DefaultCrossSeedAutomationSettings()
		settings.RescueTitleMismatches = row.rescue
		return settings, nil
	}
	svc.arrService = &spyARRLookupService{result: &arr.ExternalIDsResult{Titles: row.titles, EpisodeMap: row.episodeMap}}
	resp, err := svc.CheckWebhook(t.Context(), &WebhookCheckRequest{TorrentName: row.candidate, Size: uint64(row.candidateSize)})
	require.NoError(t, err)
	return corpusVerdictOf(slices.ContainsFunc(resp.Matches, func(match WebhookCheckMatch) bool { return match.TorrentHash == corpusSourceHash }))
}
