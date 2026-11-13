// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/trackergroup"
)

// noteLife is how long a delete note waits for its delete. A reading from
// that long after the note that still holds the torrent shows that the
// delete failed.
const noteLife = 5 * time.Minute

const maxDeletesPage = 500

type CauseKind string

const (
	CauseAutomation    CauseKind = "automation"
	CauseUser          CauseKind = "user"
	CauseBackupRestore CauseKind = "backupRestore"
	CauseClientProxy   CauseKind = "clientProxy"
	CauseOutside       CauseKind = "outside"
)

// DeleteCause is the cause of a delete. Name is the name of the automation
// rule or of the Client Proxy API key, and empty for the other kinds.
type DeleteCause struct {
	Kind CauseKind `json:"kind"`
	Name string    `json:"name,omitempty"`
}

// DeleteNote tells who is about to delete torrents of an instance. Time is
// when the caller gave the note.
type DeleteNote struct {
	InstanceID int
	Time       time.Time
	Hashes     []string
	Cause      DeleteCause
}

type deleteNote struct {
	cause DeleteCause
	time  time.Time
}

// NoteDelete gives the module a delete note. Give it before qBittorrent gets
// the delete. The note lasts until a reading from its time or later shows
// that the torrent is gone, or until a reading from 5 minutes or more after
// the note still holds the torrent. When a torrent has more than one note, its
// delete has the cause of the first one. A delete without a note has the
// cause CauseOutside. A note after Shutdown does nothing.
func (m *Module) NoteDelete(n DeleteNote) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.stopping {
		m.notes = append(m.notes, n)
	}
}

func (m *Module) keep(n DeleteNote) {
	notes := m.deleteNotes[n.InstanceID]
	if notes == nil {
		notes = make(map[string][]deleteNote, len(n.Hashes))
		m.deleteNotes[n.InstanceID] = notes
	}
	for _, hash := range n.Hashes {
		hash = strings.ToLower(hash)
		notes[hash] = append(notes[hash], deleteNote{cause: n.Cause, time: n.Time})
		// Two callers can give their notes out of the order of their time.
		slices.SortStableFunc(notes[hash], func(x, y deleteNote) int { return x.time.Compare(y.time) })
	}
}

// keepCauses makes a delete note again from each delete event of a write
// that the module cannot tell is stored. If the write is not stored, the next
// reading shows the delete again, and its note is gone. If it is stored, the
// load of the history drops the note.
func (m *Module) keepCauses(instanceID int, events []models.TransferHistoryEvent) {
	for _, e := range events {
		if e.Delete == nil || CauseKind(e.Delete.CauseKind) == CauseOutside {
			continue
		}
		m.keep(DeleteNote{
			InstanceID: instanceID, Time: e.Time, Hashes: []string{e.Record.Hash},
			Cause: DeleteCause{Kind: CauseKind(e.Delete.CauseKind), Name: e.Delete.CauseName},
		})
	}
}

// takeNote returns the cause of the delete of hash that the reading at now
// shows. The first note is the one that deleted the torrent: a later delete
// of a torrent that is gone does nothing. A note from after the reading is for
// a later delete, and stays.
func takeNote(notes map[string][]deleteNote, hash string, now time.Time) DeleteCause {
	list := notes[hash]
	later := slices.IndexFunc(list, func(n deleteNote) bool { return now.Before(n.time) })
	if len(list) == 0 || later == 0 {
		return DeleteCause{Kind: CauseOutside}
	}
	if later < 0 {
		delete(notes, hash)
	} else {
		notes[hash] = list[later:]
	}
	return list[0].cause
}

// dropOldNotes drops each note that a reading at now shows as failed.
func dropOldNotes(notes map[string][]deleteNote, now time.Time) {
	dropNotes(notes, func(_ string, n deleteNote) bool { return !now.Before(n.time.Add(noteLife)) })
}

// dropSpentNotes drops each note whose delete the loaded history of inst
// holds: the record is deleted, and the last stored reading is from the time
// of the note or later.
func dropSpentNotes(notes map[string][]deleteNote, inst *instance) {
	dropNotes(notes, func(hash string, n deleteNote) bool {
		_, deleted := inst.deleted[hash]
		return deleted && !inst.lastReadingAt.Before(n.time)
	})
}

func dropNotes(notes map[string][]deleteNote, drop func(hash string, n deleteNote) bool) {
	for hash, list := range notes {
		list = slices.DeleteFunc(list, func(n deleteNote) bool { return drop(hash, n) })
		if len(list) == 0 {
			delete(notes, hash)
		} else {
			notes[hash] = list
		}
	}
}

// CauseFilter selects the delete events of a deletes query by their cause.
type CauseFilter string

const (
	AllCauses        CauseFilter = ""
	AutomationCauses CauseFilter = "automation"
	UserCauses       CauseFilter = "user"
	// OtherCauses are a backup restore, a Client Proxy API key, and outside qui.
	OtherCauses CauseFilter = "other"
)

var causeKindsOf = map[CauseFilter][]string{
	AllCauses:        nil,
	AutomationCauses: {string(CauseAutomation)},
	UserCauses:       {string(CauseUser)},
	OtherCauses:      {string(CauseBackupRestore), string(CauseClientProxy), string(CauseOutside)},
}

// DeletesQuery asks for a page of the delete events of one instance whose
// time is in the period [From, To), with the newest first. A zero From is a period with no
// start. Times are UTC. Limit is the size of the page, from 1 to 500, and
// Offset is the number of newer events before it.
type DeletesQuery struct {
	InstanceID int
	From       time.Time
	To         time.Time
	Cause      CauseFilter
	Limit      int
	Offset     int
}

// DeleteEvent is one delete of a torrent. Uploaded and Downloaded are the
// last counters of the torrent. Ratio and SeedingSeconds are the values that
// qBittorrent reported last, and nil when qui did not read the torrent
// between its start and the delete.
type DeleteEvent struct {
	Time           time.Time   `json:"time"`
	Hash           string      `json:"hash"`
	Name           string      `json:"name"`
	TrackerGroup   string      `json:"trackerGroup"`
	Uploaded       int64       `json:"uploaded"`
	Downloaded     int64       `json:"downloaded"`
	Ratio          *float64    `json:"ratio,omitempty"`
	SeedingSeconds *int64      `json:"seedingSeconds,omitempty"`
	Cause          DeleteCause `json:"cause"`
}

// DeleteCounts is the number of delete events of a period for each cause filter.
type DeleteCounts struct {
	Automation int64 `json:"automation"`
	User       int64 `json:"user"`
	Other      int64 `json:"other"`
}

// DeletesAnswer is a page of delete events. Counts covers the whole period,
// with each cause.
type DeletesAnswer struct {
	Deletes []DeleteEvent `json:"deletes"`
	Counts  DeleteCounts  `json:"counts"`
}

// Deletes answers a deletes query. It includes each reading that the module
// got before the call, up to the last write that those readings made due.
func (m *Module) Deletes(ctx context.Context, q DeletesQuery) (DeletesAnswer, error) {
	causeKinds, known := causeKindsOf[q.Cause]
	switch {
	case !known:
		return DeletesAnswer{}, fmt.Errorf("%w: unknown cause %q", ErrInvalidQuery, q.Cause)
	case q.Limit < 1 || q.Limit > maxDeletesPage:
		return DeletesAnswer{}, fmt.Errorf("%w: the limit is not from 1 to %d", ErrInvalidQuery, maxDeletesPage)
	case q.Offset < 0:
		return DeletesAnswer{}, fmt.Errorf("%w: the offset is negative", ErrInvalidQuery)
	}
	if err := validatePeriod(q.From, q.To); err != nil {
		return DeletesAnswer{}, err
	}
	if err := m.settle(ctx); err != nil {
		return DeletesAnswer{}, err
	}
	customizations, err := m.customizations.List(ctx)
	if err != nil {
		return DeletesAnswer{}, fmt.Errorf("read tracker customizations: %w", err)
	}
	mapper := trackergroup.New(customizations)

	// An event has the time of its whole second, so both ends round up.
	var fromSecond int64
	if !q.From.IsZero() {
		fromSecond = ceilSecond(q.From)
	}
	toSecond := ceilSecond(q.To)
	rows, err := m.store.Deletes(ctx, q.InstanceID, fromSecond, toSecond, causeKinds, q.Limit, q.Offset)
	if err != nil {
		return DeletesAnswer{}, err
	}
	counts, err := m.store.DeleteCauseCounts(ctx, q.InstanceID, fromSecond, toSecond)
	if err != nil {
		return DeletesAnswer{}, err
	}

	answer := DeletesAnswer{Deletes: make([]DeleteEvent, len(rows))}
	for i, row := range rows {
		answer.Deletes[i] = DeleteEvent{
			Time:           row.Time,
			Hash:           row.Hash,
			Name:           row.Name,
			TrackerGroup:   groupOf(mapper, row.TrackerDomain, row.Private != nil && !*row.Private),
			Uploaded:       row.Uploaded,
			Downloaded:     row.Downloaded,
			Ratio:          row.Ratio,
			SeedingSeconds: row.SeedingSeconds,
			Cause:          DeleteCause{Kind: CauseKind(row.CauseKind), Name: row.CauseName},
		}
	}
	count := func(filter CauseFilter) (n int64) {
		for _, kind := range causeKindsOf[filter] {
			n += counts[kind]
		}
		return n
	}
	answer.Counts = DeleteCounts{Automation: count(AutomationCauses), User: count(UserCauses), Other: count(OtherCauses)}
	return answer, nil
}

// LibraryQuery asks for the days of one instance in the period [From, To). A
// zero From is a period with no start. Times are UTC.
type LibraryQuery struct {
	InstanceID int
	From       time.Time
	To         time.Time
}

// LibraryRow is the library of an instance on one UTC day that overlaps the
// period. Day is the start of the day. Torrents, TotalSize, and UniqueSize
// are nil when qui stored no value for the day. Added, Completed, and Deleted
// count the events of the whole day. A day with nothing stored has no row.
type LibraryRow struct {
	Day        time.Time `json:"day"`
	Torrents   *int64    `json:"torrents,omitempty"`
	TotalSize  *int64    `json:"totalSize,omitempty"`
	UniqueSize *int64    `json:"uniqueSize,omitempty"`
	Added      int64     `json:"added"`
	Completed  int64     `json:"completed"`
	Deleted    int64     `json:"deleted"`
}

// Library answers a library query, with the oldest day first. It includes
// each reading that the module got before the call, up to the last write that
// those readings made due.
func (m *Module) Library(ctx context.Context, q LibraryQuery) ([]LibraryRow, error) {
	if err := validatePeriod(q.From, q.To); err != nil {
		return nil, err
	}
	if err := m.settle(ctx); err != nil {
		return nil, err
	}
	fromDay, toDay := daysOf(q.From, q.To)
	counts, err := m.store.EventCounts(ctx, q.InstanceID, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	days := make(map[int64]*LibraryRow)
	for _, c := range counts {
		row := days[c.Day]
		if row == nil {
			row = &LibraryRow{Day: time.Unix(c.Day*86400, 0).UTC()}
			days[c.Day] = row
		}
		switch c.Kind {
		case models.TransferHistoryEventAdd:
			row.Added = c.Count
		case models.TransferHistoryEventCompletion:
			row.Completed = c.Count
		case models.TransferHistoryEventDelete:
			row.Deleted = c.Count
		}
	}
	rows := make([]LibraryRow, 0, len(days))
	for _, row := range days {
		rows = append(rows, *row)
	}
	slices.SortFunc(rows, func(x, y LibraryRow) int { return x.Day.Compare(y.Day) })
	return rows, nil
}
