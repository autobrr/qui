// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import "encoding/json"

// ResultClass is what one cross-seed result means for its reader.
type ResultClass string

const (
	ClassAdded   ResultClass = "added"
	ClassExists  ResultClass = "exists"
	ClassSkipped ResultClass = "skipped"
	// ClassStranded is an add that qBittorrent holds but qui could not finish.
	ClassStranded ResultClass = "stranded"
	ClassFailed   ResultClass = "failed"
)

// The exact statuses that the season pack add and the manual assembly ask
// about. These checks ask about one status, so they do not read the class.
const (
	StatusExists  = "exists"
	StatusBlocked = "blocked"
)

var resultClasses = map[string]ResultClass{
	"added":          ClassAdded,
	"added_hardlink": ClassAdded,
	"added_reflink":  ClassAdded,

	StatusExists: ClassExists,

	"no_match":                            ClassSkipped,
	"rejected":                            ClassSkipped,
	StatusBlocked:                         ClassSkipped,
	"requires_hardlink_reflink":           ClassSkipped,
	"below_threshold":                     ClassSkipped,
	"skipped_recheck":                     ClassSkipped,
	"skipped_unsafe_pieces":               ClassSkipped,
	contentPrefilterRejectedContentStatus: ClassSkipped,

	"alignment_failed":                 ClassStranded,
	"pause_failed":                     ClassStranded,
	partialPoolRegistrationErrorStatus: ClassStranded,

	"error":                            ClassFailed,
	"no_save_path":                     ClassFailed,
	"invalid_content_path":             ClassFailed,
	"hardlink_error":                   ClassFailed,
	"reflink_error":                    ClassFailed,
	contentPrefilterRejectedSizeStatus: ClassFailed,
	crossSeedUnavailableStatus:         ClassFailed,
	crossSeedCannotSeeStatus:           ClassFailed,
}

// classOf reads the Result class of a status from the table. A status with no
// entry is failed, so a forgotten entry shows the user a failure and not silence.
func classOf(status string) ResultClass {
	if class, ok := resultClasses[status]; ok {
		return class
	}
	return ClassFailed
}

func (r InstanceCrossSeedResult) class() ResultClass {
	return classOf(r.Status)
}

// Class is the Result class of a Pre-add check refusal or of an add. An add
// that stays paused after a failed AfterAdd is stranded.
func (r AddResult) Class() ResultClass {
	switch r.Outcome {
	case AddRefused:
		return classOf(r.Status)
	case AddLeftPaused:
		return ClassStranded
	default:
		return ClassAdded
	}
}

// MarshalJSON adds the class, which the result does not store.
func (r InstanceCrossSeedResult) MarshalJSON() ([]byte, error) {
	type plainResult InstanceCrossSeedResult
	return json.Marshal(struct {
		plainResult
		Class ResultClass `json:"class"`
	}{plainResult(r), r.class()})
}
