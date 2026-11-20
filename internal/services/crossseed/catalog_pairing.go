// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"errors"
	"path"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/services/linktree"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type catalogPairTier uint8

const (
	// catalogPairPath compares the full paths, so a renamed display name does
	// not hide identical paths.
	catalogPairPath catalogPairTier = iota
	catalogPairExact
	catalogPairBasename
	catalogPairNormalized
	catalogPairSize
)

type catalogFilePair struct {
	fileMatchInstruction
	candidateIndex int
	tier           catalogPairTier
}

type catalogPairKey struct {
	size int64
	name string
}

func catalogPairName(name, root string, tier catalogPairTier) string {
	switch tier {
	case catalogPairPath:
		return name
	case catalogPairExact:
		if root != "" {
			return strings.TrimPrefix(name, root+"/")
		}
		return name
	case catalogPairBasename:
		return strings.ToLower(path.Base(name))
	case catalogPairNormalized:
		return normalizeFileKey(name)
	case catalogPairSize:
		// Only files with the same extension can pair, so a renamed .cue and
		// .log of the same size are not ambiguous.
		return strings.ToLower(path.Ext(name))
	}
	return ""
}

func catalogPairBuckets(files qbt.TorrentFiles, used []bool, root string, tier catalogPairTier) map[catalogPairKey][]int {
	buckets := make(map[catalogPairKey][]int)
	for i, file := range files {
		if !used[i] {
			key := catalogPairKey{file.Size, catalogPairName(file.Name, root, tier)}
			buckets[key] = append(buckets[key], i)
		}
	}
	return buckets
}

func completeCatalogPairing(incoming, existing qbt.TorrentFiles, incomingRoot, existingRoot string) ([]catalogFilePair, error) {
	_, incomingSize, err := fullFileSnapshot(incoming)
	if err != nil {
		return nil, err
	}
	_, existingSize, err := fullFileSnapshot(existing)
	if err != nil {
		return nil, err
	}
	if len(incoming) != len(existing) || incomingSize != existingSize {
		return nil, errors.New("full file counts or totals differ")
	}
	usedIncoming, usedExisting := make([]bool, len(incoming)), make([]bool, len(existing))
	pairs := make([]catalogFilePair, 0, len(incoming))
	for tier := catalogPairPath; tier <= catalogPairSize; tier++ {
		left := catalogPairBuckets(incoming, usedIncoming, incomingRoot, tier)
		right := catalogPairBuckets(existing, usedExisting, existingRoot, tier)
		for i, file := range incoming {
			if usedIncoming[i] {
				continue
			}
			key := catalogPairKey{file.Size, catalogPairName(file.Name, incomingRoot, tier)}
			li, ri := left[key], right[key]
			if len(li) != 1 || len(ri) != 1 {
				continue
			}
			j := ri[0]
			if !strings.EqualFold(path.Ext(file.Name), path.Ext(existing[j].Name)) {
				continue
			}
			pairs = append(pairs, catalogFilePair{sourceIndex: i, sourcePath: file.Name, candidatePath: existing[j].Name, candidateIndex: j, tier: tier})
			usedIncoming[i], usedExisting[j] = true, true
		}
	}
	if len(pairs) != len(incoming) {
		return nil, errors.New("file correspondence failed: missing or ambiguous individual-size partners")
	}
	return pairs, nil
}

func catalogLinks(d fsops.PathDialect, pairs []catalogFilePair, existing []hardlinktree.ExistingFile) ([]linktree.Link, error) {
	links := make([]linktree.Link, 0, len(pairs))
	seen := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		if pair.candidateIndex < 0 || pair.candidateIndex >= len(existing) || existing[pair.candidateIndex].RelPath != pair.candidatePath {
			return nil, errors.New("validated link source changed")
		}
		if err := hardlinktree.ValidateRelativePath(d, pair.sourcePath); err != nil {
			return nil, err
		}
		key := d.Clean(d.FromSlash(pair.sourcePath))
		if d.Separator() == `\` {
			key = strings.ToLower(key)
		}
		if _, collision := seen[key]; collision {
			return nil, errors.New("validated target paths collide")
		}
		seen[key] = struct{}{}
		links = append(links, linktree.Link{TorrentPath: pair.sourcePath, SourcePath: existing[pair.candidateIndex].AbsPath})
	}
	for key := range seen {
		for parent := d.Dir(key); parent != d.Dir(parent); parent = d.Dir(parent) {
			if _, collision := seen[parent]; collision {
				return nil, errors.New("validated target file and directory paths collide")
			}
		}
	}
	return links, nil
}
