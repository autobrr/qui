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

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/jackett"
)

// The match corpus runs every release-name pair through every match path (see
// GLOSSARY.md). A path that gives a verdict other than the row expects fails the
// test, unless the row lists that difference with a reason.
//
// Known gap: Dir Scan is not in the corpus. Its comparison lives in the dirscan
// package, and #3036 decides when it moves to the shared release reading.
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
	pathApply        matchPath = "apply replay"
	pathSeasonPack   matchPath = "season pack check"
	pathLocalMatches matchPath = "Local matches"
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
var corpusTickets = []int{
	3036, // read a release name in one module
	3037, // decide every match through one verdict
	3045, // season pack hint
	3048, // slash titles in the season pack check and Local matches
}

func allowedDifference(verdict corpusVerdict, reason string) corpusDifference {
	return corpusDifference{verdict: verdict, reason: reason}
}

func knownDifference(verdict corpusVerdict, issue int, reason string) corpusDifference {
	if issue == 0 {
		panic("a known difference must name a ticket: " + reason)
	}
	return corpusDifference{verdict: verdict, reason: reason, issue: issue}
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
	titles      []string
	episodeMap  *models.EpisodeMap
	rescue      bool
	want        corpusVerdict
	differences map[matchPath]corpusDifference
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

// corpusEpisodeWithNFO is an episode with an NFO file, so its total size is not
// equal to the bare episode. Local matches then cannot rescue the title, and
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
	for _, path := range []matchPath{pathSearch, pathRetry, pathLocalMatches, pathPrefilter, pathCrossMatch, pathDedup, pathWebhook} {
		differences[path] = allowedDifference(corpusNoMatch, "A season pack and one episode are different torrents. Only the season pack check and the apply hint pair them, because qui can build the pack from the local episodes.")
	}
	maps.Copy(differences, extra)
	return differences
}

const (
	reasonNoAlternateTitles = "This path does not look the show up, so it holds no Sonarr alternate titles."
	reasonNoEpisodeMap      = "This path holds no episode map, because qui looks up no Sonarr data for local names."
	reasonNoTitleRescue     = "Title rescue needs the recheck after the add. This path adds nothing, so it does not rescue a title, and the worst result is one more search."
	reasonNoRelabel         = "The web source relabel is a search rule. This path does not run it."
	reasonDedupKey          = "Dedup groups torrents by the lowercase title before the matcher runs, so the two titles never meet."
	reasonCrossMatchKey     = "Cross-match sets read the parsed names without raw names or files, so the title rules that need them do not run."
	reasonHintTitle         = "The season pack hint compares only the lowercase titles, so it does not see that the pack and the episode are the same show."
	reasonDirectionalCRC    = "Dedup compares the pair in list order, and the strict CRC rule accepts a CRC tag only on the second name."
)

var corpusRows = []corpusRow{
	{
		name:          "scene episode",
		source:        "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:     "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
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
		name:          "anime absolute against seasoned with an episode map",
		source:        "[KaijuSubs] Kaiju Squad - 81 (1080p).mkv",
		candidate:     "Kaiju Squad S04E15 1080p WEB-DL AAC2.0 H.264-KaijuSubs",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusRoundedSize,
		episodeMap:    &models.EpisodeMap{Season: 4, Episode: 15, Absolute: 81},
		want:          corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathLocalMatches: knownDifference(corpusNoMatch, 3037, reasonNoEpisodeMap),
			pathPrefilter:    knownDifference(corpusNoMatch, 3037, reasonNoEpisodeMap),
			pathCrossMatch:   knownDifference(corpusNoMatch, 3037, reasonNoEpisodeMap),
			pathDedup:        knownDifference(corpusNoMatch, 3037, reasonNoEpisodeMap),
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
		name:           "season numeral pack against season pack",
		source:         "Kaiju.Squad.III.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Squad.S03.1080p.WEB.H264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize + 500,
		sourceFiles:    corpusPackFiles("Kaiju.Squad.III.1080p.WEB.H264-GRP", "Kaiju.Squad.III.S03E%02d.1080p.WEB.H264-GRP"),
		candidateFiles: corpusPackFiles("Kaiju.Squad.S03.1080p.WEB.H264-GRP", "Kaiju.Squad.S03E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathCrossMatch: knownDifference(corpusNoMatch, 3036, reasonCrossMatchKey),
			pathDedup:      knownDifference(corpusNoMatch, 3036, reasonDedupKey),
		},
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
		differences: map[matchPath]corpusDifference{
			pathCrossMatch: knownDifference(corpusNoMatch, 3036, reasonCrossMatchKey),
			pathDedup:      knownDifference(corpusNoMatch, 3036, reasonDedupKey),
		},
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
	},
	{
		name:           "AKA title",
		source:         "Kaiju.Kyoutai.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju Squad AKA Kaiju Kyoutai S02E05 1080p WEB H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusEpisodeSize + corpusNFOSize,
		candidateFiles: corpusEpisodeWithNFO("Kaiju.Squad.S02E05.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathDedup: knownDifference(corpusNoMatch, 3036, reasonDedupKey),
		},
	},
	{
		name:           "slash title",
		source:         "Kaiju.Squad.Zero.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju/Squad Zero S02E05 1080p WEB H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusEpisodeSize + corpusNFOSize,
		candidateFiles: corpusEpisodeWithNFO("Kaiju.Squad.Zero.S02E05.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathLocalMatches: knownDifference(corpusNoMatch, 3048, "Local matches gets no raw names, so the slash rule does not run."),
			pathCrossMatch:   knownDifference(corpusNoMatch, 3036, reasonCrossMatchKey),
			pathDedup:        knownDifference(corpusNoMatch, 3036, reasonDedupKey),
		},
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
			pathLocalMatches: knownDifference(corpusNoMatch, 3037, reasonNoAlternateTitles),
			pathPrefilter:    knownDifference(corpusNoMatch, 3037, reasonNoAlternateTitles),
			pathCrossMatch:   knownDifference(corpusNoMatch, 3037, reasonNoAlternateTitles),
			pathDedup:        knownDifference(corpusNoMatch, 3037, reasonNoAlternateTitles),
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
		differences: map[matchPath]corpusDifference{
			pathDedup: knownDifference(corpusNoMatch, 3037, reasonDirectionalCRC),
		},
	},
	{
		name:          "one-sided CRC on the source at a rounded size",
		source:        "[KaijuSubs] Kaiju Squad - 81 (1080p) [ABCD1234].mkv",
		candidate:     "[KaijuSubs] Kaiju Squad - 81 (1080p).mkv",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusRoundedSize,
		want:          corpusNoMatch,
		differences: map[matchPath]corpusDifference{
			pathWebhook: allowedDifference(corpusMatch, "IRC (the chat that announces releases to autobrr) rounds sizes, so the webhook check cannot reach the exact-size checksum relaxation that apply uses. So it accepts a CRC tag on one name, and apply checks the pair again with the downloaded torrent."),
			pathApply:   allowedDifference(corpusMatch, "Apply compares the pair in reverse, with the new torrent as the source, and the strict CRC rule accepts a CRC tag on the local name. The torrent file check after the download has the final say."),
			pathDedup:   knownDifference(corpusMatch, 3037, reasonDirectionalCRC),
		},
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
		name:           "web source relabel",
		source:         "Kaiju.Squad.S02.1080p.AMZN.WEBRip.DD2.0.x264-GRP",
		candidate:      "Kaiju.Squad.S02.1080p.AMZN.WEB-DL.DD+2.0.x264-GRP",
		sourceSize:     corpusPackSize,
		candidateSize:  corpusPackSize,
		sourceFiles:    corpusPackFiles("Kaiju.Squad.S02.1080p.AMZN.WEBRip.DD2.0.x264-GRP", "Kaiju.Squad.S02E%02d.1080p.AMZN.WEBRip.DD2.0.x264-GRP"),
		candidateFiles: corpusPackFiles("Kaiju.Squad.S02.1080p.AMZN.WEB-DL.DD+2.0.x264-GRP", "Kaiju.Squad.S02E%02d.1080p.AMZN.WEB-DL.DD+2.0.x264-GRP"),
		want:           corpusMatch,
		differences: map[matchPath]corpusDifference{
			pathApply:        knownDifference(corpusNoMatch, 3037, "Apply replays the search decision, and its release prefilter has no branch for the web source relabel class."),
			pathLocalMatches: knownDifference(corpusNoMatch, 3037, reasonNoRelabel),
			pathPrefilter:    knownDifference(corpusNoMatch, 3037, reasonNoRelabel),
			pathCrossMatch:   knownDifference(corpusNoMatch, 3037, reasonNoRelabel),
			pathDedup:        knownDifference(corpusNoMatch, 3037, reasonNoRelabel),
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
			pathPrefilter:  allowedDifference(corpusNoMatch, reasonNoTitleRescue),
			pathCrossMatch: allowedDifference(corpusNoMatch, reasonNoTitleRescue),
			pathDedup:      allowedDifference(corpusNoMatch, reasonNoTitleRescue),
		},
	},
	{
		name:          "unrelated show of the same size",
		source:        "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:     "Harbor.Lights.S02E05.1080p.WEB.H264-GRP",
		sourceSize:    corpusEpisodeSize,
		candidateSize: corpusEpisodeSize,
		want:          corpusNoMatch,
		differences: map[matchPath]corpusDifference{
			pathLocalMatches: knownDifference(corpusMatch, 3037, "Local matches rescues a title at an exact size even when the user turned title rescue off."),
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
		name:           "announced season pack with a Sonarr alternate title",
		source:         "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Kyoutai.S02.1080p.WEB.H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("Kaiju.Kyoutai.S02.1080p.WEB.H264-GRP", "Kaiju.Kyoutai.S02E%02d.1080p.WEB.H264-GRP"),
		titles:         []string{"Kaiju Squad", "Kaiju Kyoutai"},
		want:           corpusMatch,
		differences: packFromEpisodes(map[matchPath]corpusDifference{
			pathApply: knownDifference(corpusNoMatch, 3045, reasonHintTitle),
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
		differences: packFromEpisodes(map[matchPath]corpusDifference{
			pathApply: knownDifference(corpusNoMatch, 3045, reasonHintTitle),
		}),
	},
	{
		name:           "announced season pack with a slash title",
		source:         "Kaiju.Squad.Zero.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju/Squad Zero S02 1080p WEB H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("Kaiju Squad Zero S02 1080p WEB H264-GRP", "Kaiju.Squad.Zero.S02E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
		differences: packFromEpisodes(map[matchPath]corpusDifference{
			pathApply:      knownDifference(corpusNoMatch, 3045, reasonHintTitle),
			pathSeasonPack: knownDifference(corpusNoMatch, 3048, "The season pack check gets no raw names, so the slash rule does not run."),
		}),
	},
	{
		name:           "announced season pack with a season numeral",
		source:         "Kaiju.Squad.S02E05.1080p.WEB.H264-GRP",
		candidate:      "Kaiju.Squad.II.S02.1080p.WEB.H264-GRP",
		sourceSize:     corpusEpisodeSize,
		candidateSize:  corpusPackSize,
		candidateFiles: corpusPackFiles("Kaiju.Squad.II.S02.1080p.WEB.H264-GRP", "Kaiju.Squad.II.S02E%02d.1080p.WEB.H264-GRP"),
		want:           corpusMatch,
		differences: packFromEpisodes(map[matchPath]corpusDifference{
			pathApply: knownDifference(corpusNoMatch, 3045, reasonHintTitle),
		}),
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
	{pathLocalMatches, localMatchesCorpusVerdict},
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
	corpusInstanceID    = 1
	corpusSourceHash    = "c0a5000000000000000000000000000000000001"
	corpusCandidateHash = "c0a5000000000000000000000000000000000002"
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
	files := corpusFiles(row.source, row.sourceSize, row.sourceFiles)
	return searchCandidateInput{
		Source:                svc.searchSourceReleaseViewFromFiles(t.Context(), &source, svc.releaseCache.Parse(row.source), files),
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

// applyCorpusVerdict replays the search decision when search admitted the pair.
// For a season pack announce, apply also reports the season pack hint: the
// library holds an episode of the same show, so qui tries to build the pack.
func applyCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(false)
	req := &FindCandidatesRequest{TorrentName: row.candidate, TargetInstanceIDs: []int{corpusInstanceID}}
	if decision := svc.matcher().classifySearchCandidate(row.searchInput(t, svc)); decision.Accepted {
		req.SearchDecision = decision.provenance().bindSource(corpusInstanceID, corpusSourceHash)
	}
	// Build the target view from the downloaded files, as processCrossSeedCandidate does.
	target := svc.releaseCache.Parse(row.candidate)
	req.TargetRelease = namedRelease{release: target, rawName: row.candidate}
	if !isTVRelease(target) || req.SearchDecision.admitted() {
		req.TargetRelease = svc.applyTargetReleaseViewFromFiles(row.candidate, target,
			corpusFiles(row.candidate, row.candidateSize, row.candidateFiles), req.SearchDecision.admitted())
	}
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

// seasonPackCorpusVerdict runs the light season pack check (no torrent data) of
// an announced pack against the local episode. The check reads only local
// episodes, so it skips a row whose source is not an episode, and a row whose
// candidate is an episode.
func seasonPackCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(false)
	pack := svc.releaseCache.Parse(row.candidate)
	if !isTVEpisode(svc.releaseCache.Parse(row.source)) || isTVEpisode(pack) {
		return corpusSkip
	}
	if !isTVSeasonPack(pack) {
		// CheckSeasonPackWebhook answers not_season_pack.
		return corpusNoMatch
	}
	settings := models.DefaultCrossSeedAutomationSettings()
	settings.SeasonPackEnabled = true
	views := buildCrossInstanceViews(corpusInstance, []qbt.Torrent{row.sourceTorrent()})
	return corpusVerdictOf(len(svc.matchEpisodeCandidatesDetailed(views, pack, nil, settings, row.titles)) > 0)
}

func localMatchesCorpusVerdict(t *testing.T, row corpusRow) corpusVerdict {
	t.Helper()
	svc := row.corpusService(true)
	resp, err := svc.FindLocalMatches(t.Context(), corpusInstanceID, corpusSourceHash, true)
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
