// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import "encoding/json"

type ResultClass string

const (
	ClassAdded    ResultClass = "added"
	ClassExists   ResultClass = "exists"
	ClassSkipped  ResultClass = "skipped"
	ClassStranded ResultClass = "stranded"
	ClassFailed   ResultClass = "failed"
)

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

func ClassOf(status string) ResultClass {
	if class, ok := resultClasses[status]; ok {
		return class
	}
	return ClassFailed
}

func (r InstanceCrossSeedResult) class() ResultClass {
	return ClassOf(r.Status)
}

func (r AddResult) Class() ResultClass {
	switch r.Outcome {
	case AddRefused:
		return ClassOf(r.Status)
	case AddLeftPaused:
		return ClassStranded
	default:
		return ClassAdded
	}
}

func (r InstanceCrossSeedResult) MarshalJSON() ([]byte, error) {
	type plainResult InstanceCrossSeedResult
	return json.Marshal(struct {
		plainResult
		Class ResultClass `json:"class"`
	}{plainResult(r), r.class()})
}
