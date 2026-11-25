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

type DeleteCause struct {
	Kind CauseKind `json:"kind"`
	Name string    `json:"name,omitempty"`
}

type DeleteNote struct {
	InstanceID int
	Time       time.Time
	Hashes     []string
	Cause      DeleteCause
}

type deleteNote struct {
	cause DeleteCause
	time  time.Time
	kept  bool
}

func (m *Module) NoteDelete(n DeleteNote) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.stopping && (!m.settingsLoaded || m.settings.Recording) {
		m.notes = append(m.notes, n)
	}
}

func (m *Module) keep(n DeleteNote, kept bool) {
	notes := m.deleteNotes[n.InstanceID]
	if notes == nil {
		notes = make(map[string][]deleteNote, len(n.Hashes))
		m.deleteNotes[n.InstanceID] = notes
	}
	for _, hash := range n.Hashes {
		hash = strings.ToLower(hash)
		notes[hash] = append(notes[hash], deleteNote{cause: n.Cause, time: n.Time, kept: kept})
		slices.SortStableFunc(notes[hash], func(x, y deleteNote) int { return x.time.Compare(y.time) })
	}
}

func (m *Module) keepCauses(instanceID int, events []models.TransferHistoryEvent) {
	for _, e := range events {
		if e.Delete == nil || CauseKind(e.Delete.CauseKind) == CauseOutside {
			continue
		}
		m.keep(DeleteNote{
			InstanceID: instanceID, Time: e.Time, Hashes: []string{e.Record.Hash},
			Cause: DeleteCause{Kind: CauseKind(e.Delete.CauseKind), Name: e.Delete.CauseName},
		}, true)
	}
}

func (m *Module) dropStoredCauses(ctx context.Context, instanceID int) error {
	notes := m.deleteNotes[instanceID]
	first := int64(-1)
	for _, list := range notes {
		for _, n := range list {
			if n.kept && (first < 0 || n.time.Unix() < first) {
				first = n.time.Unix()
			}
		}
	}
	if first < 0 {
		return nil
	}
	stored, err := m.store.DeleteEventsSince(ctx, instanceID, first)
	if err != nil {
		return err
	}
	dropNotes(notes, func(hash string, n deleteNote) bool {
		_, isStored := stored[models.TransferHistoryDeleteKey{Hash: hash, Second: n.time.Unix()}]
		return n.kept && isStored
	})
	return nil
}

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

func dropOldNotes(notes map[string][]deleteNote, now time.Time) {
	dropNotes(notes, func(_ string, n deleteNote) bool { return !now.Before(n.time.Add(noteLife)) })
}

func dropKeptNotes(notes map[string][]deleteNote) {
	dropNotes(notes, func(_ string, n deleteNote) bool { return n.kept })
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

type CauseFilter string

const (
	AllCauses        CauseFilter = ""
	AutomationCauses CauseFilter = "automation"
	UserCauses       CauseFilter = "user"
	OtherCauses      CauseFilter = "other"
)

var causeKindsOf = map[CauseFilter][]string{
	AllCauses:        nil,
	AutomationCauses: {string(CauseAutomation)},
	UserCauses:       {string(CauseUser)},
	OtherCauses:      {string(CauseBackupRestore), string(CauseClientProxy), string(CauseOutside)},
}

type DeletesQuery struct {
	InstanceID int
	From       time.Time
	To         time.Time
	Cause      CauseFilter
	Limit      int
	Offset     int
}

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

type DeleteCounts struct {
	Automation int64 `json:"automation"`
	User       int64 `json:"user"`
	Other      int64 `json:"other"`
}

type DeletesAnswer struct {
	Deletes []DeleteEvent `json:"deletes"`
	Counts  DeleteCounts  `json:"counts"`
}

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

type LibraryQuery struct {
	InstanceID int
	From       time.Time
	To         time.Time
}

type LibraryRow struct {
	Day        time.Time `json:"day"`
	Torrents   *int64    `json:"torrents,omitempty"`
	TotalSize  *int64    `json:"totalSize,omitempty"`
	UniqueSize *int64    `json:"uniqueSize,omitempty"`
	Added      int64     `json:"added"`
	Completed  int64     `json:"completed"`
	Deleted    int64     `json:"deleted"`
}

func (m *Module) Library(ctx context.Context, q LibraryQuery) ([]LibraryRow, error) {
	if err := validatePeriod(q.From, q.To); err != nil {
		return nil, err
	}
	if err := m.settle(ctx); err != nil {
		return nil, err
	}
	fromDay, toDay := daysOf(q.From, q.To)
	library, err := m.store.LibraryDays(ctx, q.InstanceID, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	counts, err := m.store.EventCounts(ctx, q.InstanceID, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	days := make(map[int64]*LibraryRow)
	rowOf := func(day int64) *LibraryRow {
		row := days[day]
		if row == nil {
			row = &LibraryRow{Day: time.Unix(day*86400, 0).UTC()}
			days[day] = row
		}
		return row
	}
	for _, d := range library {
		row := rowOf(d.Day)
		row.Torrents, row.TotalSize, row.UniqueSize = new(d.Torrents), new(d.TotalSize), d.UniqueSize
	}
	for _, c := range counts {
		row := rowOf(c.Day)
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
