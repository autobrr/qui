// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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

type Control string

const (
	ControlSwitch       Control = "switch"
	ControlNumber       Control = "number"
	ControlText         Control = "text"
	ControlPassword     Control = "password"
	ControlSelect       Control = "select"
	ControlPath         Control = "path"
	ControlList         Control = "list"
	ControlWatchFolders Control = "watchFolders"
)

type OptionSource string

const (
	SourceFixed              OptionSource = "fixed"
	SourcePathAutocomplete   OptionSource = "pathAutocomplete"
	SourceNetworkInterfaces  OptionSource = "networkInterfaces"
	SourceInterfaceAddresses OptionSource = "interfaceAddresses"
)

type Mode int

const (
	Editable Mode = iota
	ReadOnly
	NotShown
)

type Gate struct {
	MinLibtorrent   string
	BelowLibtorrent string
	NotPlatforms    []string
}

type Option struct {
	Value any
	Gate  Gate
}

type Entry struct {
	Key          string
	Tab          Tab
	Section      string
	Control      Control
	WireUnit     string
	DisplayUnit  string
	OptionSource OptionSource
	Options      []Option
	Gate         Gate
	DependsOn    string
	Mode         Mode
	Reason       string
}

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

var ErrInvalidPatch = errors.New("invalid preferences patch")

var catalog = slices.Concat(behaviorEntries, downloadsEntries, connectionEntries, speedEntries, bittorrentEntries, rssEntries, webUIEntries, advancedEntries)

var byKey = func() map[string]Entry {
	entries := make(map[string]Entry, len(catalog))
	for _, entry := range catalog {
		entries[entry.Key] = entry
	}
	return entries
}()

func View(prefs qbt.AppPreferences, libtorrentVersion, platform string) ([]Field, error) {
	data, err := json.Marshal(prefs)
	if err != nil {
		return nil, err
	}
	var values map[string]jsontext.Value
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}

	numericProxyType := values["proxy_type"].Kind() == '0'

	fields := make([]Field, 0, len(values))
	for _, entry := range catalog {
		value, sent := values[entry.Key]
		if !sent || entry.Mode == NotShown || !entry.Gate.passes(libtorrentVersion, platform) {
			continue
		}
		if entry.Key == "proxy_auth_enabled" && numericProxyType {
			continue
		}
		if entry.Control == ControlPassword {
			value = jsontext.Value(`""`)
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
			_, isString := option.Value.(string)
			if option.Gate.passes(libtorrentVersion, platform) && isString == (value.Kind() == '"') {
				field.Options = append(field.Options, option.Value)
			}
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func Apply(ctx context.Context, client *qbt.Client, patch map[string]any) error {
	payload := make(map[string]any, len(patch))
	for key, value := range patch {
		checked, err := check(key, value)
		if err != nil {
			return err
		}
		if checked == "" && byKey[key].Control == ControlPassword {
			continue
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

func notShown(tab Tab, reason string, keys ...string) []Entry {
	entries := make([]Entry, len(keys))
	for i, key := range keys {
		entries[i] = Entry{Key: key, Tab: tab, Mode: NotShown, Reason: reason}
	}
	return entries
}

const reasonWebUIOnly = "changes only the qBittorrent WebUI or desktop app"

func fixedOptions(values ...any) []Option {
	options := make([]Option, len(values))
	for i, value := range values {
		options[i] = Option{Value: value}
	}
	return options
}
