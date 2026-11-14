// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/pkg/stringutils"
)

const (
	gazelleSeamInstanceID = 1
	gazelleSeamHash       = "0123456789abcdef0123456789abcdef01234567"
	gazelleSeamName       = "Lumen Harbor - Quiet Tides (2024) [FLAC]"
	redTracker            = "https://flacsfor.me/announce"
	otherTracker          = "https://tracker.example.invalid/announce"
)

// gazelleSiteAnswers is what the stub of each RED/OPS site answers.
type gazelleSiteAnswers map[string]func(ctx context.Context) (*gazellemusic.Match, error)

// newGazelleSeamService builds a service for the search of one music torrent
// with the tracker tracker. Each RED/OPS site answers with answers; a site
// with no answer finds no match. No Torznab indexer exists.
func newGazelleSeamService(t *testing.T, tracker string, settings *models.CrossSeedAutomationSettings, answers gazelleSiteAnswers) *Service {
	t.Helper()
	// Not parallel: the test replaces package-level vars. findGazelleMatch
	// below answers for the sites, so no client sends a request.
	prevNewClient := newGazelleClient
	newGazelleClient = func(host, _, apiKey string) (*gazellemusic.Client, error) {
		return gazellemusic.NewClient(host, "http://127.0.0.1:1", apiKey)
	}
	prevFindMatch := findGazelleMatch
	findGazelleMatch = func(ctx context.Context, client *gazellemusic.Client, _ []byte, _ map[string]int64, _ int64) (*gazellemusic.Match, error) {
		if answer := answers[client.Host()]; answer != nil {
			return answer(ctx)
		}
		// Like the real lookup, a site fails when the search has no time left.
		return nil, ctx.Err()
	}
	t.Cleanup(func() {
		newGazelleClient = prevNewClient
		findGazelleMatch = prevFindMatch
	})

	source := qbt.Torrent{Hash: gazelleSeamHash, Name: gazelleSeamName, Size: 123, Progress: 1, Tracker: tracker}
	return &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{gazelleSeamInstanceID: {ID: gazelleSeamInstanceID, Name: "music"}}},
		syncManager: &hashFilteringSyncManager{gazelleSkipHashSyncManager{
			torrents:    []qbt.Torrent{source},
			filesByHash: map[string]qbt.TorrentFiles{gazelleSeamHash: {{Name: gazelleSeamName + "/01 - Lumen Harbor - Quiet Tides.flac", Size: 123}}},
		}},
		releaseCache:      NewReleaseCache(),
		searchResultCache: ttlcache.New[string, cachedTorrentSearchResults](),
		stringNormalizer:  stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return settings, nil
		},
	}
}

func bothGazelleKeys() *models.CrossSeedAutomationSettings {
	return &models.CrossSeedAutomationSettings{GazelleEnabled: true, RedactedAPIKey: "red-key", OrpheusAPIKey: "ops-key"}
}

func gazelleStatuses(lookups []GazelleLookup) map[string]string {
	out := make(map[string]string, len(lookups))
	for _, l := range lookups {
		out[l.Site] = string(l.Status) + " " + string(l.Reason)
	}
	return out
}

// A dialog search reports one RED/OPS lookup for each site.
func TestSearchTorrentMatches_GazelleLookups(t *testing.T) {
	match := func(context.Context) (*gazellemusic.Match, error) {
		return &gazellemusic.Match{TorrentID: 42, Title: gazelleSeamName, Size: 123, Reason: "hash"}, nil
	}
	rejected := func(context.Context) (*gazellemusic.Match, error) {
		return nil, fmt.Errorf("%w: invalid token", gazellemusic.ErrAccessDenied)
	}
	tests := []struct {
		name     string
		tracker  string
		settings *models.CrossSeedAutomationSettings
		answers  gazelleSiteAnswers
		opts     TorrentSearchOptions
		want     map[string]string
		// wantIndexer is the indexer of the one result row, or "" for none.
		wantIndexer string
	}{
		{
			name:     "a torrent from RED asks only OPS",
			tracker:  redTracker,
			settings: bothGazelleKeys(),
			want:     map[string]string{"redacted.sh": "not_asked source_site", "orpheus.network": "no_match "},
		},
		{
			name:        "a match names the site in the result row",
			tracker:     redTracker,
			settings:    bothGazelleKeys(),
			answers:     gazelleSiteAnswers{"orpheus.network": match},
			want:        map[string]string{"redacted.sh": "not_asked source_site", "orpheus.network": "match "},
			wantIndexer: "Orpheus (API)",
		},
		{
			name:     "a rejected key fails the site",
			tracker:  redTracker,
			settings: bothGazelleKeys(),
			answers:  gazelleSiteAnswers{"orpheus.network": rejected},
			want:     map[string]string{"redacted.sh": "not_asked source_site", "orpheus.network": "failed key_rejected"},
		},
		{
			name:     "a site that the request leaves out is not selected",
			tracker:  otherTracker,
			settings: bothGazelleKeys(),
			opts:     TorrentSearchOptions{GazelleSites: []string{"redacted.sh"}},
			want:     map[string]string{"redacted.sh": "no_match ", "orpheus.network": "not_asked not_selected"},
		},
		{
			name:     "RED/OPS off is disabled for both sites",
			tracker:  otherTracker,
			settings: &models.CrossSeedAutomationSettings{GazelleEnabled: false, RedactedAPIKey: "red-key", OrpheusAPIKey: "ops-key"},
			want:     map[string]string{"redacted.sh": "not_asked disabled", "orpheus.network": "not_asked disabled"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newGazelleSeamService(t, tt.tracker, tt.settings, tt.answers)

			resp, err := svc.SearchTorrentMatches(t.Context(), gazelleSeamInstanceID, gazelleSeamHash, tt.opts)

			require.NoError(t, err)
			require.Equal(t, tt.want, gazelleStatuses(resp.GazelleLookups))
			if tt.wantIndexer == "" {
				require.Empty(t, resp.Results)
				return
			}
			require.Len(t, resp.Results, 1)
			require.Equal(t, tt.wantIndexer, resp.Results[0].Indexer)
		})
	}
}

// A site that does not answer in time fails alone; the other site still
// answers within the limit of the search.
func TestSearchTorrentMatches_SlowGazelleSiteTimesOutAlone(t *testing.T) {
	svc := newGazelleSeamService(t, otherTracker, bothGazelleKeys(), gazelleSiteAnswers{
		"redacted.sh": func(ctx context.Context) (*gazellemusic.Match, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	synctest.Test(t, func(t *testing.T) {
		resp, err := svc.SearchTorrentMatches(t.Context(), gazelleSeamInstanceID, gazelleSeamHash, TorrentSearchOptions{})

		require.NoError(t, err)
		require.Equal(t, map[string]string{"redacted.sh": "failed timed_out", "orpheus.network": "no_match "}, gazelleStatuses(resp.GazelleLookups))
	})
}

// A rejected key opens the System notice of that site, and a later answer of
// the site closes it.
func TestSearchTorrentMatches_RejectedKeyOpensSystemNotice(t *testing.T) {
	rejectKey := true
	svc := newGazelleSeamService(t, redTracker, bothGazelleKeys(), gazelleSiteAnswers{
		"orpheus.network": func(context.Context) (*gazellemusic.Match, error) {
			if rejectKey {
				return nil, fmt.Errorf("%w: invalid token", gazellemusic.ErrAccessDenied)
			}
			return nil, nil
		},
	})
	notifier := &recordingNotifier{}
	svc.systemNotices = systemnotice.New(notifier)

	_, err := svc.SearchTorrentMatches(t.Context(), gazelleSeamInstanceID, gazelleSeamHash, TorrentSearchOptions{})
	require.NoError(t, err)

	notices := svc.systemNotices.List()
	require.Len(t, notices, 1)
	require.Equal(t, systemnotice.OrpheusKeyRejected, notices[0].Kind)
	require.Len(t, notifier.Events(), 1)
	require.Equal(t, "Orpheus rejected the API key", notifier.Events()[0].Title)

	rejectKey = false
	_, err = svc.SearchTorrentMatches(t.Context(), gazelleSeamInstanceID, gazelleSeamHash, TorrentSearchOptions{})
	require.NoError(t, err)
	require.Empty(t, svc.systemNotices.List(), "an answer of the site closes its notice")
}

// The notice kind of each site has a title and a fix for the notification.
func TestKeyRejectedNoticeOfEachSiteHasATitle(t *testing.T) {
	for _, site := range gazellemusic.Sites() {
		notifier := &recordingNotifier{}
		systemnotice.New(notifier).Open(keyRejectedNotice(site), "cause")
		require.Len(t, notifier.Events(), 1)
		require.Equal(t, site.ShortName+" rejected the API key", notifier.Events()[0].Title)
		require.Contains(t, notifier.Events()[0].Message, "Torrents permission")
	}
}

// The settings save closes the notice of a site whose key it checked, cleared,
// or turned off.
func TestGazelleKeysSavedClosesNotices(t *testing.T) {
	tests := []struct {
		name     string
		settings *models.CrossSeedAutomationSettings
		checked  []string
		wantOpen []systemnotice.Kind
	}{
		{
			name:     "a stored key that the save did not check keeps its notice",
			settings: &models.CrossSeedAutomationSettings{GazelleEnabled: true, RedactedAPIKey: "<redacted>", OrpheusAPIKey: "<redacted>"},
			wantOpen: []systemnotice.Kind{systemnotice.RedactedKeyRejected, systemnotice.OrpheusKeyRejected},
		},
		{
			name:     "a key that passed the check",
			settings: &models.CrossSeedAutomationSettings{GazelleEnabled: true, RedactedAPIKey: "<redacted>", OrpheusAPIKey: "<redacted>"},
			checked:  []string{"orpheus.network"},
			wantOpen: []systemnotice.Kind{systemnotice.RedactedKeyRejected},
		},
		{
			name:     "a cleared key",
			settings: &models.CrossSeedAutomationSettings{GazelleEnabled: true, OrpheusAPIKey: "<redacted>"},
			wantOpen: []systemnotice.Kind{systemnotice.OrpheusKeyRejected},
		},
		{
			name:     "RED/OPS turned off",
			settings: &models.CrossSeedAutomationSettings{GazelleEnabled: false, RedactedAPIKey: "<redacted>", OrpheusAPIKey: "<redacted>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{systemNotices: systemnotice.New(nil)}
			svc.systemNotices.Open(systemnotice.RedactedKeyRejected, "rejected")
			svc.systemNotices.Open(systemnotice.OrpheusKeyRejected, "rejected")

			svc.GazelleKeysSaved(tt.settings, tt.checked)

			notices := svc.systemNotices.List()
			open := make([]systemnotice.Kind, 0, len(notices))
			for _, notice := range notices {
				open = append(open, notice.Kind)
			}
			require.ElementsMatch(t, tt.wantOpen, open)
		})
	}
}

// The notification of a completion search holds one RED/OPS line.
func TestExecuteCompletionSearch_NotificationHasGazelleLine(t *testing.T) {
	settings := &models.CrossSeedAutomationSettings{GazelleEnabled: true, RedactedAPIKey: "red-key"}
	svc := newGazelleSeamService(t, "https://home.opsfet.ch/announce", settings, gazelleSiteAnswers{
		"redacted.sh": func(context.Context) (*gazellemusic.Match, error) {
			return &gazellemusic.Match{TorrentID: 42, Title: gazelleSeamName, Size: 123, Reason: "hash"}, nil
		},
	})
	notifier := &recordingNotifier{}
	svc.notifier = notifier
	svc.crossSeedInvoker = func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
		return &CrossSeedResponse{Success: true}, nil
	}
	source := qbt.Torrent{Hash: gazelleSeamHash, Name: gazelleSeamName, Size: 123, Progress: 1, Tracker: "https://home.opsfet.ch/announce"}

	err := svc.executeCompletionSearch(t.Context(), gazelleSeamInstanceID, &source, settings, &models.InstanceCrossSeedCompletionSettings{InstanceID: gazelleSeamInstanceID, Enabled: true})

	require.NoError(t, err)
	require.Len(t, notifier.Events(), 1)
	require.Contains(t, notifier.Events()[0].Message, "\nRED/OPS: Redacted match, Orpheus not asked (no API key)")
}

// A Library Scan stores one RED/OPS summary for the run, and adds no row for a
// site that found no match or that the run did not ask.
func TestSearchRunLoop_StoresGazelleSummary(t *testing.T) {
	album := "Artist - Album (2024) [WEB FLAC]"
	film := "Some.Movie.2019.1080p.BluRay.x264-GRP"
	music := qbt.Torrent{Hash: fmt.Sprintf("%040x", 21), Name: album, Progress: 1.0, Size: 1_000, AddedOn: 1, Tracker: otherTracker}
	movie := qbt.Torrent{Hash: fmt.Sprintf("%040x", 22), Name: film, Progress: 1.0, Size: 8_000, AddedOn: 2, Tracker: otherTracker}
	svc, state := newSearchRunLoopFixture(t, "crossseed-runloop-gazelle-summary", &hashFilteringSyncManager{
		torrents: []qbt.Torrent{music, movie},
		filesByHash: map[string]qbt.TorrentFiles{
			music.Hash: {{Name: album + "/01 - Track.flac", Size: 1_000}},
			movie.Hash: {{Name: film + "/movie.mkv", Size: 8_000}},
		},
	})

	runSearchLoopToCompletion(t, svc, state)

	want := []models.CrossSeedGazelleSiteSummary{
		{Site: "redacted.sh", Name: "Redacted", NotAsked: map[string]int{"no_api_key": 2}},
		{Site: "orpheus.network", Name: "Orpheus", Asked: 1, NotAsked: map[string]int{"not_music": 1}},
	}
	stored, err := svc.automationStore.GetSearchRun(t.Context(), state.run.ID)
	require.NoError(t, err)
	require.Equal(t, want, stored.GazelleSummary)
	for _, row := range state.run.Results {
		require.NotContains(t, row.IndexerName, "(API)", "no match and not asked add no RED/OPS row")
	}
}

// In one Library Scan run, a site that rejected the key gets no more requests.
// Those skips must not open the notice again after the user saved a new key.
func TestSkippedRejectedSiteDoesNotReopenNotice(t *testing.T) {
	stubGazelleMatchLookupError(t, fmt.Errorf("%w: invalid token", gazellemusic.ErrAccessDenied))
	source := &qbt.Torrent{Hash: gazelleSeamHash, Name: gazelleSeamName, Size: 123, Progress: 1, Tracker: redTracker}
	files := qbt.TorrentFiles{{Name: gazelleSeamName + "/01 - Track.flac", Size: 123}}
	notifier := &recordingNotifier{}
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager:      &gazelleSkipHashSyncManager{torrents: []qbt.Torrent{*source}, filesByHash: map[string]qbt.TorrentFiles{gazelleSeamHash: files}},
		systemNotices:    systemnotice.New(notifier),
	}
	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	first := lookupGazelleForTest(t, svc, source, files, clients)
	require.Equal(t, GazelleReasonKeyRejected, gazelleLookupOf(t, first, "orpheus.network").Reason)
	require.Len(t, svc.systemNotices.List(), 1)

	svc.GazelleKeysSaved(&models.CrossSeedAutomationSettings{GazelleEnabled: true, OrpheusAPIKey: "<redacted>"}, []string{"orpheus.network"})
	second := lookupGazelleForTest(t, svc, source, files, clients)

	require.Equal(t, GazelleReasonKeyRejected, gazelleLookupOf(t, second, "orpheus.network").Reason, "the run keeps skipping the site")
	require.Empty(t, svc.systemNotices.List())
	require.Len(t, notifier.Events(), 1)
	require.True(t, gazelleRequestsSent(first), "the rejection was a request, so the run paces")
	require.False(t, gazelleRequestsSent(second), "the skip sent no request")
}

// A client from before a settings save carries the old key. Its first
// rejection must not open a notice that the save closed.
//
// regression: #4126
func TestOldClientRejectionAfterSaveOpensNoNotice(t *testing.T) {
	stubGazelleMatchLookupError(t, fmt.Errorf("%w: invalid token", gazellemusic.ErrAccessDenied))
	source := &qbt.Torrent{Hash: gazelleSeamHash, Name: gazelleSeamName, Size: 123, Progress: 1, Tracker: redTracker}
	files := qbt.TorrentFiles{{Name: gazelleSeamName + "/01 - Track.flac", Size: 123}}
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager:      &gazelleSkipHashSyncManager{torrents: []qbt.Torrent{*source}, filesByHash: map[string]qbt.TorrentFiles{gazelleSeamHash: files}},
		systemNotices:    systemnotice.New(nil),
	}
	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	svc.GazelleKeysSaved(&models.CrossSeedAutomationSettings{GazelleEnabled: true, OrpheusAPIKey: "new-key"}, []string{"orpheus.network"})
	lookups := lookupGazelleForTest(t, svc, source, files, clients)

	require.Equal(t, GazelleReasonKeyRejected, gazelleLookupOf(t, lookups, "orpheus.network").Reason)
	require.Empty(t, svc.systemNotices.List())
}

// A save that leaves the key alone does not stop a running scan from
// reporting a rejection of that key.
//
// regression: #4126
func TestUnrelatedSaveKeepsRejectionNotice(t *testing.T) {
	stubGazelleMatchLookupError(t, fmt.Errorf("%w: invalid token", gazellemusic.ErrAccessDenied))
	source := &qbt.Torrent{Hash: gazelleSeamHash, Name: gazelleSeamName, Size: 123, Progress: 1, Tracker: redTracker}
	files := qbt.TorrentFiles{{Name: gazelleSeamName + "/01 - Track.flac", Size: 123}}
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager:      &gazelleSkipHashSyncManager{torrents: []qbt.Torrent{*source}, filesByHash: map[string]qbt.TorrentFiles{gazelleSeamHash: files}},
		systemNotices:    systemnotice.New(nil),
	}
	clients, err := gazelleClientsForTest()
	require.NoError(t, err)

	svc.GazelleKeysSaved(&models.CrossSeedAutomationSettings{GazelleEnabled: true, OrpheusAPIKey: domain.RedactedStr}, nil)
	lookupGazelleForTest(t, svc, source, files, clients)

	require.Len(t, svc.systemNotices.List(), 1)
}

// An answer to a client from before a key change does not close the notice
// of the new key.
//
// regression: #4126
func TestOldClientAnswerKeepsNewKeyNotice(t *testing.T) {
	stubGazelleMatchLookup(t)
	source := &qbt.Torrent{Hash: gazelleSeamHash, Name: gazelleSeamName, Size: 123, Progress: 1, Tracker: redTracker}
	files := qbt.TorrentFiles{{Name: gazelleSeamName + "/01 - Track.flac", Size: 123}}
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager:      &gazelleSkipHashSyncManager{torrents: []qbt.Torrent{*source}, filesByHash: map[string]qbt.TorrentFiles{gazelleSeamHash: files}},
		systemNotices:    systemnotice.New(nil),
	}
	oldClients, err := gazelleClientsForTest()
	require.NoError(t, err)
	svc.GazelleKeysSaved(&models.CrossSeedAutomationSettings{GazelleEnabled: true, OrpheusAPIKey: "new-key"}, nil)
	newSite := gazelleLookupSite(GazelleLookup{Site: "orpheus.network"})
	svc.setKeyRejectedNotice(newSite, svc.gazelleKeyGensNow()["orpheus.network"], true, "new key rejected")

	lookups := lookupGazelleForTest(t, svc, source, files, oldClients)

	require.Equal(t, GazelleNoMatch, gazelleLookupOf(t, lookups, "orpheus.network").Status)
	require.Len(t, svc.systemNotices.List(), 1, "the old key answered, the new key still fails")
}

// A key that qui cannot decrypt fails the site without a request.
//
// regression: #4126
func TestUnreadableKeySendsNoRequest(t *testing.T) {
	source := &qbt.Torrent{Hash: gazelleSeamHash, Name: gazelleSeamName, Size: 123, Progress: 1, Tracker: redTracker}
	files := qbt.TorrentFiles{{Name: gazelleSeamName + "/01 - Track.flac", Size: 123}}
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		syncManager:      &gazelleSkipHashSyncManager{torrents: []qbt.Torrent{*source}, filesByHash: map[string]qbt.TorrentFiles{gazelleSeamHash: files}},
	}
	clients := &gazelleClientSet{byHost: map[string]*gazellemusic.Client{}, keyErrs: map[string]error{"orpheus.network": errors.New("cipher: message authentication failed")}}

	lookups := lookupGazelleForTest(t, svc, source, files, clients)

	require.Equal(t, GazelleReasonError, gazelleLookupOf(t, lookups, "orpheus.network").Reason)
	require.False(t, gazelleRequestsSent(lookups))
}

// The search after the dialog opens reuses the local RED/OPS checks of the
// plan, so one open and one search export the torrent once.
//
// regression: #4126
func TestSearchAfterPlanReusesLocalChecks(t *testing.T) {
	svc := newGazelleSeamService(t, redTracker, bothGazelleKeys(), nil)
	svc.gazelleLocalCache = ttlcache.New[string, gazelleLocalResult]()
	syncManager := svc.syncManager.(*hashFilteringSyncManager)
	syncManager.exportedTorrent = []byte("d4:infod4:name3:abcee")
	clients, err := svc.buildGazelleClientSet(t.Context(), nil)
	require.NoError(t, err)

	plan, err := svc.collectGazellePlan(t.Context(), gazelleSeamInstanceID, &syncManager.torrents[0], clients)
	require.NoError(t, err)
	require.Equal(t, GazelleWillAsk, gazelleLookupOf(t, plan, "orpheus.network").Status)
	_, err = svc.SearchTorrentMatches(t.Context(), gazelleSeamInstanceID, gazelleSeamHash, TorrentSearchOptions{})
	require.NoError(t, err)

	require.Equal(t, 1, syncManager.exportCalls)

	// A Library Scan stamps a cooldown from its scope, so it checks again.
	scope := svc.collectSearchScope(t.Context(), gazelleSeamInstanceID, &syncManager.torrents[0], searchScopeFacts{torznabOff: true, gazelleKeys: clients.hosts()}, false)
	require.NoError(t, scope.err)
	require.Equal(t, 2, syncManager.exportCalls)
}

// The plan of a site whose key rejected notice is open still asks the site,
// and says that the last answer was a rejection.
func TestPlanMarksSiteWithOpenRejectedNotice(t *testing.T) {
	svc := newGazelleSeamService(t, otherTracker, bothGazelleKeys(), nil)
	svc.systemNotices = systemnotice.New(nil)
	svc.systemNotices.Open(systemnotice.RedactedKeyRejected, "rejected")
	clients, err := svc.buildGazelleClientSet(t.Context(), nil)
	require.NoError(t, err)
	torrent := svc.syncManager.(*hashFilteringSyncManager).torrents[0]

	plan, err := svc.collectGazellePlan(t.Context(), gazelleSeamInstanceID, &torrent, clients)

	require.NoError(t, err)
	require.Equal(t, map[string]string{"redacted.sh": "will_ask key_rejected", "orpheus.network": "will_ask "}, gazelleStatuses(plan))
}
