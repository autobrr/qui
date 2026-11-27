// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secretshapes_test

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/api/handlers"
	"github.com/autobrr/qui/internal/api/secretshapes"
	"github.com/autobrr/qui/internal/qbittorrent/prefcatalog"
	"github.com/autobrr/qui/internal/secrets"
)

func TestEveryShapeHidesEveryString(t *testing.T) {
	checkHidesEveryString(t, secretshapes.Encoder)
}

func TestEveryShapeLeavesItsSourceWhole(t *testing.T) {
	checkLeavesSourceWhole(t, secretshapes.Encoder)
}

func TestFullAudienceEncodesAsEncodingJSON(t *testing.T) {
	checkFullEncodesAsEncodingJSON(t, secretshapes.Encoder,
		handlers.ErrorResponse{Error: "Failed to read <the instance> & retry é"},
		map[string]any{"b": []int{1, 2}, "a": "<script>", "c": nil, "d": map[string]bool{"x": true}},
		prefcatalog.Field{Key: "add_trackers", Value: jsontext.Value(`"https://tracker.example/made-up-key/announce\n<b>é</b> & ü"`), Section: "<Tracker>"},
	)
}

type recorder struct{ reports []string }

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.reports = append(r.reports, fmt.Sprintf(format, args...))
}

type fixtureTracker struct {
	URL     string
	Message string
}

type fixtureTarget struct {
	URL     string
	Tracker fixtureTracker
}

type fixtureTorrent struct {
	Name     string
	Comment  string
	Tracker  string
	Trackers []fixtureTracker
	Target   fixtureTarget
}

type fixtureRow struct {
	fixtureTorrent
	State string
}

var (
	fixtureTrackerShape = secrets.NewShape(secrets.TrackerPasskey, func(t fixtureTracker) fixtureTracker {
		t.URL = secrets.Origin(t.URL)
		return t
	}, "Message")
	fixtureTargetShape = secrets.NewShape(secrets.NotificationToken, func(t fixtureTarget) fixtureTarget {
		t.URL = secrets.Scheme(t.URL)
		return t
	})
)

func fixtureTorrentShape(hidden func(fixtureTorrent) fixtureTorrent, plain ...string) secrets.Shape[fixtureTorrent] {
	return secrets.NewShape(secrets.TrackerPasskey, hidden, plain...)
}

func hideFixtureTorrent(t fixtureTorrent) fixtureTorrent {
	t.Tracker = secrets.Origin(t.Tracker)
	t.Trackers = fixtureTrackerShape.HiddenAll(secrets.Audience{}, t.Trackers)
	t.Target.Tracker = fixtureTrackerShape.Hidden(secrets.Audience{}, t.Target.Tracker)
	return t
}

var fixtureRowShape = secrets.NewShape(secrets.TrackerPasskey, func(r fixtureRow) fixtureRow {
	r.fixtureTorrent = hideFixtureTorrent(r.fixtureTorrent)
	return r
}, "Name", "Comment", "State")

func fixtureEncoder(torrent secrets.Shape[fixtureTorrent]) *secrets.Encoder {
	return secrets.NewEncoder(fixtureTrackerShape, fixtureTargetShape, torrent, fixtureRowShape)
}

func TestTheShapeChecksPassASoundShape(t *testing.T) {
	r := &recorder{}
	enc := fixtureEncoder(fixtureTorrentShape(hideFixtureTorrent, "Name", "Comment"))

	checkHidesEveryString(r, enc)
	checkLeavesSourceWhole(r, enc)
	checkFullEncodesAsEncodingJSON(r, enc)

	assert.Empty(t, r.reports)
}

func TestTheShapeChecksNameEachFault(t *testing.T) {
	for _, tc := range []struct {
		name    string
		shape   secrets.Shape[fixtureTorrent]
		check   func(reporter, *secrets.Encoder)
		reports []string
	}{
		{
			name:    "a text field that is neither hidden nor plain",
			shape:   fixtureTorrentShape(hideFixtureTorrent, "Name"),
			check:   checkHidesEveryString,
			reports: []string{"the encoder shows the field Comment", "Hide and encoding/json shows the field Comment"},
		},
		{
			name:    "a plain field that the Shape hides",
			shape:   fixtureTorrentShape(func(t fixtureTorrent) fixtureTorrent { t = hideFixtureTorrent(t); t.Name = ""; return t }, "Name", "Comment"),
			check:   checkHidesEveryString,
			reports: []string{"the encoder hides the plain field Name", "Hide and encoding/json hides the plain field Name"},
		},
		{
			name: "a nested Shape of the same kind that the Shape leaves to the encoder",
			shape: fixtureTorrentShape(func(t fixtureTorrent) fixtureTorrent {
				t.Tracker = secrets.Origin(t.Tracker)
				return t
			}, "Name", "Comment"),
			check:   checkHidesEveryString,
			reports: []string{"shows the field Trackers.URL"},
		},
		{
			name: "a Shape of the same kind inside a Shape of another kind that the Shape leaves to the encoder",
			shape: fixtureTorrentShape(func(t fixtureTorrent) fixtureTorrent {
				t.Tracker = secrets.Origin(t.Tracker)
				t.Trackers = fixtureTrackerShape.HiddenAll(secrets.Audience{}, t.Trackers)
				return t
			}, "Name", "Comment"),
			check:   checkHidesEveryString,
			reports: []string{"the encoder shows the field Target.Tracker.URL"},
		},
		{
			name: "a Shape that writes through a slice of its source",
			shape: fixtureTorrentShape(func(t fixtureTorrent) fixtureTorrent {
				t.Tracker = secrets.Origin(t.Tracker)
				for i := range t.Trackers {
					t.Trackers[i].URL = secrets.Origin(t.Trackers[i].URL)
				}
				return t
			}, "Name", "Comment"),
			check:   checkLeavesSourceWhole,
			reports: []string{"fixtureTorrent: the encoder changes its source"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{}
			tc.check(r, fixtureEncoder(tc.shape))
			joined := strings.Join(r.reports, "\n")
			for _, want := range tc.reports {
				assert.Contains(t, joined, want)
			}
		})
	}
}
