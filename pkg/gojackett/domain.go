// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"encoding/xml"
)

// TorznabError represents a Torznab error response
type TorznabError struct {
	Code    string `xml:"code,attr"`
	Message string `xml:",chardata"`
}

type Indexers struct {
	XMLName xml.Name `xml:"indexers"`
	Text    string   `xml:",chardata"`
	Indexer []struct {
		Text        string `xml:",chardata"`
		ID          string `xml:"id,attr"`
		Configured  string `xml:"configured,attr"`
		Title       string `xml:"title"`
		Description string `xml:"description"`
		Link        string `xml:"link"`
		Language    string `xml:"language"`
		Type        string `xml:"type"`
		Caps        struct {
			Text   string `xml:",chardata"`
			Server struct {
				Text  string `xml:",chardata"`
				Title string `xml:"title,attr"`
			} `xml:"server"`
			Limits struct {
				Text    string `xml:",chardata"`
				Default string `xml:"default,attr"`
				Max     string `xml:"max,attr"`
			} `xml:"limits"`
			Searching struct {
				Text   string `xml:",chardata"`
				Search struct {
					Text            string `xml:",chardata"`
					Available       string `xml:"available,attr"`
					SupportedParams string `xml:"supportedParams,attr"`
					SearchEngine    string `xml:"searchEngine,attr"`
				} `xml:"search"`
				TvSearch struct {
					Text            string `xml:",chardata"`
					Available       string `xml:"available,attr"`
					SupportedParams string `xml:"supportedParams,attr"`
					SearchEngine    string `xml:"searchEngine,attr"`
				} `xml:"tv-search"`
				MovieSearch struct {
					Text            string `xml:",chardata"`
					Available       string `xml:"available,attr"`
					SupportedParams string `xml:"supportedParams,attr"`
					SearchEngine    string `xml:"searchEngine,attr"`
				} `xml:"movie-search"`
				MusicSearch struct {
					Text            string `xml:",chardata"`
					Available       string `xml:"available,attr"`
					SupportedParams string `xml:"supportedParams,attr"`
					SearchEngine    string `xml:"searchEngine,attr"`
				} `xml:"music-search"`
				AudioSearch struct {
					Text            string `xml:",chardata"`
					Available       string `xml:"available,attr"`
					SupportedParams string `xml:"supportedParams,attr"`
					SearchEngine    string `xml:"searchEngine,attr"`
				} `xml:"audio-search"`
				BookSearch struct {
					Text            string `xml:",chardata"`
					Available       string `xml:"available,attr"`
					SupportedParams string `xml:"supportedParams,attr"`
					SearchEngine    string `xml:"searchEngine,attr"`
				} `xml:"book-search"`
			} `xml:"searching"`
			Categories struct {
				Text     string `xml:",chardata"`
				Category []struct {
					Text   string `xml:",chardata"`
					ID     string `xml:"id,attr"`
					Name   string `xml:"name,attr"`
					Subcat []struct {
						Text string `xml:",chardata"`
						ID   string `xml:"id,attr"`
						Name string `xml:"name,attr"`
					} `xml:"subcat"`
				} `xml:"category"`
			} `xml:"categories"`
		} `xml:"caps"`
	} `xml:"indexer"`
}

type Rss struct {
	XMLName xml.Name `xml:"rss"`
	Text    string   `xml:",chardata"`
	Version string   `xml:"version,attr"`
	Atom    string   `xml:"atom,attr"`
	Torznab string   `xml:"torznab,attr"`
	Channel struct {
		Text string `xml:",chardata"`
		Link struct {
			Text string `xml:",chardata"`
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
			Type string `xml:"type,attr"`
		} `xml:"link"`
		Title       string `xml:"title"`
		Description string `xml:"description"`
		Language    string `xml:"language"`
		Category    string `xml:"category"`
		Item        []struct {
			Text           string `xml:",chardata"`
			Title          string `xml:"title"`
			Guid           string `xml:"guid"`
			Jackettindexer struct {
				Text string `xml:",chardata"`
				ID   string `xml:"id,attr"`
			} `xml:"jackettindexer"`
			Type        string   `xml:"type"`
			Comments    string   `xml:"comments"`
			PubDate     string   `xml:"pubDate"`
			Size        string   `xml:"size"`
			Files       string   `xml:"files"`
			Grabs       string   `xml:"grabs"`
			Description string   `xml:"description"`
			Link        string   `xml:"link"`
			Category    []string `xml:"category"`
			Enclosure   struct {
				Text   string `xml:",chardata"`
				URL    string `xml:"url,attr"`
				Length string `xml:"length,attr"`
				Type   string `xml:"type,attr"`
			} `xml:"enclosure"`
			Attr []struct {
				Text  string `xml:",chardata"`
				Name  string `xml:"name,attr"`
				Value string `xml:"value,attr"`
			} `xml:"attr"`
		} `xml:"item"`
	} `xml:"channel"`
}
