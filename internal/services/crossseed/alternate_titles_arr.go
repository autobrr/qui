// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"

	"github.com/autobrr/qui/internal/services/arr"
)

type arrAlternateTitles struct {
	arr *arr.Service
}

func NewArrAlternateTitles(service *arr.Service) AlternateTitleSource {
	return arrAlternateTitles{arr: service}
}

func (a arrAlternateTitles) AlternateTitles(ctx context.Context, held HeldRelease) (AlternateTitles, error) {
	contentType := mapContentTypeToARR(held.ContentType)
	if contentType == "" {
		return AlternateTitles{FromCache: true}, nil
	}
	result, err := a.arr.LookupExternalIDs(ctx, held.TorrentName, contentType)
	if err != nil {
		return AlternateTitles{}, fmt.Errorf("look up alternate titles in Sonarr or Radarr: %w", err)
	}
	if result == nil {
		return AlternateTitles{FromCache: true}, nil
	}
	return AlternateTitles{Titles: result.Titles, FromCache: result.FromCache}, nil
}
