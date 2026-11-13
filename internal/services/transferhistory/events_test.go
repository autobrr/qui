// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/services/transferhistory"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type deletesCheck struct {
	query transferhistory.DeletesQuery
	want  transferhistory.DeletesAnswer
}

type libraryCheck struct {
	query transferhistory.LibraryQuery
	want  []transferhistory.LibraryRow
	// eventsOnly compares the event counts of the days that have events.
	eventsOnly bool
}

// eventCounts returns the days of rows that have events, with their event
// counts only.
func eventCounts(rows []transferhistory.LibraryRow) []transferhistory.LibraryRow {
	var events []transferhistory.LibraryRow
	for _, r := range rows {
		if r.Added+r.Completed+r.Deleted > 0 {
			events = append(events, transferhistory.LibraryRow{Day: r.Day, Added: r.Added, Completed: r.Completed, Deleted: r.Deleted})
		}
	}
	return events
}

// The UTC day of t0 and the day after it.
var twoDays = transferhistory.LibraryQuery{InstanceID: instanceID, From: day10, To: day10.Add(48 * time.Hour)}

func note(time time.Time, cause transferhistory.DeleteCause, hashes ...string) step {
	return step{note: &transferhistory.DeleteNote{InstanceID: instanceID, Time: time, Hashes: hashes, Cause: cause}}
}

// expectDeletes checks the first 10 delete events of the two days with a
// cause that the filter selects, and the counts of the two days.
func expectDeletes(filter transferhistory.CauseFilter, counts transferhistory.DeleteCounts, want ...transferhistory.DeleteEvent) step {
	if want == nil {
		want = []transferhistory.DeleteEvent{}
	}
	return step{deletes: &deletesCheck{
		query: transferhistory.DeletesQuery{InstanceID: instanceID, From: twoDays.From, To: twoDays.To, Cause: filter, Limit: 10},
		want:  transferhistory.DeletesAnswer{Deletes: want, Counts: counts},
	}}
}

// expectEvents checks the event counts of the UTC day of t0, the only day
// with events.
func expectEvents(added, completed, deleted int64) step {
	var want []transferhistory.LibraryRow
	if added+completed+deleted > 0 {
		want = []transferhistory.LibraryRow{{Day: day10, Added: added, Completed: completed, Deleted: deleted}}
	}
	return step{library: &libraryCheck{query: twoDays, want: want, eventsOnly: true}}
}

// seeded returns t with the ratio and the seeding time that qBittorrent reports.
func seeded(t transferhistory.Torrent, ratio float64, seedingSeconds int64) transferhistory.Torrent {
	t.Ratio, t.SeedingSeconds = ratio, seedingSeconds
	return t
}

func withV1(t transferhistory.Torrent, infohashV1 string) transferhistory.Torrent {
	t.InfohashV1 = infohashV1
	return t
}

// deleted returns the delete event of a torrent of torrent() that a reading
// held with ratio 0 and seeding time 0.
func deleted(hash string, at time.Time, uploaded, downloaded int64, cause transferhistory.DeleteCause) transferhistory.DeleteEvent {
	return transferhistory.DeleteEvent{
		Time: at.Truncate(time.Second), Hash: hash, Name: "Some.Show.S01E01.1080p.WEB-DL-GRP", TrackerGroup: "tracker.example",
		Uploaded: uploaded, Downloaded: downloaded, Ratio: new(0.0), SeedingSeconds: new(int64(0)), Cause: cause,
	}
}

var (
	outside   = transferhistory.DeleteCause{Kind: transferhistory.CauseOutside}
	byUser    = transferhistory.DeleteCause{Kind: transferhistory.CauseUser}
	byRule    = transferhistory.DeleteCause{Kind: transferhistory.CauseAutomation, Name: "Remove old"}
	byProxy   = transferhistory.DeleteCause{Kind: transferhistory.CauseClientProxy, Name: "sonarr"}
	byRestore = transferhistory.DeleteCause{Kind: transferhistory.CauseBackupRestore}
)

func TestTransferHistoryEventsPostgresIntegration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "a torrent that is absent from a reading is deleted outside qui",
			steps: []step{
				reading(at(0), torrent("a", 100, 200), torrent("b", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 150, 200)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Other: 1},
					deleted("b", at(5*time.Minute), 0, 0, outside)),
				expectEvents(0, 0, 1),
			},
		},
		{
			name: "a delete with a delete note has the cause of the note",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0), torrent("c", 0, 0)),
				note(at(4*time.Minute), byUser, "b"),
				// qui read this list before qBittorrent got the delete.
				reading(at(5*time.Minute), torrent("a", 0, 0), torrent("b", 0, 0), torrent("c", 0, 0)),
				reading(at(10*time.Minute), torrent("a", 0, 0)),
				expectDeletes(transferhistory.UserCauses, transferhistory.DeleteCounts{User: 1, Other: 1},
					deleted("b", at(10*time.Minute), 0, 0, byUser)),
				expectDeletes(transferhistory.OtherCauses, transferhistory.DeleteCounts{User: 1, Other: 1},
					deleted("c", at(10*time.Minute), 0, 0, outside)),
			},
		},
		{
			name: "a delete note from after a reading does not name the cause of the delete that the reading shows",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				// The module gets the note of the second delete before it
				// processes the reading that shows the first one.
				note(at(7*time.Minute), byUser, "b"),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				reading(at(6*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(5*time.Minute+30*time.Second))),
				reading(at(10*time.Minute), torrent("a", 0, 0)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{User: 1, Other: 1},
					deleted("b", at(10*time.Minute), 0, 0, byUser),
					deleted("b", at(5*time.Minute), 0, 0, outside)),
			},
		},
		{
			name: "a delete with two delete notes has the cause of the first one",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				note(at(time.Minute), byRule, "b"),
				note(at(2*time.Minute), byProxy, "b"),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Automation: 1},
					deleted("b", at(5*time.Minute), 0, 0, byRule)),
			},
		},
		{
			name: "a delete note from after a reading stays for the next delete, also behind an earlier note",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				note(at(4*time.Minute), byUser, "b"),
				note(at(7*time.Minute), byProxy, "b"),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				reading(at(6*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(5*time.Minute+30*time.Second))),
				reading(at(10*time.Minute), torrent("a", 0, 0)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{User: 1, Other: 1},
					deleted("b", at(10*time.Minute), 0, 0, byProxy),
					deleted("b", at(5*time.Minute), 0, 0, byUser)),
			},
		},
		{
			name: "delete notes that arrive out of the order of their time name the cause by their time",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				note(at(7*time.Minute), byProxy, "b"),
				note(at(4*time.Minute), byUser, "b"),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{User: 1},
					deleted("b", at(5*time.Minute), 0, 0, byUser)),
			},
		},
		{
			name: "a delete note names a hash in upper or lower case",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				note(at(time.Minute), byRule, "B"),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Automation: 1},
					deleted("b", at(5*time.Minute), 0, 0, byRule)),
			},
		},
		{
			name: "a delete note ends when a reading 5 minutes after it still holds the torrent",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				note(at(time.Minute), byUser, "b"),
				reading(at(6*time.Minute), torrent("a", 0, 0), torrent("b", 0, 0)),
				reading(at(11*time.Minute), torrent("a", 0, 0)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Other: 1},
					deleted("b", at(11*time.Minute), 0, 0, outside)),
			},
		},
		{
			name: "a delete note of more than 5 minutes ago still names the cause when no reading held the torrent since",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				note(at(time.Minute), byUser, "b"),
				reading(at(30*time.Minute), torrent("a", 0, 0)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{User: 1},
					deleted("b", at(30*time.Minute), 0, 0, byUser)),
			},
		},
		{
			name: "the filter selects a kind of cause and the counts hold each kind",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0), torrent("c", 0, 0), torrent("d", 0, 0), torrent("e", 0, 0), torrent("f", 0, 0)),
				note(at(4*time.Minute), byUser, "b"),
				reading(at(5*time.Minute), torrent("a", 0, 0), torrent("c", 0, 0), torrent("d", 0, 0), torrent("e", 0, 0), torrent("f", 0, 0)),
				note(at(9*time.Minute), byRule, "c"),
				reading(at(10*time.Minute), torrent("a", 0, 0), torrent("d", 0, 0), torrent("e", 0, 0), torrent("f", 0, 0)),
				note(at(14*time.Minute), byProxy, "d"),
				reading(at(15*time.Minute), torrent("a", 0, 0), torrent("e", 0, 0), torrent("f", 0, 0)),
				note(at(19*time.Minute), byRestore, "e"),
				reading(at(20*time.Minute), torrent("a", 0, 0), torrent("f", 0, 0)),
				reading(at(25*time.Minute), torrent("a", 0, 0)),
				expectDeletes(transferhistory.AutomationCauses, transferhistory.DeleteCounts{Automation: 1, User: 1, Other: 3},
					deleted("c", at(10*time.Minute), 0, 0, byRule)),
				expectDeletes(transferhistory.UserCauses, transferhistory.DeleteCounts{Automation: 1, User: 1, Other: 3},
					deleted("b", at(5*time.Minute), 0, 0, byUser)),
				expectDeletes(transferhistory.OtherCauses, transferhistory.DeleteCounts{Automation: 1, User: 1, Other: 3},
					deleted("f", at(25*time.Minute), 0, 0, outside),
					deleted("e", at(20*time.Minute), 0, 0, byRestore),
					deleted("d", at(15*time.Minute), 0, 0, byProxy)),
			},
		},
		{
			name: "the deletes come in pages with the newest first",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0), torrent("c", 0, 0), torrent("d", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), torrent("b", 0, 0), torrent("c", 0, 0)),
				reading(at(10*time.Minute), torrent("a", 0, 0), torrent("b", 0, 0)),
				reading(at(15*time.Minute), torrent("a", 0, 0)),
				{deletes: &deletesCheck{
					query: transferhistory.DeletesQuery{InstanceID: instanceID, To: twoDays.To, Limit: 2},
					want: transferhistory.DeletesAnswer{Counts: transferhistory.DeleteCounts{Other: 3}, Deletes: []transferhistory.DeleteEvent{
						deleted("b", at(15*time.Minute), 0, 0, outside), deleted("c", at(10*time.Minute), 0, 0, outside),
					}},
				}},
				{deletes: &deletesCheck{
					query: transferhistory.DeletesQuery{InstanceID: instanceID, To: twoDays.To, Limit: 2, Offset: 2},
					want: transferhistory.DeletesAnswer{Counts: transferhistory.DeleteCounts{Other: 3}, Deletes: []transferhistory.DeleteEvent{
						deleted("d", at(5*time.Minute), 0, 0, outside),
					}},
				}},
				{deletes: &deletesCheck{
					// The delete of d has the time of its whole second, before this start.
					query: transferhistory.DeletesQuery{InstanceID: instanceID, From: at(5 * time.Minute).Add(time.Millisecond), To: at(12 * time.Minute), Limit: 2},
					want: transferhistory.DeletesAnswer{Counts: transferhistory.DeleteCounts{Other: 1}, Deletes: []transferhistory.DeleteEvent{
						deleted("c", at(10*time.Minute), 0, 0, outside),
					}},
				}},
			},
		},
		{
			name: "a delete event holds the final upload, download, ratio, and seeding time",
			steps: []step{
				reading(at(0), seeded(torrent("a", 100, 200), 0.5, 3600)),
				reading(at(5*time.Minute), seeded(torrent("a", 300, 250), 1.2, 3900)),
				reading(at(10 * time.Minute)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Other: 1}, transferhistory.DeleteEvent{
					Time: at(10 * time.Minute).Truncate(time.Second), Hash: "a", Name: "Some.Show.S01E01.1080p.WEB-DL-GRP", TrackerGroup: "tracker.example",
					Uploaded: 300, Downloaded: 250, Ratio: new(1.2), SeedingSeconds: new(int64(3900)), Cause: outside,
				}),
			},
		},
		{
			name: "a delete while qui does not run has the last counters and no ratio and no seeding time",
			steps: []step{
				reading(at(0), seeded(torrent("a", 100, 200), 0.5, 3600), torrent("b", 0, 0)),
				restart,
				reading(at(time.Hour), torrent("b", 0, 0)),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Other: 1}, transferhistory.DeleteEvent{
					Time: at(time.Hour).Truncate(time.Second), Hash: "a", Name: "Some.Show.S01E01.1080p.WEB-DL-GRP", TrackerGroup: "tracker.example",
					Uploaded: 100, Downloaded: 200, Cause: outside,
				}),
			},
		},
		{
			name: "a deleted torrent stays deleted after a restart, and a new add of it is an add",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				restart,
				reading(at(10*time.Minute), torrent("a", 0, 0)),
				expectEvents(0, 0, 1),
				reading(at(15*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(14*time.Minute))),
				expectEvents(1, 0, 1),
			},
		},
		{
			name: "a torrent that the user deletes and adds again shows both lives and counts the bytes of each",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 500, 0), at(4*time.Minute))),
				reading(at(10*time.Minute), torrent("a", 0, 0)),
				reading(at(15*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 50, 0), at(14*time.Minute))),
				reading(at(20*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 80, 0), at(14*time.Minute))),
				expectEvents(2, 0, 1),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Other: 1},
					deleted("b", at(10*time.Minute), 500, 0, outside)),
				expect(hourTotal, total("tracker.example", 580, 0, 0)),
			},
		},
		{
			name: "a torrent that the user adds again with no bytes yet counts the bytes of its new life after a restart",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 500, 0), at(4*time.Minute))),
				reading(at(10*time.Minute), torrent("a", 0, 0)),
				reading(at(15*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(14*time.Minute))),
				restart,
				reading(at(20*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 80, 0), at(14*time.Minute))),
				// The restart stores a Recording gap of 5 minutes.
				expect(hourTotal, transferhistory.TransferRow{
					Group: "tracker.example", Start: hourTotal.From, Width: transferhistory.BucketTotal, Uploaded: 580, GapSeconds: 300,
				}),
			},
		},
		{
			name: "a torrent that the user adds again and downloads is no Statistics cross-seed in its new life",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), complete(torrent("b", 0, 0))),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				reading(at(10*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(9*time.Minute))),
				reading(at(15*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 0, 1000), at(9*time.Minute)))),
				reading(at(20*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 100, 1000), at(9*time.Minute)))),
				expect(hourTotal, total("tracker.example", 100, 1000, 0)),
			},
		},
		{
			name: "a torrent that the user adds again with its data on disk is a Statistics cross-seed in its new life",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), complete(torrent("b", 0, 1000))),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				reading(at(10*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 0, 0), at(9*time.Minute)))),
				reading(at(15*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 100, 0), at(9*time.Minute)))),
				expect(hourTotal, total("tracker.example", 100, 0, 100)),
			},
		},
		{
			name: "the Baseline of a torrent keeps the flag of its first life",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), complete(torrent("b", 400, 0))),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				reading(at(10*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(9*time.Minute))),
				restart,
				reading(at(15*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 0, 1000), at(9*time.Minute)))),
				reading(at(20*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 0, 1000), at(9*time.Minute)))),
				expect(transferhistory.TransferQuery{To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{
						Group: "tracker.example", Width: transferhistory.BucketTotal, Downloaded: 1000, GapSeconds: 300,
						Baseline: &transferhistory.Bytes{Uploaded: 400, StatisticsCrossSeedUploaded: 400},
					}),
			},
		},
		{
			name: "the Baseline keeps the flag of the first life when a later life returns with an old added time",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), complete(torrent("b", 400, 0))),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				reading(at(10*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(9*time.Minute))),
				reading(at(15*time.Minute), torrent("a", 0, 0)),
				reading(at(80*time.Minute), torrent("a", 0, 0)),
				reading(at(85*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(9*time.Minute))),
				expect(transferhistory.TransferQuery{To: at(2 * time.Hour), Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{
						Group: "tracker.example", Width: transferhistory.BucketTotal,
						Baseline: &transferhistory.Bytes{Uploaded: 400, StatisticsCrossSeedUploaded: 400},
					}),
			},
		},
		{
			name: "the Baseline of a torrent that returns with an old added time follows the flag of the same life",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), addedAt(torrent("b", 400, 0), at(-30*24*time.Hour))),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				reading(at(10*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 400, 0), at(-30*24*time.Hour))),
				reading(at(15*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 400, 0), at(-30*24*time.Hour)))),
				expect(transferhistory.TransferQuery{To: hourTotal.To, Bucket: transferhistory.BucketTotal, GroupBy: transferhistory.GroupByTracker},
					transferhistory.TransferRow{
						Group: "tracker.example", Width: transferhistory.BucketTotal,
						Baseline: &transferhistory.Bytes{Uploaded: 400, StatisticsCrossSeedUploaded: 400},
					}),
			},
		},
		{
			name: "a torrent that returns with an old added time continues its count",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), addedAt(torrent("b", 500, 0), at(-30*24*time.Hour))),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				reading(at(10*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 520, 0), at(-30*24*time.Hour))),
				expectEvents(1, 0, 1),
				expect(hourTotal, total("tracker.example", 20, 0, 0)),
			},
		},
		{
			name: "a magnet that changes its hash gets no delete and no add, and its bytes count one time",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), addedAt(torrent("v1", 100, 0), at(4*time.Minute))),
				reading(at(10*time.Minute), torrent("a", 0, 0), withV1(addedAt(torrent("v2", 150, 0), at(4*time.Minute)), "v1")),
				expectEvents(1, 0, 0),
				expect(hourTotal, total("tracker.example", 150, 0, 0)),
				restart,
				reading(at(15*time.Minute), torrent("a", 0, 0), withV1(addedAt(torrent("v2", 180, 0), at(4*time.Minute)), "v1")),
				reading(at(20*time.Minute), torrent("a", 0, 0)),
				expectEvents(1, 0, 1),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Other: 1},
					deleted("v2", at(20*time.Minute), 180, 0, outside)),
				// The restart stores a Recording gap of 5 minutes.
				expect(hourTotal, transferhistory.TransferRow{
					Group: "tracker.example", Start: hourTotal.From, Width: transferhistory.BucketTotal, Uploaded: 180, GapSeconds: 300,
				}),
			},
		},
		{
			name: "a magnet that changes its hash before the first write of its record keeps one record",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(time.Minute), torrent("a", 0, 0), addedAt(torrent("v1", 100, 0), at(30*time.Second))),
				reading(at(2*time.Minute), torrent("a", 0, 0), withV1(addedAt(torrent("v2", 150, 0), at(30*time.Second)), "v1")),
				reading(at(5*time.Minute), torrent("a", 0, 0), withV1(addedAt(torrent("v2", 150, 0), at(30*time.Second)), "v1")),
				expectEvents(1, 0, 0),
				expect(hourTotal, total("tracker.example", 150, 0, 0)),
			},
		},
		{
			name: "a magnet that changes its hash to the hash of a deleted record continues in that record",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), torrent("v2", 1000, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				reading(at(10*time.Minute), torrent("a", 0, 0), addedAt(torrent("v1", 100, 0), at(9*time.Minute))),
				reading(at(15*time.Minute), torrent("a", 0, 0), withV1(addedAt(torrent("v2", 150, 0), at(9*time.Minute)), "v1")),
				expectEvents(1, 0, 1),
				expect(hourTotal, total("tracker.example", 150, 0, 0)),
				restart,
				reading(at(20*time.Minute), torrent("a", 0, 0)),
				expectEvents(1, 0, 2),
				expectDeletes(transferhistory.AllCauses, transferhistory.DeleteCounts{Other: 2},
					transferhistory.DeleteEvent{
						Time: at(20 * time.Minute).Truncate(time.Second), Hash: "v2", Name: "Some.Show.S01E01.1080p.WEB-DL-GRP",
						TrackerGroup: "tracker.example", Uploaded: 150, Cause: outside,
					},
					deleted("v2", at(5*time.Minute), 1000, 0, outside)),
			},
		},
		{
			name: "a hybrid torrent beside a torrent with its v1 infohash is a new torrent",
			steps: []step{
				reading(at(0), torrent("v1", 100, 0)),
				reading(at(5*time.Minute), torrent("v1", 100, 0), withV1(addedAt(torrent("v2", 150, 0), at(4*time.Minute)), "v1")),
				expectEvents(1, 0, 0),
				reading(at(10*time.Minute), withV1(addedAt(torrent("v2", 150, 0), at(4*time.Minute)), "v1")),
				expectEvents(1, 0, 1),
			},
		},
		{
			name: "the first reading of an instance gives no add event and no completion event",
			steps: []step{
				reading(at(0), complete(torrent("a", 0, 0)), torrent("b", 0, 0)),
				expectEvents(0, 0, 0),
				reading(at(5*time.Minute), complete(torrent("a", 0, 0)), complete(torrent("b", 0, 0)), complete(addedAt(torrent("c", 0, 0), at(4*time.Minute)))),
				expectEvents(1, 2, 0),
			},
		},
		{
			name: "a torrent that completes two times in one life has one completion event",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), complete(torrent("a", 0, 0))),
				// A recheck makes the torrent incomplete for a time.
				reading(at(10*time.Minute), torrent("a", 0, 0)),
				reading(at(15*time.Minute), complete(torrent("a", 0, 0))),
				expectEvents(0, 1, 0),
			},
		},
		{
			name: "a torrent that the user adds again completes again",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 0, 0), at(4*time.Minute)))),
				reading(at(10*time.Minute), torrent("a", 0, 0)),
				reading(at(15*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(14*time.Minute))),
				reading(at(20*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 0, 0), at(14*time.Minute)))),
				expectEvents(2, 2, 1),
			},
		},
		{
			name: "a torrent that the user adds again completes again after a restart",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 0, 0), at(4*time.Minute)))),
				reading(at(10*time.Minute), torrent("a", 0, 0)),
				reading(at(15*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(14*time.Minute))),
				restart,
				reading(at(20*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 0, 0), at(14*time.Minute)))),
				reading(at(25*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 0, 0), at(14*time.Minute)))),
				expectEvents(2, 2, 1),
			},
		},
		{
			name: "a torrent that the user adds again with its data complete gets a completion event",
			steps: []step{
				reading(at(0), torrent("a", 0, 0), complete(torrent("b", 0, 0))),
				reading(at(5*time.Minute), torrent("a", 0, 0)),
				reading(at(10*time.Minute), torrent("a", 0, 0), complete(addedAt(torrent("b", 0, 0), at(9*time.Minute)))),
				expectEvents(1, 1, 1),
			},
		},
		{
			name: "the library query returns the UTC days of the period that have events, with the oldest first",
			steps: []step{
				reading(at(0), torrent("a", 0, 0)),
				reading(at(5*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(4*time.Minute))),
				reading(at(24*time.Hour), torrent("a", 0, 0)),
				{library: &libraryCheck{eventsOnly: true, query: twoDays, want: []transferhistory.LibraryRow{
					{Day: day10, Added: 1},
					{Day: day10.Add(24 * time.Hour), Deleted: 1},
				}}},
				{library: &libraryCheck{
					eventsOnly: true,
					query:      transferhistory.LibraryQuery{InstanceID: instanceID, From: at(15 * time.Hour), To: at(16 * time.Hour)},
					want:       []transferhistory.LibraryRow{{Day: day10.Add(24 * time.Hour), Deleted: 1}},
				}},
				{library: &libraryCheck{
					eventsOnly: true,
					query:      transferhistory.LibraryQuery{InstanceID: instanceID, To: day10.Add(24 * time.Hour)},
					want:       []transferhistory.LibraryRow{{Day: day10, Added: 1}},
				}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engines(t, func(t *testing.T, db func(t *testing.T) *database.DB) {
				runSteps(t, db(t), tc.steps)
			})
		})
	}
}

func TestInvalidEventQueries(t *testing.T) {
	t.Parallel()
	module := transferhistory.New(nil, nil, systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

	to := day10
	for name, query := range map[string]transferhistory.DeletesQuery{
		"no limit":          {To: to},
		"a limit over 500":  {To: to, Limit: 501},
		"a negative offset": {To: to, Limit: 10, Offset: -1},
		"an unknown cause":  {To: to, Limit: 10, Cause: "rule"},
		"no end":            {Limit: 10},
		"an empty period":   {From: to, To: to, Limit: 10},
	} {
		_, err := module.Deletes(t.Context(), query)
		require.ErrorIs(t, err, transferhistory.ErrInvalidQuery, name)
	}
	_, err := module.Library(t.Context(), transferhistory.LibraryQuery{From: to, To: to})
	require.ErrorIs(t, err, transferhistory.ErrInvalidQuery)
}

func TestAdapterGivesTheDeleteActorAsTheCause(t *testing.T) {
	t.Parallel()
	db := testdb.NewMigratedSQLite(t, "transfer-history")
	module := transferhistory.New(db, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
	t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
	adapter := transferhistory.NewAdapter(module, nil)

	// The adapter stamps a note with the clock, so the readings are near it,
	// and 5 minutes apart for a write.
	now := time.Now().UTC()
	module.Record(transferhistory.Reading{InstanceID: instanceID, Time: now.Add(-5 * time.Minute), Torrents: []transferhistory.Torrent{
		torrent("a", 0, 0), torrent("b", 0, 0), torrent("c", 0, 0), torrent("d", 0, 0),
	}})
	adapter.NoteDelete(instanceID, []string{"a"}, qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorAutomation, RuleName: "Remove old"})
	adapter.NoteDelete(instanceID, []string{"b"}, qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorUser})
	adapter.NoteDelete(instanceID, []string{"c"}, qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorBackupRestore})
	adapter.NoteDelete(instanceID, []string{"d"}, qbittorrent.DeleteActor{Kind: qbittorrent.DeleteActorClientProxy, KeyName: "sonarr"})
	module.Record(transferhistory.Reading{InstanceID: instanceID, Time: now.Add(time.Minute)})

	answer, err := module.Deletes(t.Context(), transferhistory.DeletesQuery{InstanceID: instanceID, To: now.Add(time.Hour), Limit: 10})
	require.NoError(t, err)
	causes := make(map[string]transferhistory.DeleteCause)
	for _, d := range answer.Deletes {
		causes[d.Hash] = d.Cause
	}
	require.Equal(t, map[string]transferhistory.DeleteCause{"a": byRule, "b": byUser, "c": byRestore, "d": byProxy}, causes)
}

// The module loads the stored history again after a write that it cannot
// tell is stored. The delete of that write shows again at the next reading.
func TestAFailedCommitAndAFailedCheckKeepTheDeleteCausePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(&failedCommit{DB: db, fail: 2, failCheck: true}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

		module.Record(*reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)).reading)
		module.NoteDelete(*note(at(time.Minute), byRule, "b").note)
		module.Record(*reading(at(5*time.Minute), torrent("a", 0, 0)).reading)
		module.Record(*reading(at(6*time.Minute), torrent("a", 0, 0)).reading)

		answer, err := module.Deletes(t.Context(), transferhistory.DeletesQuery{InstanceID: instanceID, To: twoDays.To, Limit: 10})
		require.NoError(t, err)
		require.Len(t, answer.Deletes, 1)
		require.Equal(t, byRule, answer.Deletes[0].Cause)
	})
}

// The write with the delete is stored, but the module cannot tell. The cause
// that it kept must not name a later delete of the same torrent.
func TestALostCommitAnswerAndAFailedCheckGiveTheCauseOnceThenOutsidePostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(&lostCommitAndCheck{DB: db}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })

		record := settled(t, module)
		record(reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)))
		module.NoteDelete(*note(at(time.Minute), byRule, "b").note)
		record(reading(at(5*time.Minute), torrent("a", 0, 0)))
		record(reading(at(6*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(5*time.Minute+30*time.Second))))
		record(reading(at(7*time.Minute), torrent("a", 0, 0)))
		record(reading(at(12*time.Minute), torrent("a", 0, 0)))

		answer, err := module.Deletes(t.Context(), transferhistory.DeletesQuery{InstanceID: instanceID, To: twoDays.To, Limit: 10})
		require.NoError(t, err)
		require.Len(t, answer.Deletes, 2)
		require.Equal(t, outside, answer.Deletes[0].Cause, "the delete of the second life")
		require.Equal(t, byRule, answer.Deletes[1].Cause, "the delete of the first life")
	})
}

// The write with the delete and the new add is stored, but the module cannot
// tell. The torrent is in qBittorrent again, so only the stored events show
// that the delete is stored.
func TestALostCommitAnswerDropsTheCauseOfATorrentThatCameBackPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(&lostCommitAndCheck{DB: db}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		record := settled(t, module)

		record(reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)))
		module.NoteDelete(*note(at(time.Minute), byRule, "b").note)
		record(reading(at(2*time.Minute), torrent("a", 0, 0)))
		record(reading(at(3*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(150*time.Second))))
		record(reading(at(5*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(150*time.Second))))
		record(reading(at(6*time.Minute), torrent("a", 0, 0)))
		record(reading(at(11*time.Minute), torrent("a", 0, 0)))

		answer, err := module.Deletes(t.Context(), transferhistory.DeletesQuery{InstanceID: instanceID, To: twoDays.To, Limit: 10})
		require.NoError(t, err)
		require.Len(t, answer.Deletes, 2)
		require.Equal(t, outside, answer.Deletes[0].Cause, "the delete of the second life")
		require.Equal(t, byRule, answer.Deletes[1].Cause, "the delete of the first life")
	})
}

// The write with the delete and the new add is not stored. The torrent is in
// the first reading after the reload, so no reading shows the delete again.
func TestAFailedCommitDropsTheCauseOfATorrentThatCameBackPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, newDB func(t *testing.T) *database.DB) {
		db := newDB(t)
		module := transferhistory.New(&failedCommit{DB: db, fail: 2, failCheck: true}, models.NewTrackerCustomizationStore(db), systemnotice.New(nil))
		t.Cleanup(func() { require.NoError(t, module.Shutdown(context.Background())) })
		record := settled(t, module)

		record(reading(at(0), torrent("a", 0, 0), torrent("b", 0, 0)))
		module.NoteDelete(*note(at(time.Minute), byRule, "b").note)
		record(reading(at(2*time.Minute), torrent("a", 0, 0)))
		record(reading(at(3*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(150*time.Second))))
		record(reading(at(5*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(150*time.Second))))
		record(reading(at(6*time.Minute), torrent("a", 0, 0), addedAt(torrent("b", 0, 0), at(150*time.Second))))
		record(reading(at(6*time.Minute+30*time.Second), torrent("a", 0, 0)))
		record(reading(at(12*time.Minute), torrent("a", 0, 0)))

		answer, err := module.Deletes(t.Context(), transferhistory.DeletesQuery{InstanceID: instanceID, To: twoDays.To, Limit: 10})
		require.NoError(t, err)
		require.Len(t, answer.Deletes, 1)
		require.Equal(t, outside, answer.Deletes[0].Cause)
	})
}

// settled returns a function that gives the module a reading and waits until
// the module processed it, because at most 4 readings wait for the module.
func settled(t *testing.T, module *transferhistory.Module) func(step) {
	return func(s step) {
		t.Helper()
		module.Record(*s.reading)
		_, err := module.Status(t.Context())
		require.NoError(t, err)
	}
}
