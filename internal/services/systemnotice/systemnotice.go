// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package systemnotice holds the open System notices: faults in qui that need
// an action from the user. The web UI reads them, and the notification
// service gets one event when a notice opens.
package systemnotice

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/autobrr/qui/internal/services/notifications"
)

// Kind names a System notice. The web UI translates its title and fix by kind.
type Kind string

const (
	// RecordingStopped opens when qui cannot write the Transfer history.
	RecordingStopped Kind = "recording_stopped"
	// ShrinkFailed opens when the shrink of the database file at startup fails.
	ShrinkFailed Kind = "shrink_failed"
	// RedactedKeyRejected and OrpheusKeyRejected open when the site rejects the
	// RED/OPS API key in a cross-seed search.
	RedactedKeyRejected Kind = "redacted_key_rejected"
	OrpheusKeyRejected  Kind = "orpheus_key_rejected"
	// LowDisk opens on SQLite when the disk of the database file has little
	// free space.
	LowDisk Kind = "low_disk"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// ErrNotDismissible is the answer of Dismiss for a notice that closes itself.
var ErrNotDismissible = errors.New("this notice closes itself when the fault is gone")

type definition struct {
	severity    Severity
	title       string
	dismissible bool
	// fixes holds the fix for each reason. The key "" is the fix for an
	// unknown reason.
	fixes map[string]string
}

var definitions = map[Kind]definition{
	RecordingStopped: {
		severity: SeverityError,
		title:    "Recording stopped",
		fixes: map[string]string{
			"": "qui cannot write the transfer history. It tries again at the next write. Make sure that the disk of the qui database has free space and that qui can write the database file.",
		},
	},
	ShrinkFailed: {
		severity:    SeverityWarning,
		title:       "Shrink failed",
		dismissible: true,
		fixes: map[string]string{
			"":          "qui could not make the database file smaller. The database did not change, and qui runs as usual. The qui log has the details.",
			"disk_full": "The disk has too little free space for the shrink, which needs up to two times the size of the database file. Free space on the disk, then try again. The database did not change.",
			"locked":    "Another program held the database file. Stop the other program, for example a backup tool or a database viewer, then try again. The database did not change.",
		},
	},
	LowDisk: {
		severity:    SeverityWarning,
		title:       "Low disk",
		dismissible: true,
		fixes: map[string]string{
			"": "The disk of the qui database has little free space. When the disk is full, each write of qui fails. Free space on the disk, lower \"Keep daily detail for\", or delete older history in the Statistics settings.",
		},
	},
	RedactedKeyRejected: keyRejected("Redacted"),
	OrpheusKeyRejected:  keyRejected("Orpheus"),
}

func keyRejected(site string) definition {
	return definition{
		severity: SeverityError,
		title:    site + " rejected the API key",
		fixes: map[string]string{
			"": site + " rejected the last search. qui asks it again at the next search. If the key is wrong, make an API key with the Torrents permission in your " + site + " profile settings, and paste it in the Gazelle (OPS/RED) settings of qui. If " + site + " banned the IP of qui, a new key does not help: contact the staff of " + site + ".",
		},
	}
}

const notifyTimeout = 10 * time.Second

type Notice struct {
	Kind     Kind     `json:"kind"`
	Severity Severity `json:"severity"`
	// Reason selects the fix that the notice shows. It is empty when the kind
	// has one fix, or when qui does not know the cause.
	Reason   string    `json:"reason,omitempty"`
	Cause    string    `json:"cause"`
	OpenedAt time.Time `json:"openedAt"`
}

type Module struct {
	notifier notifications.Notifier

	mu        sync.Mutex
	open      map[Kind]Notice
	openings  uint64
	opened    map[Kind]uint64
	onDismiss map[Kind]func(context.Context) error
}

// New returns a module with no open notice. It sends each notice that opens
// to notifier.
func New(notifier notifications.Notifier) *Module {
	return &Module{notifier: notifier, open: make(map[Kind]Notice), opened: make(map[Kind]uint64), onDismiss: make(map[Kind]func(context.Context) error)}
}

// Open opens the notice of kind with its cause. A notice that is already open
// stays open with the new cause, and sends no second notification.
func (m *Module) Open(kind Kind, cause string) {
	m.OpenWithReason(kind, "", cause)
}

// OpenWithReason opens the notice as Open does, with the reason that selects
// its fix.
func (m *Module) OpenWithReason(kind Kind, reason, cause string) {
	m.mu.Lock()
	notice, isOpen := m.open[kind]
	if !isOpen {
		m.openings++
		m.opened[kind] = m.openings
		notice = Notice{Kind: kind, Severity: definitions[kind].severity, OpenedAt: time.Now().UTC()}
	}
	notice.Reason = reason
	notice.Cause = cause
	m.open[kind] = notice
	m.mu.Unlock()
	if !isOpen {
		m.send(notice)
	}
}

// Close closes the notice of kind. It does nothing when the notice is not open.
func (m *Module) Close(kind Kind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.open, kind)
}

// Opening returns a number that changes each time the notice of kind opens,
// also after it closed again, or 0 when it never opened. It also reports
// whether the notice is open now.
func (m *Module) Opening(kind Kind) (opening uint64, isOpen bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, isOpen = m.open[kind]
	return m.opened[kind], isOpen
}

// OnDismiss sets fn to run in place of the close when the user dismisses the
// notice of kind. fn closes the notice. When fn returns an error, Dismiss
// returns that error.
func (m *Module) OnDismiss(kind Kind, fn func(context.Context) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onDismiss[kind] = fn
}

// Dismiss closes the notice of kind for the user. It returns
// ErrNotDismissible for a kind that closes itself.
func (m *Module) Dismiss(ctx context.Context, kind Kind) error {
	if !definitions[kind].dismissible {
		return ErrNotDismissible
	}
	m.mu.Lock()
	fn := m.onDismiss[kind]
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	m.Close(kind)
	return nil
}

// List returns the open notices, oldest first.
func (m *Module) List() []Notice {
	m.mu.Lock()
	defer m.mu.Unlock()
	notices := slices.SortedFunc(maps.Values(m.open), func(a, b Notice) int {
		return cmp.Or(a.OpenedAt.Compare(b.OpenedAt), strings.Compare(string(a.Kind), string(b.Kind)))
	})
	if notices == nil {
		// The API answers [] when no notice is open, not null.
		return []Notice{}
	}
	return notices
}

func (m *Module) send(notice Notice) {
	if m.notifier == nil {
		return
	}
	def := definitions[notice.Kind]
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	m.notifier.Notify(ctx, notifications.Event{
		Type:         notifications.EventSystemNotice,
		Title:        def.title,
		Message:      def.fix(notice.Reason) + "\nCause: " + notice.Cause,
		ErrorMessage: notice.Cause,
	})
}

func (d definition) fix(reason string) string {
	if fix, ok := d.fixes[reason]; ok {
		return fix
	}
	return d.fixes[""]
}
