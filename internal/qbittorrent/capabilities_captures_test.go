// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"encoding/json/v2"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"
)

// captureSkips holds the table entries that no capture checks, with the reason.
var captureSkips = map[Capability]string{
	CapExportOmitsTrackers: "the 4.6.0 and 4.6.7 probes keep the tracker on export, but they test only " +
		"linuxserver builds with libtorrent 2.0 and a torrent with one tracker",
}

// endpointEntries maps each endpoint of a feature probe to the entry that it shows.
var endpointEntries = map[string]Capability{
	"app/getDirectoryContent": CapPathAutocomplete,
	"app/processInfo":         CapProcessInfo,
	"app/sendTestEmail":       CapTestEmail,
	"rss/setFeedURL":          CapSetRSSFeedURL,
	"torrentcreator/status":   CapTorrentCreation,
	"torrents/editTracker":    CapTrackerEditing,
	"torrents/export":         CapTorrentExport,
	"torrents/filePrio":       CapFilePriority,
	"torrents/rename":         CapRenameTorrent,
	"torrents/renameFile":     CapRenameFile,
	"torrents/renameFolder":   CapRenameFolder,
	"torrents/setComment":     CapSetComment,
	"torrents/setTags":        CapSetTags,
}

// captureCheck is what a capture shows for entries: observed must equal
// tableSays.
type captureCheck struct {
	entries   []Capability
	observed  bool
	tableSays func(*semver.Version) bool
}

func showsEntry(capability Capability, observed bool) captureCheck {
	return captureCheck{
		entries:   []Capability{capability},
		observed:  observed,
		tableSays: func(v *semver.Version) bool { return versionSupports(v, capability) },
	}
}

type capture struct {
	kind, release, webAPI string
	checks                []captureCheck
}

// TestCapabilityTableMatchesCaptures checks capabilityVersions against the
// captures of real qBittorrent releases in go-qbittorrent's testdata.
func TestCapabilityTableMatchesCaptures(t *testing.T) {
	t.Parallel()

	testdata := filepath.Join(goQbittorrentDir(t), "testdata")
	names := capabilityNames(t)

	features := featureCaptures(t, testdata)
	webAPI := map[string]string{}
	for _, c := range features {
		webAPI[c.release] = c.webAPI
	}
	captures := slices.Concat(features, torrentCaptures(t, testdata), preferenceCaptures(t, testdata, webAPI))

	shown := map[Capability]bool{}
	for _, c := range captures {
		version, err := semver.NewVersion(c.webAPI)
		if err != nil {
			t.Errorf("%s capture of qBittorrent %s has no known Web API version: %q", c.kind, c.release, c.webAPI)
			continue
		}
		for _, check := range c.checks {
			entryNames := make([]string, 0, len(check.entries))
			for _, capability := range check.entries {
				shown[capability] = true
				entryNames = append(entryNames, names[capability])
			}
			if got := check.tableSays(version); got != check.observed {
				t.Errorf("%s: the %s capture of qBittorrent %s (Web API %s) shows %t, the table says %t",
					strings.Join(entryNames, " and "), c.kind, c.release, c.webAPI, check.observed, got)
			}
		}
	}

	for capability := range capabilityVersions {
		if _, skipped := captureSkips[capability]; !skipped && !shown[capability] {
			t.Errorf("%s: no capture shows this entry. Add a capture to go-qbittorrent, or add the entry to captureSkips with the reason", names[capability])
		}
	}
}

// goQbittorrentDir returns the go-qbittorrent module directory in the module
// cache. GOPROXY=off keeps the go command off the network.
func goQbittorrentDir(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/autobrr/go-qbittorrent")
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	out, err := cmd.Output()
	require.NoError(t, err)
	dir := strings.TrimSpace(string(out))
	require.NotEmpty(t, dir, "go-qbittorrent is not in the module cache; run go mod download")
	return dir
}

// capabilityNames reads the names of the Capability constants from
// capabilities.go, where they count up from 0 with iota.
func capabilityNames(t *testing.T) map[Capability]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "capabilities.go", nil, 0)
	require.NoError(t, err)
	names := map[Capability]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		if typ, ok := gen.Specs[0].(*ast.ValueSpec).Type.(*ast.Ident); ok && typ.Name == "Capability" {
			for i, spec := range gen.Specs {
				names[Capability(i)] = spec.(*ast.ValueSpec).Names[0].Name
			}
		}
	}
	return names
}

func readCaptureJSON(t *testing.T, file string, v any) {
	t.Helper()
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, v), file)
}

// captureFiles maps the release of each file in dir that ends in suffix to
// the file path.
func captureFiles(t *testing.T, dir, suffix string) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*"+suffix))
	require.NoError(t, err)
	releases := map[string]string{}
	for _, file := range files {
		releases[strings.TrimSuffix(filepath.Base(file), suffix)] = file
	}
	return releases
}

func featureCaptures(t *testing.T, testdata string) []capture {
	t.Helper()
	files := captureFiles(t, filepath.Join(testdata, "features"), ".json")
	captures := make([]capture, 0, len(files))
	for release, file := range files {
		var probe struct {
			WebAPIVersion  string         `json:"webapi_version"`
			Endpoints      map[string]int `json:"endpoints"`
			SetShareLimits struct {
				WithoutShareLimitAction int `json:"without_share_limit_action"`
				WithoutShareLimitsMode  int `json:"without_share_limits_mode"`
			} `json:"set_share_limits"`
			SubcategorySavePath string `json:"subcategory_save_path"`
			SavePathEdit        struct {
				Before struct {
					DownloadPath *string `json:"download_path"`
				} `json:"before"`
				After struct {
					DownloadPath *string `json:"download_path"`
				} `json:"after"`
			} `json:"save_path_edit"`
		}
		readCaptureJSON(t, file, &probe)

		var checks []captureCheck
		for endpoint, status := range probe.Endpoints {
			if capability, ok := endpointEntries[endpoint]; ok {
				// An action that does not exist answers 404.
				checks = append(checks, showsEntry(capability, status != 404))
			}
		}
		checks = append(checks,
			// A release that knows the parameter rejects a call without it.
			showsEntry(CapShareLimitsAction, probe.SetShareLimits.WithoutShareLimitAction == 400),
			showsEntry(CapShareLimitsMode, probe.SetShareLimits.WithoutShareLimitsMode == 400),
			// Category p has save path /p. Subcategory p/x:y has none.
			showsEntry(CapCategorySavePathNesting, strings.HasPrefix(probe.SubcategorySavePath, "/p/")),
			showsEntry(CapCategoryFileNames, path.Base(probe.SubcategorySavePath) == "x:y"),
		)
		// The edit sends only a new save path. 4.3.9 has no download path to keep.
		if before, after := probe.SavePathEdit.Before.DownloadPath, probe.SavePathEdit.After.DownloadPath; before != nil {
			checks = append(checks, showsEntry(CapPartialCategoryEdit, after != nil && *after == *before))
		}
		captures = append(captures, capture{kind: "feature probe", release: release, webAPI: probe.WebAPIVersion, checks: checks})
	}
	return captures
}

func torrentCaptures(t *testing.T, testdata string) []capture {
	t.Helper()
	dir := filepath.Join(testdata, "torrents")
	files := captureFiles(t, dir, "-source.json")
	captures := make([]capture, 0, len(files))
	for release, file := range files {
		var source struct {
			WebAPIVersion string `json:"webapi_version"`
		}
		readCaptureJSON(t, file, &source)
		var full struct {
			Torrents map[string]map[string]any `json:"torrents"`
		}
		readCaptureJSON(t, filepath.Join(dir, release+"-full.json"), &full)
		require.Len(t, full.Torrents, 1, "torrents in the %s maindata capture", release)
		var withTrackers []map[string]any
		readCaptureJSON(t, filepath.Join(dir, release+"-info-trackers.json"), &withTrackers)
		require.Len(t, withTrackers, 1, "torrents in the %s includeTrackers capture", release)

		torrent := slices.Collect(maps.Values(full.Torrents))[0]
		has := func(fields ...string) bool {
			for _, field := range fields {
				if _, ok := torrent[field]; !ok {
					return false
				}
			}
			return true
		}
		_, trackers := withTrackers[0]["trackers"]
		captures = append(captures, capture{kind: "torrents", release: release, webAPI: source.WebAPIVersion, checks: []captureCheck{
			showsEntry(CapLiveReannounce, has("reannounce")),
			showsEntry(CapListFieldsV5, has("popularity", "private", "comment")),
			showsEntry(CapListCreatedBy, has("created_by")),
			showsEntry(CapAnnounceFlags, has("has_tracker_error", "has_tracker_warning", "has_other_announce_error")),
			showsEntry(CapTorrentTmpPath, has("download_path")),
			showsEntry(CapTrackerHealth, trackers),
		}})
	}
	return captures
}

// preferenceCaptures takes the Web API version of each release from webAPI,
// because a preferences capture does not record it.
func preferenceCaptures(t *testing.T, testdata string, webAPI map[string]string) []capture {
	t.Helper()
	files := captureFiles(t, filepath.Join(testdata, "preferences"), ".json")
	captures := make([]capture, 0, len(files))
	for release, file := range files {
		var preferences map[string]any
		readCaptureJSON(t, file, &preferences)
		_, toggle := preferences["use_subcategories"]
		// The setting exists only while subcategories are optional.
		captures = append(captures, capture{kind: "preferences", release: release, webAPI: webAPI[release], checks: []captureCheck{{
			entries:  []Capability{CapSubcategories, CapSubcategoriesAlwaysEnabled},
			observed: toggle,
			tableSays: func(v *semver.Version) bool {
				return versionSupports(v, CapSubcategories) && !versionSupports(v, CapSubcategoriesAlwaysEnabled)
			},
		}}})
	}
	return captures
}
