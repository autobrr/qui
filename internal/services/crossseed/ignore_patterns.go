// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

// DefaultIgnoredExtensions contains file extensions that should be ignored
// during cross-seed matching. These are typically scene metadata files,
// subtitles, and other sidecar files that don't affect content matching.
var DefaultIgnoredExtensions = []string{
	// Scene release files
	".nfo",
	".srr",

	// Subtitles
	".srt",
	".sub",
	".idx",
	".ass",
	".ssa",
	".sup",
	".vtt",

	// Text files
	".txt",
}

// DefaultIgnoredPathKeywords contains path substrings that indicate files
// should be ignored during matching. These typically represent sample files,
// proof screenshots, and bonus content.
var DefaultIgnoredPathKeywords = []string{
	"sample",
	"!sample",
	"proof",
	"extras",
	"bonus",
	"trailer",
	"featurette",
}
