// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import "encoding/json"

// resultClass is what one cross-seed result means for its reader.
type resultClass string

const (
	classAdded   resultClass = "added"
	classExists  resultClass = "exists"
	classSkipped resultClass = "skipped"
	// classStranded is an add that qBittorrent holds but qui could not finish.
	classStranded resultClass = "stranded"
	classFailed   resultClass = "failed"
)

// The exact statuses that Dir Scan, the season pack add, and the manual
// assembly ask about. These checks ask about one status, so they do not read
// the class.
const (
	StatusExists  = "exists"
	StatusBlocked = "blocked"
)

var resultClasses = map[string]resultClass{
	"added":          classAdded,
	"added_hardlink": classAdded,
	"added_reflink":  classAdded,

	StatusExists: classExists,

	"no_match":                            classSkipped,
	"rejected":                            classSkipped,
	StatusBlocked:                         classSkipped,
	"requires_hardlink_reflink":           classSkipped,
	"below_threshold":                     classSkipped,
	"skipped_recheck":                     classSkipped,
	"skipped_unsafe_pieces":               classSkipped,
	contentPrefilterRejectedContentStatus: classSkipped,

	"alignment_failed":                 classStranded,
	"pause_failed":                     classStranded,
	partialPoolRegistrationErrorStatus: classStranded,

	"error":                            classFailed,
	"no_save_path":                     classFailed,
	"invalid_content_path":             classFailed,
	"hardlink_error":                   classFailed,
	"reflink_error":                    classFailed,
	contentPrefilterRejectedSizeStatus: classFailed,
	crossSeedUnavailableStatus:         classFailed,
	crossSeedCannotSeeStatus:           classFailed,
}

// class reads the Result class from the status. A status with no table entry
// is failed, so a forgotten entry shows the user a failure and not silence.
func (r InstanceCrossSeedResult) class() resultClass {
	if class, ok := resultClasses[r.Status]; ok {
		return class
	}
	return classFailed
}

// MarshalJSON adds the class, which the result does not store.
func (r InstanceCrossSeedResult) MarshalJSON() ([]byte, error) {
	type plainResult InstanceCrossSeedResult
	return json.Marshal(struct {
		plainResult
		Class resultClass `json:"class"`
	}{plainResult(r), r.class()})
}
