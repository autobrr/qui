// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"path"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
)

type catalogFileEvidence struct {
	Kind           gazellemusic.MatchKind
	Host           string
	TorrentID      int64
	TargetHash     string
	QueriedHash    string
	Total          int64
	SourceSnapshot [32]byte
	Family         rls.Type
}

func (e catalogFileEvidence) valid() bool {
	if e.TorrentID <= 0 || e.Total <= 0 || e.SourceSnapshot == [32]byte{} {
		return false
	}
	if e.Family != rls.Music && e.Family != rls.Book {
		return false
	}
	if _, ok := gazellemusic.SiteByHost(e.Host); !ok {
		return false
	}
	switch e.Kind {
	case gazellemusic.MatchHash:
		hash, err := hex.DecodeString(e.TargetHash)
		return err == nil && len(hash) == 20 && strings.EqualFold(e.QueriedHash, e.TargetHash)
	case gazellemusic.MatchSize:
		// The torrent ID of the download URL names the target.
		return e.TargetHash == ""
	}
	return false
}

func (p searchDecisionProvenance) catalog() bool {
	return p.Class == searchCandidateClassFileEvidence
}

func fullFileSnapshot(files qbt.TorrentFiles) ([32]byte, int64, error) {
	if len(files) == 0 {
		return [32]byte{}, 0, errors.New("empty full file table")
	}
	type tableEntry struct {
		name  string
		size  int64
		index int
	}
	ordered := make([]tableEntry, len(files))
	snapshotLen := 8
	for i, file := range files {
		ordered[i] = tableEntry{name: file.Name, size: file.Size, index: file.Index}
		snapshotLen += 24 + len(file.Name)
	}
	slices.SortFunc(ordered, func(a, b tableEntry) int { return strings.Compare(a.name, b.name) })
	snapshot := make([]byte, 0, snapshotLen)
	snapshot = binary.BigEndian.AppendUint64(snapshot, uint64(len(ordered)))
	seen := make(map[string]struct{}, len(ordered))
	var total int64
	for i, file := range ordered {
		clean, safe := safeTorrentRelativeFilePath(file.name)
		if !safe || strings.ContainsRune(file.name, 0) || clean != file.name || (i > 0 && ordered[i-1].name == file.name) || catalogFileAncestor(file.name, seen) || file.size < 0 || file.size > math.MaxInt64-total {
			return [32]byte{}, 0, errors.New("invalid or duplicate full file table entry")
		}
		seen[file.name] = struct{}{}
		total += file.size
		snapshot = binary.BigEndian.AppendUint64(snapshot, uint64(len(file.name)))
		snapshot = append(snapshot, file.name...)
		snapshot = binary.BigEndian.AppendUint64(snapshot, uint64(file.size))
		snapshot = binary.BigEndian.AppendUint64(snapshot, uint64(int64(file.index)))
	}
	if total <= 0 {
		return [32]byte{}, 0, errors.New("nonpositive full torrent size")
	}
	return sha256.Sum256(snapshot), total, nil
}

// catalogFileFamily reads a file table that fullFileSnapshot accepted.
func catalogFileFamily(files qbt.TorrentFiles) rls.Type {
	var total, audio, books int64
	for _, file := range files {
		total += file.Size
		ext := strings.ToLower(path.Ext(file.Name))
		_, video := videoExtensions[ext]
		if video || isArchiveFilename(strings.ToLower(file.Name)) {
			return rls.Unknown
		}
		switch ext {
		case ".vob", ".mpg", ".mpeg", ".webm", ".m4v", ".iso":
			return rls.Unknown
		}
		if audioFileExtensions[ext] {
			audio += file.Size
		} else if gazellePlausibleExtensions[ext] {
			books += file.Size
		}
	}
	if audio > 0 {
		return rls.Music
	}
	if books > 0 && books >= total-books && audio == 0 {
		return rls.Book
	}
	return rls.Unknown
}

// gazelleDecision returns no decision for content other than music and books,
// so Add matches that result by name, like a result from before file evidence.
func (s *Service) gazelleDecision(instanceID int, source *qbt.Torrent, files qbt.TorrentFiles, match *gazellemusic.Match) (searchDecisionProvenance, error) {
	snapshot, _, err := fullFileSnapshot(files)
	if err != nil {
		return searchDecisionProvenance{}, err
	}
	family := catalogFileFamily(files)
	if family != rls.Music && family != rls.Book {
		return searchDecisionProvenance{}, nil
	}
	e := catalogFileEvidence{Kind: match.Kind, Host: match.Host, TorrentID: match.TorrentID, TargetHash: normalizeHash(match.TargetHash), QueriedHash: match.QueriedHash, Total: match.Size, SourceSnapshot: snapshot, Family: family}
	sourceRead := s.matcher().readName(source.Name)
	candidateRead := s.matcher().readName(match.Title)
	verdict := s.matcher().matchVerdict(sourceRead, candidateRead, matchEvidence{SourceSize: searchSourceSize(source), CandidateSize: match.Size, SourceFiles: files, Catalog: e}, matchPolicy{CatalogFiles: true})
	if !verdict.Matched {
		return searchDecisionProvenance{}, fmt.Errorf("no admissible file evidence: %s", verdict.RejectReason)
	}
	return searchDecisionProvenance{Class: verdict.Class, SearchCandidateName: match.Title, SourceInstanceID: instanceID, SourceHash: normalizeHash(source.Hash), Relaxed: verdict.Relaxed, Catalog: e}, nil
}

func validateCatalogTarget(decision searchDecisionProvenance, hash string, files qbt.TorrentFiles) error {
	e := decision.Catalog
	if !e.valid() || decision.SourceInstanceID <= 0 || decision.SourceHash == "" {
		return errors.New("incomplete file-evidence decision")
	}
	_, total, err := fullFileSnapshot(files)
	if err != nil {
		return err
	}
	if (e.Kind == gazellemusic.MatchHash && normalizeHash(hash) != e.TargetHash) || total != e.Total {
		return errors.New("selected target hash or full size changed")
	}
	if catalogFileFamily(files) != e.Family {
		return errors.New("selected target content family changed")
	}
	return nil
}

func validateCatalogSource(decision searchDecisionProvenance, torrent *qbt.Torrent, files qbt.TorrentFiles) error {
	for _, file := range files {
		if file.Progress < 1 {
			return errors.New("bound source has incomplete full-torrent files")
		}
	}
	snapshot, total, err := fullFileSnapshot(files)
	if err != nil {
		return err
	}
	if snapshot != decision.Catalog.SourceSnapshot || total != decision.Catalog.Total || total != searchSourceSize(torrent) || catalogFileFamily(files) != decision.Catalog.Family {
		return errors.New("bound source file table, full size or content family changed")
	}
	return nil
}

func (s *Service) findCatalogCandidate(ctx context.Context, req *FindCandidatesRequest) (*FindCandidatesResponse, error) {
	p := req.SearchDecision
	if !p.Catalog.valid() || p.SourceInstanceID <= 0 || p.SourceHash == "" || (len(req.TargetInstanceIDs) > 0 && !slices.Contains(req.TargetInstanceIDs, p.SourceInstanceID)) {
		return nil, errors.New("file-evidence source binding mismatch")
	}
	instance, err := s.instanceStore.Get(ctx, p.SourceInstanceID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, errors.New("bound source instance unavailable")
	}
	torrents, err := s.syncManager.GetTorrentRefs(ctx, p.SourceInstanceID, qbittorrent.TorrentRefFilter{Hashes: []string{p.SourceHash}})
	if err != nil {
		return nil, err
	}
	for _, torrent := range torrents {
		if normalizeHash(torrent.Hash) != p.SourceHash {
			continue
		}
		if s.sourceGate().check(torrent, req.sourceFilter) != sourcePass {
			return nil, errors.New("bound source incomplete or ineligible")
		}
		files, err := s.syncManager.GetTorrentFilesBatch(qbittorrent.WithForceFilesRefresh(ctx), p.SourceInstanceID, []string{p.SourceHash})
		if err != nil {
			return nil, err
		}
		if err := validateCatalogSource(p, torrent, files[p.SourceHash]); err != nil {
			return nil, err
		}
		return &FindCandidatesResponse{SourceTorrent: &TorrentInfo{Name: req.TorrentName}, Candidates: []CrossSeedCandidate{{InstanceID: p.SourceInstanceID, InstanceName: instance.Name, Torrents: []*qbt.Torrent{torrent}, MatchType: "size"}}}, nil
	}
	return nil, errors.New("bound source unavailable")
}

func (m matcher) catalogVerdict(source, candidate namedRelease, evidence matchEvidence) matchVerdict {
	strict := m.compareReleases(source, candidate, nil, nil, false, false)
	v := matchVerdict{Class: searchCandidateClassRejected, Strict: strict, RejectReason: "incomplete or inconsistent catalog-file evidence"}
	e := evidence.Catalog
	snapshot, total, err := fullFileSnapshot(evidence.SourceFiles)
	if err != nil || !e.valid() || snapshot != e.SourceSnapshot || total != evidence.SourceSize || !positiveExactSize(total, evidence.CandidateSize) || total != e.Total || catalogFileFamily(evidence.SourceFiles) != e.Family {
		return v
	}
	v.Class, v.Matched, v.Relaxed, v.RejectReason = searchCandidateClassFileEvidence, true, strict.differences, ""
	return v
}

func catalogFileAncestor(name string, seen map[string]struct{}) bool {
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if _, exists := seen[parent]; exists {
			return true
		}
	}
	return false
}
