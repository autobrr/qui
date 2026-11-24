// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"slices"

	"github.com/autobrr/qui/internal/services/arr"
)

type arrAlternateTitles struct {
	arr          *arr.Service
	contentTypes []arr.ContentType
}

func NewArrAlternateTitles(service *arr.Service) []AlternateTitleSource {
	return []AlternateTitleSource{
		arrAlternateTitles{arr: service, contentTypes: []arr.ContentType{arr.ContentTypeTV, arr.ContentTypeAnime}},
		arrAlternateTitles{arr: service, contentTypes: []arr.ContentType{arr.ContentTypeMovie}},
	}
}

func (a arrAlternateTitles) AlternateTitles(ctx context.Context, held HeldRelease) (AlternateTitles, error) {
	contentType := mapContentTypeToARR(held.ContentType)
	if !slices.Contains(a.contentTypes, contentType) {
		return AlternateTitles{}, nil
	}
	result, err := a.arr.LookupExternalIDs(ctx, held.TorrentName, contentType)
	if err != nil {
		return AlternateTitles{Requested: true}, fmt.Errorf("look up the alternate titles of %s: %w", contentType, err)
	}
	if result == nil {
		return AlternateTitles{}, nil
	}
	return AlternateTitles{Titles: result.Titles, Requested: !result.FromCache || refetched(result), Incomplete: result.Incomplete}, nil
}

func refetched(cached *arr.ExternalIDsResult) bool {
	return cached.IDs != nil && !cached.EpisodeMapKnown
}
