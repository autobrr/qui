// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package prefcatalog is the preference catalog: for each qBittorrent
// preference key, it says where qui shows the key and whether a user can
// change it. It is the only code that sends a preference write to qBittorrent.
package prefcatalog

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/hashicorp/go-version"
)

// Tab is a tab of the qBittorrent preferences dialog.
type Tab string

const (
	TabBehavior   Tab = "behavior"
	TabDownloads  Tab = "downloads"
	TabConnection Tab = "connection"
	TabSpeed      Tab = "speed"
	TabBitTorrent Tab = "bittorrent"
	TabRSS        Tab = "rss"
	TabWebUI      Tab = "webui"
	TabAdvanced   Tab = "advanced"
)

// Control is the widget that shows a preference.
type Control string

const (
	ControlSwitch Control = "switch"
	ControlNumber Control = "number"
	ControlText   Control = "text"
	ControlSelect Control = "select"
	ControlPath   Control = "path"
	// ControlList is a multi-line text box with one item per line.
	ControlList Control = "list"
	// ControlWatchFolders is the scan_dirs table: each row is a path and a destination.
	ControlWatchFolders Control = "watchFolders"
)

// OptionSource says where the options of a select come from.
type OptionSource string

const (
	SourceFixed            OptionSource = "fixed"
	SourcePathAutocomplete OptionSource = "pathAutocomplete"
)

// Mode says whether qui shows a key and whether a user can change it.
type Mode int

const (
	Editable Mode = iota
	ReadOnly
	NotShown
)

// Gate limits a field or an option to some libtorrent versions and platforms.
// An empty libtorrent version or platform passes the gate.
type Gate struct {
	MinLibtorrent   string   // the lowest libtorrent version that shows it
	BelowLibtorrent string   // the libtorrent version from which it is hidden
	NotPlatforms    []string // the platforms that hide it
}

// Option is one value of a select with a fixed option list.
type Option struct {
	Value any
	Gate  Gate
}

// Entry is the catalog entry of one preference key.
type Entry struct {
	Key          string
	Tab          Tab
	Section      string
	Control      Control
	WireUnit     string // the unit that qBittorrent sends
	DisplayUnit  string // the unit that qui shows
	OptionSource OptionSource
	Options      []Option
	Gate         Gate
	DependsOn    string // the switch that enables the field
	Mode         Mode
	Reason       string // why a key is read-only or not shown
}

// Field is one preference in the view: its value and how to show it.
type Field struct {
	Key          string         `json:"key"`
	Value        jsontext.Value `json:"value"`
	Tab          Tab            `json:"tab"`
	Section      string         `json:"section"`
	Control      Control        `json:"control"`
	WireUnit     string         `json:"wireUnit,omitempty"`
	DisplayUnit  string         `json:"displayUnit,omitempty"`
	OptionSource OptionSource   `json:"optionSource,omitempty"`
	Options      []any          `json:"options,omitempty"`
	DependsOn    string         `json:"dependsOn,omitempty"`
	ReadOnly     bool           `json:"readOnly,omitzero"`
	Reason       string         `json:"reason,omitempty"`
}

// ErrInvalidPatch is the error of a patch that Apply rejects.
var ErrInvalidPatch = errors.New("invalid preferences patch")

// catalog holds every entry, in the order of the tabs and then of the rows in each tab.
var catalog = slices.Concat(behaviorEntries, downloadsEntries, connectionEntries, speedEntries, bittorrentEntries, rssEntries, webUIEntries, advancedEntries)

var byKey = func() map[string]Entry {
	entries := make(map[string]Entry, len(catalog))
	for _, entry := range catalog {
		entries[entry.Key] = entry
	}
	return entries
}()

// View returns the fields that qui shows for prefs, in catalog order. It drops
// the keys that qBittorrent did not send, the keys whose gate fails, and the
// keys that are not shown.
func View(prefs qbt.AppPreferences, libtorrentVersion, platform string) ([]Field, error) {
	// MarshalJSON writes only the keys that qBittorrent sent, and never web_ui_api_key.
	data, err := json.Marshal(prefs)
	if err != nil {
		return nil, err
	}
	var values map[string]jsontext.Value
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}

	fields := make([]Field, 0, len(values))
	for _, entry := range catalog {
		value, sent := values[entry.Key]
		if !sent || entry.Mode == NotShown || !entry.Gate.passes(libtorrentVersion, platform) {
			continue
		}
		field := Field{
			Key:          entry.Key,
			Value:        value,
			Tab:          entry.Tab,
			Section:      entry.Section,
			Control:      entry.Control,
			WireUnit:     entry.WireUnit,
			DisplayUnit:  entry.DisplayUnit,
			OptionSource: entry.OptionSource,
			DependsOn:    entry.DependsOn,
			ReadOnly:     entry.Mode == ReadOnly,
			Reason:       entry.Reason,
		}
		for _, option := range entry.Options {
			if option.Gate.passes(libtorrentVersion, platform) {
				field.Options = append(field.Options, option.Value)
			}
		}
		fields = append(fields, field)
	}
	return fields, nil
}

// Apply checks patch and sends it to qBittorrent. It rejects a key that is not
// in the catalog, a key that a user cannot change, and a value of the wrong
// type, and then nothing is sent. It does not check ranges: qBittorrent clamps
// or ignores a bad value itself.
func Apply(ctx context.Context, client *qbt.Client, patch map[string]any) error {
	payload := make(map[string]any, len(patch))
	for key, value := range patch {
		checked, err := check(key, value)
		if err != nil {
			return err
		}
		payload[key] = checked
	}
	return client.SetPreferencesCtx(ctx, payload)
}

func check(key string, value any) (any, error) {
	entry, ok := byKey[key]
	switch {
	case !ok:
		return nil, fmt.Errorf("%w: unknown key %q", ErrInvalidPatch, key)
	case entry.Mode == ReadOnly:
		return nil, fmt.Errorf("%w: %q is read-only", ErrInvalidPatch, key)
	case entry.Mode == NotShown:
		return nil, fmt.Errorf("%w: %q cannot be changed in qui", ErrInvalidPatch, key)
	case value == nil:
		return nil, fmt.Errorf("%w: %q has no value", ErrInvalidPatch, key)
	}

	// The AppPreferences field of the key has the type that qBittorrent sends.
	data, err := json.Marshal(map[string]any{key: value})
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrInvalidPatch, key, err)
	}
	var prefs qbt.AppPreferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil, fmt.Errorf("%w: %q has a value of the wrong type", ErrInvalidPatch, key)
	}
	if key == "scan_dirs" {
		return prefs.ScanDirs, nil
	}
	return value, nil
}

func (g Gate) passes(libtorrentVersion, platform string) bool {
	// An unknown libtorrent version passes the libtorrent gate.
	if libtorrent, err := version.NewVersion(libtorrentVersion); err == nil {
		if g.MinLibtorrent != "" && libtorrent.LessThan(version.Must(version.NewVersion(g.MinLibtorrent))) {
			return false
		}
		if g.BelowLibtorrent != "" && !libtorrent.LessThan(version.Must(version.NewVersion(g.BelowLibtorrent))) {
			return false
		}
	}
	return platform == "" || !slices.Contains(g.NotPlatforms, platform)
}

// notShown returns entries that qui does not show, with one reason.
func notShown(tab Tab, reason string, keys ...string) []Entry {
	entries := make([]Entry, len(keys))
	for i, key := range keys {
		entries[i] = Entry{Key: key, Tab: tab, Mode: NotShown, Reason: reason}
	}
	return entries
}

// reasonWebUIOnly is the reason for a key that changes only how the qBittorrent
// WebUI or desktop app looks or acts.
const reasonWebUIOnly = "changes only the qBittorrent WebUI or desktop app"

// fixedOptions returns the options of a select that no gate limits.
func fixedOptions(values ...any) []Option {
	options := make([]Option, len(values))
	for i, value := range values {
		options[i] = Option{Value: value}
	}
	return options
}
